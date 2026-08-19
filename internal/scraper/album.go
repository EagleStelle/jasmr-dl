package scraper

import "time"

// Source says what kind of link a Track carries.
type Source int

const (
	// SourceDirect serves bytes as-is.
	SourceDirect Source = iota
	// SourceHLS is the site's stream, not the work's file. Newest posts only.
	SourceHLS
)

// Album is everything a japaneseasmr.com post page yields.
type Album struct {
	PageURL   string
	Title     string
	JacketURL string
	Artists   string
	Circle    string
	RJCode    string
	Date      string
	Tracks    []Track

	// ImageURLs is the post's gallery in page order, the jacket first.
	ImageURLs []string

	// Chapters is present only where one stream holds the whole work.
	Chapters []Chapter
}

// Track is one downloadable file scraped from a player.
type Track struct {
	// Title is the sanitized URL basename, used only if the host names nothing.
	Title string

	// Name labels the part within the post ("Track 1", "Omake"), if any.
	Name    string
	LinkURL string

	// Other encodings, in page order. Pages advertise ones never uploaded.
	Alternates []string
	Source     Source
}

// Chapter marks where a track begins inside a stream.
type Chapter struct {
	Start time.Duration
	Title string
}

func (a *Album) Source() Source {
	if len(a.Tracks) == 0 {
		return SourceDirect
	}
	return a.Tracks[0].Source
}
