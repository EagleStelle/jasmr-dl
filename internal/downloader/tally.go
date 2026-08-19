package downloader

import "sync"

// sizeTally projects a byte total from the parts already written, for a set
// whose size nothing declares up front. Parts of a near-uniform size put the
// mean of the finished ones across the count within a few percent, and it is
// exact once the last lands. Where they vary the early figure is looser, but it
// never reads under the bytes actually on disk.
type sizeTally struct {
	mu    sync.Mutex
	count int64 // parts the set holds
	done  int64 // parts fully written
	whole int64 // bytes those finished parts hold
	bytes int64 // bytes on disk, parts in flight included
}

// advance records bytes written, whether or not their part is finished.
func (s *sizeTally) advance(n int64) {
	s.mu.Lock()
	s.bytes += n
	s.mu.Unlock()
}

func (s *sizeTally) finish(size int64) {
	s.mu.Lock()
	s.whole += size
	s.done++
	s.mu.Unlock()
}

// stat returns the parts finished, the bytes on disk and the projected total,
// which is 0 until the first part finishes.
func (s *sizeTally) stat() (units, bytes, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done == 0 {
		return 0, s.bytes, 0
	}
	total = max(s.whole*s.count/s.done, s.bytes) // never under what is on disk
	return s.done, s.bytes, total
}
