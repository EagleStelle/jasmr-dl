package scraper

import (
	"net/url"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"jasmr-dl/internal/util"
)

const playlistExt = ".m3u8"

// directTracks reads the players' files in page order. Real files beat HLS.
func directTracks(doc *goquery.Document, base *url.URL) []Track {
	var (
		files     []Track
		playlists []Track
		seen      = map[string]bool{}
	)

	// Each player appears twice, gallery and audio list, with identical sources.
	doc.Find("video, audio").Each(func(_ int, sel *goquery.Selection) {
		audio, playlist := mediaCandidates(sel, base)

		if len(audio) > 0 {
			if seen[audio[0]] {
				return
			}
			seen[audio[0]] = true
			files = append(files, Track{
				Title:      util.Sanitize(util.URLBase(audio[0])),
				LinkURL:    audio[0],
				Alternates: audio[1:],
				Source:     SourceDirect,
			})
			return
		}

		if len(playlist) > 0 && !seen[playlist[0]] {
			seen[playlist[0]] = true
			playlists = append(playlists, Track{
				// The remux picks the container.
				Title:   util.Sanitize(strings.TrimSuffix(util.URLBase(playlist[0]), playlistExt)),
				LinkURL: playlist[0],
				Source:  SourceHLS,
			})
		}
	})

	if len(files) > 0 {
		return files
	}
	return playlists
}

// mediaCandidates splits a player's sources into files and playlists.
func mediaCandidates(sel *goquery.Selection, base *url.URL) (audio, playlists []string) {
	add := func(raw string) {
		abs := util.ResolveURL(base, raw)
		if abs == "" {
			return
		}

		name := util.URLBase(abs)
		target := &audio
		switch {
		case util.IsAudioFile(name):
		case strings.EqualFold(path.Ext(name), playlistExt):
			target = &playlists
		default:
			return
		}

		for _, have := range *target {
			if have == abs {
				return
			}
		}
		*target = append(*target, abs)
	}

	if v, ok := sel.Attr("src"); ok {
		add(v)
	}
	sel.Find("source[src]").Each(func(_ int, s *goquery.Selection) {
		v, _ := s.Attr("src")
		add(v)
	})

	return audio, playlists
}
