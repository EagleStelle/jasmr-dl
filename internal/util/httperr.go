package util

import (
	"fmt"
	"net/http"
	"strings"
)

// BadStatus names the URL alongside the status, the pair needed to tell which of
// a job's several hops refused.
func BadStatus(target string, resp *http.Response) error {
	if IsChallenge(resp) {
		return fmt.Errorf("GET %s: %s, a Cloudflare challenge rather than a dead link.\n"+
			"       The cookies are missing or have expired. Open the page in a browser,\n"+
			"       export its cookies as cookies.txt, and leave the file beside this binary.", target, resp.Status)
	}
	return fmt.Errorf("GET %s: %s", target, resp.Status)
}

// IsChallenge reports whether Cloudflare served an interstitial, which otherwise
// looks like an ordinary 403.
func IsChallenge(resp *http.Response) bool {
	return strings.EqualFold(resp.Header.Get("Cf-Mitigated"), "challenge")
}
