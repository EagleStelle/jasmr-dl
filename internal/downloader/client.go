package downloader

import (
	"net/http"
)

// NewClient builds the single client shared by every request in a run. Its
// transport handshakes as Chrome; see tls.go. A nil jar means no cookies, which
// is the normal case.
func NewClient(jar http.CookieJar) *http.Client {
	return &http.Client{
		Transport: newBrowserTransport(),
		Jar:       jar,
	}
}
