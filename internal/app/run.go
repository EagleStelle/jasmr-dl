package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/naming"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
)

const (
	genre = "ASMR"

	imagesName   = "images"
	chaptersName = "chapters"
)

// run is one Run in progress.
type run struct {
	cfg   Config
	state *state

	// stderrMu keeps two posts from splitting a line between them.
	stderrMu sync.Mutex
}

// Run downloads every target in cfg. The error is for a run that got nowhere: a
// bad setting, a cancelled context, or every post failing.
func Run(ctx context.Context, cfg Config) (Summary, error) {
	cfg, err := cfg.prepared()
	if err != nil {
		return Summary{}, err
	}

	r := &run{cfg: cfg}
	if r.state, err = newState(ctx, cfg); err != nil {
		return Summary{}, err
	}

	plans, broken, err := r.planAll(ctx)
	if err != nil {
		return Summary{}, err
	}
	if len(plans) == 0 {
		return r.summarize(nil, broken)
	}

	r.debugf("%s at once, %d requests in flight, %d retries each",
		plural(cfg.Concurrency, "file"), cfg.Connections, cfg.Retries)

	// One budget for the whole run: the ceilings are what the host sees, so
	// they cannot be per post.
	budget := downloader.NewBudget(cfg.Concurrency, cfg.Connections)
	prog := downloader.NewProgress(cfg.Progress)

	results := make([]PostResult, len(plans))
	g := new(errgroup.Group)
	g.SetLimit(cfg.Concurrency)
	for i, p := range plans {
		g.Go(func() error {
			results[i] = r.runPost(ctx, prog, budget, p)
			return nil
		})
	}
	_ = g.Wait() // a post never returns an error; failures live in its results

	// Drain the renderer first, or it fights the summary for the same lines.
	prog.Wait()

	return r.summarize(results, broken)
}

// --- posts ------------------------------------------------------------------

// plan is one post, read and ready to download.
type plan struct {
	album    *scraper.Album
	dir      string
	tmpl     *naming.Template
	chapters []scraper.Chapter
	split    bool
	jobs     []downloader.Job
	lines    postLines
}

// planAll reads every post, one at a time so a challenge is cleared in one
// browser. It returns the posts it could read and a count of the ones it could not.
func (r *run) planAll(ctx context.Context) ([]*plan, int, error) {
	var (
		plans  []*plan
		broken int
	)
	for _, target := range r.cfg.Targets {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}

		p, err := r.planFor(ctx, target)
		if err != nil {
			// A lone post failing is the run failing, and its own error says
			// more than a count of one ever could.
			if len(r.cfg.Targets) == 1 {
				return nil, 0, err
			}
			r.warnf("[error] %s: %v", target, err)
			broken++
			continue
		}
		plans = append(plans, p)
	}
	return plans, broken, nil
}

func (r *run) planFor(ctx context.Context, target string) (*plan, error) {
	r.infof("[info] fetching %s", target)
	album, err := r.fetchAlbum(ctx, target)
	if err != nil {
		return nil, err
	}
	// A page came back, so the clearance works and is worth keeping.
	r.state.keep(ctx, r)
	r.reportSource(album)

	r.debugf("cover: %s", orNone(album.CoverURL))
	r.debugf("gallery: %d images", len(album.ImageURLs))
	r.debugf("album: %s, circle: %s, date: %s", orNone(album.RJCode), orNone(album.Circle), orNone(album.Date))
	r.debugf("chapters: %d", len(album.Chapters))
	for _, t := range album.Tracks {
		r.debugf("%s: %s", t.Title, t.LinkURL)
	}

	chapters := r.chaptersFor(album)
	split := r.splitting(chapters)

	tmpl, err := templateFor(r.cfg.Template, split, len(album.Tracks))
	if err != nil {
		return nil, err
	}

	fields := fieldsFor(album)
	dir := underBasePath(r.cfg.BasePath, tmpl.Dir(fields))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	jobs := make([]downloader.Job, 0, len(album.Tracks))
	for i, t := range album.Tracks {
		f := fields
		f.Number, f.Total = i+1, len(album.Tracks)
		jobs = append(jobs, downloader.Job{
			Name:       t.Title,
			LinkURL:    t.LinkURL,
			Source:     t.Source,
			Alternates: t.Alternates,
			Referer:    album.PageURL,
			Tags:       r.tagsFor(album, t, i+1),
			Fields:     f,
		})
	}

	return &plan{
		album:    album,
		dir:      dir,
		tmpl:     tmpl,
		chapters: chapters,
		split:    split,
		jobs:     jobs,
		lines:    linesFor(album),
	}, nil
}

// runPost saves one post's pictures and then its recordings.
func (r *run) runPost(ctx context.Context, prog *downloader.Progress, budget downloader.Budget, p *plan) PostResult {
	client, userAgent := r.state.transport()
	d := &downloader.Downloader{
		Client:         client,
		UserAgent:      userAgent,
		OutputDir:      p.dir,
		Template:       p.tmpl,
		Budget:         budget,
		Retries:        r.cfg.Retries,
		Chapters:       p.chapters,
		Split:          p.split,
		OnPictureError: func(err error) { r.warnf("[warn] picture not saved: %v", err) },
		OnCoverError:  func(name string, err error) { r.warnf("[warn] %s: %v", name, err) },
		OnChapterDropped: func(name string, start, total time.Duration) {
			r.warnf("[warn] chapter %s begins at %s, past the end of a %s stream; skipped",
				name, start.Round(time.Second), total.Round(time.Second))
		},
		OnStart: func(name string, total int64) {
			prog.Start(p.lines.line(downloader.KindRecording, name), total)
		},
		OnProgress: func(name string, done, total int64) {
			prog.Update(p.lines.key(downloader.KindRecording, name), done, total)
		},
		OnSplitStart: func(total int) {
			prog.StartCount(p.lines.line(downloader.KindChapter, chaptersName), int64(total))
		},
		OnSplitProgress: func(done, total int) {
			prog.Update(p.lines.key(downloader.KindChapter, chaptersName), int64(done), int64(total))
		},
	}

	// The cover has to be on disk before the audio it is embedded in.
	d.CoverPath = r.fetchPictures(ctx, d, prog, p)

	return PostResult{Label: p.lines.label, Dir: p.dir, Results: d.Run(ctx, p.jobs)}
}

// fetchPictures saves the cover and the gallery on one line, and returns the
// cover the audio embeds. Best-effort.
func (r *run) fetchPictures(ctx context.Context, d *downloader.Downloader, prog *downloader.Progress, p *plan) string {
	coverURL, gallery := r.pictureURLs(p.album)
	count := len(gallery)
	if coverURL != "" {
		count++
	}
	if count == 0 {
		return ""
	}

	line := p.lines.line(downloader.KindImage, imagesName)
	prog.StartUnits(line, int64(count))

	pics := d.FetchPictures(ctx, coverURL, gallery, p.album.PageURL,
		func(done int, bytes, total int64) {
			prog.SetUnits(line.Key, int64(done))
			prog.Update(line.Key, bytes, total)
		})

	r.debugf("cover: %s", orNone(pics.Cover))
	r.debugf("gallery: %d saved", len(pics.Gallery))
	return pics.Cover
}

func (r *run) reportSource(album *scraper.Album) {
	kind := "file"
	if album.Source() == scraper.SourceHLS {
		kind = "stream"
	}
	r.infof("[info] source: %s", kind)
}

// --- streams ------------------------------------------------------------------

func (r *run) infof(format string, args ...any) {
	writeLine(r.cfg.Stdout, format, args...)
}

func (r *run) warnf(format string, args ...any) {
	r.stderrMu.Lock()
	defer r.stderrMu.Unlock()
	writeLine(r.cfg.Stderr, format, args...)
}

func (r *run) debugf(format string, args ...any) {
	r.cfg.Log(format, args...)
}

func writeLine(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format+"\n", args...)
}

// plural avoids having to write "file(s)".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
