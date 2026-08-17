package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Resolved is the outcome of walking the file host's hop chain: a signed,
// short-lived URL that actually serves bytes.
type Resolved struct {
	URL     string
	Referer string
}

// Resolve walks a dlc.php link to the URL that serves the file.
//
//	/d/<id>                        -> 302 -> landing page
//	parse hx-get="/<id>/download?t=<token>"
//	GET it with HX-Request: true   -> 204 + Hx-Redirect
//	/d/<id>?v=<signature>          -> the file
//
// The signature is short-lived, so this must run per download rather than as a
// batch pre-pass, and its result must not be cached across runs.
func Resolve(ctx context.Context, client *http.Client, userAgent, linkURL string) (*Resolved, error) {
	// Hop 1-2: the 302 to the landing page is an ordinary redirect, so the
	// client follows it. resp.Request.URL is where we actually landed.
	doc, landing, err := fetchDoc(ctx, client, userAgent, linkURL, "")
	if err != nil {
		return nil, fmt.Errorf("landing page: %w", err)
	}

	// Hop 3: the download trigger is an htmx attribute, not an anchor href.
	tokenPath, err := findDownloadPath(doc)
	if err != nil {
		return nil, err
	}
	trigger, err := landing.Parse(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("bad download path %q: %w", tokenPath, err)
	}

	// Hop 4: answers 204 with the real URL in a header. This is an htmx
	// convention, not an HTTP redirect, so http.Client will not follow it.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trigger.String(), nil)
	if err != nil {
		return nil, err
	}
	setScriptFetch(req, userAgent)
	req.Header.Set("Referer", landing.String())
	req.Header.Set("HX-Request", "true") // omit this and the HTML comes back instead

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download trigger: %w", err)
	}
	defer resp.Body.Close()

	final := strings.TrimSpace(resp.Header.Get("Hx-Redirect"))
	if final == "" {
		return nil, fmt.Errorf("no Hx-Redirect header, got %s", resp.Status)
	}
	abs, err := landing.Parse(final)
	if err != nil {
		return nil, fmt.Errorf("bad Hx-Redirect %q: %w", final, err)
	}
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return nil, fmt.Errorf("Hx-Redirect is not http(s): %q", final)
	}

	return &Resolved{URL: abs.String(), Referer: landing.String()}, nil
}

// findDownloadPath picks the primary download trigger out of the landing page.
// The page also carries a mirror (alt=true) and an in-browser preview, both
// using the same attribute.
func findDownloadPath(doc *goquery.Document) (string, error) {
	var primary, mirror string

	doc.Find("[hx-get]").Each(func(_ int, sel *goquery.Selection) {
		v, _ := sel.Attr("hx-get")
		if !strings.Contains(v, "/download") {
			return
		}
		if strings.Contains(v, "alt=") {
			if mirror == "" {
				mirror = v
			}
			return
		}
		if primary == "" {
			primary = v
		}
	})

	switch {
	case primary != "":
		return primary, nil
	case mirror != "":
		return mirror, nil
	default:
		return "", errors.New("no download trigger on landing page")
	}
}

// fetchDoc GETs a URL and parses HTML, returning the URL actually landed on
// after redirects.
func fetchDoc(ctx context.Context, client *http.Client, userAgent, target, referer string) (*goquery.Document, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, nil, err
	}
	site := "cross-site"
	if referer != "" {
		req.Header.Set("Referer", referer)
		site = "same-origin"
	}
	setNavigation(req, userAgent, site)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("GET %s: %s", target, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return doc, resp.Request.URL, nil
}
