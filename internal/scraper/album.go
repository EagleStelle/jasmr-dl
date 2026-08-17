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

func (s Source) String() string {
	switch s {
	case SourceDirect:
		return "direct"
	case SourceHLS:
		return "hls"
	default:
		return "unknown"
	}
}

// Album is everything a japaneseasmr.com post page yields.
type Album struct {
	PageURL  string
	Title    string
	RJCode   string
	CoverURL string
	Artists  string
	Tracks   []Track

	// Chapters is present only where one stream holds the whole work.
	Chapters []Chapter
}

// Track is one downloadable file scraped from a player.
type Track struct {
	// Title is the sanitized URL basename, used only if the host names nothing.
	Title string

	LinkURL string

	// Other encodings, in page order. Pages advertise ones never uploaded.
	Alternates []string

	Source Source
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
