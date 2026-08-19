package downloader

import (
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/term"
)

// What a line reports, printed in a column of its own.
const (
	KindImage     = "image"
	KindRecording = "recording"
	KindChapter   = "chapter"
)

// kindCols fits "[recording] ", the widest of the three.
const kindCols = len("[recording]") + 1

// sizeCols holds the byte counters, so every line's ETA starts in the same
// column. "999.9 MiB / 999.9 MiB" is 21 columns.
const sizeCols = 21

// etaCols fits "ETA 12:34:56", the longest form printed.
const etaCols = 12

// percentCols fits the percentage and the column mpb pads it into.
const percentCols = 5

// progressTag prefixes every progress line, matching [info] and [done].
const progressTag = "[progress]"

const (
	// fixedCols is every column but the label.
	fixedCols = len(progressTag) + kindCols + 1 + 1 + percentCols + 2 + sizeCols + 1 + etaCols

	minLabelCols = 14
	maxLabelCols = 40

	// assumedCols stands in where the output has no width to read.
	assumedCols = 80
)

// Line is one row of the display. Key tells rows apart; Label is what shows.
type Line struct {
	Key   string
	Kind  string
	Label string
}

// Progress renders one live line per file: percentage, bytes on disk over the
// bytes expected, and the time left. Safe for concurrent use.
type Progress struct {
	p     *mpb.Progress
	mu    sync.Mutex
	lines map[string]*mpb.Bar
	// totals mirrors each line's total; mpb exposes no getter.
	totals map[string]int64

	labelCols int
}

// NewProgress renders to w.
func NewProgress(w io.Writer) *Progress {
	return newProgress(w)
}

func newProgress(w io.Writer, opts ...mpb.ContainerOption) *Progress {
	return &Progress{
		p:         mpb.New(append([]mpb.ContainerOption{mpb.WithOutput(w)}, opts...)...),
		lines:     make(map[string]*mpb.Bar),
		totals:    make(map[string]int64),
		labelCols: labelCols(w),
	}
}

// labelCols fits the label column to the terminal, falling back to assumedCols
// where there is no width to read.
func labelCols(w io.Writer) int {
	width := assumedCols
	if f, ok := w.(*os.File); ok {
		if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 {
			width = cols
		}
	}
	return min(max(width-fixedCols, minLabelCols), maxLabelCols)
}

// Start creates the line for l.Key, reusing one that exists. A non-positive
// total means the size is not known yet; Update adopts the real one later.
func (b *Progress) Start(l Line, total int64) {
	b.start(l, total, decor.CountersKibiByte("% .1f / % .1f", decor.WC{W: sizeCols, C: decor.DindentRight}))
}

func (b *Progress) StartCount(l Line, total int64) {
	b.start(l, total, decor.CountersNoUnit("%d / %d", decor.WC{W: sizeCols, C: decor.DindentRight}))
}

func (b *Progress) start(l Line, total int64, counters decor.Decorator) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.lines[l.Key]; ok {
		return
	}
	if total < 0 {
		total = 0 // mpb treats a non-positive total as unknown
	}
	b.totals[l.Key] = total
	// NopStyle draws no bar, leaving just the decorators.
	b.lines[l.Key] = b.p.New(total, mpb.NopStyle(),
		mpb.PrependDecorators(
			decor.Name(progressTag),
			decor.Name("["+l.Kind+"]", decor.WC{W: kindCols, C: decor.DindentRight}),
			decor.Name(truncate(l.Label, b.labelCols), decor.WC{W: b.labelCols + 1, C: decor.DindentRight}),
			decor.Name(" "), // the label column clips flush
			decor.Percentage(decor.WC{W: percentCols}),
			decor.Name("  "),
			counters,
			decor.Name(" "),
			eta(),
		),
	)
}

// Update advances the line for key. done is an absolute byte count.
func (b *Progress) Update(key string, done, total int64) {
	b.mu.Lock()
	line, ok := b.lines[key]
	stale := ok && total > 0 && b.totals[key] != total
	if stale {
		b.totals[key] = total
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

// Wait drains the renderer, aborting unfinished lines first so it cannot hang.
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
