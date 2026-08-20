package app

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func sampleInfo() Info {
	return Info{
		Version: infoVersion,
		URL:     "https://japaneseasmr.com/12345/",
		Title:   "ある夏の日",
		RJCode:  "RJ123456",
		Circle:  "サークル",
		Artist:  "声優",
		Date:    "2024-05-01",
		Genre:   genre,
		Tags:    []string{"ASMR", "耳かき"},
		Source:  sourceStream,
		Cover:   "cover.jpg",
		Tracks: []InfoTrack{
			{File: "01_はじめに.m4a", Title: "はじめに", Track: 1, TrackTotal: 2},
			{File: "02_耳かき.m4a", Title: "耳かき", Track: 2, TrackTotal: 2},
		},
	}
}

// The post's URL and its tags have no album element of their own, so they go
// where a server will display them, under both names one goes by.
func TestNFOCarriesTheURLAndTags(t *testing.T) {
	a := nfoFor(sampleInfo())

	for _, field := range []struct{ name, value string }{
		{"plot", a.Plot},
		{"review", a.Review},
	} {
		if !strings.Contains(field.value, "https://japaneseasmr.com/12345/") {
			t.Errorf("<%s> = %q, does not carry the post URL", field.name, field.value)
		}
		if !strings.Contains(field.value, "ASMR, 耳かき") {
			t.Errorf("<%s> = %q, does not carry the tag list", field.name, field.value)
		}
	}
}

// Jellyfin and Emby read <tag>, Kodi's music scraper <style>. Writing both
// leaves no server without the post's tags.
func TestNFOWritesTagsUnderBothNames(t *testing.T) {
	a := nfoFor(sampleInfo())

	want := []string{"ASMR", "耳かき"}
	if !slices.Equal(a.Tags, want) {
		t.Errorf("<tag> = %v, want %v", a.Tags, want)
	}
	if !slices.Equal(a.Styles, want) {
		t.Errorf("<style> = %v, want %v", a.Styles, want)
	}
}

// Kodi reads <releasedate>, Jellyfin <premiered>, and both want a bare <year>.
func TestNFOWritesTheDateUnderEveryName(t *testing.T) {
	a := nfoFor(sampleInfo())

	if a.ReleaseDate != "2024-05-01" || a.Premiered != "2024-05-01" {
		t.Errorf("releasedate = %q, premiered = %q, want the post's date in both", a.ReleaseDate, a.Premiered)
	}
	if a.Year != "2024" {
		t.Errorf("year = %q, want 2024", a.Year)
	}
}

func TestNFOCarriesTheWorkAndItsTracks(t *testing.T) {
	a := nfoFor(sampleInfo())

	if a.Title != "ある夏の日 [RJ123456]" {
		t.Errorf("title = %q, want the post's own with its RJ code", a.Title)
	}
	if a.AlbumArtist != "サークル" {
		t.Errorf("albumartist = %q, want the circle", a.AlbumArtist)
	}
	if a.Artist != "声優" || a.ArtistDesc != "声優" {
		t.Errorf("artist = %q, artistdesc = %q, want the voice actor in both", a.Artist, a.ArtistDesc)
	}
	if a.Genre != genre {
		t.Errorf("genre = %q, want %q", a.Genre, genre)
	}
	if a.Thumb != "cover.jpg" {
		t.Errorf("thumb = %q, want the cover beside it", a.Thumb)
	}
	if a.UniqueID == nil || a.UniqueID.Value != "RJ123456" || a.UniqueID.Type != "rjcode" {
		t.Errorf("uniqueid = %+v, want the RJ code", a.UniqueID)
	}

	if len(a.Tracks) != 2 {
		t.Fatalf("%d tracks, want 2", len(a.Tracks))
	}
	for i, track := range a.Tracks {
		if track.Position != i+1 {
			t.Errorf("track %d at position %d, want %d", i, track.Position, i+1)
		}
	}
}

// A post with no RJ code writes no uniqueid at all, rather than an empty one a
// server would key on.
func TestNFOOmitsAnEmptyUniqueID(t *testing.T) {
	info := sampleInfo()
	info.RJCode = ""

	if a := nfoFor(info); a.UniqueID != nil {
		t.Errorf("uniqueid = %+v, want none", a.UniqueID)
	}
}

// A record that names no track numbers still numbers them in order.
func TestNFONumbersUnnumberedTracks(t *testing.T) {
	info := sampleInfo()
	for i := range info.Tracks {
		info.Tracks[i].Track = 0
	}

	a := nfoFor(info)
	for i, track := range a.Tracks {
		if track.Position != i+1 {
			t.Errorf("track %d at position %d, want %d", i, track.Position, i+1)
		}
	}
}

// The file has to open with a declaration and parse as XML, or no server reads
// past the first line.
func TestWriteNFOIsReadableXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), nfoName)
	if err := writeNFO(path, sampleInfo()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), `<?xml version="1.0" encoding="UTF-8"`) {
		t.Errorf("file opens %q, want an XML declaration", strings.SplitN(string(data), "\n", 2)[0])
	}

	var got nfoAlbum
	if err := xml.Unmarshal(data, &got); err != nil {
		t.Fatalf("the file does not parse: %v", err)
	}
	if got.XMLName.Local != "album" {
		t.Errorf("root is <%s>, want <album>", got.XMLName.Local)
	}
	if got.Title != "ある夏の日 [RJ123456]" || len(got.Tracks) != 2 {
		t.Errorf("read back %+v, want the post it was written from", got)
	}
}
