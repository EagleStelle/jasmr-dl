package downloader

import (
	"os"
	"strings"
	"testing"
	"time"

	"jasmr-dl/internal/scraper"
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
		`semi\;colon\=equals\#hash`,
		"トラック1：リアス部長の花嫁修行！？_wav",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if n := strings.Count(s, "[CHAPTER]"); n != 3 {
		t.Errorf("got %d chapters, want 3", n)
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
