package downloader

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

// The host throttles per request, not per connection, so speed follows the
// number in flight. Measured: 8 gave 0.6 MB/s, 32 gave 2.2, 128 gave 5.4, with
// no rate limiting. HTTP/1.1 and HTTP/2 measured the same.
const (
	defaultConnections = 32
	maxConnections     = 128

	// The host resets streams that live for minutes, so pieces stay small
	// enough to finish in about half of one.
	pieceSize = 2 << 20

	// Below this, splitting costs more requests than it saves time.
	minChunk = 4 << 20
)

// connections caps the requests in flight across the run.
func (d *Downloader) connections() int {
	if d.Connections < 1 {
		return defaultConnections
	}
	if d.Connections > maxConnections {
		return maxConnections
	}
	return d.Connections
}

func pieceCount(total int64) int {
	return int((total + pieceSize - 1) / pieceSize)
}

// pieceRange is the range piece i covers; the last one is short.
func pieceRange(i int, total int64) (start, end int64) {
	start = int64(i) * pieceSize
	end = start + pieceSize - 1
	if end > total-1 {
		end = total - 1
	}
	return start, end
}

func worthChunking(total int64) bool {
	return total >= minChunk
}

// chunked fills part from a queue of pieces, each worker writing at its own
// offset. A queue rather than one range per worker, so none idles at the end
// while the slowest range finishes.
func (d *Downloader) chunked(ctx context.Context, res *Resolved, total int64, name, final, part string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", permanent(err)
	}

	f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return "", permanent(err)
	}
	defer f.Close()
	if err := f.Truncate(total); err != nil {
		return "", permanent(err)
	}

	n := pieceCount(total)
	state, err := openPieceState(part+stateSuffix, total)
	if err != nil {
		return "", permanent(err)
	}
	defer state.Close()

	var done atomic.Int64
	done.Store(state.bytesHeld(total))

	if d.OnStart != nil {
		d.OnStart(name, total)
	}
	if d.OnProgress != nil {
		d.OnProgress(name, done.Load(), total)
	}

	var next atomic.Int64
	g, gctx := errgroup.WithContext(ctx)
	for range min(d.connections(), n) {
		g.Go(func() error {
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return nil
				}
				if state.has(i) {
					continue
				}
				if err := d.piece(gctx, res, f, state, i, total, name, &done); err != nil {
					start, end := pieceRange(i, total)
					return fmt.Errorf("bytes %d-%d: %w", start, end, err)
				}
			}
		})
	}
	if err := g.Wait(); err != nil {
		return "", err
	}

	if err := f.Close(); err != nil {
		return "", err
	}
	if err := state.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	os.Remove(part + stateSuffix)
	return final, nil
}

// piece retries on its own, so a reset stream costs one piece, not the file.
func (d *Downloader) piece(ctx context.Context, res *Resolved, f *os.File, state *pieceState, i int, total int64, name string, done *atomic.Int64) error {
	var lastErr error
	for attempt := 0; attempt <= d.Retries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoffDelay(attempt, lastErr)); err != nil {
				return err
			}
		}

		err := d.fetchPiece(ctx, res, f, i, total, name, done)
		if err == nil {
			return state.set(i)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		var perm permanentError
		if errors.As(err, &perm) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("after %d attempts: %w", d.Retries+1, lastErr)
}

// fetchPiece runs one ranged request and writes it at its offset.
func (d *Downloader) fetchPiece(ctx context.Context, res *Resolved, f *os.File, i int, total int64, name string, done *atomic.Int64) error {
	if err := d.acquire(ctx); err != nil {
		return err
	}
	defer d.release()

	start, end := pieceRange(i, total)
	req, err := d.mediaRequest(ctx, res.URL, res.Referer)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		return permanent(errRangeIgnored)
	default:
		return transferStatus(resp)
	}

	want := end - start + 1
	var counted int64
	w := d.tee(&offsetWriter{f: f, off: start}, func(n int) {
		counted += int64(n)
		d.OnProgress(name, done.Add(int64(n)), total)
	})

	written, err := io.Copy(w, io.LimitReader(resp.Body, want))
	if err == nil && written != want {
		err = fmt.Errorf("short piece: got %d of %d bytes", written, want)
	}
	if err != nil {
		// Refetched from its start, so these bytes are not held.
		if counted > 0 && d.OnProgress != nil {
			d.OnProgress(name, done.Add(-counted), total)
		}
		return err
	}
	return nil
}

// offsetWriter writes at a position, so workers share one file without seeking
// against each other.
type offsetWriter struct {
	f   *os.File
	off int64
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	return n, err
}

// --- resume ---------------------------------------------------------------

const (
	stateSuffix = ".pieces"
	// stateMagic is bumped when the layout changes, retiring stale files.
	stateMagic  = "jasmr\x01"
	stateHeader = len(stateMagic) + 8 // magic, then the total it was written for
)

// pieceState is the bitmap a resume reads. Pieces finish out of order, so a
// part file's length cannot stand in for it. A piece is marked only once its
// bytes are written, so a crash can lose one but never invent one.
type pieceState struct {
	f  *os.File
	mu sync.Mutex
	// bits is guarded by mu: eight pieces share a byte.
	bits []byte
}

// openPieceState starts fresh when the file on disk describes another download.
func openPieceState(path string, total int64) (*pieceState, error) {
	n := pieceCount(total)
	size := stateHeader + (n+7)/8

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, size)
	read, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		f.Close()
		return nil, err
	}

	fresh := read < size ||
		string(buf[:len(stateMagic)]) != stateMagic ||
		int64(binary.BigEndian.Uint64(buf[len(stateMagic):stateHeader])) != total
	if fresh {
		clear(buf)
		copy(buf, stateMagic)
		binary.BigEndian.PutUint64(buf[len(stateMagic):stateHeader], uint64(total))
		if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, err
		}
		if _, err := f.WriteAt(buf, 0); err != nil {
			f.Close()
			return nil, err
		}
	}

	return &pieceState{f: f, bits: buf[stateHeader:]}, nil
}

func (s *pieceState) has(i int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bits[i/8]&(1<<(i%8)) != 0
}

// set marks piece i held, in memory and on disk.
func (s *pieceState) set(i int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bits[i/8] |= 1 << (i % 8)
	_, err := s.f.WriteAt(s.bits[i/8:i/8+1], int64(stateHeader+i/8))
	return err
}

// bytesHeld is what the marked pieces account for.
func (s *pieceState) bytesHeld(total int64) int64 {
	var held int64
	for i := range pieceCount(total) {
		if s.has(i) {
			start, end := pieceRange(i, total)
			held += end - start + 1
		}
	}
	return held
}

func (s *pieceState) Close() error {
	err := s.f.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}
