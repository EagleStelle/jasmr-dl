package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

// applyInfo writes a record's metadata, chapters and art into the files it
// names, fetching nothing. The files are read relative to the record, so a
// directory that was moved as a whole still applies.
func (r *run) applyInfo(ctx context.Context) (Summary, error) {
	path := r.cfg.LoadInfoJSON
	info, err := readInfoJSON(path)
	if err != nil {
		return Summary{}, err
	}
	dir := filepath.Dir(path)
	r.infof("[info] applying %s", filepath.Base(path))

	d := &downloader.Downloader{CoverPath: r.coverFrom(info, dir)}
	if !r.cfg.EmbedMetadata && !r.cfg.EmbedChapters && d.CoverPath == "" {
		return Summary{}, errors.New("nothing to write: name what to embed")
	}

	results := make([]downloader.Result, 0, len(info.Tracks))
	for _, t := range info.Tracks {
		res := downloader.Result{Job: downloader.Job{Name: t.File}}
		file := beside(dir, t.File)
		if err := r.applyTrack(ctx, d, info, t, file); err != nil {
			res.Err = err
		} else {
			res.Files = []downloader.OutputFile{{Path: file, Chapter: -1}}
		}
		results = append(results, res)
	}
	return r.summarizeApply(info, dir, results)
}

// coverFrom is the art to embed, which is the post's own cover where the run
// asked for it and it is still beside the record.
func (r *run) coverFrom(info Info, dir string) string {
	if !r.cfg.EmbedCover || info.Cover == "" {
		return ""
	}
	cover := beside(dir, info.Cover)
	if !exists(cover) {
		r.warnf("[warn] %s: not on disk, no art embedded", info.Cover)
		return ""
	}
	return cover
}

// applyTrack rewrites one file.
func (r *run) applyTrack(ctx context.Context, d *downloader.Downloader, info Info, t InfoTrack, file string) error {
	if !exists(file) {
		return errors.New("not on disk")
	}

	var (
		tags     downloader.Tags
		chapters []scraper.Chapter
	)
	if r.cfg.EmbedMetadata {
		tags = r.trackTags(info, t)
	}
	if r.cfg.EmbedChapters {
		chapters = chaptersFrom(t.Chapters)
	}
	if tags == (downloader.Tags{}) && len(chapters) == 0 && d.CoverPath == "" {
		return errors.New("the record holds nothing for it")
	}

	r.debugf("applying to %s", file)
	return d.Retag(ctx, tags, chapters, file)
}

// trackTags is what goes into one file: the metadata the record holds, with
// every rule applied over it. A record is already written, so the rules run
// per track here rather than per post; the fields they read are the same ones.
func (r *run) trackTags(info Info, t InfoTrack) downloader.Tags {
	tags := t.tags()
	if len(r.rules) == 0 {
		return tags
	}

	return withMeta(tags, r.rewrite(trackMeta(info, t)))
}

// summarizeApply reports what was written, failing only where nothing was.
func (r *run) summarizeApply(info Info, dir string, results []downloader.Result) (Summary, error) {
	s := Summary{
		Posts:   1,
		Results: []PostResult{{Label: infoLabel(info), Dir: dir, Results: results}},
	}
	for _, res := range results {
		if res.Err != nil {
			r.warnf("[error] %s: %v", res.Job.Name, res.Err)
			continue
		}
		s.Files++
	}

	if s.Files == 0 {
		s.FailedPosts = 1
		return s, fmt.Errorf("none of the %s could be written", plural(len(results), "file"))
	}
	r.infof("[done] %s written in %s", plural(s.Files, "file"), dir)
	return s, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
