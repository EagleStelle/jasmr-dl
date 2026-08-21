package downloader

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/EagleStelle/jasmr-dl/internal/metadata"
	"github.com/EagleStelle/jasmr-dl/internal/naming"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

// tone writes seconds of AAC in the container a stream is remuxed into.
func tone(t *testing.T, ffmpeg, path string, seconds int) {
	t.Helper()
	out, err := runFFmpeg(context.Background(), ffmpeg, slices.Concat(ffmpegQuiet, []string{
		"-f", "lavfi", "-i", "sine=frequency=440:duration=" + strconv.Itoa(seconds),
		"-c:a", "aac", "-f", hlsOutputFormat, path,
	})...)
	if err != nil {
		t.Fatalf("build the source: %v: %s", err, out)
	}
}

// A chapter is bounded by a length, not by an absolute time on a stream that
// has already been seeked: -to would measure the window from the wrong end.
func TestCutTakesTheChapterLength(t *testing.T) {
	tools, err := findFFmpeg()
	if err != nil {
		t.Skip("ffmpeg not installed:", err)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "full.m4a")
	tone(t, tools.ffmpeg, src, 30)

	var d Downloader
	dst := filepath.Join(dir, "02_chapter.m4a")
	if err := d.cut(context.Background(), tools.ffmpeg, src, dst, 10*time.Second, 8*time.Second); err != nil {
		t.Fatal(err)
	}

	got, err := d.probeDuration(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	if got < 7500*time.Millisecond || got > 8500*time.Millisecond {
		t.Errorf("cut ran %v, want about 8s", got)
	}
	if _, err := os.Stat(dst + ".part"); err == nil {
		t.Error("the staged file was left behind")
	}
}

func TestPiecePathsNameEveryChapter(t *testing.T) {
	tmpl, err := naming.Parse("{number}_{chapter}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	d := Downloader{
		OutputDir: "out",
		Template:  tmpl,
		Chapters: []scraper.Chapter{
			{Start: 0, Title: "はじめに"},
			{Start: time.Minute, Title: "耳かき"},
			{Start: 2 * time.Minute, Title: "Track 3/4"},
		},
	}

	got := d.piecePaths(naming.Fields{Fields: metadata.Fields{Title: "work", RJCode: "RJ1"}})
	want := []string{
		filepath.Join("out", "1_はじめに.m4a"),
		filepath.Join("out", "2_耳かき.m4a"),
		// The separator in a chapter title cannot open a directory.
		filepath.Join("out", "3_Track 3_4.m4a"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("piecePaths =\n%q\nwant\n%q", got, want)
	}
}

// A template of its own still gets the chapter, the number and the work.
func TestPiecePathsHonourTheTemplate(t *testing.T) {
	tmpl, err := naming.Parse("{rjcode}/{number} - {chapter}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	d := Downloader{OutputDir: "out", Template: tmpl}
	for i := range 12 {
		d.Chapters = append(d.Chapters, scraper.Chapter{
			Start: time.Duration(i) * time.Minute,
			Title: "c",
		})
	}

	got := d.piecePaths(naming.Fields{Fields: metadata.Fields{RJCode: "RJ1"}})
	if want := filepath.Join("out", "01 - c.m4a"); got[0] != want {
		t.Errorf("first = %q, want %q", got[0], want)
	}
	if want := filepath.Join("out", "12 - c.m4a"); got[11] != want {
		t.Errorf("last = %q, want %q", got[11], want)
	}
}

func TestFinishedPieces(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "1.m4a"),
		filepath.Join(dir, "2.m4a"),
	}

	if _, ok := finishedPieces(paths); ok {
		t.Error("nothing on disk read as finished")
	}

	if err := os.WriteFile(paths[0], []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := finishedPieces(paths)
	if ok {
		t.Error("a gap read as finished")
	}
	if len(got) != 1 {
		t.Errorf("got %d pieces, want 1", len(got))
	}

	if err := os.WriteFile(paths[1], []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := finishedPieces(paths); !ok || len(got) != 2 {
		t.Errorf("got %d pieces (finished %v), want 2 and finished", len(got), ok)
	}
}
