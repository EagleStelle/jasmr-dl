package downloader

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

// Tags is metadata written to one finished file.
type Tags struct {
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	Date        string
	Genre       string
	Comment     string

	Track      int
	TrackTotal int
	Disc       int
	DiscTotal  int
}

func (t Tags) args(muxer string) []string {
	var out []string
	for _, kv := range [][2]string{
		{"title", t.Title},
		{"artist", t.Artist},
		{"album_artist", t.AlbumArtist},
		{"album", t.Album},
		{"date", t.Date},
		{"genre", t.Genre},
		{"comment", t.Comment},
	} {
		if kv[1] != "" {
			out = append(out, "-metadata", kv[0]+"="+kv[1])
		}
	}
	out = append(out, counter(muxer, "track", "TRACKNUMBER", "TRACKTOTAL", t.Track, t.TrackTotal)...)
	return append(out, counter(muxer, "disc", "DISCNUMBER", "DISCTOTAL", t.Disc, t.DiscTotal)...)
}

// counter writes a numbered-of-total tag. FLAC carries the two as separate
// Vorbis comments; the other muxers take one "n/total" value.
func counter(muxer, key, vorbisNum, vorbisTotal string, n, total int) []string {
	if n <= 0 {
		return nil
	}
	if muxer == "flac" {
		out := []string{"-metadata", vorbisNum + "=" + strconv.Itoa(n)}
		if total > 0 {
			out = append(out, "-metadata", vorbisTotal+"="+strconv.Itoa(total))
		}
		return out
	}
	v := strconv.Itoa(n)
	if total > 0 {
		v += "/" + strconv.Itoa(total)
	}
	return []string{"-metadata", key + "=" + v}
}

// tagging returns the extra ffmpeg inputs for art and chapters, plus the map,
// codec and metadata options that go after them. next is the first free input
// index, the primary audio being input 0.
func (d *Downloader) tagging(tags Tags, next int, muxer, chapMeta string) (inputs, opts []string) {
	opts = []string{"-map", "0:a"}

	if d.JacketPath != "" {
		inputs = append(inputs, "-i", d.JacketPath)
		opts = append(opts, "-map", strconv.Itoa(next)+":v")
		next++
	}
	if chapMeta != "" {
		inputs = append(inputs, "-i", chapMeta)
		opts = append(opts, "-map_metadata", strconv.Itoa(next), "-map_chapters", strconv.Itoa(next))
	}

	opts = append(opts, "-c:a", "copy")
	if d.JacketPath != "" {
		// Players widely ignore non-JPEG art. The two metadata values are the
		// names the ID3 attached-picture frame goes by, not ours to rename.
		opts = append(opts,
			"-c:v", "mjpeg",
			"-vf", `pad=max(iw\,ih):max(iw\,ih):(ow-iw)/2:(oh-ih)/2:color=black`,
			"-disposition:v", "attached_pic",
			"-metadata:s:v", "title=Album cover",
			"-metadata:s:v", "comment=Cover (front)",
		)
	}
	opts = append(opts, tags.args(muxer)...)
	if muxer == "mp3" {
		// v2.3 would split the date across TYER and TDAT instead of TDRC.
		opts = append(opts, "-id3v2_version", "4")
	}
	return inputs, opts
}

// nothingToTag reports whether a file would be rewritten for no reason.
func (d *Downloader) nothingToTag(tags Tags, chapters []scraper.Chapter) bool {
	return d.JacketPath == "" && len(chapters) == 0 && tags == (Tags{})
}

// tag attaches art, chapters and metadata to a finished file, rewriting it.
func (d *Downloader) tag(ctx context.Context, tags Tags, chapters []scraper.Chapter, audio string) error {
	muxer, ok := jacketMuxers[extOf(audio)]
	if !ok || d.nothingToTag(tags, chapters) {
		return nil
	}
	tools, err := findFFmpeg()
	if err != nil {
		return err
	}

	var chapMeta string
	if len(chapters) > 0 {
		// Chapter ends need the file's real length, so probe it first.
		total, err := d.probeDuration(ctx, audio)
		if err != nil {
			return err
		}
		d.reportOverrun(chapters, total)
		chapMeta = audio + ".ffmeta"
		if err := writeChapterMeta(chapMeta, chapters, total); err != nil {
			return err
		}
		defer os.Remove(chapMeta)
	}

	inputs, opts := d.tagging(tags, 1, muxer, chapMeta)

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
