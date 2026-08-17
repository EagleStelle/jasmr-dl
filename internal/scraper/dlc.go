package scraper

import (
	"context"
	"errors"
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

// ErrListingUnavailable means dlc.php answered 200 but served its soft-error
// page instead of the listing. The site does this under load or when a client
// has been fetching too often, so it is transient rather than fatal.
var ErrListingUnavailable = errors.New("download listing temporarily unavailable (site returned its error page) — most likely rate limiting; wait a few minutes")

// listingErrorMarker is the ASCII portion of the soft-error page. The rest of
// that page is mojibake and not safe to match on.
const listingErrorMarker = "Something went wrong"

// listingAttempts covers a brief throttle. Each retry waits the crawl delay.
const listingAttempts = 3

// tracks fetches the dlc.php listing and returns one Track per audio anchor,
// retrying through the site's transient error page.
func (s *Scraper) tracks(ctx context.Context, dlcURL string) ([]Track, error) {
	var lastErr error
	for i := 0; i < listingAttempts; i++ {
		tracks, err := s.tracksOnce(ctx, dlcURL)
		if err == nil {
			return tracks, nil
		}
		if !errors.Is(err, ErrListingUnavailable) {
			return nil, err
		}
		lastErr = err
		// fetchDoc already waits the crawl delay before each retry.
	}
	return nil, lastErr
}

func (s *Scraper) tracksOnce(ctx context.Context, dlcURL string) ([]Track, error) {
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
		// Distinguish "site is throttling us" from "our selectors broke".
		// Both arrive as HTTP 200 with no links, but only one is worth
		// retrying, and reporting the wrong one sends you debugging
		// selectors that are fine.
		if strings.Contains(doc.Find("body").Text(), listingErrorMarker) {
			return nil, ErrListingUnavailable
		}
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
