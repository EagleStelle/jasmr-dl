package scraper

import (
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	taglineSelector = "p.entry-tagline"
	// tagsSelector is the tag line under the post. Its anchors carry rel="tag",
	// where a category anchor carries rel="category tag".
	tagsSelector = `p.post-tags a[rel="tag"]`
	ratingPath   = "/category/rating/"
	isoDate      = "2006-01-02"
)

// notCircle are the categories describing a post's audience, not its author.
var notCircle = map[string]bool{
	"requested": true,
	"sfw":       true, "全年齢": true, "全年齢向け": true,
	"r-15": true, "r15": true,
	"nsfw": true, "r18": true, "r-18": true,
	"otome": true, "女性向け": true,
	"boys’ love": true, "boys' love": true, "bl": true, "ボーイズラブ": true,
	"yuri": true, "girls love": true, "百合": true,
	"otoko no ko": true, "男の娘": true,
	"futanari": true, "フタナリ": true,
	"r-18g": true, "グロ": true,
}

// extractCircle takes the one category that is neither a rating nor "Requested".
func extractCircle(doc *goquery.Document) string {
	var circle string
	doc.Find(taglineSelector + " span.post-meta-span-category a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		if strings.Contains(href, ratingPath) {
			return true
		}
		label := strings.TrimSpace(a.Text())
		if label == "" || isNotCircle(label) {
			return true
		}
		circle = label
		return false
	})
	return circle
}

// isNotCircle tests each half of a label, since the menu writes "SFW / 全年齢"
// where the tagline shows one side.
func isNotCircle(label string) bool {
	for _, part := range strings.Split(label, "/") {
		if notCircle[strings.ToLower(strings.TrimSpace(part))] {
			return true
		}
	}
	return false
}

// extractTags lists the post's tags in page order. A post carries the same tag
// under both its English and Japanese label, so each label is kept once.
func extractTags(doc *goquery.Document) []string {
	var (
		tags []string
		seen = map[string]bool{}
	)
	doc.Find(tagsSelector).Each(func(_ int, a *goquery.Selection) {
		label := strings.TrimSpace(a.Text())
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		tags = append(tags, label)
	})
	return tags
}

// extractDate reads the datetime attribute, not the text beside it.
func extractDate(doc *goquery.Document) string {
	v, ok := doc.Find(taglineSelector + " time[datetime]").First().Attr("datetime")
	if !ok {
		return ""
	}
	v = strings.TrimSpace(v)
	if _, err := time.Parse(isoDate, v); err != nil {
		return ""
	}
	return v
}
