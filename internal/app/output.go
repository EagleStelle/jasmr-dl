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

// splitting reports whether the run cuts a stream on its chapters. Only a
// stream is ever cut: a post serving its own file is downloaded whole, so a
// track list beside one is embedded rather than split on.
func (r *run) splitting(album *scraper.Album, chapters []scraper.Chapter) bool {
	if !r.cfg.SplitChapters || len(chapters) == 0 {
		return false
	}
	if album.Source() != scraper.SourceHLS {
		r.debugf("%d chapters not split: the post serves its own file", len(chapters))
		return false
	}
	return true
}

// chaptersFor returns chapters only when one file holds the whole work. What is
// done with them is the run's to say: they are recorded whether or not anything
// embeds or splits on them.
func (r *run) chaptersFor(album *scraper.Album) []scraper.Chapter {
	if len(album.Chapters) == 0 {
		return nil
	}
	if len(album.Tracks) != 1 {
		r.debugf("%d chapters ignored: the work is already split across files", len(album.Chapters))
		return nil
	}
	return album.Chapters
}

// embedTags is what the downloader writes into a file. The full tags are kept
// either way, since the info record carries them whatever is embedded.
func (r *run) embedTags(tags downloader.Tags) downloader.Tags {
	if !r.cfg.EmbedMetadata {
		return downloader.Tags{}
	}
	return tags
}

// tagsFor builds the metadata for one file, n being its place in the post.
func (r *run) tagsFor(album *scraper.Album, track scraper.Track, n int) downloader.Tags {
	return downloader.Tags{
		Title:       titleFor(album, track),
		Artist:      album.Artists,
		AlbumArtist: album.Circle,
		Album:       album.RJCode,
		Date:        album.Date,
		Genre:       genre,
		Comment:     commentFor(album.PageURL, album.Tags),
		Track:       n,
		TrackTotal:  len(album.Tracks),
		Disc:        1,
		DiscTotal:   1,
	}
}

// commentFor packs what no tag of its own carries into the comment field, one
// key per line. The NFO's plot is the same text, for the same reason.
func commentFor(url string, tags []string) string {
	lines := []string{"URL: " + url}
	if len(tags) > 0 {
		lines = append(lines, "Tags: "+strings.Join(tags, ", "))
	}
	return strings.Join(lines, "\n")
}

func titleFor(album *scraper.Album, track scraper.Track) string {
	if len(album.Tracks) < 2 || track.Name == "" {
		return album.Title
	}
	return album.Title + " - " + track.Name
}

// pictureURLs splits a post's pictures into the cover and the gallery behind
// it, dropping whatever the settings turn off. The cover comes down for either
// of the two settings that want it, one keeping it and the other embedding it.
func (r *run) pictureURLs(album *scraper.Album) (cover string, gallery []string) {
	if r.cfg.WriteCover || r.cfg.EmbedCover {
		cover = album.CoverURL
	}
	if !r.cfg.WriteImages {
		return cover, nil
	}

	for _, u := range album.ImageURLs {
		// The cover already sits beside the audio. Dropping it here rather
		// than counting on it being first keeps the numbering the same on a
		// post that opens its gallery with something else.
		if u != album.CoverURL {
			gallery = append(gallery, u)
		}
	}
	return cover, gallery
}
