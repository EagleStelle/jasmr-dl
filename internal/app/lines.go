package app

import (
	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

// postLines names one post's progress rows, all titled alike and told apart by
// a key the post's address and the row's kind scope.
type postLines struct {
	scope string
	label string
}

func linesFor(album *scraper.Album) postLines {
	return postLines{scope: album.PageURL, label: progressLabel(album)}
}

// key scopes a row name by kind, so a track titled "images" opens a line of its
// own rather than driving the gallery's.
func (p postLines) key(kind, name string) string {
	return p.scope + "\x00" + kind + "\x00" + name
}

func (p postLines) line(kind, name string) downloader.Line {
	return downloader.Line{Key: p.key(kind, name), Kind: kind, Label: p.label}
}

func progressLabel(album *scraper.Album) string {
	switch {
	case album.Title != "":
		return album.Title
	case album.RJCode != "":
		return album.RJCode
	}
	return album.PageURL
}
