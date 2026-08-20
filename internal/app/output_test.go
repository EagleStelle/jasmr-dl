package app

import (
	"path/filepath"
	"testing"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/naming"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

func fields(n, total int) naming.Fields {
	return naming.Fields{
		RJCode:  "RJ123456",
		Title:   "ある夏の日",
		Year:    "2024",
		Chapter: "耳かき",
		Ext:     "mp3",
		Number:  n,
		Total:   total,
	}
}

func TestCommentForCarriesTheURLAndTags(t *testing.T) {
	want := "URL: https://japaneseasmr.com/12345/\nTags: 2024, ASMR"
	if got := commentFor("https://japaneseasmr.com/12345/", []string{"2024", "ASMR"}); got != want {
		t.Errorf("comment = %q, want %q", got, want)
	}
}

func TestCommentForOmitsAnEmptyTagLine(t *testing.T) {
	const url = "https://japaneseasmr.com/12345/"
	if got, want := commentFor(url, nil), "URL: "+url; got != want {
		t.Errorf("comment = %q, want %q", got, want)
	}
}

// The post's own metadata is gathered whatever a run embeds: the record carries
// it even where no file is written over.
func TestTagsForIsGatheredWhateverIsEmbedded(t *testing.T) {
	r := &run{cfg: Config{}}
	album := &scraper.Album{
		PageURL: "https://japaneseasmr.com/12345/",
		Title:   "ある夏の日",
		RJCode:  "RJ123456",
		Tags:    []string{"ASMR"},
	}

	tags := r.tagsFor(album, scraper.Track{}, 1)
	if tags.Title != album.Title || tags.Album != "ある夏の日 [RJ123456]" {
		t.Errorf("tags = %+v, want the post's own", tags)
	}
	if got := r.embedTags(tags); got != (downloader.Tags{}) {
		t.Errorf("embedded = %+v, want none without --embed-metadata", got)
	}

	r.cfg.EmbedMetadata = true
	if got := r.embedTags(tags); got != tags {
		t.Errorf("embedded = %+v, want the full %+v", got, tags)
	}
}

// Chapters are read off the page whatever is done with them, so the record can
// carry a track list a run neither embedded nor split on.
func TestChaptersForIgnoresTheEmbedSettings(t *testing.T) {
	r := &run{cfg: Config{Log: func(string, ...any) {}}}
	album := &scraper.Album{
		Tracks:   []scraper.Track{{Source: scraper.SourceHLS}},
		Chapters: []scraper.Chapter{{Title: "はじめに"}, {Title: "耳かき"}},
	}

	if got := r.chaptersFor(album); len(got) != 2 {
		t.Errorf("got %d chapters, want the post's 2", len(got))
	}
	if r.splitting(album, album.Chapters) {
		t.Error("a run split a stream without --split-chapters")
	}

	r.cfg.SplitChapters = true
	if !r.splitting(album, album.Chapters) {
		t.Error("--split-chapters did not split a chaptered stream")
	}
}

// Only a stream is cut up. A post serving its own file is downloaded whole, so
// a track list beside one is left to --embed-chapters rather than dropped
// between a split that never happens and a marker that was never written.
func TestSplittingOnlyCutsAStream(t *testing.T) {
	r := &run{cfg: Config{SplitChapters: true, Log: func(string, ...any) {}}}

	album := &scraper.Album{
		Tracks:   []scraper.Track{{Source: scraper.SourceDirect}},
		Chapters: []scraper.Chapter{{Title: "はじめに"}, {Title: "耳かき"}},
	}
	if r.splitting(album, album.Chapters) {
		t.Error("a post serving its own file was split")
	}

	album.Tracks[0].Source = scraper.SourceHLS
	if !r.splitting(album, album.Chapters) {
		t.Error("a stream was not split")
	}
}

// A work already split across files has no stream to cut, whatever was asked.
func TestChaptersForDropsThemOnAMultiFilePost(t *testing.T) {
	r := &run{cfg: Config{SplitChapters: true, Log: func(string, ...any) {}}}
	album := &scraper.Album{
		Tracks:   []scraper.Track{{}, {}},
		Chapters: []scraper.Chapter{{Title: "はじめに"}},
	}

	chapters := r.chaptersFor(album)
	if chapters != nil {
		t.Errorf("got %d chapters, want none", len(chapters))
	}
	if r.splitting(album, chapters) {
		t.Error("a post already split across files was split again")
	}
}

func TestPictureURLsFollowTheSettings(t *testing.T) {
	album := &scraper.Album{
		CoverURL:  "https://cdn/cover.jpg",
		ImageURLs: []string{"https://cdn/cover.jpg", "https://cdn/1.jpg"},
	}

	for _, tc := range []struct {
		name    string
		cfg     Config
		cover   string
		gallery int
	}{
		{"nothing asked for", Config{}, "", 0},
		{"kept", Config{WriteCover: true}, album.CoverURL, 0},
		{"embedded only", Config{EmbedCover: true}, album.CoverURL, 0},
		{"gallery", Config{WriteImages: true}, "", 1},
	} {
		r := &run{cfg: tc.cfg}
		cover, gallery := r.pictureURLs(album)
		if cover != tc.cover {
			t.Errorf("%s: cover = %q, want %q", tc.name, cover, tc.cover)
		}
		if len(gallery) != tc.gallery {
			t.Errorf("%s: %d gallery pictures, want %d", tc.name, len(gallery), tc.gallery)
		}
	}
}

func TestUnderBasePath(t *testing.T) {
	if got, want := underBasePath("", "2024"), "2024"; got != want {
		t.Errorf("without a base: %q, want %q", got, want)
	}

	base := filepath.Join("out", "audio")
	if got, want := underBasePath(base, "2024"), filepath.Join(base, "2024"); got != want {
		t.Errorf("with a base: %q, want %q", got, want)
	}
}

func TestUnderBasePathLeavesARootedTemplate(t *testing.T) {
	for _, dir := range []string{
		string(filepath.Separator) + filepath.Join("srv", "audio"),
		"C:" + string(filepath.Separator) + "Audio",
		"C:",
	} {
		if got := underBasePath("out", dir); got != dir {
			t.Errorf("underBasePath(%q) = %q, want it untouched", dir, got)
		}
	}
}

func TestTemplateForKeepsBothDefaults(t *testing.T) {
	for _, tc := range []struct {
		split bool
		want  string
	}{
		{false, "RJ123456_2.mp3"},
		{true, "2_耳かき.mp3"},
	} {
		tmpl, err := templateFor("", tc.split, 3)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.File(fields(2, 3)); got != tc.want {
			t.Errorf("split=%v: File = %q, want %q", tc.split, got, tc.want)
		}
	}
}

func TestTemplateForDividesOnTheGroup(t *testing.T) {
	const given = "{year}/<{rjcode}_{number}.{ext}|{number}_{chapter}.{ext}>"

	for _, tc := range []struct {
		split bool
		want  string
	}{
		{false, "RJ123456_2.mp3"},
		{true, "2_耳かき.mp3"},
	} {
		tmpl, err := templateFor(given, tc.split, 3)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.File(fields(2, 3)); got != tc.want {
			t.Errorf("split=%v: File = %q, want %q", tc.split, got, tc.want)
		}
	}
}

func TestTemplateForKeepsAStarBranchDefault(t *testing.T) {
	tmpl, err := templateFor("<*|{number}. {chapter}.{ext}>", false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tmpl.File(fields(2, 3)), "RJ123456_2.mp3"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}

// A -o naming no counter still writes every file of a post to its own name.
func TestTemplateForNumbersWhatTheTemplateDoesNot(t *testing.T) {
	for _, tc := range []struct {
		split bool
		files int
		want  string
	}{
		{false, 1, "ある夏の日.mp3"},
		{false, 3, "ある夏の日_2.mp3"},
		{true, 3, "2_ある夏の日.mp3"},
	} {
		tmpl, err := templateFor("{year}/{title}.{ext}", tc.split, tc.files)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.File(fields(2, tc.files)); got != tc.want {
			t.Errorf("split=%v files=%d: File = %q, want %q", tc.split, tc.files, got, tc.want)
		}
	}
}

func TestTemplateForDropsTheNumberOnASingleFile(t *testing.T) {
	for _, tc := range []struct {
		given string
		want  string
	}{
		{"", "RJ123456.mp3"},
		{"{year}/{title} - {number}.{ext}", "ある夏の日.mp3"},
		{"{year}/{number}. {title}.{ext}", "ある夏の日.mp3"},
		{"{year}/{title} ({number}).{ext}", "ある夏の日.mp3"},
		{"{year}/{number}.{ext}", "1.mp3"},
	} {
		tmpl, err := templateFor(tc.given, false, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.File(fields(1, 1)); got != tc.want {
			t.Errorf("templateFor(%q): File = %q, want %q", tc.given, got, tc.want)
		}
	}
}

func TestTemplateForRejectsABadTemplate(t *testing.T) {
	for _, given := range []string{
		"{nope}.{ext}",
		"{year}/{rjcode}",
		"<{a}.{ext}|{b}.{ext}",
		"<*|*>",
	} {
		for _, split := range []bool{false, true} {
			if _, err := templateFor(given, split, 3); err == nil {
				t.Errorf("templateFor(%q, split=%v) succeeded, want an error", given, split)
			}
		}
	}
}

// A bad template is settled before anything is fetched.
func TestConfigRejectsABadTemplate(t *testing.T) {
	cfg := Config{Targets: []string{"https://japaneseasmr.com/12345/"}, Template: "{nope}.{ext}"}
	if _, err := cfg.prepared(); err == nil {
		t.Error("prepared accepted a bad template, want an error")
	}
}
