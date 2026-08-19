package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/EagleStelle/jasmr-dl/internal/util"
)

const (
	// maxImageBytes only stops a mislabelled link filling the disk. A post's
	// work-part art runs to several MiB a picture, so the ceiling sits well
	// clear of anything the gallery legitimately holds.
	maxImageBytes = 64 << 20

	// jacketName is the album art, kept beside the audio it is embedded in.
	jacketName = "jacket"

	// imagesDirName holds the rest of the gallery, which no file carries.
	imagesDirName = "images"

	// imageWorkers bounds the pictures in flight across a run, posts included.
	// They come off a different host from the audio, so the connection ceiling
	// does not cover them; a gallery is a handful of small files, which this is
	// plenty for.
	imageWorkers = 8
)

// Extensions whose muxer can carry art and chapters.
var jacketMuxers = map[string]string{
	".m4a":  "ipod",
	".mp3":  "mp3",
	".flac": "flac",
}

// Pictures is what a post carries beside its audio.
type Pictures struct {
	// Jacket is the art the audio embeds, empty where none came down.
	Jacket string

	// Gallery is everything else, in page order.
	Gallery []string
}

// picture is one image to save: where it comes from and where it lands.
type picture struct {
	url    string
	dir    string
	name   string
	jacket bool
}

// FetchPictures saves the jacket beside the audio and the gallery under
// images/, every picture in flight together up to what the budget allows.
// onProgress reports the pictures finished, the bytes on disk and the total
// those project to. One that will not come down goes to OnPictureError and is
// skipped, since pictures are not what the run is for.
func (d *Downloader) FetchPictures(ctx context.Context, jacketURL string, gallery []string, referer string, onProgress func(done int, bytes, total int64)) Pictures {
	targets := pictureTargets(jacketURL, gallery, d.OutputDir)
	if len(targets) == 0 {
		return Pictures{}
	}

	sizes := &sizeTally{count: int64(len(targets))}
	report := func() {
		if onProgress != nil {
			units, bytes, total := sizes.stat()
			onProgress(int(units), bytes, total)
		}
	}

	report()

	paths := make([]string, len(targets))
	g, gctx := errgroup.WithContext(ctx)
	for i, p := range targets {
		g.Go(func() error {
			if err := d.Budget.pictures.enter(gctx); err != nil {
				return err
			}
			defer d.Budget.pictures.leave()

			// Both are this goroutine's alone, so neither needs a lock.
			var (
				got        int64
				advertised int64
			)
			path, n, err := fetchImage(gctx, d.Client, d.UserAgent, p, referer, func(sz int64) {
				advertised = sz
				sizes.know(sz)
				report()
			}, func(add int64) {
				got += add
				sizes.advance(add)
				report()
			})
			if err != nil {
				// Nothing was written, so those bytes are not on disk.
				if got > 0 {
					sizes.advance(-got)
				}
				sizes.rollback(advertised)
				report()
				// A cancelled run would otherwise report every picture left.
				if gctx.Err() != nil {
					return gctx.Err()
				}
				if d.OnPictureError != nil {
					d.OnPictureError(err)
				}
				return nil
			}
			paths[i] = path
			sizes.finish(advertised, n)
			report()
			return nil
		})
	}
	// A picture failing is not the run's problem; the paths say what landed.
	_ = g.Wait()

	var out Pictures
	for i, p := range targets {
		switch {
		case paths[i] == "":
		case p.jacket:
			out.Jacket = paths[i]
		default:
			out.Gallery = append(out.Gallery, paths[i])
		}
	}
	return out
}

// pictureTargets names every picture a post opens. The jacket leads, so a run
// that saves nothing else still has its art.
func pictureTargets(jacketURL string, gallery []string, dir string) []picture {
	var targets []picture
	if jacketURL != "" {
		targets = append(targets, picture{url: jacketURL, dir: dir, name: jacketName, jacket: true})
	}

	imagesDir := filepath.Join(dir, imagesDirName)
	for i, u := range gallery {
		// Numbered by position, not by how many landed, so one picture
		// failing does not shift the names of the pictures after it.
		targets = append(targets, picture{url: u, dir: imagesDir, name: fmt.Sprintf("%02d", i+1)})
	}
	return targets
}

// fetchImage saves one picture, carrying the extension its bytes call for, and
// returns its path and size. onHeaders reports the advertised size, add reports
// each read as it lands.
func fetchImage(ctx context.Context, client *http.Client, userAgent string, p picture, referer string, onHeaders func(size int64), add func(n int64)) (string, int64, error) {
	req, err := newMediaRequest(ctx, p.url, referer, userAgent)
	if err != nil {
		return "", 0, err
	}
	// Art is an image, not the audio the shared headers ask for.
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp,image/*;q=0.8")
	req.Header.Set("Sec-Fetch-Dest", "image")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, util.BadStatus(p.url, resp)
	}

	if onHeaders != nil && resp.ContentLength > 0 && resp.ContentLength <= maxImageBytes {
		onHeaders(resp.ContentLength)
	}

	// One byte past the cap, so a picture that overruns it is refused rather
	// than written short: a truncated file still sniffs as an image, so nothing
	// downstream would ever notice the missing half.
	var body io.Reader = io.LimitReader(resp.Body, maxImageBytes+1)
	if add != nil {
		body = &countingReader{r: body, add: add}
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return "", 0, err
	}
	if len(data) > maxImageBytes {
		return "", 0, fmt.Errorf("%q is larger than the %d MiB an image may be", p.url, maxImageBytes>>20)
	}
	// This CDN serves WebP from a .jpg URL, declaring image/jpeg.
	ext := imageExt(data)
	if ext == "" {
		return "", 0, fmt.Errorf("%q is not an image this tool recognizes", p.url)
	}

	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return "", 0, err
	}
	path := filepath.Join(p.dir, p.name+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", 0, err
	}
	return path, int64(len(data)), nil
}

// countingReader reports each read as it lands, so a set of pictures can be
// counted while they are still coming down.
type countingReader struct {
	r   io.Reader
	add func(n int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.add(int64(n))
	}
	return n, err
}

func imageExt(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

// probeDuration reads a file's length, needed to close the last chapter.
func (d *Downloader) probeDuration(ctx context.Context, audio string) (time.Duration, error) {
	out, err := d.ffprobe(ctx,
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", audio)
	if err != nil {
		return 0, fmt.Errorf("probe duration: %w: %s", err, out)
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
	if err != nil || secs <= 0 {
		return 0, fmt.Errorf("unreadable duration %q", out)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// hasJacket stops a rerun re-downloading a tagged file, whose length changed.
func (d *Downloader) hasJacket(ctx context.Context, audio string) bool {
	out, err := d.ffprobe(ctx,
		"-select_streams", "v",
		"-show_entries", "stream=codec_type",
		"-of", "csv=p=0", audio)
	if err != nil {
		return false
	}
	return strings.Contains(out, "video")
}

// ffprobe reads one value out of a file. Errors are quiet: only the value the
// caller asked for reaches stdout.
func (d *Downloader) ffprobe(ctx context.Context, args ...string) (string, error) {
	tools, err := findFFmpeg()
	if err != nil {
		return "", err
	}
	return runProbe(ctx, tools.ffprobe, slices.Concat([]string{"-v", "error"}, args)...)
}

// extOf is a lowercase file extension, including the dot.
func extOf(path string) string {
	return strings.ToLower(filepath.Ext(path))
}
