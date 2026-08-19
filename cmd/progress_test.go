package cmd

import (
	"testing"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

// A post can hold a track named after one of the rows opened beside it. Kind
// scopes the key, so the two never drive the same line.
func TestPostLinesKeepsAKindApartFromATrackOfTheSameName(t *testing.T) {
	lines := linesFor(&scraper.Album{PageURL: "https://japaneseasmr.com/12345/", Title: "ある夏の日"})

	for _, name := range []string{jacketName, imagesName, chaptersName} {
		if lines.key(downloader.KindRecording, name) == lines.key(downloader.KindImage, name) {
			t.Errorf("a recording named %q shares the gallery's key", name)
		}
	}
}

// Every row of a post is titled alike, whatever the post names itself by.
func TestProgressLabelFallsBackToWhatThePostHas(t *testing.T) {
	for _, tc := range []struct {
		album scraper.Album
		want  string
	}{
		{scraper.Album{Title: "ある夏の日", RJCode: "RJ123456"}, "ある夏の日"},
		{scraper.Album{RJCode: "RJ123456"}, "RJ123456"},
		{scraper.Album{PageURL: "https://japaneseasmr.com/12345/"}, "https://japaneseasmr.com/12345/"},
	} {
		if got := progressLabel(&tc.album); got != tc.want {
			t.Errorf("progressLabel(%+v) = %q, want %q", tc.album, got, tc.want)
		}
	}
}
