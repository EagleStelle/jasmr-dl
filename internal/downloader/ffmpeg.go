package downloader

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// ffmpegPair is the two binaries remuxing and tagging need.
type ffmpegPair struct {
	ffmpeg  string
	ffprobe string
}

// findFFmpeg locates the pair on PATH, then beside this binary, so a portable
// copy dropped next to jasmr-dl works without a system install.
//
// The two are resolved together because they are used at different stages: an
// ffmpeg with no ffprobe beside it would remux happily and then fail at cover
// art, with the whole download already spent.
var findFFmpeg = sync.OnceValues(func() (ffmpegPair, error) {
	for _, find := range []func() (ffmpegPair, error){pairOnPath, pairBesideBinary} {
		if pair, err := find(); err == nil {
			return pair, nil
		}
	}
	return ffmpegPair{}, errors.New("ffmpeg and ffprobe not found: install them, " +
		"or put both beside jasmr-dl")
})

func pairOnPath() (ffmpegPair, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ffmpegPair{}, err
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return ffmpegPair{}, err
	}
	return ffmpegPair{ffmpeg: ffmpeg, ffprobe: ffprobe}, nil
}

func pairBesideBinary() (ffmpegPair, error) {
	exe, err := os.Executable()
	if err != nil {
		return ffmpegPair{}, err
	}

	// Borrow this binary's own extension: ".exe" on Windows, nothing elsewhere.
	dir, ext := filepath.Dir(exe), filepath.Ext(exe)
	pair := ffmpegPair{
		ffmpeg:  filepath.Join(dir, "ffmpeg"+ext),
		ffprobe: filepath.Join(dir, "ffprobe"+ext),
	}
	for _, bin := range []string{pair.ffmpeg, pair.ffprobe} {
		if _, err := os.Stat(bin); err != nil {
			return ffmpegPair{}, err
		}
	}
	return pair, nil
}
