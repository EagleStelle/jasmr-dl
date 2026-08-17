package downloader

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"jasmr-dl/internal/util"
)

// A 6-hour work runs to roughly 2300 segments.
const maxSegments = 20000

// Bounds goroutines only; the connection budget bounds requests.
const maxSegmentWorkers = 64

// tsSync is the sync byte every MPEG-TS packet starts with.
const tsSync = 0x47

// ipod is ffmpeg's muxer for .m4a. Segments carry AAC, so the copy is lossless.
const (
	hlsOutputExt    = ".m4a"
	hlsOutputFormat = "ipod"
)

// assembleHLS rebuilds a work from its segments, kept until the remux succeeds.
func (d *Downloader) assembleHLS(ctx context.Context, job Job) (string, error) {
	// Before fetching anything: failing after 160 MB is no use.
	tools, err := findFFmpeg()
	if err != nil {
		return "", permanent(err)
	}

	name := util.Sanitize(strings.TrimSuffix(job.Name, hlsOutputExt) + hlsOutputExt)
	if !util.IsAudioFile(name) {
		return "", permanent(fmt.Errorf("refusing %q: not an allowlisted audio file", name))
	}
	final := filepath.Join(d.OutputDir, name)
	if _, statErr := os.Stat(final); statErr == nil {
		return final, nil
	}

	segments, err := d.fetchPlaylist(ctx, job.LinkURL, job.Referer)
	if err != nil {
		return "", err
	}

	segDir := final + ".segments"
	if err := os.MkdirAll(segDir, 0o755); err != nil {
		return "", permanent(err)
	}

	paths, err := d.fetchSegments(ctx, job, segments, segDir, name)
	if err != nil {
		return "", err
	}
	if err := d.remux(ctx, tools.ffmpeg, segDir, paths, final); err != nil {
		return "", err
	}
	os.RemoveAll(segDir)

	// Tag from the finished file: chapter ends need its real length, and the
	// nominal EXTINF sum overshoots it.
	return d.embedded(ctx, final)
}

// fetchPlaylist follows one variant level if given a master playlist.
func (d *Downloader) fetchPlaylist(ctx context.Context, target, referer string) ([]string, error) {
	body, base, err := d.getText(ctx, target, referer)
	if err != nil {
		return nil, err
	}

	if variant, ok := firstVariant(body, base); ok {
		body, base, err = d.getText(ctx, variant, referer)
		if err != nil {
			return nil, err
		}
	}
	return parsePlaylist(body, base)
}

func (d *Downloader) getText(ctx context.Context, target, referer string) (string, *url.URL, error) {
	resp, err := d.getOK(ctx, target, referer)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", nil, err
	}
	return string(body), resp.Request.URL, nil
}

func firstVariant(body string, base *url.URL) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(body))
	var pending bool
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF"):
			pending = true
		case line == "" || strings.HasPrefix(line, "#"):
		case pending:
			if abs := util.ResolveURL(base, line); abs != "" {
				return abs, true
			}
			pending = false
		}
	}
	return "", false
}

// parsePlaylist reads segment URIs. Page-supplied, so off-host ones are refused.
func parsePlaylist(body string, base *url.URL) ([]string, error) {
	var (
		segments []string
		endList  bool
	)

	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXT-X-KEY"):
			if !strings.Contains(line, "METHOD=NONE") {
				return nil, permanent(errors.New("playlist is encrypted"))
			}
		case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
			endList = true
		case strings.HasPrefix(line, "#"):
		default:
			abs := util.ResolveURL(base, line)
			if abs == "" {
				return nil, permanent(fmt.Errorf("bad segment URI %q", line))
			}
			if !strings.EqualFold(hostOf(abs), base.Hostname()) {
				return nil, permanent(fmt.Errorf("segment %s is off the playlist's host", abs))
			}
			if len(segments) >= maxSegments {
				return nil, permanent(fmt.Errorf("playlist exceeds %d segments", maxSegments))
			}
			segments = append(segments, abs)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	if len(segments) == 0 {
		return nil, permanent(errors.New("playlist lists no segments"))
	}
	if !endList {
		return nil, permanent(errors.New("playlist is live, with no fixed end"))
	}
	return segments, nil
}

// fetchSegments downloads into dir in playlist order, skipping what is there.
func (d *Downloader) fetchSegments(ctx context.Context, job Job, segments []string, dir, label string) ([]string, error) {
	paths := make([]string, len(segments))
	for i := range segments {
		paths[i] = filepath.Join(dir, fmt.Sprintf("%06d.ts", i))
	}

	// A playlist carries no byte total, and asking costs a request per segment,
	// so the segments already written supply it.
	sizes := &segmentSizes{count: int64(len(segments))}
	if d.OnStart != nil {
		d.OnStart(label, 0) // unknown until the first segment lands
	}
	report := func() {
		if d.OnProgress != nil {
			done, total := sizes.stat()
			d.OnProgress(label, done, total)
		}
	}
	report()

	g, gctx := errgroup.WithContext(ctx)
	// The connection budget bounds requests; this bounds goroutines.
	g.SetLimit(maxSegmentWorkers)
	for i, target := range segments {
		g.Go(func() error {
			if have := fileSize(paths[i]); have > 0 {
				sizes.advance(have)
				sizes.finish(have)
				report()
				return nil
			}
			if err := d.acquire(gctx); err != nil {
				return err
			}
			written, err := d.fetchSegment(gctx, target, job.Referer, paths[i], func(n int) {
				sizes.advance(int64(n))
				report()
			})
			d.release()
			if err != nil {
				return fmt.Errorf("segment %d: %w", i, err)
			}
			sizes.finish(written)
			report()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return paths, nil
}

// fetchSegment stages through .part so a partial never looks finished. It
// reports each write to add and returns the segment's size.
func (d *Downloader) fetchSegment(ctx context.Context, target, referer, path string, add func(n int)) (int64, error) {
	resp, err := d.getOK(ctx, target, referer)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return 0, permanent(err)
	}

	head := make([]byte, 1)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		f.Close()
		os.Remove(part)
		return 0, fmt.Errorf("empty segment %s: %w", target, err)
	}
	if head[0] != tsSync {
		f.Close()
		os.Remove(part)
		return 0, permanent(fmt.Errorf("%s is not MPEG-TS", target))
	}

	w := d.tee(f, add)
	n, writeErr := w.Write(head)
	written := int64(n)
	if writeErr == nil {
		var copied int64
		copied, writeErr = io.Copy(w, resp.Body)
		written += copied
	}
	closeErr := f.Close()

	switch {
	case writeErr != nil:
		return 0, writeErr
	case closeErr != nil:
		return 0, closeErr
	}
	if err := os.Rename(part, path); err != nil {
		return 0, err
	}
	return written, nil
}

// segmentSizes projects a playlist's byte total from the segments already
// written. Segments run to a near-uniform length, so the mean of the finished
// ones across the count is within a few percent, and exact once the last lands.
type segmentSizes struct {
	mu    sync.Mutex
	count int64 // segments the playlist lists
	done  int64 // segments fully written
	whole int64 // bytes those finished segments hold
	bytes int64 // bytes on disk, segments in flight included
}

// advance records bytes written, whether or not their segment is finished.
func (s *segmentSizes) advance(n int64) {
	s.mu.Lock()
	s.bytes += n
	s.mu.Unlock()
}

func (s *segmentSizes) finish(size int64) {
	s.mu.Lock()
	s.whole += size
	s.done++
	s.mu.Unlock()
}

// stat returns the bytes on disk and the projected total, 0 until the first
// segment finishes.
func (s *segmentSizes) stat() (done, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done == 0 {
		return s.bytes, 0
	}
	total = max(s.whole*s.count/s.done, s.bytes) // never under what is on disk
	return s.bytes, total
}

// remux concatenates the segments; 2000-odd paths need a list file.
func (d *Downloader) remux(ctx context.Context, ffmpeg, dir string, paths []string, final string) error {
	list := filepath.Join(dir, "segments.txt")
	var b strings.Builder
	for _, p := range paths {
		// concat resolves paths against the list file, so basenames suffice.
		b.WriteString("file '" + strings.ReplaceAll(filepath.Base(p), "'", `'\''`) + "'\n")
	}
	if err := os.WriteFile(list, []byte(b.String()), 0o644); err != nil {
		return permanent(err)
	}

	staged := final + ".part"
	// The .part suffix hides the extension ffmpeg infers the format from.
	out, err := runFFmpeg(ctx, ffmpeg, slices.Concat(ffmpegQuiet, []string{
		"-f", "concat", "-safe", "0", "-i", list,
		"-c", "copy", "-movflags", "+faststart",
		"-f", hlsOutputFormat, staged,
	})...)
	if err != nil {
		os.Remove(staged)
		return fmt.Errorf("ffmpeg: %w: %s", err, out)
	}
	return os.Rename(staged, final)
}

// ffmpegQuiet suppresses the banner and never stops on a prompt. Every call to
// either binary starts with it.
var ffmpegQuiet = []string{"-nostdin", "-loglevel", "error", "-y"}

// runFFmpeg returns combined output, where ffmpeg puts anything useful.
func runFFmpeg(ctx context.Context, bin string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return strings.TrimSpace(string(out)), err
}

// runProbe reads stdout only, so a warning on stderr cannot corrupt a value
// being parsed out of it.
func runProbe(ctx context.Context, bin string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return strings.TrimSpace(string(out)), err
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
