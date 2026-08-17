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

	"jasmr-dl/internal/scraper"
	"jasmr-dl/internal/util"
)

// Job is one file to fetch. Name is used only if the server declines to name it.
type Job struct {
	Name    string
	LinkURL string

	Source     scraper.Source
	Alternates []string

	// The media host answers 403 without it.
	Referer string
}

// ProgressFunc reports transfer progress. total is -1 when unknown.
type ProgressFunc func(name string, done, total int64)

// Downloader fetches jobs into OutputDir.
type Downloader struct {
	Client    *http.Client
	UserAgent string
	OutputDir string
	Retries   int

	// FFmpeg is the binary; empty means look on PATH.
	FFmpeg string

	CoverPath string

	// Chapters only mean anything when one file holds the whole work.
	Chapters []scraper.Chapter

	Tags Tags

	// Files at once. Run derives chunks from it.
	Concurrency int
	chunks      int
	budget      *semaphore.Weighted

	OnStart    func(name string, total int64)
	OnProgress ProgressFunc

	// OnCoverError reports art that could not be attached; the audio is fine.
	OnCoverError func(name string, err error)

	// OnStartCount counts segments; an HLS byte total needs a request each.
	OnStartCount func(name string, total int64)
}

// Download fetches one job, resuming a partial file if one is present. It
// returns the final path on disk.
func (d *Downloader) Download(ctx context.Context, job Job) (string, error) {
	var lastErr error

	// One extra attempt beyond Retries so Retries=0 still tries once.
	for attempt := 0; attempt <= d.Retries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoffDelay(attempt, lastErr)); err != nil {
				return "", err
			}
		}

		path, err := d.attempt(ctx, job)
		if err == nil {
			return path, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		var perm permanentError
		if errors.As(err, &perm) {
			return "", perm.err
		}
		lastErr = err
	}
	return "", fmt.Errorf("after %d attempts: %w", d.Retries+1, lastErr)
}

// attempt runs one resolve-and-transfer cycle.
func (d *Downloader) attempt(ctx context.Context, job Job) (string, error) {
	// A playlist has no single file behind it.
	if job.Source == scraper.SourceHLS {
		return d.assembleHLS(ctx, job)
	}

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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.URL, nil)
	if err != nil {
		return "", permanent(err)
	}
	d.setTransfer(req, res)

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

	name := filenameFrom(resp, fallback)
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

	// This host throttles per connection, so one socket wastes bandwidth.
	if resp.Header.Get("Accept-Ranges") == "bytes" {
		if plan, ok := planChunks(resp.ContentLength, d.chunks); ok {
			resp.Body.Close()
			release() // the chunks take their own slots
			path, err := d.chunked(ctx, res, plan, name, final, part)
			if errors.Is(err, errRangeIgnored) {
				if err := d.acquire(ctx); err != nil {
					return "", err
				}
				held = true
				return d.single(ctx, res, name, final, part)
			}
			if err != nil {
				return "", err
			}
			return d.embedded(ctx, path)
		}
	}

	// Restart the request as a ranged one if a partial file exists. The first
	// response is discarded; it only existed to learn the name and size.
	if fi, statErr := os.Stat(part); statErr == nil && fi.Size() > 0 {
		resp.Body.Close()
		resp, err = d.rangedGet(ctx, res, fi.Size())
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	path, err := d.writeBody(ctx, resp, name, final, part)
	if err != nil {
		return "", err
	}
	return d.embedded(ctx, path)
}

// single serves hosts that advertise ranges without honoring them.
func (d *Downloader) single(ctx context.Context, res *Resolved, name, final, part string) (string, error) {
	resp, err := d.rangedGet(ctx, res, 0)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	path, err := d.writeBody(ctx, resp, name, final, part)
	if err != nil {
		return "", err
	}
	return d.embedded(ctx, path)
}

// embedded attaches art and chapters. Failure is non-fatal: the audio is fine.
func (d *Downloader) embedded(ctx context.Context, path string) (string, error) {
	if d.nothingToTag() {
		return path, nil
	}
	if err := d.tag(ctx, path); err != nil {
		if d.OnCoverError != nil {
			d.OnCoverError(filepath.Base(path), err)
		}
	}
	return path, nil
}

// finished tests for a complete download. Tagging changes a file's length, so a
// size mismatch alone proves nothing.
func (d *Downloader) finished(ctx context.Context, final string, advertised int64) bool {
	fi, err := os.Stat(final)
	if err != nil {
		return false
	}
	if advertised > 0 && fi.Size() == advertised {
		return true
	}
	return d.CoverPath != "" && fi.Size() > 0 && d.hasCover(ctx, final)
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

func (d *Downloader) setTransfer(req *http.Request, res *Resolved) {
	setMediaFetch(req, d.UserAgent)
	req.Header.Set("Referer", res.Referer)
}

func (d *Downloader) rangedGet(ctx context.Context, res *Resolved, from int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.URL, nil)
	if err != nil {
		return nil, permanent(err)
	}
	d.setTransfer(req, res)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))

	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
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
	case http.StatusForbidden, http.StatusUnauthorized:
		// Signed URL expired. Retrying re-resolves from scratch.
		return "", fmt.Errorf("%s: signed URL rejected, will re-resolve", resp.Status)
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return "", retryAfterError{status: resp.Status, wait: parseRetryAfter(resp.Header.Get("Retry-After"))}
	default:
		if resp.StatusCode >= 500 {
			return "", fmt.Errorf("server error: %s", resp.Status)
		}
		return "", permanent(fmt.Errorf("unexpected status: %s", resp.Status))
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
	var w io.Writer = f
	if d.OnProgress != nil {
		w = io.MultiWriter(f, writerFunc(func(p []byte) (int, error) {
			done += int64(len(p))
			d.OnProgress(name, done, total)
			return len(p), nil
		}))
	}

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
