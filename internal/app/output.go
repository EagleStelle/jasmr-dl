package app

import (
	"path/filepath"
	"strings"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/naming"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

// templateShapes is every branch templateFor can pick. A split post holds one
// track, which is why no shape pairs a split with more.
var templateShapes = []struct {
	split bool
	files int
}{
	{split: false, files: 1}, // the whole post in one file, no counter
	{split: false, files: 2}, // a file per track, trailing counter
	{split: true, files: 1},  // a file per chapter, leading counter
}

// underBasePath puts a relative template under base. A template that names its
// own root already says where it goes.
func underBasePath(base, dir string) string {
	if base == "" || rooted(dir) {
		return dir
	}
	return filepath.Join(base, dir)
}

// rooted reports whether dir says for itself where it starts. A leading
// separator counts: Windows calls that drive-relative rather than absolute, but
// it is still not somewhere a base path should move.
func rooted(dir string) bool {
	return filepath.IsAbs(dir) ||
		strings.HasPrefix(dir, "/") ||
		strings.HasPrefix(dir, `\`) ||
		hasDriveLetter(dir)
}

func hasDriveLetter(dir string) bool {
	if len(dir) < 2 || dir[1] != ':' {
		return false
	}
	c := dir[0]
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// templateFor resolves the template a run writes from. files is how many names
// it has to keep apart.
func templateFor(given string, split bool, files int) (*naming.Template, error) {
	raw := naming.Default
	if given != "" {
		raw = given
	}

	chosen, err := naming.Select(raw, split)
	if err != nil {
		return nil, err
	}

	// A file per chapter always leads with its number, a file per track only
	// carries one where the post holds more than one, and a template that
	// places {number} itself is left to say where.
	switch {
	case split:
		chosen = naming.WithNumber(chosen, true)
	case files > 1:
		chosen = naming.WithNumber(chosen, false)
	default:
		chosen = naming.WithoutNumber(chosen)
	}
	return naming.Parse(chosen)
}

// fieldsFor is the album half of the output template.
func fieldsFor(album *scraper.Album) naming.Fields {
	year, month, day := splitDate(album.Date)
	return naming.Fields{
		Title:  album.Title,
		RJCode: album.RJCode,
		Circle: album.Circle,
		Artist: album.Artists,
		Date:   album.Date,
		Year:   year,
		Month:  month,
		Day:    day,
	}
}

// splitDate breaks the scraper's ISO date apart.
func splitDate(date string) (year, month, day string) {
	parts := strings.Split(date, "-")
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

// splitting reports whether the run cuts a stream on its chapters.
func (r *run) splitting(chapters []scraper.Chapter) bool {
	return !r.cfg.NoSplit && len(chapters) > 0
}

// chaptersFor returns chapters only when one file holds the whole work.
func (r *run) chaptersFor(album *scraper.Album) []scraper.Chapter {
	if r.cfg.NoChapters || len(album.Chapters) == 0 {
		return nil
	}
	if len(album.Tracks) != 1 {
		r.debugf("%d chapters ignored: the work is already split across files", len(album.Chapters))
		return nil
	}
	return album.Chapters
}

// tagsFor builds the metadata written into one file, n being its place in the post.
func (r *run) tagsFor(album *scraper.Album, track scraper.Track, n int) downloader.Tags {
	if r.cfg.NoTags {
		return downloader.Tags{}
	}
	return downloader.Tags{
		Title:       titleFor(album, track),
		Artist:      album.Artists,
		AlbumArtist: album.Circle,
		Album:       album.RJCode,
		Date:        album.Date,
		Genre:       genre,
		Comment:     album.PageURL,
		Track:       n,
		TrackTotal:  len(album.Tracks),
		Disc:        1,
		DiscTotal:   1,
	}
}

func titleFor(album *scraper.Album, track scraper.Track) string {
	if len(album.Tracks) < 2 || track.Name == "" {
		return album.Title
	}
	return album.Title + " - " + track.Name
}

// pictureURLs splits a post's pictures into the jacket the audio embeds and the
// gallery behind it, dropping whatever the settings turn off.
func (r *run) pictureURLs(album *scraper.Album) (jacket string, gallery []string) {
	if !r.cfg.NoJacket {
		jacket = album.JacketURL
	}
	if r.cfg.NoImages {
		return jacket, nil
	}

	for _, u := range album.ImageURLs {
		// The jacket already sits beside the audio. Dropping it here rather
		// than counting on it being first keeps the numbering the same on a
		// post that opens its gallery with something else.
		if u != album.JacketURL {
			gallery = append(gallery, u)
		}
	}
	return jacket, gallery
}
