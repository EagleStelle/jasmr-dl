package downloader

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EagleStelle/jasmr-dl/internal/naming"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

const chapterTimebase = "1/1000"

// chapterName numbers a chapter within its work. A split writes it as the
// filename, a whole file as the embedded chapter title; nothing else carries
// the order in either place.
func chapterName(i, total int, title string) string {
	return fmt.Sprintf("%0*d_%s", naming.Width(total), i+1, title)
}

// chapterSpan is where one chapter runs, bounded by the next or by total. ok is
// false where the stream ends at or before the chapter starts.
func chapterSpan(chapters []scraper.Chapter, i int, total time.Duration) (start, end time.Duration, ok bool) {
	start = chapters[i].Start
	end = total
	if i+1 < len(chapters) {
		end = chapters[i+1].Start
	}
	return start, end, end > start
}

// writeChapterMeta writes ffmetadata. ffmpeg requires an END on every chapter.
func writeChapterMeta(path string, chapters []scraper.Chapter, total time.Duration) error {
	var b strings.Builder
	b.WriteString(";FFMETADATA1\n")

	for i, c := range chapters {
		start, end, ok := chapterSpan(chapters, i, total)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\n[CHAPTER]\nTIMEBASE=%s\nSTART=%d\nEND=%d\ntitle=%s\n",
			chapterTimebase, start.Milliseconds(), end.Milliseconds(),
			escapeMeta(chapterName(i, len(chapters), c.Title)))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// reportOverrun names every chapter the stream ends before.
func (d *Downloader) reportOverrun(chapters []scraper.Chapter, total time.Duration) {
	if d.OnChapterDropped == nil {
		return
	}
	for i, c := range chapters {
		if start, _, ok := chapterSpan(chapters, i, total); !ok {
			d.OnChapterDropped(chapterName(i, len(chapters), c.Title), start, total)
		}
	}
}

// escapeMeta quotes what ffmetadata treats as syntax.
func escapeMeta(s string) string {
	s = strings.NewReplacer(
		`\`, `\\`,
		"=", `\=`,
		";", `\;`,
		"#", `\#`,
		"\n", " ",
		"\r", " ",
	).Replace(s)
	return strings.TrimSpace(s)
}
