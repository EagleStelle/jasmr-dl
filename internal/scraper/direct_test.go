package scraper

import (
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

const postURL = "https://japaneseasmr.com/25385/"

func parse(t *testing.T, html string) (*goquery.Document, *url.URL) {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	base, err := url.Parse(postURL)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	return doc, base
}

// Mirrors an old post: two players, each offering an mp3 and an m4a, repeated
// in the plain audio list below the gallery.
const twoPlayerPage = `
<video title="Track 1" poster="https://pic.example/x.jpg">
  <source src="https://v.example.xyz/RJ282759.mp3" type="audio/mpeg"/>
  <source src="https://v.example.xyz/RJ282759.m4a" type="audio/mp4"/>
</video>
<video title="Track 2" poster="https://pic.example/x.jpg">
  <source src="https://v.example.xyz/RJ282759 2.mp3" type="audio/mpeg"/>
  <source src="https://v.example.xyz/RJ282759 2.m4a" type="audio/mp4"/>
</video>
<audio controls preload="metadata">
  <source src="https://v.example.xyz/RJ282759.mp3" type="audio/mpeg">
  <source src="https://v.example.xyz/RJ282759.m4a" type="audio/mp4">
</audio>
<audio controls preload="metadata">
  <source src="https://v.example.xyz/RJ282759 2.mp3" type="audio/mpeg"/>
  <source src="https://v.example.xyz/RJ282759 2.m4a" type="audio/mp4"/>
</audio>`

func TestDirectTracksDedupesPlayersAndKeepsAlternates(t *testing.T) {
	doc, base := parse(t, twoPlayerPage)
	tracks := directTracks(doc, base)

	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2 (the audio list repeats the gallery)", len(tracks))
	}

	for i, want := range []string{
		"https://v.example.xyz/RJ282759.mp3",
		"https://v.example.xyz/RJ282759%202.mp3",
	} {
		if tracks[i].LinkURL != want {
			t.Errorf("track %d LinkURL = %q, want %q", i, tracks[i].LinkURL, want)
		}
		if tracks[i].Source != SourceDirect {
			t.Errorf("track %d Source = %v, want SourceDirect", i, tracks[i].Source)
		}
		// The m4a is advertised but often absent, so it must survive as a
		// candidate rather than being chosen or dropped.
		if len(tracks[i].Alternates) != 1 {
			t.Errorf("track %d has %d alternates, want 1", i, len(tracks[i].Alternates))
		}
	}

	if tracks[0].Title != "RJ282759.mp3" {
		t.Errorf("title = %q, want the host's own RJ name", tracks[0].Title)
	}
	for i, want := range []string{"Track 1", "Track 2"} {
		if tracks[i].Name != want {
			t.Errorf("track %d Name = %q, want %q", i, tracks[i].Name, want)
		}
	}
}

func TestDirectTracksSinglePlayer(t *testing.T) {
	doc, base := parse(t, `<audio src="https://v.example.xyz/RJ1.mp3"></audio>`)
	tracks := directTracks(doc, base)

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	if tracks[0].Title != "RJ1.mp3" {
		t.Errorf("title = %q", tracks[0].Title)
	}
}

func TestDirectTracksFallsToPlaylistOnly(t *testing.T) {
	doc, base := parse(t, `
		<video title="RJ01632730"><source src="https://v.example.xyz/RJ01632730.m3u8"/></video>
		<audio src="https://v.example.xyz/RJ01632730.m3u8"></audio>`)
	tracks := directTracks(doc, base)

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	if tracks[0].Source != SourceHLS {
		t.Errorf("Source = %v, want SourceHLS", tracks[0].Source)
	}
	// The remux picks the container, so the name carries no extension.
	if tracks[0].Title != "RJ01632730" {
		t.Errorf("title = %q, want the playlist stem", tracks[0].Title)
	}
	// The player's label is the RJ code the file already carries.
	if tracks[0].Name != "" {
		t.Errorf("Name = %q, want none", tracks[0].Name)
	}
}

func TestDirectTracksPrefersFilesOverPlaylist(t *testing.T) {
	doc, base := parse(t, `
		<audio>
		  <source src="https://v.example.xyz/RJ1.m3u8"/>
		  <source src="https://v.example.xyz/RJ1.mp3"/>
		</audio>`)
	tracks := directTracks(doc, base)

	if len(tracks) != 1 || tracks[0].Source != SourceDirect {
		t.Fatalf("a real file must beat the playlist, got %+v", tracks)
	}
	if tracks[0].LinkURL != "https://v.example.xyz/RJ1.mp3" {
		t.Errorf("LinkURL = %q, want the mp3", tracks[0].LinkURL)
	}
}

func TestDirectTracksDropsNonAudio(t *testing.T) {
	// The extension is the only allowlist, so an executable dressed as a
	// player source must not become a job.
	doc, base := parse(t, `
		<video><source src="https://v.example.xyz/setup.exe"/></video>
		<audio src="https://v.example.xyz/notes.pdf"></audio>
		<audio src="javascript:alert(1)"></audio>`)

	if tracks := directTracks(doc, base); len(tracks) != 0 {
		t.Fatalf("got %d tracks, want none: %+v", len(tracks), tracks)
	}
}

func TestDirectTracksResolvesRelativeAndQueriedURLs(t *testing.T) {
	doc, base := parse(t, `<audio src="/media/RJ9.mp3?v=2"></audio>`)
	tracks := directTracks(doc, base)

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1: a query must not hide the extension", len(tracks))
	}
	if want := "https://japaneseasmr.com/media/RJ9.mp3?v=2"; tracks[0].LinkURL != want {
		t.Errorf("LinkURL = %q, want %q", tracks[0].LinkURL, want)
	}
	if tracks[0].Title != "RJ9.mp3" {
		t.Errorf("title = %q, want the path basename without the query", tracks[0].Title)
	}
}
