package app

import (
	"encoding/xml"
	"os"
)

// nfoName is the file Kodi, Jellyfin and Emby all read a music folder's details
// from.
const nfoName = "album.nfo"

// xmlHeader opens the file. standalone="yes" is what every one of these servers
// writes itself, and none of them reads a DTD.
const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// nfoAlbum is an album.nfo. Several values go out twice under the names each
// server knows: <plot> is Jellyfin's and Emby's overview and <review> Kodi's,
// <releasedate> is Kodi's date and <premiered> Jellyfin's, and a post's tags
// are <tag> to Jellyfin and Emby but <style> to Kodi's music scraper. A server
// ignores the elements it does not know, so writing both costs nothing and
// leaves no reader without the value.
type nfoAlbum struct {
	XMLName     xml.Name `xml:"album"`
	Title       string   `xml:"title,omitempty"`
	ArtistDesc  string   `xml:"artistdesc,omitempty"`
	Artist      string   `xml:"artist,omitempty"`
	AlbumArtist string   `xml:"albumartist,omitempty"`
	Genre       string   `xml:"genre,omitempty"`
	Year        string   `xml:"year,omitempty"`
	ReleaseDate string   `xml:"releasedate,omitempty"`
	Premiered   string   `xml:"premiered,omitempty"`

	// Plot and Review carry the post's URL and tag list, which no album element
	// of its own holds, and which a server will at least display.
	Plot   string `xml:"plot,omitempty"`
	Review string `xml:"review,omitempty"`

	Thumb  string   `xml:"thumb,omitempty"`
	Styles []string `xml:"style,omitempty"`
	Tags   []string `xml:"tag,omitempty"`

	UniqueID *nfoID     `xml:"uniqueid,omitempty"`
	Tracks   []nfoTrack `xml:"track,omitempty"`
}

type nfoID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

type nfoTrack struct {
	Position int    `xml:"position"`
	Title    string `xml:"title,omitempty"`
}

// nfoFor turns the record into what a media server reads.
func nfoFor(info Info) nfoAlbum {
	plot := commentFor(info.URL, info.Tags)
	year, _, _ := splitDate(info.Date)

	a := nfoAlbum{
		Title:       info.Title,
		ArtistDesc:  info.Artist,
		Artist:      info.Artist,
		AlbumArtist: info.Circle,
		Genre:       info.Genre,
		Year:        year,
		ReleaseDate: info.Date,
		Premiered:   info.Date,
		Plot:        plot,
		Review:      plot,
		Thumb:       info.Cover,
		Styles:      info.Tags,
		Tags:        info.Tags,
	}
	if info.RJCode != "" {
		a.UniqueID = &nfoID{Type: "rjcode", Default: true, Value: info.RJCode}
	}

	for i, t := range info.Tracks {
		position := t.Track
		if position <= 0 {
			position = i + 1
		}
		a.Tracks = append(a.Tracks, nfoTrack{Position: position, Title: t.Title})
	}
	return a
}

func writeNFO(path string, info Info) error {
	body, err := xml.MarshalIndent(nfoFor(info), "", "  ")
	if err != nil {
		return err
	}

	data := make([]byte, 0, len(xmlHeader)+len(body)+1)
	data = append(data, xmlHeader...)
	data = append(data, body...)
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
