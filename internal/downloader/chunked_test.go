package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

// rangeServer serves body over ranged requests. fail cuts a response short the
// way a reset stream does.
type rangeServer struct {
	body []byte
	reqs atomic.Int64
	fail func(n int64) bool
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := s.reqs.Add(1)

	var start, end int64
	if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(s.body)
		return
	}
	if end >= int64(len(s.body)) {
		end = int64(len(s.body)) - 1
	}

	slice := s.body[start : end+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.body)))
	w.Header().Set("Content-Length", strconv.Itoa(len(slice)))
	w.WriteHeader(http.StatusPartialContent)

	if s.fail != nil && s.fail(n) {
		w.Write(slice[:len(slice)/2]) // then hang up

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	w.Write(slice)
}

func newChunkTest(t *testing.T, srv *rangeServer) (*Downloader, string, string) {
	t.Helper()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	dir := t.TempDir()
	d := &Downloader{
		Client:      ts.Client(),
		UserAgent:   "test",
		OutputDir:   dir,
		Connections: 8,
		Retries:     4,
	}
	return d, ts.URL + "/a.mp3", dir
}

func randomBody(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(1)).Read(b)
	return b
}

func TestChunkedAssemblesEveryPiece(t *testing.T) {
	body := randomBody(9*pieceSize + 12345) // a short last piece
	srv := &rangeServer{body: body}
	d, url, dir := newChunkTest(t, srv)

	final := filepath.Join(dir, "a.mp3")
	got, err := d.chunked(context.Background(), &Resolved{URL: url}, int64(len(body)), "a.mp3", final, final+".part")
	if err != nil {
		t.Fatalf("chunked: %v", err)
	}

	on, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(on, body) {
		t.Fatalf("file is %d bytes and does not match the %d served", len(on), len(body))
	}
	if _, err := os.Stat(final + ".part" + stateSuffix); !os.IsNotExist(err) {
		t.Error("the state file outlived the download")
	}
	if want := int64(pieceCount(int64(len(body)))); srv.reqs.Load() != want {
		t.Errorf("%d requests for %d pieces", srv.reqs.Load(), want)
	}
}

// A piece whose stream dies must cost only that piece.
func TestChunkedRetriesOnlyTheBrokenPiece(t *testing.T) {
	body := randomBody(8 * pieceSize)
	var broke atomic.Bool
	srv := &rangeServer{body: body, fail: func(n int64) bool {
		return n == 3 && broke.CompareAndSwap(false, true)
	}}
	d, url, dir := newChunkTest(t, srv)

	final := filepath.Join(dir, "a.mp3")
	got, err := d.chunked(context.Background(), &Resolved{URL: url}, int64(len(body)), "a.mp3", final, final+".part")
	if err != nil {
		t.Fatalf("chunked: %v", err)
	}

	on, _ := os.ReadFile(got)
	if !bytes.Equal(on, body) {
		t.Fatal("the retried piece did not land correctly")
	}
	if want := int64(pieceCount(int64(len(body))) + 1); srv.reqs.Load() != want {
		t.Errorf("%d requests, want %d: one refetch and no more", srv.reqs.Load(), want)
	}
}

// Progress must not double-count a piece that is fetched twice.
func TestChunkedProgressNeverExceedsTheTotal(t *testing.T) {
	body := randomBody(6 * pieceSize)
	var broke atomic.Bool
	srv := &rangeServer{body: body, fail: func(n int64) bool {
		return n == 2 && broke.CompareAndSwap(false, true)
	}}
	d, url, dir := newChunkTest(t, srv)

	var peak atomic.Int64
	d.OnProgress = func(_ string, done, total int64) {
		for {
			was := peak.Load()
			if done <= was || peak.CompareAndSwap(was, done) {
				break
			}
		}
		if done > total {
			t.Errorf("progress reported %d of %d", done, total)
		}
	}

	final := filepath.Join(dir, "a.mp3")
	if _, err := d.chunked(context.Background(), &Resolved{URL: url}, int64(len(body)), "a.mp3", final, final+".part"); err != nil {
		t.Fatalf("chunked: %v", err)
	}
	if peak.Load() != int64(len(body)) {
		t.Errorf("progress ended at %d, want %d", peak.Load(), len(body))
	}
}

// An interrupted run must refetch only what it did not already hold.
func TestChunkedResumesFromTheStateFile(t *testing.T) {
	body := randomBody(10 * pieceSize)
	total := int64(len(body))

	dir := t.TempDir()
	final := filepath.Join(dir, "a.mp3")
	part := final + ".part"

	// A part file holding pieces 0-4, as an interrupted run would leave.
	f, err := os.Create(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(total); err != nil {
		t.Fatal(err)
	}
	state, err := openPieceState(part+stateSuffix, total)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		start, end := pieceRange(i, total)
		if _, err := f.WriteAt(body[start:end+1], start); err != nil {
			t.Fatal(err)
		}
		if err := state.set(i); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	state.Close()

	srv := &rangeServer{body: body}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	d := &Downloader{Client: ts.Client(), UserAgent: "test", OutputDir: dir, Connections: 8}

	got, err := d.chunked(context.Background(), &Resolved{URL: ts.URL + "/a.mp3"}, total, "a.mp3", final, part)
	if err != nil {
		t.Fatalf("chunked: %v", err)
	}
	on, _ := os.ReadFile(got)
	if !bytes.Equal(on, body) {
		t.Fatal("the resumed file does not match what was served")
	}
	if srv.reqs.Load() != 5 {
		t.Errorf("%d requests, want 5: the held pieces were refetched", srv.reqs.Load())
	}
}

// A host answering 200 to a ranged request cannot be chunked; the caller must
// hear that rather than assemble garbage.
func TestChunkedReportsAnIgnoredRange(t *testing.T) {
	body := randomBody(8 * pieceSize)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer ts.Close()

	dir := t.TempDir()
	d := &Downloader{Client: ts.Client(), UserAgent: "test", OutputDir: dir, Connections: 4}
	final := filepath.Join(dir, "a.mp3")

	_, err := d.chunked(context.Background(), &Resolved{URL: ts.URL + "/a.mp3"}, int64(len(body)), "a.mp3", final, final+".part")
	if !errors.Is(err, errRangeIgnored) {
		t.Fatalf("got %v, want errRangeIgnored", err)
	}
}
