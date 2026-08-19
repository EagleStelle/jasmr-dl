package downloader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionsClampToTheCeiling(t *testing.T) {
	for _, tc := range []struct{ set, want int }{
		{0, defaultConnections},
		{-5, defaultConnections},
		{1, 1},
		{32, 32},
		{maxConnections, maxConnections},
		{maxConnections + 1, maxConnections},
	} {
		if got := NewBudget(1, tc.set).connectionCount(); got != tc.want {
			t.Errorf("connections=%d gave %d, want %d", tc.set, got, tc.want)
		}
	}
	if got := (Budget{}).connectionCount(); got != defaultConnections {
		t.Errorf("no budget gave %d, want %d", got, defaultConnections)
	}
}

func TestPiecesTileTheFileExactly(t *testing.T) {
	for _, total := range []int64{1, pieceSize - 1, pieceSize, pieceSize + 1, 200 << 20} {
		n := pieceCount(total)
		if n < 1 {
			t.Fatalf("total %d gave %d pieces", total, n)
		}

		var next int64
		for i := range n {
			start, end := pieceRange(i, total)
			if start != next {
				t.Errorf("total %d: piece %d starts at %d, want %d", total, i, start, next)
			}
			if end < start {
				t.Errorf("total %d: piece %d is empty (%d-%d)", total, i, start, end)
			}
			next = end + 1
		}
		if next != total {
			t.Errorf("total %d: pieces cover %d bytes", total, next)
		}
	}
}

func TestWorthChunkingSkipsSmallFiles(t *testing.T) {
	if worthChunking(minChunk - 1) {
		t.Error("a file under minChunk should stream in one request")
	}
	if !worthChunking(minChunk) {
		t.Error("a file at minChunk should split")
	}
	// An unknown length arrives as -1.
	if worthChunking(-1) {
		t.Error("an unknown length should stream in one request")
	}
}

func TestPieceStateRemembersAcrossOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.part"+stateSuffix)
	const total = int64(10 * pieceSize)

	s, err := openPieceState(path, total)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, i := range []int{0, 3, 9} {
		if err := s.set(i); err != nil {
			t.Fatalf("set %d: %v", i, err)
		}
	}
	if got, want := s.bytesHeld(total), int64(3*pieceSize); got != want {
		t.Errorf("bytesHeld = %d, want %d", got, want)
	}
	s.Close()

	again, err := openPieceState(path, total)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	for i := range pieceCount(total) {
		want := i == 0 || i == 3 || i == 9
		if again.has(i) != want {
			t.Errorf("after reopen, piece %d held = %v, want %v", i, again.has(i), want)
		}
	}
}

// State from a different download must not be taken as a resume point, or its
// pieces would be spliced into the wrong file.
func TestPieceStateDiscardsAnotherDownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.part"+stateSuffix)

	s, err := openPieceState(path, 10*pieceSize)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.set(2); err != nil {
		t.Fatalf("set: %v", err)
	}
	s.Close()

	again, err := openPieceState(path, 20*pieceSize)
	if err != nil {
		t.Fatalf("reopen at a new size: %v", err)
	}
	defer again.Close()
	if again.has(2) {
		t.Error("a state file written for another total was adopted")
	}
	if got := again.bytesHeld(20 * pieceSize); got != 0 {
		t.Errorf("bytesHeld = %d, want 0", got)
	}
}

func TestPieceStateStartsFreshOnGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.part"+stateSuffix)
	if err := os.WriteFile(path, []byte("not a state file at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := openPieceState(path, 10*pieceSize)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if got := s.bytesHeld(10 * pieceSize); got != 0 {
		t.Errorf("bytesHeld = %d, want 0", got)
	}
}
