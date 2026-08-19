package downloader

import "sync"

// sizeTally projects a byte total from the parts whose size is known, whether a
// header advertised it or the part landed, for a set whose size nothing
// declares up front. Parts of a near-uniform size put the mean across the count
// within a few percent, and it is exact once every size is known. Where they
// vary the early figure is looser, but it never reads under the bytes actually
// on disk.
type sizeTally struct {
	mu    sync.Mutex
	count int64 // parts the set holds
	done  int64 // parts fully written
	known int64 // parts whose size is known
	whole int64 // bytes those known parts hold
	bytes int64 // bytes on disk, parts in flight included
}

// know records a size a header advertised, before the bytes arrive.
func (s *sizeTally) know(size int64) {
	if size <= 0 {
		return
	}
	s.mu.Lock()
	s.known++
	s.whole += size
	s.mu.Unlock()
}

// advance records bytes written, whether or not their part is finished.
func (s *sizeTally) advance(n int64) {
	s.mu.Lock()
	s.bytes += n
	s.mu.Unlock()
}

// finish records a part fully written. advertised is what know was told for
// this same part, 0 where nothing was, so a header that read short or long is
// corrected rather than counted twice.
func (s *sizeTally) finish(advertised, size int64) {
	s.mu.Lock()
	s.done++
	switch {
	case advertised > 0:
		s.whole += size - advertised
	case size > 0:
		s.known++
		s.whole += size
	}
	s.mu.Unlock()
}

// rollback drops a part that advertised a size but never landed.
func (s *sizeTally) rollback(advertised int64) {
	if advertised <= 0 {
		return
	}
	s.mu.Lock()
	s.known--
	s.whole -= advertised
	s.mu.Unlock()
}

// stat returns the parts finished, the bytes on disk and the projected total,
// which is 0 until some part's size is known.
func (s *sizeTally) stat() (units, bytes, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.known == 0 {
		return s.done, s.bytes, 0
	}
	total = max(s.whole*s.count/s.known, s.bytes) // never under what is on disk
	return s.done, s.bytes, total
}
