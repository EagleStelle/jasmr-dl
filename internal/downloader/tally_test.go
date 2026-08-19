package downloader

import "testing"

// A line reports bytes, so a set that names no total needs one projected from
// the parts already written.
func TestSizeTallyProjectsTotal(t *testing.T) {
	s := &sizeTally{count: 10}

	if units, done, total := s.stat(); units != 0 || done != 0 || total != 0 {
		t.Fatalf("stat() = %d, %d, %d before any part, want 0, 0, 0", units, done, total)
	}

	// Two of ten parts, 100 bytes each: a 1000-byte set.
	for range 2 {
		s.advance(100)
		s.finish(100)
	}
	if units, done, total := s.stat(); units != 2 || done != 200 || total != 1000 {
		t.Fatalf("stat() = %d, %d, %d, want 2, 200, 1000", units, done, total)
	}

	// A third part-way in counts on disk but not toward the mean.
	s.advance(40)
	if units, done, total := s.stat(); units != 2 || done != 240 || total != 1000 {
		t.Fatalf("stat() = %d, %d, %d mid-part, want 2, 240, 1000", units, done, total)
	}
}

// Parts running longer than the mean must not project a total under what is
// already written, which would read as over 100%.
func TestSizeTallyTotalNeverTrailsDisk(t *testing.T) {
	s := &sizeTally{count: 2}
	s.advance(100)
	s.finish(100)
	s.advance(5000) // still in flight, and far longer than the first

	_, done, total := s.stat()
	if total < done {
		t.Fatalf("stat() = %d, %d: total trails the bytes on disk", done, total)
	}
}

// A picture that fails leaves nothing on disk, so its bytes are handed back.
func TestSizeTallyRollsBackAFailedPart(t *testing.T) {
	s := &sizeTally{count: 2}
	s.advance(100)
	s.finish(100)

	s.advance(60) // a second part, abandoned part-way
	s.advance(-60)

	if _, done, total := s.stat(); done != 100 || total != 200 {
		t.Fatalf("stat() = %d, %d after a rollback, want 100, 200", done, total)
	}
}
