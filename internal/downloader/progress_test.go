package downloader

import (
	"io"
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
