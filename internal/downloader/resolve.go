package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/EagleStelle/jasmr-dl/internal/util"
)

// Resolved is a URL that serves bytes, plus what must accompany it.
type Resolved struct {
	URL     string
	Referer string

	// Name comes from the URL; the media host sends no Content-Disposition.
	Name string
}

// pickEncoding probes a track's encodings; pages advertise ones never uploaded.
func (d *Downloader) pickEncoding(ctx context.Context, job Job) (*Resolved, error) {
	found := func(target string) *Resolved {
		return &Resolved{URL: target, Referer: job.Referer, Name: urlName(target)}
	}
	if len(job.Alternates) == 0 {
		return found(job.LinkURL), nil
	}

	var lastErr error
	for _, candidate := range append([]string{job.LinkURL}, job.Alternates...) {
		err := d.probe(ctx, candidate, job.Referer)
		if err == nil {
			return found(candidate), nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
	}
	return nil, fmt.Errorf("the page offers no encoding this host has: %w", lastErr)
}

// urlName is the URL's own filename, or "" where the path carries none.
func urlName(raw string) string {
	if base := util.URLBase(raw); base != "" {
		return util.Sanitize(base)
	}
	return ""
}

// probe asks for one byte to learn whether the host holds the URL.
func (d *Downloader) probe(ctx context.Context, target, referer string) error {
	req, err := d.mediaRequest(ctx, target, referer)
	if err != nil {
		return err
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	// Drain so the connection is reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		return nil
	default:
		return util.BadStatus(target, resp)
	}
}
