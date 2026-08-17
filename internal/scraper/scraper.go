package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// PoliteDelay is the Crawl-delay declared in the site's robots.txt. It applies
// between fetches of japaneseasmr.com itself, not to the file host, which is a
// separate origin under a separate robots.txt.
const PoliteDelay = 10 * time.Second

var rjPattern = regexp.MustCompile(`RJ\d+`)

// Scraper reads album metadata. It is safe to reuse across albums; the delay is
// applied before every fetch after the first.
type Scraper struct {
	client    *http.Client
	userAgent string
	delay     time.Duration
	fetched   bool
}

// New returns a Scraper honoring the robots.txt crawl delay.
func New(client *http.Client, userAgent string) *Scraper {
	return &Scraper{client: client, userAgent: userAgent, delay: PoliteDelay}
}

// SetDelay overrides the inter-fetch delay. Intended for tests; lowering it
// against the live site ignores the declared Crawl-delay.
func (s *Scraper) SetDelay(d time.Duration) { s.delay = d }

// Album fetches a post page and its dlc.php listing, returning both together.
// It performs two requests against japaneseasmr.com, spaced by the crawl delay.
func (s *Scraper) Album(ctx context.Context, pageURL string) (*Album, error) {
	doc, err := s.fetchDoc(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("fetch album page: %w", err)
	}

	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("parse page URL: %w", err)
	}

	album := &Album{
		PageURL:  pageURL,
		Title:    extractTitle(doc),
		CoverURL: extractCover(doc, base),
		RJCode:   extractRJCode(doc),
	}
	if album.RJCode == "" {
		return nil, fmt.Errorf("no RJ code found on %s: page layout may have changed", pageURL)
	}

	dlcURL, err := extractDLCLink(doc, base, album.RJCode)
	if err != nil {
		return nil, err
	}

	tracks, err := s.tracks(ctx, dlcURL)
	if err != nil {
		return nil, err
	}
	album.Tracks = tracks

	return album, nil
}

// fetchDoc issues one GET and parses the body as HTML, waiting out the crawl
// delay first on every call after the first.
func (s *Scraper) fetchDoc(ctx context.Context, target string) (*goquery.Document, error) {
	if s.fetched && s.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.delay):
		}
	}
	s.fetched = true

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	// The Go default UA ("Go-http-client/1.1") is widely blocked.
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja,en;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", target, resp.Status)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

func extractTitle(doc *goquery.Document) string {
	if v, ok := doc.Find(`meta[property="og:title"]`).Attr("content"); ok {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(doc.Find("h1").First().Text()); v != "" {
		return v
	}
	return strings.TrimSpace(doc.Find("title").First().Text())
}

// extractCover prefers the player's poster attribute over og:image, which on
// this site is sometimes a site-wide default rather than the album art.
//
// Note the cover and the media stream live on two visually similar but
// different hosts, differing only by transposed characters. Always read each
// from its own attribute; never derive one host from the other.
func extractCover(doc *goquery.Document, base *url.URL) string {
	if v, ok := doc.Find("video[poster]").First().Attr("poster"); ok {
		if abs := resolve(base, v); abs != "" {
			return abs
		}
	}
	if v, ok := doc.Find(`meta[property="og:image"]`).Attr("content"); ok {
		return resolve(base, v)
	}
	return ""
}

// extractRJCode reads the work identifier that keys the dlc.php listing.
func extractRJCode(doc *goquery.Document) string {
	if v, ok := doc.Find("video[title]").First().Attr("title"); ok {
		if m := rjPattern.FindString(v); m != "" {
			return m
		}
	}
	if v, ok := doc.Find("audio[title]").First().Attr("title"); ok {
		if m := rjPattern.FindString(v); m != "" {
			return m
		}
	}
	// Fall back to the "RJ Code: RJnnnnnn" line in the post body.
	return rjPattern.FindString(doc.Find("body").Text())
}

// extractDLCLink finds the download-listing URL hidden in the page. It falls
// back to constructing the URL from the RJ code, since the anchor lives in a
// display:none div that is easy for a theme change to drop.
func extractDLCLink(doc *goquery.Document, base *url.URL, rj string) (string, error) {
	var found string
	doc.Find(`a[href*="dlc.php"]`).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if href, ok := sel.Attr("href"); ok {
			if found = resolve(base, href); found != "" {
				return false
			}
		}
		return true
	})
	if found != "" {
		return found, nil
	}

	if rj == "" {
		return "", fmt.Errorf("no dlc.php link on page and no RJ code to build one from")
	}
	u := *base
	u.Path = "/dlc.php"
	u.RawQuery = url.Values{"f": {rj}}.Encode()
	u.Fragment = ""
	return u.String(), nil
}

// resolve turns a possibly-relative href into an absolute URL string.
func resolve(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return base.ResolveReference(u).String()
}
