package downloader

import (
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// nameCols bounds the label column in terminal columns rather than runes.
// Japanese titles are double-width, so a rune budget would still wrap the line.
// With the columns beside it, a line fits an 80-column terminal.
const nameCols = 24

// sizeCols holds the byte counters, so every line's ETA starts in the same
// column. "999.9 MiB / 999.9 MiB" is 21 columns.
const sizeCols = 21

// etaCols fits "ETA 12:34:56", the longest form printed.
const etaCols = 12

// progressTag prefixes every progress line, matching [info] and [done].
const progressTag = "[progress] "

// Progress renders one live line per file: percentage, bytes on disk over the
// bytes expected, and the time left. Its Start and Update methods satisfy
// Downloader.OnStart and Downloader.OnProgress.
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
// existing line is reused rather than duplicated. A non-positive total means
// the size is not known yet; Update adopts the real one when it arrives.
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
			decor.CountersKibiByte("% .1f / % .1f", decor.WC{W: sizeCols, C: decor.DindentRight}),
			decor.Name(" "),
			eta(),
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

const (
	// rateWindow is how far back the speed behind the ETA looks.
	rateWindow = 5 * time.Second

	// maxETA caps what is worth printing; past it the figure is noise.
	maxETA = 24 * time.Hour

	// etaUnknown stands in until a rate and a total both exist.
	etaUnknown = "ETA --:--"
)

// eta renders the time left. Each redraw is one reading for the meter kept
// here; redraws run on the line's own goroutine, so it needs no lock.
func eta() decor.Decorator {
	var m rateMeter
	return decor.Any(func(s decor.Statistics) string {
		rate := m.observe(time.Now(), s.Current)
		if s.Completed || s.Aborted {
			return ""
		}

		remaining := s.Total - s.Current
		if s.Total <= 0 || remaining <= 0 || rate <= 0 {
			return etaUnknown
		}
		left := float64(remaining) / rate
		if left > maxETA.Seconds() {
			return etaUnknown
		}
		return "ETA " + formatETA(time.Duration(left*float64(time.Second)))
	}, decor.WC{W: etaCols, C: decor.DindentRight})
}

// rateMeter turns absolute progress readings into bytes per second. Samples
// decay over rateWindow, so a burst or a stall moves the figure without
// throwing it; dividing by the weight they carry keeps the first seconds from
// reading as a fraction of the real speed.
type rateMeter struct {
	sum    float64 // decayed sum of the samples
	weight float64 // decayed weight those samples carry
	last   time.Time
	prev   int64
	seen   bool
}

// observe folds one reading into the rate and returns bytes per second.
func (m *rateMeter) observe(now time.Time, current int64) float64 {
	if !m.seen {
		m.seen, m.last, m.prev = true, now, current
		return 0
	}

	dt := now.Sub(m.last).Seconds()
	if dt <= 0 {
		return m.rate()
	}

	delta := current - m.prev
	if delta < 0 {
		delta = 0 // a re-estimated total can walk a line backwards
	}
	m.last, m.prev = now, current

	decay := math.Exp(-dt / rateWindow.Seconds())
	m.sum = decay*m.sum + (1-decay)*(float64(delta)/dt)
	m.weight = decay*m.weight + (1 - decay)
	return m.rate()
}

func (m *rateMeter) rate() float64 {
	if m.weight <= 0 {
		return 0
	}
	return m.sum / m.weight
}

// formatETA prints h:mm:ss past an hour and mm:ss below it.
func formatETA(d time.Duration) string {
	d = d.Round(time.Second)
	h := int64(d / time.Hour)
	m := int64(d/time.Minute) % 60
	s := int64(d/time.Second) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// truncate clips s to at most cols terminal columns, cutting whole runes.
func truncate(s string, cols int) string {
	return runewidth.Truncate(s, cols, "")
}
