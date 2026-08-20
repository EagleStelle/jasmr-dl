package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/EagleStelle/jasmr-dl/internal/util"
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
		PageURL:   pageURL,
		Title:     extractTitle(doc),
		JacketURL: extractJacket(doc, base),
		Artists:   extractArtists(doc),
		Circle:    extractCircle(doc),
		RJCode:    extractRJCode(doc),
		Date:      extractDate(doc),
		Tracks:    directTracks(doc, base),
		Chapters:  extractChapters(doc),
		ImageURLs: extractImages(doc, base),
		Tags:      extractTags(doc),
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

// extractJacket prefers poster: og:image is sometimes a site-wide default and
// the first <img> is the logo. Art and media hosts differ only by transposed
// characters (weeabo0 vs weeab0o), so never derive one from the other.
func extractJacket(doc *goquery.Document, base *url.URL) string {
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

// gallerySelector is the post's lightbox, one empty anchor per picture. The
// <img> tags beside it are lazy-load placeholders and thumbnails, so the full
// size of a picture is only ever in an href.
const gallerySelector = ".fotorama a[href]"

// extractImages lists the post's gallery in page order, the jacket first. A page
// can name one picture twice, so each URL is kept once.
func extractImages(doc *goquery.Document, base *url.URL) []string {
	var (
		images []string
		seen   = map[string]bool{}
	)
	doc.Find(gallerySelector).Each(func(_ int, sel *goquery.Selection) {
		abs := util.ResolveURL(base, sel.AttrOr("href", ""))
		if abs == "" || seen[abs] || !util.IsImageFile(util.URLBase(abs)) {
			return
		}
		seen[abs] = true
		images = append(images, abs)
	})
	return images
}

var (
	cvLine = regexp.MustCompile(`^CV\s*[:：]`)
	rjLine = regexp.MustCompile(`^RJ\s*Code\s*[:：]`)
	rjCode = regexp.MustCompile(`^RJ\d+$`)
)

// extractArtists reads the voice actor line. Posts predating the id carry the
// same line unmarked.
func extractArtists(doc *goquery.Document) string {
	if v := strings.TrimSpace(doc.Find("#voice_actors").First().Text()); v != "" {
		return afterLabel(v)
	}
	return labelled(doc, cvLine)
}

// extractRJCode reads the DLsite code, the one id every file of a post shares.
func extractRJCode(doc *goquery.Document) string {
	code := labelled(doc, rjLine)
	if !rjCode.MatchString(code) {
		return ""
	}
	return code
}

// labelled returns the value of the first post line carrying label.
func labelled(doc *goquery.Document, label *regexp.Regexp) string {
	var v string
	doc.Find(".entry-content p").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		if t := strings.TrimSpace(p.Text()); label.MatchString(t) {
			v = t
			return false
		}
		return true
	})
	return afterLabel(v)
}

func afterLabel(v string) string {
	for _, sep := range []string{":", "："} {
		if _, rest, ok := strings.Cut(v, sep); ok {
			return strings.TrimSpace(rest)
		}
	}
	return strings.TrimSpace(v)
}
