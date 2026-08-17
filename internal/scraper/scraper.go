package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"jasmr-dl/internal/util"
)

// Scraper reads album metadata. Safe to reuse across albums.
type Scraper struct {
	client    *http.Client
	userAgent string
}

// New returns a Scraper that reads pages as the given browser.
func New(client *http.Client, userAgent string) *Scraper {
	return &Scraper{client: client, userAgent: userAgent}
}

// Album reads a post page's audio in one request.
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
		Artists:  extractArtists(doc),
		Tracks:   directTracks(doc, base),
		Chapters: extractChapters(doc),
	}
	if len(album.Tracks) == 0 {
		return nil, fmt.Errorf("no audio in any player on %s", pageURL)
	}
	return album, nil
}

// fetchDoc GETs and parses HTML. The headers are a browser opening the page by
// typing its address, which Cloudflare weighs; the handshake carries the other
// half, see downloader/tls.go.
func (s *Scraper) fetchDoc(ctx context.Context, target string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	h := req.Header
	// The Go default UA ("Go-http-client/1.1") is widely blocked.
	h.Set("User-Agent", s.userAgent)
	h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	h.Set("Accept-Language", "ja,en-US;q=0.9,en;q=0.8")
	h.Set("Upgrade-Insecure-Requests", "1")
	h.Set("Sec-Fetch-Dest", "document")
	h.Set("Sec-Fetch-Mode", "navigate")
	h.Set("Sec-Fetch-Site", "none")
	h.Set("Sec-Fetch-User", "?1")
	// Hints come from the UA so --user-agent moves both together.
	if v := util.ChromeMajorVersion(s.userAgent); v != "" {
		h.Set("Sec-CH-UA", fmt.Sprintf(`"Chromium";v="%s", "Not_A Brand";v="24", "Google Chrome";v="%s"`, v, v))
		h.Set("Sec-CH-UA-Mobile", "?0")
		h.Set("Sec-CH-UA-Platform", `"Windows"`)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, util.BadStatus(target, resp)
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

// extractCover prefers poster: og:image is sometimes a site-wide default and
// the first <img> is the logo. Art and media hosts differ only by transposed
// characters (weeabo0 vs weeab0o), so never derive one from the other.
func extractCover(doc *goquery.Document, base *url.URL) string {
	for _, sel := range []struct{ query, attr string }{
		{"video[poster]", "poster"},
		{`meta[property="og:image"]`, "content"},
		// Art is lazy-loaded: src holds a placeholder, data-src the real URL.
		{`img[data-src*="pic."]`, "data-src"},
	} {
		if v, ok := doc.Find(sel.query).First().Attr(sel.attr); ok {
			if abs := util.ResolveURL(base, v); abs != "" {
				return abs
			}
		}
	}
	return ""
}

// extractArtists reads the voice actor line, dropping its "CV:" label.
func extractArtists(doc *goquery.Document) string {
	v := strings.TrimSpace(doc.Find("#voice_actors").First().Text())
	if v == "" {
		return ""
	}
	if _, rest, ok := strings.Cut(v, ":"); ok {
		v = strings.TrimSpace(rest)
	}
	return v
}
