package downloader

import (
	"io"
	"sync"

	"github.com/mattn/go-runewidth"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// nameCols bounds the label column in terminal columns rather than runes.
// Japanese titles are double-width, so a rune budget would still wrap the line.
const nameCols = 28

// progressTag prefixes every progress line, matching [info] and [done].
const progressTag = "[progress] "

// Progress renders one live line per file: percentage, bytes done over bytes
// total, and speed. Its Start and Update methods satisfy Downloader.OnStart
// and Downloader.OnProgress.
//
// Safe for concurrent use: every worker reports into the same set.
type Progress struct {
	p     *mpb.Progress
	mu    sync.Mutex
	lines map[string]*mpb.Bar
	// totals mirrors each line's total; mpb exposes no getter, and a line
	// created before the size was known needs it set exactly once.
	totals map[string]int64
}

// NewProgress renders to w. Pass the command's output writer rather than
// assuming os.Stdout, so output can be redirected and tested.
func NewProgress(w io.Writer) *Progress {
	return &Progress{
		p:      mpb.New(mpb.WithOutput(w)),
		lines:  make(map[string]*mpb.Bar),
		totals: make(map[string]int64),
	}
}

// Start creates the line for name. A retry re-reports the same name, so an
// existing line is reused rather than duplicated.
func (b *Progress) Start(name string, total int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.lines[name]; ok {
		return
	}
	if total < 0 {
		total = 0 // mpb treats a non-positive total as unknown
	}
	b.totals[name] = total
	// NopStyle draws no bar, leaving just the decorators.
	b.lines[name] = b.p.New(total, mpb.NopStyle(),
		mpb.PrependDecorators(
			decor.Name(progressTag),
			decor.Name(truncate(name, nameCols), decor.WC{W: nameCols + 1, C: decor.DindentRight}),
			decor.Name(" "), // the name column clips flush, so separate it here
			decor.Percentage(decor.WC{W: 5}),
			decor.Name("  "),
			decor.CountersKibiByte("% .1f / % .1f"),
		),
	)
}

// Update advances the line for name. done is an absolute byte count, so a
// resumed transfer starting mid-file reports correctly without extra state.
func (b *Progress) Update(name string, done, total int64) {
	b.mu.Lock()
	line, ok := b.lines[name]
	stale := ok && total > 0 && b.totals[name] != total
	if stale {
		b.totals[name] = total
	}
	b.mu.Unlock()

	if !ok {
		return
	}
	if stale {
		line.SetTotal(total, false)
	}
	line.SetCurrent(done)
}

// Wait drains the renderer. Lines for failed transfers are aborted first:
// mpb.Progress.Wait blocks until every line completes, so an unfinished one
// would hang the program.
func (b *Progress) Wait() {
	b.mu.Lock()
	for _, line := range b.lines {
		if !line.Completed() {
			line.Abort(false)
		}
	}
	b.mu.Unlock()
	b.p.Wait()
}

// truncate clips s to at most cols terminal columns, cutting whole runes.
func truncate(s string, cols int) string {
	return runewidth.Truncate(s, cols, "")
}
