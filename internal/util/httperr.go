package util

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrChallenge marks a refusal Cloudflare made rather than a dead link, so a
// caller can clear it and try again.
var ErrChallenge = errors.New("Cloudflare challenge")

// BadStatus names the URL alongside the status, the pair needed to tell which of
// a job's several hops refused.
func BadStatus(target string, resp *http.Response) error {
	if IsChallenge(resp) {
		return fmt.Errorf("GET %s: %s: %w", target, resp.Status, ErrChallenge)
	}
	return fmt.Errorf("GET %s: %s", target, resp.Status)
}

// IsChallenge reports whether Cloudflare served an interstitial, which otherwise
// looks like an ordinary 403.
func IsChallenge(resp *http.Response) bool {
	return strings.EqualFold(resp.Header.Get("Cf-Mitigated"), "challenge")
}
