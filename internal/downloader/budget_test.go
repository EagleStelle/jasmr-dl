package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/EagleStelle/jasmr-dl/internal/naming"
)

// mustTemplate names a file by its number, so two jobs of a post do not land on
// one name.
func mustTemplate(t *testing.T) *naming.Template {
	t.Helper()
	tmpl, err := naming.Parse("{number}.{ext}")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

// peakServer serves ranges and records how many requests ever overlapped.
type peakServer struct {
	body []byte

	mu   sync.Mutex
	live int
	peak int
}

func (s *peakServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.enter()
	// Held long enough that anything allowed through together is seen together.
	time.Sleep(20 * time.Millisecond)
	defer s.leave()

	var start, end int64
	if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
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
	w.Write(slice)
}

func (s *peakServer) enter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live++
	s.peak = max(s.peak, s.live)
}

func (s *peakServer) leave() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live--
}

func (s *peakServer) highWater() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak
}

// Posts each carry their own Downloader, so the ceiling has to live outside one
// or two posts at -j 4 would open 8 at a host that only ever counts requests.
func TestBudgetCapsConnectionsAcrossDownloaders(t *testing.T) {
	const (
		ceiling = 4
		posts   = 3
	)

	body := randomBody(6 * pieceSize)
	srv := &peakServer{body: body}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	budget := NewBudget(posts, ceiling)

	var g errgroup.Group
	for i := range posts {
		g.Go(func() error {
			dir := t.TempDir()
			d := &Downloader{
				Client:    ts.Client(),
				UserAgent: "test",
				OutputDir: dir,
				Budget:    budget,
			}
			final := filepath.Join(dir, fmt.Sprintf("%d.mp3", i))
			_, err := d.chunked(context.Background(), &Resolved{URL: ts.URL + "/a.mp3"},
				int64(len(body)), "a.mp3", final, final+".part")
			return err
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("chunked: %v", err)
	}

	if got := srv.highWater(); got > ceiling {
		t.Errorf("%d requests overlapped, want no more than %d across %d posts", got, ceiling, posts)
	}
}

// The pictures come off another host, on a ceiling of their own, which is just
// as much the run's as the connections are.
func TestBudgetCapsPicturesAcrossDownloaders(t *testing.T) {
	var (
		mu         sync.Mutex
		live, peak int
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		live++
		peak = max(peak, live)
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		live--
		mu.Unlock()

		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBody(1024))
	}))
	defer ts.Close()

	// Well past imageWorkers between them, so a per-post ceiling would show.
	var gallery []string
	for i := range imageWorkers {
		gallery = append(gallery, fmt.Sprintf("%s/%d.jpg", ts.URL, i))
	}

	budget := NewBudget(1, 1)

	var g errgroup.Group
	for range 3 {
		g.Go(func() error {
			d := &Downloader{
				Client:    ts.Client(),
				UserAgent: "test",
				OutputDir: t.TempDir(),
				Budget:    budget,
			}
			d.FetchPictures(context.Background(), "", gallery, ts.URL, nil)
			return nil
		})
	}
	_ = g.Wait()

	mu.Lock()
	got := peak
	mu.Unlock()
	if got > imageWorkers {
		t.Errorf("%d pictures in flight, want no more than %d across every post", got, imageWorkers)
	}
}

// -N is the run's too: three posts of two files each at -N 2 must still be two
// downloads at a time, not six.
//
// Every request is held open rather than timed, so the count does not turn on
// how the stagger before each job happens to fall.
func TestBudgetCapsFilesAcrossDownloaders(t *testing.T) {
	const (
		ceiling = 2
		posts   = 3
		jobs    = 2 // per post
	)

	arrived := make(chan struct{}, posts*jobs)
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-time.After(10 * time.Second): // a wedged test fails, not hangs
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(pngBody(64)) // not audio, but nothing here reads it
	}))
	defer ts.Close()

	budget := NewBudget(ceiling, 32)

	var g errgroup.Group
	for range posts {
		g.Go(func() error {
			d := &Downloader{
				Client:    ts.Client(),
				UserAgent: "test",
				OutputDir: t.TempDir(),
				Template:  mustTemplate(t),
				Budget:    budget,
			}
			d.Run(context.Background(), []Job{
				{Name: "a.mp3", LinkURL: ts.URL + "/a.mp3", Fields: naming.Fields{Number: 1}},
				{Name: "b.mp3", LinkURL: ts.URL + "/b.mp3", Fields: naming.Fields{Number: 2}},
			})
			return nil
		})
	}

	for i := range ceiling {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatalf("%d of %d downloads in flight, want the ceiling filled", i, ceiling)
		}
	}

	// Past the longest stagger, so everything left is queued on the budget
	// rather than still on its way to it.
	select {
	case <-arrived:
		close(release)
		t.Fatalf("a %drd download went out, want no more than %d across %d posts", ceiling+1, ceiling, posts)
	case <-time.After(time.Second):
	}

	close(release)
	_ = g.Wait()
}
