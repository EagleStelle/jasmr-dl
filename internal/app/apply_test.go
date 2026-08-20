package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// applying runs a record against a directory, with both streams captured.
func applying(t *testing.T, cfg Config) (stdout, stderr string, s Summary, err error) {
	t.Helper()

	var out, errBuf bytes.Buffer
	cfg.Stdout, cfg.Stderr = &out, &errBuf
	s, err = Run(context.Background(), cfg)
	return out.String(), errBuf.String(), s, err
}

// record writes an info.json naming one file, and returns its path.
func record(t *testing.T, dir string, info Info) string {
	t.Helper()

	path := filepath.Join(dir, "RJ123456"+infoSuffix)
	if err := writeInfoJSON(path, info); err != nil {
		t.Fatal(err)
	}
	return path
}

// A record naming files that are not there fails, rather than reporting a run
// that wrote nothing as a success.
func TestApplyFailsWhenNoFileIsThere(t *testing.T) {
	dir := t.TempDir()
	path := record(t, dir, Info{
		Version: infoVersion,
		URL:     "https://japaneseasmr.com/12345/",
		Tracks:  []InfoTrack{{File: "gone.mp3", Title: "ある夏の日"}},
	})

	_, stderr, _, err := applying(t, Config{LoadInfoJSON: path, EmbedMetadata: true})
	if err == nil {
		t.Fatal("applying a record to nothing succeeded, want an error")
	}
	if !strings.Contains(stderr, "gone.mp3: not on disk") {
		t.Errorf("stderr does not name the missing file\ngot:\n%s", stderr)
	}
}

// A URL says to download, a record says to write over what is already there.
// Asking for both at once is a mistake worth reporting rather than guessing at.
func TestApplyRefusesAURLAlongsideARecord(t *testing.T) {
	_, _, _, err := applying(t, Config{
		Targets:      []string{"https://japaneseasmr.com/12345/"},
		LoadInfoJSON: "RJ123456.info.json",
	})
	if err == nil {
		t.Fatal("a record and a URL together were accepted, want an error")
	}
	if !strings.Contains(err.Error(), "no URL") {
		t.Errorf("err = %q, want it to say a record takes no URL", err)
	}
}

// Naming no embed flag at all leaves nothing to write, which is worth saying
// rather than reporting every file as written.
func TestApplyRefusesWithNothingToWrite(t *testing.T) {
	dir := t.TempDir()
	path := record(t, dir, Info{
		Version: infoVersion,
		Tracks:  []InfoTrack{{File: "a.mp3", Title: "ある夏の日"}},
	})

	_, _, _, err := applying(t, Config{LoadInfoJSON: path})
	if err == nil {
		t.Fatal("a record applied with nothing to write succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "nothing to write") {
		t.Errorf("err = %q, want it to say nothing was named", err)
	}
}

// --- the real thing, which needs ffmpeg -------------------------------------

func ffmpegOrSkip(t *testing.T) (ffmpeg, ffprobe string) {
	t.Helper()

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed:", err)
	}
	ffprobe, err = exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed:", err)
	}
	return ffmpeg, ffprobe
}

// tone writes seconds of AAC to path, the container a stream is remuxed into.
func tone(t *testing.T, ffmpeg, path string, seconds int) {
	t.Helper()

	out, err := exec.Command(ffmpeg, "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(seconds),
		"-c:a", "aac", "-f", "ipod", path).CombinedOutput()
	if err != nil {
		t.Fatalf("build the source: %v: %s", err, out)
	}
}

func probe(t *testing.T, ffprobe string, args ...string) string {
	t.Helper()

	out, err := exec.Command(ffprobe, append([]string{"-v", "error"}, args...)...).Output()
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// A record written by one run has to write back into the files a later one
// finds beside it, without anything being fetched.
func TestApplyWritesTheRecordIntoTheFile(t *testing.T) {
	ffmpeg, ffprobe := ffmpegOrSkip(t)

	dir := t.TempDir()
	audio := filepath.Join(dir, "RJ123456.m4a")
	tone(t, ffmpeg, audio, 20)

	path := record(t, dir, Info{
		Version: infoVersion,
		URL:     "https://japaneseasmr.com/12345/",
		Title:   "ある夏の日",
		RJCode:  "RJ123456",
		Tracks: []InfoTrack{{
			File:    "RJ123456.m4a",
			Title:   "ある夏の日",
			Album:   "RJ123456",
			Artist:  "声優",
			Genre:   genre,
			Comment: "URL: https://japaneseasmr.com/12345/",
			Chapters: []InfoChapter{
				{Start: 0, Title: "はじめに"},
				{Start: 10, Title: "耳かき"},
			},
		}},
	})

	// Naming nothing writes everything the record holds.
	stdout, _, s, err := applying(t, Config{LoadInfoJSON: path, EmbedMetadata: true, EmbedChapters: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Files != 1 {
		t.Errorf("%d files written, want 1", s.Files)
	}
	if !strings.Contains(stdout, "[done] 1 file written") {
		t.Errorf("stdout does not report the file\ngot:\n%s", stdout)
	}

	got := probe(t, ffprobe, "-show_entries", "format_tags=title",
		"-of", "default=noprint_wrappers=1:nokey=1", audio)
	if got != "ある夏の日" {
		t.Errorf("title = %q, want the record's own", got)
	}

	chapters := probe(t, ffprobe, "-show_entries", "chapter_tags=title", "-of", "csv=p=0", audio)
	for _, want := range []string{"はじめに", "耳かき"} {
		if !strings.Contains(chapters, want) {
			t.Errorf("chapters = %q, do not carry %q", chapters, want)
		}
	}
}

// Naming one embed flag narrows a record to what was named: the chapters go in,
// the metadata stays out.
func TestApplyWritesOnlyWhatWasNamed(t *testing.T) {
	ffmpeg, ffprobe := ffmpegOrSkip(t)

	dir := t.TempDir()
	audio := filepath.Join(dir, "RJ123456.m4a")
	tone(t, ffmpeg, audio, 20)

	path := record(t, dir, Info{
		Version: infoVersion,
		Tracks: []InfoTrack{{
			File:     "RJ123456.m4a",
			Title:    "ある夏の日",
			Chapters: []InfoChapter{{Start: 0, Title: "はじめに"}},
		}},
	})

	if _, _, _, err := applying(t, Config{LoadInfoJSON: path, EmbedChapters: true}); err != nil {
		t.Fatal(err)
	}

	if got := probe(t, ffprobe, "-show_entries", "format_tags=title",
		"-of", "default=noprint_wrappers=1:nokey=1", audio); got != "" {
		t.Errorf("title = %q, want none written without --embed-metadata", got)
	}
	if got := probe(t, ffprobe, "-show_entries", "chapter_tags=title", "-of", "csv=p=0", audio); !strings.Contains(got, "はじめに") {
		t.Errorf("chapters = %q, want the record's own", got)
	}
}

// One file missing does not take the rest of the record down with it.
func TestApplyCarriesOnPastAMissingFile(t *testing.T) {
	ffmpeg, _ := ffmpegOrSkip(t)

	dir := t.TempDir()
	tone(t, ffmpeg, filepath.Join(dir, "a.m4a"), 5)

	path := record(t, dir, Info{
		Version: infoVersion,
		Tracks: []InfoTrack{
			{File: "gone.m4a", Title: "gone"},
			{File: "a.m4a", Title: "ある夏の日"},
		},
	})

	_, stderr, s, err := applying(t, Config{LoadInfoJSON: path, EmbedMetadata: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Files != 1 {
		t.Errorf("%d files written, want the 1 that was there", s.Files)
	}
	if !strings.Contains(stderr, "gone.m4a") {
		t.Errorf("stderr does not name the missing file\ngot:\n%s", stderr)
	}
}

// A record moved with its directory still applies: paths are read beside it,
// never as the absolute ones the first run happened to write to.
func TestApplyFollowsAMovedDirectory(t *testing.T) {
	ffmpeg, ffprobe := ffmpegOrSkip(t)

	first := filepath.Join(t.TempDir(), "RJ123456")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	tone(t, ffmpeg, filepath.Join(first, "a.m4a"), 5)
	record(t, first, Info{
		Version: infoVersion,
		Tracks:  []InfoTrack{{File: "a.m4a", Title: "ある夏の日"}},
	})

	moved := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Rename(first, moved); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(moved, "RJ123456"+infoSuffix)
	if _, _, s, err := applying(t, Config{LoadInfoJSON: path, EmbedMetadata: true}); err != nil {
		t.Fatal(err)
	} else if s.Files != 1 {
		t.Errorf("%d files written, want 1", s.Files)
	}

	got := probe(t, ffprobe, "-show_entries", "format_tags=title",
		"-of", "default=noprint_wrappers=1:nokey=1", filepath.Join(moved, "a.m4a"))
	if got != "ある夏の日" {
		t.Errorf("title = %q, want the record's own", got)
	}
}
