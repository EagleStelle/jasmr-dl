package downloader

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	path, err := fetchImage(context.Background(), ts.Client(), "test", ts.URL+"/a.jpg", ts.URL, dir, "01")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := filepath.Base(path); got != "01.png" {
		t.Errorf("saved as %q, want 01.png from the sniffed content", got)
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
	_, err := fetchImage(context.Background(), ts.Client(), "test", ts.URL+"/big.jpg", ts.URL, dir, "01")
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

// filler reads one byte forever, so a body can outrun the cap without the test
// holding it in memory.
type filler byte

func (b filler) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
