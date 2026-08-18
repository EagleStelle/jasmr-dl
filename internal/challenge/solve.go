// Package challenge clears a Cloudflare interstitial by driving the browser this
// machine already has, and returns the cookie and User-Agent it earned.
//
// The interstitial is Turnstile: obfuscated JavaScript, WebAssembly and a canvas
// fingerprint. Nothing in Go reproduces that for long, so a real browser handles
// the one page and the rest of the program keeps its own fast HTTP path.
package challenge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jasmr-dl/internal/downloader"
)

// ErrNoBrowser means there is nothing on this machine to drive.
var ErrNoBrowser = errors.New("no Chrome, Chromium or Edge installed to clear the challenge with")

const (
	// DefaultTimeout is how long the challenge is given. A managed one reloads
	// the page a few times before it settles.
	DefaultTimeout = 45 * time.Second

	// launchTimeout bounds getting a browser up, kept separate so a cold start
	// neither eats the challenge's budget nor is reported as it running out.
	launchTimeout = 30 * time.Second

	pollInterval    = 250 * time.Millisecond
	clearanceCookie = "cf_clearance"
)

// Options configures one solve.
type Options struct {
	// BrowserPath names the browser to drive. Empty means find one.
	BrowserPath string
	// ProfileDir is the browser profile to keep, so trust accumulates across runs.
	ProfileDir string
	// Visible shows the window instead of running headless.
	Visible bool
	// Timeout bounds the wait. Zero means DefaultTimeout.
	Timeout time.Duration
	// Log receives progress, for --verbose.
	Log func(format string, args ...any)
}

func (o Options) logf(format string, args ...any) {
	if o.Log != nil {
		o.Log(format, args...)
	}
}

// Result is what a solve earned.
type Result struct {
	Cookies   []downloader.Cookie
	UserAgent string
}

// Solve opens pageURL in a real browser and waits for Cloudflare to clear it.
//
// The cookies and the User-Agent come back together because they only work
// together: cf_clearance is bound to the User-Agent that earned it.
func Solve(ctx context.Context, pageURL string, o Options) (*Result, error) {
	host, err := hostOf(pageURL)
	if err != nil {
		return nil, err
	}

	exe := o.BrowserPath
	if exe == "" {
		if exe = findBrowser(); exe == "" {
			return nil, ErrNoBrowser
		}
	}
	o.logf("browser: %s", exe)

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	userAgent, err := probeUserAgent(ctx, exe, o)
	if err != nil {
		return nil, err
	}
	o.logf("user-agent: %s", userAgent)

	profileDir, err := profile(o.ProfileDir)
	if err != nil {
		return nil, err
	}

	started, cancel := context.WithTimeout(ctx, launchTimeout)
	b, err := launch(started, exe, o, userAgent, profileDir)
	cancel()
	if err != nil {
		return nil, err
	}
	defer b.Close()

	// The challenge gets its own clock, now there is a browser to give it.
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	var target struct {
		TargetID string `json:"targetId"`
	}
	// Creating a target navigates it, so this is the whole of the page handling.
	if err := b.conn.call(ctx, "Target.createTarget", map[string]any{"url": pageURL}, &target); err != nil {
		return nil, err
	}
	o.logf("opened %s, waiting up to %s for %s", pageURL, timeout, clearanceCookie)

	cookies, err := waitForClearance(ctx, b, host, timeout)
	if err != nil {
		return nil, err
	}
	_ = b.conn.call(ctx, "Target.closeTarget", map[string]any{"targetId": target.TargetID}, nil)

	return &Result{Cookies: cookies, UserAgent: userAgent}, nil
}

// probeUserAgent asks the browser its name, with the headless marker taken out.
//
// Headless Chrome writes "HeadlessChrome" into its User-Agent, which is both an
// admission to the challenge and a string the rest of the program cannot send.
// --user-agent only takes effect at startup, hence a throwaway launch first.
func probeUserAgent(ctx context.Context, exe string, o Options) (string, error) {
	dir, err := os.MkdirTemp("", "jasmr-probe-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()

	b, err := launch(ctx, exe, o, "", dir)
	if err != nil {
		return "", err
	}
	defer b.Close()

	var version struct {
		UserAgent string `json:"userAgent"`
	}
	if err := b.conn.call(ctx, "Browser.getVersion", nil, &version); err != nil {
		return "", err
	}
	if version.UserAgent == "" {
		return "", fmt.Errorf("the browser reported no User-Agent")
	}
	return strings.ReplaceAll(version.UserAgent, "HeadlessChrome/", "Chrome/"), nil
}

// waitForClearance polls until Cloudflare has cleared host. The cookie is the
// only signal worth watching: a challenge reloads the page on its own, so
// navigation says nothing about whether it passed.
func waitForClearance(ctx context.Context, b *browser, host string, timeout time.Duration) ([]downloader.Cookie, error) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()

	for {
		cookies, err := readCookies(ctx, b)
		if err != nil {
			return nil, err
		}
		if hasClearance(cookies, host) {
			return cookies, nil
		}

		select {
		case <-t.C:
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("no %s after %s, so the challenge was not cleared", clearanceCookie, timeout)
			}
			return nil, ctx.Err()
		}
	}
}

// readCookies reads every cookie the browser holds. Storage.getCookies is
// browser-level, so no page has to be attached to ask.
func readCookies(ctx context.Context, b *browser) ([]downloader.Cookie, error) {
	var out struct {
		Cookies []struct {
			Name     string  `json:"name"`
			Value    string  `json:"value"`
			Domain   string  `json:"domain"`
			Path     string  `json:"path"`
			Expires  float64 `json:"expires"` // seconds, -1 for a session cookie
			HTTPOnly bool    `json:"httpOnly"`
			Secure   bool    `json:"secure"`
		} `json:"cookies"`
	}
	if err := b.conn.call(ctx, "Storage.getCookies", nil, &out); err != nil {
		return nil, err
	}

	cookies := make([]downloader.Cookie, 0, len(out.Cookies))
	for _, c := range out.Cookies {
		cookie := downloader.Cookie{
			Domain:   c.Domain,
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
		}
		if c.Expires > 0 {
			whole, frac := math.Modf(c.Expires)
			cookie.Expires = time.Unix(int64(whole), int64(frac*float64(time.Second)))
		}
		cookies = append(cookies, cookie)
	}
	return cookies, nil
}

// hasClearance reports whether host is cleared. The cookie is set for
// ".example.com", which covers www.example.com too.
func hasClearance(cookies []downloader.Cookie, host string) bool {
	for _, c := range cookies {
		if c.Name != clearanceCookie || c.Value == "" {
			continue
		}
		if d := c.Host(); d == host || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// profile prepares the profile directory and names it absolutely: given a
// relative --user-data-dir, Chrome exits at once, silently.
func profile(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("no browser profile directory given")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create browser profile directory: %w", err)
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve browser profile directory: %w", err)
	}
	return abs, nil
}

func hostOf(pageURL string) (string, error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", err
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("no host in %q", pageURL)
	}
	return strings.ToLower(u.Hostname()), nil
}
