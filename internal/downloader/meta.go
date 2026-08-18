package downloader

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

const chapterTimebase = "1/1000"

// writeChapterMeta writes ffmetadata. ffmpeg requires an END on every chapter.
func writeChapterMeta(path string, chapters []scraper.Chapter, total time.Duration) error {
	var b strings.Builder
	b.WriteString(";FFMETADATA1\n")

	for i, c := range chapters {
		end := total
		if i+1 < len(chapters) {
			end = chapters[i+1].Start
		}
		if end <= c.Start {
			continue
		}
		fmt.Fprintf(&b, "\n[CHAPTER]\nTIMEBASE=%s\nSTART=%d\nEND=%d\ntitle=%s\n",
			chapterTimebase, c.Start.Milliseconds(), end.Milliseconds(), escapeMeta(c.Title))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
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
