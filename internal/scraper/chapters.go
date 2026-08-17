package scraper

import (
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const maxChapters = 2000

// extractChapters reads the track list; offsets are data-value seconds.
func extractChapters(doc *goquery.Document) []Chapter {
	var out []Chapter

	doc.Find("#basic-chapter-playlist tr, #plyr-chapter-playlist tr").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		if len(out) >= maxChapters {
			return false
		}

		cell := row.Find("a[data-track-title]").First()
		title, ok := cell.Attr("data-track-title")
		if !ok {
			return true
		}
		value, ok := cell.Attr("data-value")
		if !ok {
			return true
		}
		secs, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || secs < 0 {
			return true
		}

		title = strings.TrimSpace(title)
		if title == "" {
			return true
		}
		out = append(out, Chapter{
			Start: time.Duration(secs) * time.Second,
			Title: title,
		})
		return true
	})

	// Both player skins list the same chapters.
	return dedupeChapters(out)
}

func dedupeChapters(in []Chapter) []Chapter {
	var (
		out  []Chapter
		seen = map[time.Duration]bool{}
	)
	for _, c := range in {
		if seen[c.Start] {
			continue
		}
		seen[c.Start] = true
		out = append(out, c)
	}
	if len(out) < 2 {
		return nil // one chapter describes nothing
	}
	return out
}
