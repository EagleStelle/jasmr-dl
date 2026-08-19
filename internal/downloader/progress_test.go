package downloader

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/vbauerster/mpb/v8"
)

// waitOrFail runs bs.Wait in a goroutine so a hang shows up as a test failure
// rather than a stuck test binary.
func waitOrFail(t *testing.T, bs *Progress) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		bs.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return; a bar was left incomplete")
	}
}

func line(key string) Line {
	return Line{Key: key, Kind: KindRecording, Label: "ある夏の日"}
}

func TestWaitReturnsWhenBarsComplete(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Start(line("a.m4a"), 100)
	bs.Update("a.m4a", 100, 100)
	waitOrFail(t, bs)
}

// A failed transfer leaves its bar short of total. mpb.Progress.Wait blocks
// until every bar completes, so Wait must abort those or the program hangs
// after any download error.
func TestWaitReturnsWhenTransferFailedMidway(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Start(line("a.m4a"), 1000)
	bs.Update("a.m4a", 300, 1000)
	waitOrFail(t, bs)
}

// A bar created before the size is known must adopt the real total once it
// arrives, otherwise it can never reach completion.
func TestUnknownTotalIsAdoptedLater(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Start(line("a.m4a"), -1)

	if got := bs.totals["a.m4a"]; got != 0 {
		t.Fatalf("negative total should normalize to 0 (unknown), got %d", got)
	}

	bs.Update("a.m4a", 50, 500)
	if got := bs.totals["a.m4a"]; got != 500 {
		t.Fatalf("total = %d, want 500", got)
	}

	bs.Update("a.m4a", 500, 500)
	waitOrFail(t, bs)
}

// Retries re-report the same filename. That must not stack duplicate lines.
func TestRetryReusesBar(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Start(line("a.m4a"), 100)
	bs.Start(line("a.m4a"), 100)

	if len(bs.lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(bs.lines))
	}
	bs.Update("a.m4a", 100, 100)
	waitOrFail(t, bs)
}

// Two posts can share a title; the key is what tells their rows apart.
func TestSharedLabelStillOpensItsOwnLine(t *testing.T) {
	bs := NewProgress(io.Discard)
	const label = "ある夏の日"

	bs.Start(Line{Key: "first\x00a.m4a", Kind: KindRecording, Label: label}, 100)
	bs.Start(Line{Key: "second\x00a.m4a", Kind: KindRecording, Label: label}, 100)

	if len(bs.lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(bs.lines))
	}
	bs.Update("first\x00a.m4a", 100, 100)
	bs.Update("second\x00a.m4a", 100, 100)
	waitOrFail(t, bs)
}

func TestLineLeadsWithItsKindThenTheTitle(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want string
	}{
		{KindImage, "[progress][image]     ある夏の日"},
		{KindRecording, "[progress][recording] ある夏の日"},
		{KindChapter, "[progress][chapter]   ある夏の日"},
	} {
		var buf bytes.Buffer
		// A buffer is no terminal, so mpb only draws when told to refresh.
		bs := newProgress(&buf, mpb.WithAutoRefresh())
		bs.Start(Line{Key: "k", Kind: tc.kind, Label: "ある夏の日"}, 100)
		bs.Update("k", 100, 100)
		waitOrFail(t, bs)

		if got := buf.String(); !strings.Contains(got, tc.want) {
			t.Errorf("%s row = %q, want it to carry %q", tc.kind, got, tc.want)
		}
	}
}

// Image line carries discrete tally #/N, no percentage, and empty size column.
func TestImageLineCarriesCountAndNoPercentage(t *testing.T) {
	var buf bytes.Buffer
	bs := newProgress(&buf, mpb.WithAutoRefresh())

	bs.StartUnits(Line{Key: "i", Kind: KindImage, Label: "ある夏の日"}, 6)
	bs.SetUnits("i", 2)
	bs.Update("i", 2202009, 7340032)
	waitOrFail(t, bs)

	got := buf.String()
	if !strings.Contains(got, "2/6") {
		t.Errorf("image row = %q, want it to carry %q", got, "2/6")
	}
	if strings.Contains(got, "%") {
		t.Errorf("image row = %q, want no percentage", got)
	}
	if strings.Contains(got, "MiB") {
		t.Errorf("image row = %q, want no byte counters in image row", got)
	}
}

// Chapter line carries discrete step count #/N and no percentage.
func TestChapterLineCarriesCountAndNoPercentage(t *testing.T) {
	var buf bytes.Buffer
	bs := newProgress(&buf, mpb.WithAutoRefresh())

	bs.StartCount(Line{Key: "c", Kind: KindChapter, Label: "ある夏の日"}, 5)
	bs.Update("c", 2, 5)
	waitOrFail(t, bs)

	got := buf.String()
	if !strings.Contains(got, "2/5") {
		t.Errorf("chapter row = %q, want it to carry %q", got, "2/5")
	}
	if strings.Contains(got, "%") {
		t.Errorf("chapter row = %q, want no percentage", got)
	}
}

// Recording line carries percentage, bytes, and ETA.
func TestRecordingLineCarriesPercentage(t *testing.T) {
	var buf bytes.Buffer
	bs := newProgress(&buf, mpb.WithAutoRefresh())

	bs.Start(Line{Key: "r", Kind: KindRecording, Label: "ある夏の日"}, 1000)
	bs.Update("r", 500, 1000)
	waitOrFail(t, bs)

	got := buf.String()
	if !strings.Contains(got, "%") {
		t.Errorf("recording row = %q, want it to carry %%", got)
	}
}

// The counter shares the percentage's column, so a count that overran it would
// push every column after it right and cost the label its width.
func TestUnitsFitTheStatusColumn(t *testing.T) {
	for _, count := range []int64{6, 30, 100, 999} {
		if got := len(fmt.Sprintf("%d/%d", count, count)); got > statusCols {
			t.Errorf("%d units take %d columns, over the %d the status field holds", count, got, statusCols)
		}
	}
}

func TestSetUnitsWithoutStartUnitsIsIgnored(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Start(line("a.m4a"), 100)
	bs.SetUnits("a.m4a", 3)
	bs.SetUnits("ghost", 3)

	bs.Update("a.m4a", 100, 100)
	waitOrFail(t, bs)
}

func TestKindColumnFitsEveryKind(t *testing.T) {
	for _, kind := range []string{KindImage, KindRecording, KindChapter} {
		if got := len("[" + kind + "]"); got >= kindCols {
			t.Errorf("[%s] takes %d columns, which the %d-column kind field cannot pad", kind, got, kindCols)
		}
	}
}

func TestLabelColsStayWithinTheTerminal(t *testing.T) {
	for _, width := range []int{0, 40, 80, 100, 200} {
		cols := min(max(width-fixedCols, minLabelCols), maxLabelCols)
		switch {
		case cols < minLabelCols:
			t.Errorf("width %d: label column %d is under the %d-column floor", width, cols, minLabelCols)
		case cols > maxLabelCols:
			t.Errorf("width %d: label column %d is over the %d-column ceiling", width, cols, maxLabelCols)
		case width >= 80 && cols+fixedCols > width:
			t.Errorf("width %d: the line runs to %d columns", width, cols+fixedCols)
		}
	}
}

func TestLabelColsFallBackOffATerminal(t *testing.T) {
	want := min(max(assumedCols-fixedCols, minLabelCols), maxLabelCols)
	if got := labelCols(io.Discard); got != want {
		t.Fatalf("labelCols(io.Discard) = %d, want %d", got, want)
	}
}

// Progress for a name that was never started is ignored rather than panicking
// on a nil bar.
func TestUpdateWithoutStartIsIgnored(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Update("ghost.m4a", 10, 100)
	waitOrFail(t, bs)
}

// A steady transfer must read as its real speed, and read it early: an ETA
// that starts at a fraction of the true rate is worse than none.
func TestRateMeterReadsSteadySpeed(t *testing.T) {
	const perTick = 150 << 10 // 150 KiB every 150 ms
	want := float64(perTick) / 0.15

	var (
		m       rateMeter
		now     = time.Now()
		current int64
	)
	m.observe(now, current)

	for i := 1; i <= 40; i++ {
		now = now.Add(150 * time.Millisecond)
		current += perTick
		rate := m.observe(now, current)
		if off := math.Abs(rate-want) / want; off > 0.01 {
			t.Fatalf("sample %d: rate = %.0f B/s, want %.0f", i, rate, want)
		}
	}
}

// Readings that repeat inside the window are the redraw outrunning the bytes,
// not a stall, so the rate has to hold rather than bleed off.
func TestRateMeterHoldsBetweenRedraws(t *testing.T) {
	var (
		m   rateMeter
		now = time.Now()
	)
	m.observe(now, 0)
	moving := m.observe(now.Add(time.Second), 1<<20)

	now = now.Add(time.Second + 100*time.Millisecond)
	if got := m.observe(now, 1<<20); got != moving {
		t.Fatalf("rate = %.0f B/s on a repeat reading, want it held at %.0f", got, moving)
	}
}

// A connection that stops moving must bleed the rate down, or the ETA counts
// toward a finish that is not coming.
func TestRateMeterDecaysWhileStalled(t *testing.T) {
	var (
		m       rateMeter
		now     = time.Now()
		current int64
		rate    float64
	)
	m.observe(now, current)
	for range 20 {
		now = now.Add(150 * time.Millisecond)
		current += 150 << 10
		rate = m.observe(now, current)
	}

	now = now.Add(30 * time.Second)
	stalled := m.observe(now, current)
	if stalled >= rate/100 {
		t.Fatalf("stalled rate = %.0f B/s, want well under %.0f", stalled, rate/100)
	}
}

// An estimated total can walk a line backwards. That must not read as a
// negative speed, which would print an ETA in the past.
func TestRateMeterIgnoresBackwardsReadings(t *testing.T) {
	var (
		m   rateMeter
		now = time.Now()
	)
	m.observe(now, 1000)
	if rate := m.observe(now.Add(time.Second), 400); rate < 0 {
		t.Fatalf("rate = %.0f B/s, want at least 0", rate)
	}
}

func TestFormatETA(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "00:00"},
		{45 * time.Second, "00:45"},
		{90*time.Second + 400*time.Millisecond, "01:30"},
		{59*time.Minute + 59*time.Second, "59:59"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1:02:03"},
		{12*time.Hour + 34*time.Minute + 56*time.Second, "12:34:56"},
	}
	for _, c := range cases {
		if got := formatETA(c.in); got != c.want {
			t.Errorf("formatETA(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short.m4a", 28, "short.m4a"},
		{"", 5, ""},
		// Budget is terminal columns: each of these takes two, so an odd
		// budget leaves one column unusable rather than clipping a rune.
		{"あいうえお", 4, "あい"},
		{"あいうえお", 5, "あい"},
		{"あいうえお", 10, "あいうえお"},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

// The label column must stay bounded regardless of how long a title is, or the
// progress line is pushed off-screen. Double-width runes are the case that a
// rune-count budget gets wrong, so measure columns.
func TestTruncateBoundsColumns(t *testing.T) {
	cols := labelCols(io.Discard)
	long := strings.Repeat("耳", 200)
	if got := runewidth.StringWidth(truncate(long, cols)); got > cols {
		t.Fatalf("truncated to %d columns, want at most %d", got, cols)
	}
}
