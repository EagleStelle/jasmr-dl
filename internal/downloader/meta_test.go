package downloader

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

func TestWriteChapterMeta(t *testing.T) {
	path := t.TempDir() + "/m.ffmeta"
	chapters := []scraper.Chapter{
		{Start: 0, Title: "トラック1：リアス部長の花嫁修行！？_wav"},
		{Start: 323 * time.Second, Title: "semi;colon=equals#hash"},
		{Start: 1260 * time.Second, Title: "last"},
	}
	if err := writeChapterMeta(path, chapters, 2000*time.Second); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)

	if !strings.HasPrefix(s, ";FFMETADATA1\n") {
		t.Error("missing header")
	}
	for _, want := range []string{
		"START=0\nEND=323000",
		"START=323000\nEND=1260000",
		"START=1260000\nEND=2000000",
		// Numbered, the same form a split gives each file.
		"title=1_トラック1：リアス部長の花嫁修行！？_wav",
		`title=2_semi\;colon\=equals\#hash`,
		"title=3_last",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if n := strings.Count(s, "[CHAPTER]"); n != 3 {
		t.Errorf("got %d chapters, want 3", n)
	}
}

// Past nine chapters the numbers pad, so a listing sorts in order.
func TestWriteChapterMetaPadsPastNine(t *testing.T) {
	path := t.TempDir() + "/m.ffmeta"
	chapters := make([]scraper.Chapter, 12)
	for i := range chapters {
		chapters[i] = scraper.Chapter{Start: time.Duration(i) * time.Minute, Title: "c"}
	}
	if err := writeChapterMeta(path, chapters, time.Hour); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)

	for _, want := range []string{"title=01_c", "title=09_c", "title=12_c"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestWriteChapterMetaDropsOverrun(t *testing.T) {
	path := t.TempDir() + "/m.ffmeta"
	// A chapter starting at or past the end cannot be closed.
	chapters := []scraper.Chapter{{Start: 0, Title: "a"}, {Start: 900 * time.Second, Title: "b"}}
	if err := writeChapterMeta(path, chapters, 500*time.Second); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if n := strings.Count(string(got), "[CHAPTER]"); n != 1 {
		t.Errorf("got %d chapters, want 1", n)
	}
}
