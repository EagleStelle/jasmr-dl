package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// pictureFetcher is a Downloader with nothing set but what saving a post's
// pictures needs, carrying a real budget so the ceiling is the one a run holds.
func pictureFetcher(ts *httptest.Server, dir string, onError func(error)) *Downloader {
	return &Downloader{
		Client:         ts.Client(),
		UserAgent:      "test",
		OutputDir:      dir,
		Budget:         NewBudget(1, 1),
		OnPictureError: onError,
	}
}

// pngBody is a PNG signature with filler behind it: enough for the sniffer,
// which reads no further than the first bytes.
func pngBody(n int) []byte {
	body := make([]byte, n)
	copy(body, "\x89PNG\r\n\x1a\n")
	return body
}

// The bytes on disk must be the bytes on the wire. This CDN serves WebP from
// .jpg URLs, so the extension comes from the content, but nothing re-encodes
// the picture on the way past.
func TestFetchImageStoresTheSourceBytes(t *testing.T) {
	body := pngBody(4096)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Declared type is a lie the sniffer has to see through.
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(body)
	}))
	defer ts.Close()

	dir := t.TempDir()
	p := picture{url: ts.URL + "/a.jpg", dir: dir, name: "01"}
	path, n, err := fetchImage(context.Background(), ts.Client(), "test", p, ts.URL, nil, nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := filepath.Base(path); got != "01.png" {
		t.Errorf("saved as %q, want 01.png from the sniffed content", got)
	}
	if want := int64(len(body)); n != want {
		t.Errorf("reported %d bytes, want %d", n, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("stored %d bytes, want the %d served", len(got), len(body))
	}
}

// A picture past the ceiling must be refused. Writing it short would pass every
// check after it: a truncated file still sniffs as an image.
func TestFetchImageRefusesOversizeRatherThanTruncating(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBody(1024))
		io.CopyN(w, filler(0), maxImageBytes)
	}))
	defer ts.Close()

	dir := t.TempDir()
	p := picture{url: ts.URL + "/big.jpg", dir: dir, name: "01"}
	_, _, err := fetchImage(context.Background(), ts.Client(), "test", p, ts.URL, nil, nil)
	if err == nil {
		t.Fatal("oversize picture accepted, want an error")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("got %v, want the size cap named", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d files left behind, want none", len(entries))
	}
}

// pictureServer serves a PNG of size for any path, declaring its length so the
// projection has real figures to work from.
func pictureServer(t *testing.T, size int) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := pngBody(size)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// The cover and the gallery share one line, so one call has to place both:
// the cover beside the audio, everything else numbered under images/.
func TestFetchPicturesSavesTheCoverBesideTheGallery(t *testing.T) {
	ts := pictureServer(t, 2048)

	dir := t.TempDir()
	gallery := []string{ts.URL + "/1.jpg", ts.URL + "/2.jpg", ts.URL + "/3.jpg"}
	d := pictureFetcher(ts, dir, nil)
	pics := d.FetchPictures(context.Background(), PictureJob{
		CoverURL: ts.URL + "/cover.jpg",
		Gallery:  gallery,
		Referer:  ts.URL,
	}, nil)

	if got, want := pics.Cover, filepath.Join(dir, CoverName+".png"); got != want {
		t.Errorf("cover at %q, want %q", got, want)
	}
	if len(pics.Gallery) != len(gallery) {
		t.Fatalf("%d gallery pictures, want %d", len(pics.Gallery), len(gallery))
	}
	for i, path := range pics.Gallery {
		want := filepath.Join(dir, imagesDirName, fmt.Sprintf("%02d.png", i+1))
		if path != want {
			t.Errorf("gallery %d at %q, want %q", i, path, want)
		}
	}
}

// Numbering is by position, so one picture failing does not shift the names of
// the ones after it, however the parallel fetches interleave.
func TestFetchPicturesNumbersByPositionThroughAFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/2.jpg") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBody(1024))
	}))
	defer ts.Close()

	var mu sync.Mutex
	var failures int

	dir := t.TempDir()
	gallery := []string{ts.URL + "/1.jpg", ts.URL + "/2.jpg", ts.URL + "/3.jpg"}
	d := pictureFetcher(ts, dir, func(error) {
		mu.Lock()
		failures++
		mu.Unlock()
	})
	pics := d.FetchPictures(context.Background(), PictureJob{Gallery: gallery, Referer: ts.URL}, nil)

	if failures != 1 {
		t.Errorf("%d failures reported, want 1", failures)
	}
	want := []string{
		filepath.Join(dir, imagesDirName, "01.png"),
		filepath.Join(dir, imagesDirName, "03.png"),
	}
	if len(pics.Gallery) != len(want) {
		t.Fatalf("saved %v, want %v", pics.Gallery, want)
	}
	for i, path := range pics.Gallery {
		if path != want[i] {
			t.Errorf("gallery %d at %q, want %q", i, path, want[i])
		}
	}
}

// Pictures go down together, not one after another. Every request blocks until
// all of them have arrived, so a sequential fetch cannot finish this at all.
func TestFetchPicturesFetchesInParallel(t *testing.T) {
	const n = 6 // under imageWorkers, so nothing waits on a slot

	arrived := make(chan struct{}, n)
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-time.After(10 * time.Second): // a wedged test fails, not hangs
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBody(1024))
	}))
	defer ts.Close()

	var gallery []string
	for i := range n {
		gallery = append(gallery, fmt.Sprintf("%s/%d.jpg", ts.URL, i))
	}

	d := pictureFetcher(ts, t.TempDir(), nil)
	done := make(chan Pictures, 1)
	go func() {
		done <- d.FetchPictures(context.Background(), PictureJob{Gallery: gallery, Referer: ts.URL}, nil)
	}()

	for i := range n {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatalf("%d of %d requests in flight, want them overlapping", i, n)
		}
	}
	close(release)

	if pics := <-done; len(pics.Gallery) != n {
		t.Errorf("saved %d pictures, want %d", len(pics.Gallery), n)
	}
}

// The line carries units and bytes for progress and ETA.
func TestFetchPicturesReportsUnitsAndBytes(t *testing.T) {
	const size = 4096
	ts := pictureServer(t, size)

	gallery := []string{ts.URL + "/1.jpg", ts.URL + "/2.jpg", ts.URL + "/3.jpg"}

	var mu sync.Mutex
	var lastDone int
	var lastBytes, lastTotal int64

	d := pictureFetcher(ts, t.TempDir(), nil)
	d.FetchPictures(context.Background(), PictureJob{Gallery: gallery, Referer: ts.URL},
		func(done int, bytes, total int64) {
			mu.Lock()
			lastDone, lastBytes, lastTotal = done, bytes, total
			mu.Unlock()
		})

	if lastDone != len(gallery) {
		t.Errorf("finished at %d pictures, want %d", lastDone, len(gallery))
	}
	if want := int64(len(gallery) * size); lastBytes != want {
		t.Errorf("finished at %d bytes, want %d", lastBytes, want)
	}
	if lastTotal != lastBytes {
		t.Errorf("total %d, want it exact at %d once the last landed", lastTotal, lastBytes)
	}
}

// filler reads one byte forever, so a body can outrun the cap without the test
// holding it in memory.
type filler byte

func (b filler) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
