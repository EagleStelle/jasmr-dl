package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const maxCoverBytes = 16 << 20

// Extensions whose muxer can carry art and chapters.
var coverMuxers = map[string]string{
	".m4a":  "ipod",
	".mp3":  "mp3",
	".flac": "flac",
}

// FetchCover saves album art into dir and returns its path.
func FetchCover(ctx context.Context, client *http.Client, userAgent, coverURL, referer, dir string) (string, error) {
	req, err := newMediaRequest(ctx, coverURL, referer, userAgent)
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
		return "", badStatus(coverURL, resp)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes))
	if err != nil {
		return "", err
	}
	// This CDN serves WebP from a .jpg URL, declaring image/jpeg.
	ext := imageExt(data)
	if ext == "" {
		return "", fmt.Errorf("cover %q is not an image this tool recognizes", coverURL)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "cover"+ext)
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
	bin, err := d.ffprobePath()
	if err != nil {
		return "", err
	}
	return runProbe(ctx, bin, slices.Concat([]string{"-v", "error"}, args)...)
}

func (d *Downloader) ffprobePath() (string, error) {
	ffmpeg, err := d.ffmpegPath()
	if err != nil {
		return "", err
	}
	probe := filepath.Join(filepath.Dir(ffmpeg), "ffprobe"+filepath.Ext(ffmpeg))
	if _, err := os.Stat(probe); err != nil {
		return "", errors.New("ffprobe not found beside ffmpeg")
	}
	return probe, nil
}

// extOf is a lowercase file extension, including the dot.
func extOf(path string) string {
	return strings.ToLower(filepath.Ext(path))
}
