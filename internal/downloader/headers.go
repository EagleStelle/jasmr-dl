package downloader

import "net/http"

// setMediaFetch marks a player's media load. The host ignores these and checks
// Referer, answering 403 unless it names the site.
func setMediaFetch(req *http.Request, userAgent string) {
	h := req.Header
	h.Set("User-Agent", userAgent)
	h.Set("Accept", "audio/webm,audio/ogg,audio/wav,audio/*;q=0.9,application/ogg;q=0.7,video/*;q=0.6,*/*;q=0.5")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Sec-Fetch-Dest", "audio")
	h.Set("Sec-Fetch-Mode", "no-cors")
	h.Set("Sec-Fetch-Site", "cross-site")
}
