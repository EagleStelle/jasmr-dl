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
		s.finish(0, 100)
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

// Advertised headers project the total before downloading starts.
func TestSizeTallyProjectsFromAdvertisedHeaders(t *testing.T) {
	s := &sizeTally{count: 10}
	s.know(200) // 1 of 10 parts known to be 200 bytes
	if units, done, total := s.stat(); units != 0 || done != 0 || total != 2000 {
		t.Fatalf("stat() = %d, %d, %d from header, want 0, 0, 2000", units, done, total)
	}

	s.advance(50)
	if units, done, total := s.stat(); units != 0 || done != 50 || total != 2000 {
		t.Fatalf("stat() = %d, %d, %d while downloading, want 0, 50, 2000", units, done, total)
	}

	s.finish(200, 200)
	if units, done, total := s.stat(); units != 1 || done != 50 || total != 2000 {
		t.Fatalf("stat() = %d, %d, %d finished, want 1, 50, 2000", units, done, total)
	}
}

// A header that read short must not leave the part counted at both figures.
func TestSizeTallyCorrectsAnAdvertisedSize(t *testing.T) {
	s := &sizeTally{count: 4}
	s.know(100)
	s.advance(160)
	s.finish(100, 160)

	if _, _, total := s.stat(); total != 640 {
		t.Fatalf("stat() total = %d after a short header, want 640", total)
	}
}

// Servers that advertise a size for some parts and not others must not have one
// part's size stand in for another's.
func TestSizeTallyMixesAdvertisedAndSilentParts(t *testing.T) {
	s := &sizeTally{count: 2}
	s.know(1000) // the first part advertises

	// The second lands first, having advertised nothing.
	s.advance(500)
	s.finish(0, 500)

	s.advance(1000)
	s.finish(1000, 1000)

	if _, done, total := s.stat(); done != 1500 || total != 1500 {
		t.Fatalf("stat() = %d, %d, want 1500, 1500 once both landed", done, total)
	}
}

// Handing back a failed part must take its advertised size and nothing else.
func TestSizeTallyRollsBackOnlyTheFailedPart(t *testing.T) {
	s := &sizeTally{count: 2}
	s.know(1000) // the first part advertises, then fails

	s.advance(500)
	s.finish(0, 500) // the second lands, having advertised nothing

	s.rollback(1000)

	if units, done, total := s.stat(); units != 1 || done != 500 || total != 1000 {
		t.Fatalf("stat() = %d, %d, %d after a rollback, want 1, 500, 1000", units, done, total)
	}
}

// Parts running longer than the mean must not project a total under what is
// already written, which would read as over 100%.
func TestSizeTallyTotalNeverTrailsDisk(t *testing.T) {
	s := &sizeTally{count: 2}
	s.advance(100)
	s.finish(0, 100)
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
	s.finish(0, 100)

	s.advance(60) // a second part, abandoned part-way
	s.advance(-60)

	if _, done, total := s.stat(); done != 100 || total != 200 {
		t.Fatalf("stat() = %d, %d after a rollback, want 100, 200", done, total)
	}
}
