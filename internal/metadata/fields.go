// Package metadata holds a post's metadata as it is written: into the audio
// files, into an album.nfo, and into an info.json. A Rule rewrites it on the
// way, which is what --parse-metadata gives a run.
package metadata

import (
	"maps"
	"slices"
	"strings"
)

// Fields is one post's metadata, and the vocabulary both --parse-metadata and
// the output template are written in.
type Fields struct {
	Title  string
	RJCode string
	Circle string
	Artist string
	Date   string
	Genre  string

	// Album stands in for the name derived from the title and the RJ code,
	// where a rule named one of its own.
	Album string
}

// readField is every field a template can read.
var readField = map[string]func(Fields) string{
	"title":  func(f Fields) string { return f.Title },
	"rjcode": func(f Fields) string { return f.RJCode },
	"circle": func(f Fields) string { return f.Circle },
	"artist": func(f Fields) string { return f.Artist },
	"album":  Fields.AlbumName,
	"date":   func(f Fields) string { return f.Date },
	"year":   func(f Fields) string { y, _, _ := SplitDate(f.Date); return y },
	"month":  func(f Fields) string { _, m, _ := SplitDate(f.Date); return m },
	"day":    func(f Fields) string { _, _, d := SplitDate(f.Date); return d },
	"genre":  func(f Fields) string { return f.Genre },
}

// writeField is every field a rule can set: the read set less the date read
// apart, which follows {date} rather than leading it.
var writeField = map[string]func(*Fields, string){
	"title":  func(f *Fields, v string) { f.Title = v },
	"rjcode": func(f *Fields, v string) { f.RJCode = v },
	"circle": func(f *Fields, v string) { f.Circle = v },
	"artist": func(f *Fields, v string) { f.Artist = v },
	"album":  func(f *Fields, v string) { f.Album = v },
	"date":   func(f *Fields, v string) { f.Date = v },
	"genre":  func(f *Fields, v string) { f.Genre = v },
}

// Read is the text of one field, and whether the name is one.
func (f Fields) Read(name string) (string, bool) {
	read, ok := readField[name]
	if !ok {
		return "", false
	}
	return read(f), true
}

// Readable reports whether name is a field a template can read.
func Readable(name string) bool {
	_, ok := readField[name]
	return ok
}

// ReadNames lists the fields a template can read.
func ReadNames() []string {
	return slices.Collect(maps.Keys(readField))
}

// AlbumName is the album every file of the post is written under: the title
// with its DLsite code after it, or whichever half the post carries.
func (f Fields) AlbumName() string {
	switch {
	case f.Album != "":
		return f.Album
	case f.Title == "":
		return f.RJCode
	case f.RJCode == "":
		return f.Title
	}
	return f.Title + " [" + f.RJCode + "]"
}

// SplitDate breaks the scraper's ISO date apart.
func SplitDate(date string) (year, month, day string) {
	parts := strings.Split(date, "-")
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}
