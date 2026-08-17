package scraper

// Album is everything the scraper can learn from a japaneseasmr.com post page
// plus its dlc.php listing.
type Album struct {
	PageURL  string
	Title    string
	RJCode   string
	CoverURL string
	Tracks   []Track
}

// Track is one downloadable file. LinkURL is the file-host landing link taken
// straight from dlc.php; it is not a file URL and must go through the
// downloader's hop-chain resolution before any bytes can be read.
type Track struct {
	// Index is the 1-based track number parsed from the "NN_" filename
	// prefix. It is 0 for the combined whole-work file.
	Index int

	// Title is the anchor text from dlc.php, already sanitized for use as a
	// filename. The authoritative name comes from Content-Disposition at
	// download time; this is the fallback.
	Title string

	LinkURL string

	// Combined marks the single file containing the entire work. dlc.php
	// lists it alongside the individual tracks, so downloading everything
	// blindly transfers the same audio twice.
	Combined bool
}

// Mode selects which of a listing's files to download.
type Mode int

const (
	// ModeCombined takes only the single whole-work file.
	ModeCombined Mode = 0
	// ModeSplit takes only the individual tracks.
	ModeSplit Mode = 1
	// ModeAll takes both, which downloads the same audio twice.
	ModeAll Mode = 2
)

// Valid reports whether m is a known mode.
func (m Mode) Valid() bool { return m >= ModeCombined && m <= ModeAll }

func (m Mode) String() string {
	switch m {
	case ModeCombined:
		return "combined"
	case ModeSplit:
		return "split"
	case ModeAll:
		return "all"
	default:
		return "invalid"
	}
}

// Select returns the tracks matching m, in listing order.
func (a *Album) Select(m Mode) []Track {
	out := make([]Track, 0, len(a.Tracks))
	for _, t := range a.Tracks {
		switch m {
		case ModeCombined:
			if !t.Combined {
				continue
			}
		case ModeSplit:
			if t.Combined {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// HasCombined reports whether the listing includes a whole-work file. Not every
// album does, so ModeCombined can legitimately select nothing.
func (a *Album) HasCombined() bool {
	for _, t := range a.Tracks {
		if t.Combined {
			return true
		}
	}
	return false
}
