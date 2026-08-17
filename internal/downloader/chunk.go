package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

// The media host throttles per connection, so files are split across requests.
const (
	minChunk  = 4 << 20
	maxChunks = 4

	// Throughput plateaus here: 8 conns ~640 KB/s, 16 ~620.
	maxConnections = 8
)

// connectionPlan divides the ceiling among files in flight. More files than
// connections would only leave the extras waiting at zero, so files clamp too.
func connectionPlan(files int) (concurrency, chunks int) {
	if files < 1 {
		files = 1
	}
	if files > maxConnections {
		files = maxConnections
	}

	chunks = maxConnections / files
	if chunks > maxChunks {
		chunks = maxChunks
	}
	return files, chunks
}

type chunkPlan struct {
	total  int64
	ranges [][2]int64
}

// planChunks splits total into ranges, or false if one stream is better.
func planChunks(total int64, budget int) (chunkPlan, bool) {
	if total < minChunk*2 || budget < 2 {
		return chunkPlan{}, false
	}

	n := min(int(total/minChunk), budget, maxChunks)
	if n < 2 {
		return chunkPlan{}, false
	}

	size := total / int64(n)
	plan := chunkPlan{total: total}
	for i := 0; i < n; i++ {
		start := int64(i) * size
		end := start + size - 1
		if i == n-1 {
			end = total - 1
		}
		plan.ranges = append(plan.ranges, [2]int64{start, end})
	}
	return plan, true
}

// chunked joins parallel ranged requests. Each chunk's part file length is its
// own resume point.
func (d *Downloader) chunked(ctx context.Context, res *Resolved, plan chunkPlan, name, final, part string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", permanent(err)
	}

	paths := make([]string, len(plan.ranges))
	for i := range plan.ranges {
		paths[i] = fmt.Sprintf("%s.%d", part, i)
	}

	var done atomic.Int64
	for i, r := range plan.ranges {
		if have := fileSize(paths[i]); have > 0 {
			if have > r[1]-r[0]+1 {
				os.Remove(paths[i]) // longer than its range: not ours
				continue
			}
			done.Add(have)
		}
	}

	if d.OnStart != nil {
		d.OnStart(name, plan.total)
	}
	if d.OnProgress != nil {
		d.OnProgress(name, done.Load(), plan.total)
	}

	g, gctx := errgroup.WithContext(ctx)
	for i, r := range plan.ranges {
		g.Go(func() error {
			if err := d.chunk(gctx, res, paths[i], r, name, plan.total, &done); err != nil {
				return fmt.Errorf("bytes %d-%d: %w", r[0], r[1], err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return "", err
	}

	if err := joinChunks(paths, part, plan.total); err != nil {
		return "", err
	}
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	for _, p := range paths {
		os.Remove(p)
	}
	return final, nil
}

func (d *Downloader) chunk(ctx context.Context, res *Resolved, path string, r [2]int64, name string, total int64, done *atomic.Int64) error {
	have := fileSize(path)
	want := r[1] - r[0] + 1
	if have == want {
		return nil
	}

	if err := d.acquire(ctx); err != nil {
		return err
	}
	defer d.release()

	req, err := d.mediaRequest(ctx, res.URL, res.Referer)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r[0]+have, r[1]))

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

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return permanent(err)
	}

	w := d.tee(f, func(n int) {
		d.OnProgress(name, done.Add(int64(n)), total)
	})

	_, copyErr := io.Copy(w, io.LimitReader(resp.Body, want-have))
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		return copyErr
	case closeErr != nil:
		return closeErr
	}

	if got := fileSize(path); got != want {
		return fmt.Errorf("short chunk: got %d of %d bytes", got, want)
	}
	return nil
}

func joinChunks(paths []string, part string, total int64) error {
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return permanent(err)
	}

	var written int64
	for _, p := range paths {
		in, err := os.Open(p)
		if err != nil {
			out.Close()
			return err
		}
		n, err := io.Copy(out, in)
		in.Close()
		if err != nil {
			out.Close()
			return err
		}
		written += n
	}
	if err := out.Close(); err != nil {
		return err
	}

	if written != total {
		os.Remove(part)
		return fmt.Errorf("joined %d bytes, want %d", written, total)
	}
	return nil
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}
