package downloader

import (
	"context"
	"net/http"

	"jasmr-dl/internal/util"
)

// newMediaRequest builds the GET every media fetch starts from: a player's
// headers plus the Referer. The host ignores the Sec-Fetch set and checks
// Referer, answering 403 unless it names the site.
func newMediaRequest(ctx context.Context, target, referer, userAgent string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, permanent(err)
	}

	h := req.Header
	h.Set("User-Agent", userAgent)
	h.Set("Accept", "audio/webm,audio/ogg,audio/wav,audio/*;q=0.9,application/ogg;q=0.7,video/*;q=0.6,*/*;q=0.5")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Sec-Fetch-Dest", "audio")
	h.Set("Sec-Fetch-Mode", "no-cors")
	h.Set("Sec-Fetch-Site", "cross-site")
	h.Set("Referer", referer)

	return req, nil
}

// mediaRequest is newMediaRequest with the run's User-Agent.
func (d *Downloader) mediaRequest(ctx context.Context, target, referer string) (*http.Request, error) {
	return newMediaRequest(ctx, target, referer, d.UserAgent)
}

// getOK fetches target and hands back a 200 response, whose body the caller
// owns and must close. Anything else is an error with the body already closed.
func (d *Downloader) getOK(ctx context.Context, target, referer string) (*http.Response, error) {
	req, err := d.mediaRequest(ctx, target, referer)
	if err != nil {
		return nil, err
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, util.BadStatus(target, resp)
	}
	return resp, nil
}
