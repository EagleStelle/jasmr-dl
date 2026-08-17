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

	"jasmr-dl/internal/util"
)

// Job is one file to fetch. LinkURL is an unresolved dlc.php link; Name is the
// listing's label, used only if the server declines to name the file.
type Job struct {
	Name    string
	LinkURL string
}

// ProgressFunc reports transfer progress. total is -1 when unknown.
type ProgressFunc func(name string, done, total int64)

// Downloader fetches jobs into OutputDir.
type Downloader struct {
	Client     *http.Client
	UserAgent  string
	OutputDir  string
	Retries    int
	OnStart    func(name string, total int64)
	OnProgress ProgressFunc
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

// attempt runs the full resolve-and-transfer cycle once. Resolution happens
// inside the retry loop on purpose: the signed URL is short-lived, so a retry
// after a long backoff needs a fresh one rather than a stale cached URL.
func (d *Downloader) attempt(ctx context.Context, job Job) (string, error) {
	res, err := Resolve(ctx, d.Client, d.UserAgent, job.LinkURL)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.URL, nil)
	if err != nil {
		return "", permanent(err)
	}
	setNavigation(req, d.UserAgent, "same-origin")
	req.Header.Set("Referer", res.Referer)

	// Probe for an existing part file before choosing a filename: the part
	// path depends on the name, which the server supplies, so name first.
	resp, err := d.Client.Do(req)
	if err != nil {
		return "", err
	}

	name := filenameFrom(resp, job.Name)
	if !util.IsAudioFile(name) {
		resp.Body.Close()
		return "", permanent(fmt.Errorf("refusing %q: not an allowlisted audio file", name))
	}

	final := filepath.Join(d.OutputDir, name)
	part := final + ".part"

	// If the finished file is already there at the advertised size, skip.
	if fi, statErr := os.Stat(final); statErr == nil && resp.ContentLength > 0 && fi.Size() == resp.ContentLength {
		resp.Body.Close()
		return final, nil
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

	return d.writeBody(ctx, resp, name, final, part)
}

func (d *Downloader) rangedGet(ctx context.Context, res *Resolved, from int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.URL, nil)
	if err != nil {
		return nil, permanent(err)
	}
	setNavigation(req, d.UserAgent, "same-origin")
	req.Header.Set("Referer", res.Referer)
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
