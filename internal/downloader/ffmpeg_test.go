package downloader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPairOnPath(t *testing.T) {
	pair, err := pairOnPath()
	if err != nil {
		t.Skip("ffmpeg not installed:", err)
	}
	if pair.ffmpeg == "" || pair.ffprobe == "" {
		t.Errorf("got %+v, want both filled", pair)
	}
}

// TestPairBesideBinary plants the pair next to the test binary, which is what
// os.Executable reports during a test run.
func TestPairBesideBinary(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, ext := filepath.Dir(exe), filepath.Ext(exe)

	want := ffmpegPair{
		ffmpeg:  filepath.Join(dir, "ffmpeg"+ext),
		ffprobe: filepath.Join(dir, "ffprobe"+ext),
	}

	// ffmpeg alone must not satisfy the search, or the run fails later at jacket
	// art with the download already spent.
	plant(t, want.ffmpeg)
	if _, err := pairBesideBinary(); err == nil {
		t.Error("ffmpeg without ffprobe was accepted")
	}

	plant(t, want.ffprobe)
	got, err := pairBesideBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func plant(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
}
