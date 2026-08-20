package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

var errTest = errors.New("dead link")

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// streamPlan is a post serving one chaptered stream, the shape a split cuts up.
func streamPlan(t *testing.T, dir string) *plan {
	t.Helper()

	album := &scraper.Album{
		PageURL:  "https://japaneseasmr.com/12345/",
		Title:    "ある夏の日",
		RJCode:   "RJ123456",
		Circle:   "サークル",
		Artists:  "声優",
		Date:     "2024-05-01",
		Tags:     []string{"ASMR", "耳かき"},
		Tracks:   []scraper.Track{{Title: "stream", Source: scraper.SourceHLS}},
		CoverURL: "https://cdn/cover.jpg",
		Chapters: []scraper.Chapter{
			{Start: 0, Title: "はじめに"},
			{Start: 10 * time.Minute, Title: "耳かき"},
		},
	}

	r := &run{cfg: Config{Log: func(string, ...any) {}}}
	tags := r.tagsFor(album, album.Tracks[0], 1)
	return &plan{
		album:    album,
		dir:      dir,
		chapters: album.Chapters,
		tags:     []downloader.Tags{tags},
		lines:    linesFor(album),
	}
}

// A split writes one file per chapter, each titled for it and numbered within
// the work, and none of them carries a chapter list of its own.
func TestInfoForSplitPieces(t *testing.T) {
	dir := t.TempDir()
	p := streamPlan(t, dir)
	p.split = true

	r := &run{cfg: Config{SplitChapters: true}}
	info := r.infoFor(p, downloader.Pictures{}, []downloader.Result{{
		Files: []downloader.OutputFile{
			{Path: filepath.Join(dir, "1_はじめに.m4a"), Chapter: 0},
			{Path: filepath.Join(dir, "2_耳かき.m4a"), Chapter: 1},
		},
	}})

	if len(info.Chapters) != 2 {
		t.Fatalf("%d chapters recorded, want the post's 2", len(info.Chapters))
	}
	if len(info.Tracks) != 2 {
		t.Fatalf("%d tracks, want 2", len(info.Tracks))
	}
	for i, want := range []string{"はじめに", "耳かき"} {
		got := info.Tracks[i]
		if got.Title != want {
			t.Errorf("track %d titled %q, want the chapter's %q", i, got.Title, want)
		}
		if got.Track != i+1 || got.TrackTotal != 2 {
			t.Errorf("track %d numbered %d/%d, want %d/2", i, got.Track, got.TrackTotal, i+1)
		}
		if got.Chapters != nil {
			t.Errorf("track %d carries a chapter list; a piece is one chapter already", i)
		}
		if got.Album != "RJ123456" {
			t.Errorf("track %d on album %q, want the work's own", i, got.Album)
		}
	}
}

// A stream left whole carries the whole track list, so a later run can write it
// into the file without reading the page again.
func TestInfoForWholeStreamKeepsTheChapters(t *testing.T) {
	dir := t.TempDir()
	p := streamPlan(t, dir)

	r := &run{cfg: Config{}}
	info := r.infoFor(p, downloader.Pictures{
		Cover:   filepath.Join(dir, "cover.jpg"),
		Gallery: []string{filepath.Join(dir, "images", "01.jpg")},
	}, []downloader.Result{{
		Files: []downloader.OutputFile{{Path: filepath.Join(dir, "RJ123456.m4a"), Chapter: -1}},
	}})

	if info.Source != sourceStream {
		t.Errorf("source = %q, want %q", info.Source, sourceStream)
	}
	if got := info.Tracks[0].Chapters; len(got) != 2 {
		t.Fatalf("%d chapters on the file, want 2", len(got))
	}
	if got, want := info.Tracks[0].Chapters[1].Start, (10 * time.Minute).Seconds(); got != want {
		t.Errorf("second chapter at %v seconds, want %v", got, want)
	}
	if info.Tracks[0].Title != "ある夏の日" {
		t.Errorf("title = %q, want the post's own", info.Tracks[0].Title)
	}
	if info.Cover != "cover.jpg" {
		t.Errorf("cover = %q, want it relative to the record", info.Cover)
	}
	if !slices.Equal(info.Images, []string{"images/01.jpg"}) {
		t.Errorf("images = %v, want forward slashes relative to the record", info.Images)
	}
}

// A post serving its own files has no stream, so no chapters go anywhere.
func TestInfoForSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	album := &scraper.Album{
		PageURL: "https://japaneseasmr.com/12345/",
		Title:   "ある夏の日",
		RJCode:  "RJ123456",
		Tracks:  []scraper.Track{{Title: "a", Name: "Track 1"}, {Title: "b", Name: "Track 2"}},
	}

	r := &run{cfg: Config{Log: func(string, ...any) {}}}
	p := &plan{
		album:    album,
		dir:      dir,
		chapters: r.chaptersFor(album),
		tags: []downloader.Tags{
			r.tagsFor(album, album.Tracks[0], 1),
			r.tagsFor(album, album.Tracks[1], 2),
		},
	}

	info := r.infoFor(p, downloader.Pictures{}, []downloader.Result{
		{Files: []downloader.OutputFile{{Path: filepath.Join(dir, "RJ123456_1.mp3"), Chapter: -1}}},
		{Files: []downloader.OutputFile{{Path: filepath.Join(dir, "RJ123456_2.mp3"), Chapter: -1}}},
	})

	if info.Source != sourceFile {
		t.Errorf("source = %q, want %q", info.Source, sourceFile)
	}
	if info.Chapters != nil {
		t.Errorf("%d chapters recorded for a post that serves files", len(info.Chapters))
	}
	if len(info.Tracks) != 2 {
		t.Fatalf("%d tracks, want 2", len(info.Tracks))
	}
	for i, track := range info.Tracks {
		if track.Track != i+1 || track.TrackTotal != 2 {
			t.Errorf("track %d numbered %d/%d, want %d/2", i, track.Track, track.TrackTotal, i+1)
		}
		if track.Chapters != nil {
			t.Errorf("track %d carries chapters it never had", i)
		}
	}
}

// A download that failed names no file, so the record does not claim one.
func TestInfoForSkipsAFailedDownload(t *testing.T) {
	dir := t.TempDir()
	p := streamPlan(t, dir)

	r := &run{cfg: Config{}}
	info := r.infoFor(p, downloader.Pictures{}, []downloader.Result{{Err: errTest}})
	if len(info.Tracks) != 0 {
		t.Errorf("%d tracks recorded, want none", len(info.Tracks))
	}
}

// The record survives the round trip: everything a later run writes back has to
// come out the way it went in.
func TestInfoJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := streamPlan(t, dir)

	r := &run{cfg: Config{}}
	want := r.infoFor(p, downloader.Pictures{Cover: filepath.Join(dir, "cover.jpg")},
		[]downloader.Result{{
			Files: []downloader.OutputFile{{Path: filepath.Join(dir, "RJ123456.m4a"), Chapter: -1}},
		}})

	path := filepath.Join(dir, infoName(p.album))
	if err := writeInfoJSON(path, want); err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(path); got != "RJ123456.info.json" {
		t.Errorf("record written to %q, want it named after the work", got)
	}

	got, err := readInfoJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != infoVersion {
		t.Errorf("version = %d, want %d", got.Version, infoVersion)
	}
	if got.URL != want.URL || got.RJCode != want.RJCode || got.Circle != want.Circle {
		t.Errorf("post = %+v, want %+v", got, want)
	}
	if !slices.Equal(got.Tags, want.Tags) {
		t.Errorf("tags = %v, want %v", got.Tags, want.Tags)
	}
	if len(got.Tracks) != 1 {
		t.Fatalf("%d tracks, want 1", len(got.Tracks))
	}
	if got.Tracks[0].File != want.Tracks[0].File {
		t.Errorf("file = %q, want %q", got.Tracks[0].File, want.Tracks[0].File)
	}
	if got.Tracks[0].tags() != want.Tracks[0].tags() {
		t.Errorf("tags = %+v, want %+v", got.Tracks[0].tags(), want.Tracks[0].tags())
	}
	if !slices.Equal(chaptersFrom(got.Tracks[0].Chapters), p.chapters) {
		t.Errorf("chapters = %v, want %v", got.Tracks[0].Chapters, p.chapters)
	}
}

func TestReadInfoJSONRejectsWhatItCannotUse(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, body, want string }{
		{"broken.info.json", "{", "broken.info.json"},
		{"empty.info.json", `{"info_version":1,"url":"x"}`, "names no file"},
		{"future.info.json", `{"info_version":99,"tracks":[{"file":"a.mp3"}]}`, "newer version"},
	} {
		path := filepath.Join(dir, tc.name)
		write(t, path, tc.body)
		_, err := readInfoJSON(path)
		if err == nil {
			t.Errorf("%s was accepted, want an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %q, want it to name %q", tc.name, err, tc.want)
		}
	}
}

func TestInfoNameFallsBackPastAMissingRJCode(t *testing.T) {
	for _, tc := range []struct {
		album *scraper.Album
		want  string
	}{
		{&scraper.Album{RJCode: "RJ123456", Title: "ある夏の日"}, "RJ123456.info.json"},
		{&scraper.Album{Title: "ある夏の日"}, "ある夏の日.info.json"},
		{&scraper.Album{}, "post.info.json"},
		// A separator in a scraped value cannot open a directory.
		{&scraper.Album{Title: "a/b"}, "a_b.info.json"},
	} {
		if got := infoName(tc.album); got != tc.want {
			t.Errorf("infoName(%+v) = %q, want %q", tc.album, got, tc.want)
		}
	}
}

// A record is read back beside itself, so a directory moved as a whole applies.
func TestUnderAndBesideRoundTrip(t *testing.T) {
	dir := filepath.Join("out", "RJ123456")
	for _, name := range []string{"a.mp3", filepath.Join("images", "01.jpg")} {
		path := filepath.Join(dir, name)

		rel := under(dir, path)
		if strings.ContainsRune(rel, '\\') {
			t.Errorf("under(%q) = %q, want forward slashes", path, rel)
		}
		if got := beside(dir, rel); got != path {
			t.Errorf("beside(under(%q)) = %q, want it back", path, got)
		}
	}
	if got := under("out", ""); got != "" {
		t.Errorf("under of nothing = %q, want empty", got)
	}
}

// A run that saves no cover records none, rather than the temporary file it
// fetched to embed.
func TestInfoForRecordsNoCoverWhenNoneWasKept(t *testing.T) {
	dir := t.TempDir()
	p := streamPlan(t, dir)

	r := &run{cfg: Config{EmbedCover: true}}
	info := r.infoFor(p, downloader.Pictures{}, nil)
	if info.Cover != "" {
		t.Errorf("cover = %q, want none recorded", info.Cover)
	}
}

// naming.Fields is what the template reads; the record is what a later run
// writes back. Neither stands in for the other.
func TestInfoTrackTagsRoundTrip(t *testing.T) {
	tags := downloader.Tags{
		Title: "ある夏の日", Artist: "声優", AlbumArtist: "サークル",
		Album: "RJ123456", Date: "2024-05-01", Genre: genre,
		Comment: "URL: https://japaneseasmr.com/12345/",
		Track:   2, TrackTotal: 3, Disc: 1, DiscTotal: 1,
	}
	p := &plan{dir: "out"}
	track := (&run{}).infoTrack(p, tags, downloader.OutputFile{Path: filepath.Join("out", "a.mp3"), Chapter: -1})

	if got := track.tags(); got != tags {
		t.Errorf("tags = %+v, want %+v", got, tags)
	}
	if track.File != "a.mp3" {
		t.Errorf("file = %q, want it relative to the record", track.File)
	}
}
