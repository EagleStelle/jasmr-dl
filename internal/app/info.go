package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/metadata"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
	"github.com/EagleStelle/jasmr-dl/internal/util"
)

const (
	// infoSuffix names the record, after the work so several posts can share a
	// directory without overwriting one another.
	infoSuffix = ".info.json"

	// infoVersion is the record's shape, so a later reader knows what it holds.
	infoVersion = 1

	// sourceFile and sourceStream are the two shapes a post takes.
	sourceFile   = "file"
	sourceStream = "stream"
)

// Info is a post as written to disk. It carries the whole of what the page
// said, not only what a run embedded, which is what lets a later run write it
// into the files without reading the page again.
type Info struct {
	Version int    `json:"info_version"`
	URL     string `json:"url"`

	Title  string `json:"title,omitempty"`
	RJCode string `json:"rjcode,omitempty"`
	Circle string `json:"circle,omitempty"`
	Artist string `json:"artist,omitempty"`
	Date   string `json:"date,omitempty"`
	Genre  string `json:"genre,omitempty"`

	// Album is derived from the title and the RJ code, recorded because a rule
	// may have named something else. A record carrying none is read back with
	// the derived name.
	Album string `json:"album,omitempty"`

	// Tags is the post's tag list in page order.
	Tags []string `json:"tags,omitempty"`

	// Source is "file" where the post serves its own files, "stream" where it
	// serves the site's.
	Source string `json:"source,omitempty"`

	// Cover and Images are relative to the directory the record sits in, with
	// forward slashes, so either platform reads them back.
	Cover  string   `json:"cover,omitempty"`
	Images []string `json:"images,omitempty"`

	// Chapters is the track list the post carried, whether or not the run
	// embedded or split on it.
	Chapters []InfoChapter `json:"chapters,omitempty"`

	Tracks []InfoTrack `json:"tracks"`
}

// InfoChapter marks where a chapter begins. The end is whatever the next
// chapter or the file's own length says, so nothing here can contradict it.
type InfoChapter struct {
	Start float64 `json:"start_time"`
	Title string  `json:"title,omitempty"`
}

// InfoTrack is one file the post came to, and the metadata for it.
type InfoTrack struct {
	// File is relative to the directory the record sits in.
	File string `json:"file"`

	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Date        string `json:"date,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Comment     string `json:"comment,omitempty"`

	Track      int `json:"track,omitempty"`
	TrackTotal int `json:"track_total,omitempty"`
	Disc       int `json:"disc,omitempty"`
	DiscTotal  int `json:"disc_total,omitempty"`

	// Chapters is set only where this one file holds the whole work. A file cut
	// out of a stream is one chapter already.
	Chapters []InfoChapter `json:"chapters,omitempty"`
}

// infoFor gathers the post and what it came to on disk. results is in job
// order, which is the order p.tags is in.
func (r *run) infoFor(p *plan, pics downloader.Pictures, results []downloader.Result) Info {
	info := Info{
		Version:  infoVersion,
		URL:      p.album.PageURL,
		Title:    p.meta.Title,
		RJCode:   p.meta.RJCode,
		Circle:   p.meta.Circle,
		Artist:   p.meta.Artist,
		Date:     p.meta.Date,
		Genre:    p.meta.Genre,
		Album:    p.meta.AlbumName(),
		Tags:     p.album.Tags,
		Source:   sourceName(p.album.Source()),
		Cover:    under(p.dir, pics.Cover),
		Chapters: infoChapters(p.chapters),
	}
	for _, path := range pics.Gallery {
		info.Images = append(info.Images, under(p.dir, path))
	}

	for i, res := range results {
		if res.Err != nil || i >= len(p.tags) {
			continue
		}
		for _, f := range res.Files {
			info.Tracks = append(info.Tracks, r.infoTrack(p, p.tags[i], f))
		}
	}
	return info
}

// infoTrack describes one file. A piece cut out of a stream is titled for its
// chapter and carries none of its own; a whole file carries the post's.
func (r *run) infoTrack(p *plan, tags downloader.Tags, f downloader.OutputFile) InfoTrack {
	chapters := p.chapters
	if f.Chapter >= 0 && f.Chapter < len(p.chapters) {
		c := p.chapters[f.Chapter]
		tags = downloader.PieceTags(tags, c.Title, f.Chapter+1, len(p.chapters))
		chapters = nil
	}

	return InfoTrack{
		File:        under(p.dir, f.Path),
		Title:       tags.Title,
		Artist:      tags.Artist,
		AlbumArtist: tags.AlbumArtist,
		Album:       tags.Album,
		Date:        tags.Date,
		Genre:       tags.Genre,
		Comment:     tags.Comment,
		Track:       tags.Track,
		TrackTotal:  tags.TrackTotal,
		Disc:        tags.Disc,
		DiscTotal:   tags.DiscTotal,
		Chapters:    infoChapters(chapters),
	}
}

// meta reads a record back as the post metadata it was written from.
func (i Info) meta() metadata.Fields {
	return metadata.Fields{
		Title:  i.Title,
		RJCode: i.RJCode,
		Circle: i.Circle,
		Artist: i.Artist,
		Date:   i.Date,
		Genre:  i.Genre,
		Album:  i.Album,
	}
}

// trackMeta reads one track back as metadata, the post's RJ code behind it, so
// a rule sees the fields it would see on a download.
func trackMeta(info Info, t InfoTrack) metadata.Fields {
	f := info.meta()
	f.Title, f.Artist, f.Circle = t.Title, t.Artist, t.AlbumArtist
	f.Album, f.Date, f.Genre = t.Album, t.Date, t.Genre
	return f
}

// tags is the metadata to write into the file this track names.
func (t InfoTrack) tags() downloader.Tags {
	return downloader.Tags{
		Title:       t.Title,
		Artist:      t.Artist,
		AlbumArtist: t.AlbumArtist,
		Album:       t.Album,
		Date:        t.Date,
		Genre:       t.Genre,
		Comment:     t.Comment,
		Track:       t.Track,
		TrackTotal:  t.TrackTotal,
		Disc:        t.Disc,
		DiscTotal:   t.DiscTotal,
	}
}

func infoChapters(chapters []scraper.Chapter) []InfoChapter {
	if len(chapters) == 0 {
		return nil
	}
	out := make([]InfoChapter, len(chapters))
	for i, c := range chapters {
		out[i] = InfoChapter{Start: c.Start.Seconds(), Title: c.Title}
	}
	return out
}

func chaptersFrom(list []InfoChapter) []scraper.Chapter {
	if len(list) == 0 {
		return nil
	}
	out := make([]scraper.Chapter, len(list))
	for i, c := range list {
		out[i] = scraper.Chapter{
			Start: time.Duration(c.Start * float64(time.Second)),
			Title: c.Title,
		}
	}
	return out
}

// infoName names the record after the work.
func infoName(album *scraper.Album) string {
	switch {
	case album.RJCode != "":
		return util.Sanitize(album.RJCode) + infoSuffix
	case album.Title != "":
		return util.Sanitize(album.Title) + infoSuffix
	}
	return "post" + infoSuffix
}

// infoLabel names the post a record came from, for the lines a run prints.
func infoLabel(info Info) string {
	switch {
	case info.Title != "":
		return info.Title
	case info.RJCode != "":
		return info.RJCode
	}
	return info.URL
}

func sourceName(s scraper.Source) string {
	if s == scraper.SourceHLS {
		return sourceStream
	}
	return sourceFile
}

// under names a file relative to the directory the record sits in, with the one
// separator both platforms read back.
func under(dir, path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

// beside resolves what under wrote, against the directory holding the record.
func beside(dir, name string) string {
	return filepath.Join(dir, filepath.FromSlash(name))
}

func writeInfoJSON(path string, info Info) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func readInfoJSON(path string) (Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}

	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if info.Version > infoVersion {
		return Info{}, fmt.Errorf("%s was written by a newer version of jasmr-dl", filepath.Base(path))
	}
	if len(info.Tracks) == 0 {
		return Info{}, fmt.Errorf("%s names no file", filepath.Base(path))
	}
	return info, nil
}
