package downloader

import (
	"io"
	"sync"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// barNameRunes bounds the label column. Track titles are Japanese and long
// enough to push the bar off-screen otherwise.
const barNameRunes = 28

// BarSet renders one progress bar per file. Its Start and Update methods
// satisfy Downloader.OnStart and Downloader.OnProgress.
//
// Safe for concurrent use: every worker reports into the same set.
type BarSet struct {
	p    *mpb.Progress
	mu   sync.Mutex
	bars map[string]*mpb.Bar
	// totals mirrors each bar's total; mpb exposes no getter, and a bar
	// created before the size was known needs it set exactly once.
	totals map[string]int64
}

// NewBarSet renders to w. Pass the command's output writer rather than
// assuming os.Stdout, so output can be redirected and tested.
func NewBarSet(w io.Writer) *BarSet {
	return &BarSet{
		p:      mpb.New(mpb.WithWidth(40), mpb.WithOutput(w)),
		bars:   make(map[string]*mpb.Bar),
		totals: make(map[string]int64),
	}
}

// Start creates the bar for name. A retry re-reports the same name, so an
// existing bar is reused rather than duplicated.
func (b *BarSet) Start(name string, total int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.bars[name]; ok {
		return
	}
	if total < 0 {
		total = 0 // mpb treats a non-positive total as unknown
	}
	b.totals[name] = total
	b.bars[name] = b.p.AddBar(total,
		mpb.PrependDecorators(
			decor.Name(truncate(name, barNameRunes), decor.WC{W: barNameRunes + 1, C: decor.DindentRight}),
			decor.Percentage(decor.WC{W: 5}),
		),
		mpb.AppendDecorators(
			decor.CountersKibiByte("% .1f / % .1f"),
			decor.Name(" "),
			decor.AverageSpeed(decor.SizeB1024(0), "% .1f"),
		),
	)
}

// Update advances the bar for name. done is an absolute byte count, so a
// resumed transfer starting mid-file reports correctly without extra state.
func (b *BarSet) Update(name string, done, total int64) {
	b.mu.Lock()
	bar, ok := b.bars[name]
	stale := ok && total > 0 && b.totals[name] != total
	if stale {
		b.totals[name] = total
	}
	b.mu.Unlock()

	if !ok {
		return
	}
	if stale {
		bar.SetTotal(total, false)
	}
	bar.SetCurrent(done)
}

// Wait drains the renderer. Bars for failed transfers are aborted first:
// mpb.Progress.Wait blocks until every bar completes, so an unfinished bar
// would hang the program.
func (b *BarSet) Wait() {
	b.mu.Lock()
	for _, bar := range b.bars {
		if !bar.Completed() {
			bar.Abort(false)
		}
	}
	b.mu.Unlock()
	b.p.Wait()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
