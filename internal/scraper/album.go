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

// Downloadable returns the tracks to fetch, excluding the combined file unless
// withCombined is set.
func (a *Album) Downloadable(withCombined bool) []Track {
	out := make([]Track, 0, len(a.Tracks))
	for _, t := range a.Tracks {
		if t.Combined && !withCombined {
			continue
		}
		out = append(out, t)
	}
	return out
}
