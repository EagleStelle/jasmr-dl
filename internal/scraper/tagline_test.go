package scraper

import "testing"

func tagline(inner string) string {
	return `<p class="entry-tagline">
	  <span class="post-meta-span post-meta-span-time"><time datetime="2026-06-06" pubdate>June 6, 2026</time></span>
	  <span class="post-meta-span post-meta-span-category">` + inner + `</span></p>`
}

func cat(slug, label string) string {
	return `<a href="https://host.example/category/` + slug + `/" rel="category tag">` + label + `</a>`
}

func TestExtractCircleAndDate(t *testing.T) {
	doc, _ := parse(t, tagline(cat("rating/maniax", "NSFW")+cat("requested", "Requested")+cat("circle-slug", "さーくる")))

	if got := extractCircle(doc); got != "さーくる" {
		t.Errorf("circle = %q", got)
	}
	if got := extractDate(doc); got != "2026-06-06" {
		t.Errorf("date = %q", got)
	}
}

func TestExtractCircleSkipsRatingsOutsideRatingPath(t *testing.T) {
	// Futanari is a rating in the menu but filed at the top level.
	doc, _ := parse(t, tagline(cat("futanari", "Futanari")+cat("rating/maniax", "NSFW")+cat("circle-slug", "さーくる")))

	if got := extractCircle(doc); got != "さーくる" {
		t.Errorf("circle = %q, want the circle after the rating", got)
	}
}

func TestExtractCircleAcceptsEitherHalfOfAPairedLabel(t *testing.T) {
	doc, _ := parse(t, tagline(cat("a", "SFW / 全年齢")+cat("b", "百合")+cat("c", "さーくる")))

	if got := extractCircle(doc); got != "さーくる" {
		t.Errorf("circle = %q", got)
	}
}

func TestExtractCircleEmptyWhenOnlyRatings(t *testing.T) {
	doc, _ := parse(t, tagline(cat("rating/sfw", "SFW")+cat("requested", "Requested")))

	if got := extractCircle(doc); got != "" {
		t.Errorf("circle = %q, want none rather than a guess", got)
	}
}

func TestExtractDateRejectsUnparseable(t *testing.T) {
	doc, _ := parse(t, `<p class="entry-tagline"><time datetime="June 6, 2026">June 6, 2026</time></p>`)

	if got := extractDate(doc); got != "" {
		t.Errorf("date = %q, want none", got)
	}
}

func TestExtractArtistsFallsBackToTheUnmarkedLine(t *testing.T) {
	doc, _ := parse(t, `<div class="entry-content">
		<p><strong>work title</strong></p>
		<p>CV: 声優あ</p>
		<p>RJ Code: RJ000000</p></div>`)

	if got := extractArtists(doc); got != "声優あ" {
		t.Errorf("artists = %q", got)
	}
	if got := extractRJCode(doc); got != "RJ000000" {
		t.Errorf("rj code = %q", got)
	}
}

func TestExtractRJCodeRejectsAnythingElse(t *testing.T) {
	doc, _ := parse(t, `<div class="entry-content"><p>RJ Code: not a code</p></div>`)

	if got := extractRJCode(doc); got != "" {
		t.Errorf("rj code = %q, want none", got)
	}
}

func TestExtractArtistsPrefersTheMarkedLine(t *testing.T) {
	doc, _ := parse(t, `<div class="entry-content">
		<p id="voice_actors">CV：声優あ, 声優い</p>
		<p>CV: ignored</p></div>`)

	if got := extractArtists(doc); got != "声優あ, 声優い" {
		t.Errorf("artists = %q", got)
	}
}
