package scraper

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"jasmr-dl/internal/util"
)

// trackNumber matches the "NN_" ordinal in an anchor label such as
// "RJnnnnnn - 03_<title>.m4a". Anchors without it are the combined whole-work
// file rather than an individual track.
var trackNumber = regexp.MustCompile(`-\s*(\d+)_`)

// tracks fetches the dlc.php listing and returns one Track per audio anchor.
func (s *Scraper) tracks(ctx context.Context, dlcURL string) ([]Track, error) {
	doc, err := s.fetchDoc(ctx, dlcURL)
	if err != nil {
		return nil, fmt.Errorf("fetch download listing: %w", err)
	}

	base, err := url.Parse(dlcURL)
	if err != nil {
		return nil, fmt.Errorf("parse listing URL: %w", err)
	}

	var (
		tracks []Track
		seen   = map[string]bool{}
	)

	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href, _ := sel.Attr("href")
		link := resolve(base, href)
		if link == "" || seen[link] {
			return
		}

		label := strings.TrimSpace(sel.Text())

		// The listing page warns that this file host also carries
		// executables and that only audio is legitimately uploaded. The
		// label is attacker-influencable, so allowlist the extension
		// rather than filtering known-bad ones.
		if !util.IsAudioFile(label) {
			return
		}
		if !isHTTP(link) {
			return
		}

		seen[link] = true

		t := Track{
			Title:   util.Sanitize(label),
			LinkURL: link,
		}
		if m := trackNumber.FindStringSubmatch(label); m != nil {
			t.Index, _ = strconv.Atoi(m[1])
		} else {
			t.Combined = true
		}
		tracks = append(tracks, t)
	})

	if len(tracks) == 0 {
		return nil, fmt.Errorf("no audio links found at %s: listing layout may have changed", dlcURL)
	}
	return tracks, nil
}

func isHTTP(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}
