package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/EagleStelle/jasmr-dl/internal/naming"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
	"github.com/EagleStelle/jasmr-dl/internal/util"
)

// Job is one file to fetch. Name is used only if the server declines to name it.
type Job struct {
	Name    string
	LinkURL string

	Source     scraper.Source
	Alternates []string

	// The media host answers 403 without it.
	Referer string

	Tags Tags

	// Fields names the output. Name and Ext are filled in from the response
	// where the server picks the filename.
	Fields naming.Fields
}

// ProgressFunc reports transfer progress. total is -1 when unknown.
type ProgressFunc func(name string, done, total int64)

// Downloader fetches jobs into OutputDir.
type Downloader struct {
	Client    *http.Client
	UserAgent string
	OutputDir string
	Retries   int

	JacketPath string

	// Template names each finished file under OutputDir.
	Template *naming.Template

	// Chapters only mean anything when one file holds the whole work.
	Chapters []scraper.Chapter

	// Split cuts a chaptered stream into one file per chapter instead of
	// embedding the chapters into a single file.
	Split bool

	// Connections caps the requests in flight across the run, which is what
	// sets throughput. Zero means defaultConnections.
	Connections int

	budget *semaphore.Weighted

	// OnStart opens a line; total is 0 when the size is not known yet, which
	// is every HLS job, since a playlist gives no byte total up front.
	OnStart    func(name string, total int64)
	OnProgress ProgressFunc

	// OnJacketError reports art that could not be attached; the audio is fine.
	OnJacketError func(name string, err error)

	// OnChapterDropped reports a chapter the stream ends before, whose file or
	// marker is never written.
	OnChapterDropped func(name string, start, total time.Duration)

	// OnSplitStart opens the chapter counter, OnSplitProgress advances it.
	OnSplitStart    func(total int)
	OnSplitProgress func(done, total int)
}

// Download fetches one job, resuming a partial file if one is present. It
// returns the final paths on disk, more than one where a stream was split.
func (d *Downloader) Download(ctx context.Context, job Job) ([]string, error) {
	var lastErr error

	// One extra attempt beyond Retries so Retries=0 still tries once.
	for attempt := 0; attempt <= d.Retries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoffDelay(attempt, lastErr)); err != nil {
				return nil, err
			}
		}

		paths, err := d.attempt(ctx, job)
		if err == nil {
			return paths, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var perm permanentError
		if errors.As(err, &perm) {
			return nil, perm.err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("after %d attempts: %w", d.Retries+1, lastErr)
}

// attempt runs one resolve-and-transfer cycle.
func (d *Downloader) attempt(ctx context.Context, job Job) ([]string, error) {
	// A playlist has no single file behind it.
	if job.Source == scraper.SourceHLS {
		return d.assembleHLS(ctx, job)
	}

	path, err := d.direct(ctx, job)
	if err != nil {
		return nil, err
	}
	return []string{path}, nil
}

// direct fetches a job the host serves as a file.
func (d *Downloader) direct(ctx context.Context, job Job) (string, error) {
	res, err := d.pickEncoding(ctx, job)
	if err != nil {
		return "", err
	}

	if err := d.acquire(ctx); err != nil {
		return "", err
	}
	held := true
	release := func() {
		if held {
			held = false
			d.release()
		}
	}
	defer release()

	req, err := d.mediaRequest(ctx, res.URL, res.Referer)
	if err != nil {
		return "", err
	}

	// Name first: the part path depends on it.
	resp, err := d.Client.Do(req)
	if err != nil {
		return "", err
	}

	// res.Name names the encoding that actually answered.
	fallback := job.Name
	if res.Name != "" {
		fallback = res.Name
	}

	served := filenameFrom(resp, fallback)
	if !util.IsAudioFile(served) {
		resp.Body.Close()
		return "", permanent(fmt.Errorf("refusing %q: not an allowlisted audio file", served))
	}

	name := d.leaf(job.Fields, served)
	if !util.IsAudioFile(name) {
		resp.Body.Close()
		return "", permanent(fmt.Errorf("refusing %q: not an allowlisted audio file", name))
	}

	final := filepath.Join(d.OutputDir, name)
	part := final + ".part"

	if d.finished(ctx, final, resp.ContentLength) {
		resp.Body.Close()
		return final, nil
	}

	// This host throttles per request, so one stream wastes bandwidth.
	if resp.Header.Get("Accept-Ranges") == "bytes" && worthChunking(resp.ContentLength) {
		total := resp.ContentLength
		resp.Body.Close()
		release() // the pieces take their own slots

		path, err := d.chunked(ctx, res, total, name, final, part)
		if errors.Is(err, errRangeIgnored) {
			if err := d.acquire(ctx); err != nil {
				return "", err
			}
			held = true
			// The host advertises ranges without honoring them: one stream
			// from the start is all it will serve.
			os.Remove(part + stateSuffix)
			return d.transfer(ctx, job, res, 0, name, final, part)
		}
		if err != nil {
			return "", err
		}
		return d.embedded(ctx, job, path)
	}

	// Restart the request as a ranged one if a partial file exists. The first
	// response is discarded; it only existed to learn the name and size.
	if have := fileSize(part); have > 0 {
		resp.Body.Close()
		return d.transfer(ctx, job, res, have, name, final, part)
	}
	defer resp.Body.Close()

	return d.store(ctx, job, resp, name, final, part)
}

// transfer runs one ranged request from byte from, then writes what it answers.
func (d *Downloader) transfer(ctx context.Context, job Job, res *Resolved, from int64, name, final, part string) (string, error) {
	resp, err := d.rangedGet(ctx, res, from)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	return d.store(ctx, job, resp, name, final, part)
}

// store writes a response to disk and tags the finished file.
func (d *Downloader) store(ctx context.Context, job Job, resp *http.Response, name, final, part string) (string, error) {
	path, err := d.writeBody(ctx, resp, name, final, part)
	if err != nil {
		return "", err
	}
	return d.embedded(ctx, job, path)
}

// embedded attaches art and chapters. Failure is non-fatal: the audio is fine.
func (d *Downloader) embedded(ctx context.Context, job Job, path string) (string, error) {
	if err := d.tag(ctx, job.Tags, d.embeddedChapters(), path); err != nil {
		if d.OnJacketError != nil {
			d.OnJacketError(filepath.Base(path), err)
		}
	}
	return path, nil
}

// embeddedChapters is the list to write into a file. A split run cuts on them
// instead, and each piece is one chapter.
func (d *Downloader) embeddedChapters() []scraper.Chapter {
	if d.Split {
		return nil
	}
	return d.Chapters
}

// finished tests for a complete download. Tagging changes a file's length, so a
// size mismatch alone proves nothing.
func (d *Downloader) finished(ctx context.Context, final string, advertised int64) bool {
	size := fileSize(final)
	if size == 0 {
		return false
	}
	if advertised > 0 && size == advertised {
		return true
	}
	return d.JacketPath != "" && d.hasJacket(ctx, final)
}

func (d *Downloader) acquire(ctx context.Context) error {
	if d.budget == nil {
		return nil
	}
	return d.budget.Acquire(ctx, 1)
}

func (d *Downloader) release() {
	if d.budget != nil {
		d.budget.Release(1)
	}
}

func (d *Downloader) rangedGet(ctx context.Context, res *Resolved, from int64) (*http.Response, error) {
	req, err := d.mediaRequest(ctx, res.URL, res.Referer)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))

	return d.Client.Do(req)
}

func (d *Downloader) writeBody(ctx context.Context, resp *http.Response, name, final, part string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", permanent(err)
	}

	var (
		flags  = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		offset int64
	)

	switch resp.StatusCode {
	case http.StatusOK:
		// Range ignored (or none sent): start clean.
	case http.StatusPartialContent:
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		offset = rangeStart(resp.Header.Get("Content-Range"))
	case http.StatusRequestedRangeNotSatisfiable:
		// Already have every byte the server has.
		if err := os.Rename(part, final); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return final, nil
	default:
		return "", transferStatus(resp)
	}

	total := resp.ContentLength
	if resp.StatusCode == http.StatusPartialContent {
		// Under a 206, Content-Length is only the slice length. The real
		// size is the figure after the slash in Content-Range.
		if t := rangeTotal(resp.Header.Get("Content-Range")); t > 0 {
			total = t
		}
	}
	if d.OnStart != nil {
		d.OnStart(name, total)
	}

	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return "", permanent(err)
	}

	done := offset
	w := d.tee(f, func(n int) {
		done += int64(n)
		d.OnProgress(name, done, total)
	})

	_, copyErr := io.Copy(w, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}

	// Stage then rename, so a half-written file never looks complete.
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	return final, nil
}

// --- filename -------------------------------------------------------------

// leaf names one file. Only the extension comes from the host's own name; what
// the file is called is the template's to say.
func (d *Downloader) leaf(f naming.Fields, served string) string {
	f.Ext = strings.TrimPrefix(extOf(served), ".")
	return d.Template.File(f)
}

var (
	// RFC 5987 form: filename*=UTF-8''<percent-encoded>
	reFilenameStar = regexp.MustCompile(`(?i)filename\*\s*=\s*([^'\s]*)'[^']*'([^;]+)`)
	reFilenameQ    = regexp.MustCompile(`(?i)filename\s*=\s*"([^"]*)"`)
	reFilenameBare = regexp.MustCompile(`(?i)filename\s*=\s*([^;"]+)`)
)

// filenameFrom reads Content-Disposition, preferring the RFC 5987 starred form.
//
// mime.ParseMediaType is unusable here: this host sends both filename and
// filename*, and Go rejects that pair with "duplicate parameter name" because
// the values differ. They differ precisely because the plain form replaces
// every non-ASCII character with an underscore, destroying Japanese titles.
func filenameFrom(resp *http.Response, fallback string) string {
	cd := resp.Header.Get("Content-Disposition")

	if m := reFilenameStar.FindStringSubmatch(cd); m != nil {
		if dec, err := url.PathUnescape(strings.TrimSpace(m[2])); err == nil {
			if name := clean(dec); name != "" {
				return name
			}
		}
	}
	if m := reFilenameQ.FindStringSubmatch(cd); m != nil {
		if name := clean(m[1]); name != "" {
			return name
		}
	}
	if m := reFilenameBare.FindStringSubmatch(cd); m != nil {
		if name := clean(m[1]); name != "" {
			return name
		}
	}
	return clean(fallback)
}

// clean reduces a server-supplied string to a safe single path component.
func clean(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\\", "/")
	s = filepath.Base(s) // strip any directory traversal outright
	if s == "." || s == "/" {
		return ""
	}
	return util.Sanitize(s)
}

// --- ranges ---------------------------------------------------------------

var reContentRange = regexp.MustCompile(`bytes\s+(\d+)-(\d+)/(\d+|\*)`)

func rangeStart(h string) int64 {
	if m := reContentRange.FindStringSubmatch(h); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		return n
	}
	return 0
}

func rangeTotal(h string) int64 {
	if m := reContentRange.FindStringSubmatch(h); m != nil && m[3] != "*" {
		n, _ := strconv.ParseInt(m[3], 10, 64)
		return n
	}
	return -1
}

// --- retry ----------------------------------------------------------------

// errRangeIgnored: range support advertised, then the whole file sent anyway.
var errRangeIgnored = errors.New("host ignored the Range header")

// transferStatus turns a status no transfer can use into an error that says
// whether retrying stands a chance. Callers handle the codes they can use
// first, so anything reaching here has already failed.
func transferStatus(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		// Signed URL expired. Retrying re-resolves from scratch.
		return fmt.Errorf("%s: signed URL rejected, will re-resolve", resp.Status)
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return retryAfterError{status: resp.Status, wait: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("server error: %s", resp.Status)
	}
	return permanent(fmt.Errorf("unexpected status: %s", resp.Status))
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func permanent(err error) error { return permanentError{err} }

type retryAfterError struct {
	status string
	wait   time.Duration
}

func (e retryAfterError) Error() string { return e.status }

// backoffDelay grows exponentially but yields to a server-supplied Retry-After.
func backoffDelay(attempt int, last error) time.Duration {
	var ra retryAfterError
	if errors.As(last, &ra) && ra.wait > 0 {
		return ra.wait
	}
	d := time.Duration(1<<uint(attempt-1)) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// tee returns w with each write's length reported to add, or w unchanged when
// nothing is listening. Callers count bytes their own way: a single stream
// tracks its own offset, chunks share an atomic.
func (d *Downloader) tee(w io.Writer, add func(n int)) io.Writer {
	if d.OnProgress == nil {
		return w
	}
	return io.MultiWriter(w, writerFunc(func(p []byte) (int, error) {
		add(len(p))
		return len(p), nil
	}))
}
