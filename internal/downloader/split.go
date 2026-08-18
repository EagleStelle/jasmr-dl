package downloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/EagleStelle/jasmr-dl/internal/naming"
)

// assemblingName holds the whole stream while it is being cut up. It is kept on
// failure so a rerun resumes rather than fetching the work again.
const assemblingName = ".assembling" + hlsOutputExt

// splitStream assembles the stream, then writes one file per chapter.
func (d *Downloader) splitStream(ctx context.Context, job Job, tools ffmpegPair) ([]string, error) {
	pieces := d.piecePaths(job.Fields)
	if done, ok := finishedPieces(pieces); ok {
		return done, nil
	}

	full := filepath.Join(d.OutputDir, assemblingName)
	if fileSize(full) == 0 {
		label := job.Fields.Title
		if label == "" {
			label = job.Name
		}
		if err := d.buildStream(ctx, job, tools, full, label); err != nil {
			return nil, err
		}
	}

	// The nominal EXTINF sum overshoots, so the last chapter needs the real one.
	total, err := d.probeDuration(ctx, full)
	if err != nil {
		return nil, err
	}

	var written []string
	for i, path := range pieces {
		start := d.Chapters[i].Start
		end := total
		if i+1 < len(d.Chapters) {
			end = d.Chapters[i+1].Start
		}
		if end <= start {
			continue
		}
		if err := d.cut(ctx, tools.ffmpeg, full, path, start, end-start); err != nil {
			return nil, err
		}

		tags := pieceTags(job.Tags, d.Chapters[i].Title, i+1, len(d.Chapters))
		if err := d.tag(ctx, tags, nil, path); err != nil && d.OnCoverError != nil {
			d.OnCoverError(filepath.Base(path), err)
		}
		written = append(written, path)
	}
	if len(written) == 0 {
		return nil, permanent(fmt.Errorf("no chapter of %q has any length", full))
	}

	os.Remove(full)
	return written, nil
}

// cut copies one chapter out of src. -ss seeks the input by keyframe, which for
// AAC lands within a frame; -t bounds the output, so it is not read as an
// absolute time on the seeked stream.
func (d *Downloader) cut(ctx context.Context, ffmpeg, src, dst string, start, length time.Duration) error {
	staged := dst + ".part"
	out, err := runFFmpeg(ctx, ffmpeg, slices.Concat(ffmpegQuiet, []string{
		"-ss", seconds(start), "-i", src, "-t", seconds(length),
		"-c", "copy", "-movflags", "+faststart",
		"-f", hlsOutputFormat, staged,
	})...)
	if err != nil {
		os.Remove(staged)
		return fmt.Errorf("split: %w: %s", err, out)
	}
	return os.Rename(staged, dst)
}

// piecePaths names one file per chapter.
func (d *Downloader) piecePaths(f naming.Fields) []string {
	f.Ext = strings.TrimPrefix(hlsOutputExt, ".")
	f.Width = naming.Width(len(d.Chapters))
	f.TrackTotal = len(d.Chapters)

	paths := make([]string, len(d.Chapters))
	for i, c := range d.Chapters {
		g := f
		g.Number, g.Track = i+1, i+1
		g.Chapter = c.Title
		paths[i] = filepath.Join(d.OutputDir, streamName(d.Template.File(g)))
	}
	return paths
}

// pieceTags titles one chapter and numbers it within the work.
func pieceTags(base Tags, title string, n, total int) Tags {
	if base == (Tags{}) {
		return base
	}
	base.Title = title
	base.Track, base.TrackTotal = n, total
	return base
}

// finishedPieces reports the pieces already on disk, and whether a rerun can
// stop there. A gap means the last run did not get through them all.
func finishedPieces(pieces []string) ([]string, bool) {
	var done []string
	for _, path := range pieces {
		if fileSize(path) > 0 {
			done = append(done, path)
		}
	}
	return done, len(done) == len(pieces)
}

func seconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}
