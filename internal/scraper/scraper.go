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

// PoliteDelay is the robots.txt Crawl-delay. It applies between fetches of the
// site itself, not the media host.
const PoliteDelay = 10 * time.Second

var rjPattern = regexp.MustCompile(`RJ\d+`)

// Scraper reads album metadata. Safe to reuse across albums.
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

// SetDelay is for tests; lowering it live ignores the declared Crawl-delay.
func (s *Scraper) SetDelay(d time.Duration) { s.delay = d }

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
		RJCode:   extractRJCode(doc),
		Artists:  extractArtists(doc),
		Tracks:   directTracks(doc, base),
		Chapters: extractChapters(doc),
	}
	if len(album.Tracks) == 0 {
		return nil, fmt.Errorf("no audio in any player on %s", pageURL)
	}
	return album, nil
}

// fetchDoc GETs and parses HTML, waiting the crawl delay after the first call.
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

// extractCover prefers poster: og:image is sometimes a site-wide default and
// the first <img> is the logo. Art and media hosts differ only by transposed
// characters (weeabo0 vs weeab0o), so never derive one from the other.
func extractCover(doc *goquery.Document, base *url.URL) string {
	if v, ok := doc.Find("video[poster]").First().Attr("poster"); ok {
		if abs := resolve(base, v); abs != "" {
			return abs
		}
	}
	if v, ok := doc.Find(`meta[property="og:image"]`).Attr("content"); ok {
		if abs := resolve(base, v); abs != "" {
			return abs
		}
	}
	// Art is lazy-loaded: src holds a placeholder, data-src the real URL.
	if v, ok := doc.Find(`img[data-src*="pic."]`).First().Attr("data-src"); ok {
		return resolve(base, v)
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

func extractRJCode(doc *goquery.Document) string {
	for _, sel := range []string{"video[title]", "audio[title]"} {
		if v, ok := doc.Find(sel).First().Attr("title"); ok {
			if m := rjPattern.FindString(v); m != "" {
				return m
			}
		}
	}
	// The "RJ Code: RJnnnnnn" line in the post body.
	return rjPattern.FindString(doc.Find("body").Text())
}

// resolve makes a possibly-relative href absolute.
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

func isHTTP(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}
