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

	"jasmr-dl/internal/util"
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
)

// Extensions whose muxer can carry art and chapters.
var coverMuxers = map[string]string{
	".m4a":  "ipod",
	".mp3":  "mp3",
	".flac": "flac",
}

// FetchCover saves album art into dir as the jacket and returns its path.
func FetchCover(ctx context.Context, client *http.Client, userAgent, coverURL, referer, dir string) (string, error) {
	return fetchImage(ctx, client, userAgent, coverURL, referer, dir, jacketName)
}

// FetchImages saves urls into an images subfolder of dir, numbered in page
// order, and returns the paths written. A picture that will not come down goes
// to onError and is skipped, since the gallery is not what the run is for.
func FetchImages(ctx context.Context, client *http.Client, userAgent string, urls []string, referer, dir string, onError func(err error)) []string {
	dir = filepath.Join(dir, imagesDirName)

	var paths []string
	for i, u := range urls {
		// Numbered by position, not by how many landed, so one picture
		// failing does not shift the names of the pictures after it.
		path, err := fetchImage(ctx, client, userAgent, u, referer, dir, fmt.Sprintf("%02d", i+1))
		if err != nil {
			if onError != nil {
				onError(err)
			}
			// A cancelled run would otherwise report every picture left.
			if ctx.Err() != nil {
				break
			}
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// fetchImage saves one picture into dir under name, carrying the extension its
// bytes call for.
func fetchImage(ctx context.Context, client *http.Client, userAgent, imageURL, referer, dir, name string) (string, error) {
	req, err := newMediaRequest(ctx, imageURL, referer, userAgent)
	if err != nil {
		return "", err
	}
	// Art is an image, not the audio the shared headers ask for.
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp,image/*;q=0.8")
	req.Header.Set("Sec-Fetch-Dest", "image")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", util.BadStatus(imageURL, resp)
	}

	// One byte past the cap, so a picture that overruns it is refused rather
	// than written short: a truncated file still sniffs as an image, so nothing
	// downstream would ever notice the missing half.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxImageBytes {
		return "", fmt.Errorf("%q is larger than the %d MiB an image may be", imageURL, maxImageBytes>>20)
	}
	// This CDN serves WebP from a .jpg URL, declaring image/jpeg.
	ext := imageExt(data)
	if ext == "" {
		return "", fmt.Errorf("%q is not an image this tool recognizes", imageURL)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
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

// hasCover stops a rerun re-downloading a tagged file, whose length changed.
func (d *Downloader) hasCover(ctx context.Context, audio string) bool {
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
