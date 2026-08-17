package downloader

import (
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
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

func TestWaitReturnsWhenBarsComplete(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Start("a.m4a", 100)
	bs.Update("a.m4a", 100, 100)
	waitOrFail(t, bs)
}

// A failed transfer leaves its bar short of total. mpb.Progress.Wait blocks
// until every bar completes, so Wait must abort those or the program hangs
// after any download error.
func TestWaitReturnsWhenTransferFailedMidway(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Start("a.m4a", 1000)
	bs.Update("a.m4a", 300, 1000)
	waitOrFail(t, bs)
}

// A bar created before the size is known must adopt the real total once it
// arrives, otherwise it can never reach completion.
func TestUnknownTotalIsAdoptedLater(t *testing.T) {
	bs := NewProgress(io.Discard)
	bs.Start("a.m4a", -1)

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
	bs.Start("a.m4a", 100)
	bs.Start("a.m4a", 100)

	if len(bs.lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(bs.lines))
	}
	bs.Update("a.m4a", 100, 100)
	waitOrFail(t, bs)
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
	long := strings.Repeat("耳", 200)
	if got := runewidth.StringWidth(truncate(long, nameCols)); got > nameCols {
		t.Fatalf("truncated to %d columns, want at most %d", got, nameCols)
	}
}
