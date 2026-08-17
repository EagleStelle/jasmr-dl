package downloader

import "net/http"

// The file host's edge rejects, with 403, any request that carries no
// Sec-Fetch-* header at all, whatever the User-Agent claims. Every hop
// therefore has to declare how it was initiated, the way a browser does.
//
// The values need only be internally consistent, not literally true: the check
// is for their presence, not for a plausible navigation history.

const acceptHTML = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

// setNavigation marks a request as a top-level document load. site is the
// Sec-Fetch-Site value: "cross-site" when arriving from another origin,
// "same-origin" when following a link within the host.
func setNavigation(req *http.Request, userAgent, site string) {
	h := req.Header
	h.Set("User-Agent", userAgent)
	h.Set("Accept", acceptHTML)
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Upgrade-Insecure-Requests", "1")
	h.Set("Sec-Fetch-Dest", "document")
	h.Set("Sec-Fetch-Mode", "navigate")
	h.Set("Sec-Fetch-Site", site)
	h.Set("Sec-Fetch-User", "?1")
}

// setScriptFetch marks a request as one issued from page script rather than
// from a click. The download trigger is an htmx call, so it looks like this.
func setScriptFetch(req *http.Request, userAgent string) {
	h := req.Header
	h.Set("User-Agent", userAgent)
	h.Set("Accept", "*/*")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Sec-Fetch-Dest", "empty")
	h.Set("Sec-Fetch-Mode", "cors")
	h.Set("Sec-Fetch-Site", "same-origin")
}
