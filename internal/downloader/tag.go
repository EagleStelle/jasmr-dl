package downloader

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
)

// Tags is metadata written to every finished file.
type Tags struct {
	Title   string
	Artist  string
	Album   string
	Comment string
}

func (t Tags) args() []string {
	var out []string
	for _, kv := range [][2]string{
		{"title", t.Title},
		{"artist", t.Artist},
		{"album", t.Album},
		{"comment", t.Comment},
	} {
		if kv[1] != "" {
			out = append(out, "-metadata", kv[0]+"="+kv[1])
		}
	}
	return out
}

// tagging returns the extra ffmpeg inputs for art and chapters, plus the map,
// codec and metadata options that go after them. next is the first free input
// index, the primary audio being input 0.
func (d *Downloader) tagging(next int, muxer, chapMeta string) (inputs, opts []string) {
	opts = []string{"-map", "0:a"}

	if d.CoverPath != "" {
		inputs = append(inputs, "-i", d.CoverPath)
		opts = append(opts, "-map", strconv.Itoa(next)+":v")
		next++
	}
	if chapMeta != "" {
		inputs = append(inputs, "-i", chapMeta)
		opts = append(opts, "-map_metadata", strconv.Itoa(next), "-map_chapters", strconv.Itoa(next))
	}

	opts = append(opts, "-c:a", "copy")
	if d.CoverPath != "" {
		// Players widely ignore non-JPEG cover art.
		opts = append(opts,
			"-c:v", "mjpeg",
			"-disposition:v", "attached_pic",
			"-metadata:s:v", "title=Album cover",
			"-metadata:s:v", "comment=Cover (front)",
		)
	}
	opts = append(opts, d.Tags.args()...)
	if muxer == "mp3" {
		opts = append(opts, "-id3v2_version", "3")
	}
	return inputs, opts
}

// nothingToTag reports whether a file would be rewritten for no reason.
func (d *Downloader) nothingToTag() bool {
	return d.CoverPath == "" && len(d.Chapters) == 0 && d.Tags == (Tags{})
}

// tag attaches art, chapters and metadata to a finished file, rewriting it.
func (d *Downloader) tag(ctx context.Context, audio string) error {
	muxer, ok := coverMuxers[extOf(audio)]
	if !ok || d.nothingToTag() {
		return nil
	}
	tools, err := findFFmpeg()
	if err != nil {
		return err
	}

	var chapMeta string
	if len(d.Chapters) > 0 {
		// Chapter ends need the file's real length, so probe it first.
		total, err := d.probeDuration(ctx, audio)
		if err != nil {
			return err
		}
		chapMeta = audio + ".ffmeta"
		if err := writeChapterMeta(chapMeta, d.Chapters, total); err != nil {
			return err
		}
		defer os.Remove(chapMeta)
	}

	inputs, opts := d.tagging(1, muxer, chapMeta)

	// The suffix hides the extension ffmpeg would infer the format from.
	staged := audio + ".tagged"
	args := slices.Concat(ffmpegQuiet,
		[]string{"-i", audio}, inputs, opts,
		[]string{"-f", muxer, staged})

	out, err := runFFmpeg(ctx, tools.ffmpeg, args...)
	if err != nil {
		os.Remove(staged)
		return fmt.Errorf("tag: %w: %s", err, out)
	}
	return os.Rename(staged, audio)
}
