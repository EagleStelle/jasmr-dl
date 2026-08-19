package downloader

import (
	"context"

	"golang.org/x/sync/semaphore"
)

// Budget is the ceiling a run holds itself to, shared by every post in it. The
// host counts requests, not posts, so five posts at -j 32 have to draw on one
// budget of 32 between them rather than opening 160.
//
// Its zero value is no ceiling at all, which only a test wants.
type Budget struct {
	conns int

	files       gate // whole downloads in flight
	connections gate // ranged requests in flight
	pictures    gate // pictures in flight, off another host
}

// NewBudget builds what a run shares. files is how many downloads run at once
// across every post, connections how many ranged requests they hold between
// them.
func NewBudget(files, connections int) Budget {
	conns := clampConnections(connections)
	return Budget{
		conns:       conns,
		files:       newGate(max(files, 1)),
		connections: newGate(conns),
		pictures:    newGate(imageWorkers),
	}
}

// clampConnections holds -j inside what this host is worth opening.
func clampConnections(n int) int {
	switch {
	case n < 1:
		return defaultConnections
	case n > maxConnections:
		return maxConnections
	}
	return n
}

// connectionCount is the ceiling itself, which bounds how many workers one file
// opens: past it they would only queue.
func (b Budget) connectionCount() int {
	if b.conns < 1 {
		return defaultConnections
	}
	return b.conns
}

// gate is a ceiling on how many of one thing run at once. A gate with no
// semaphore behind it lets everything through.
type gate struct{ sem *semaphore.Weighted }

func newGate(n int) gate {
	return gate{sem: semaphore.NewWeighted(int64(n))}
}

func (g gate) enter(ctx context.Context) error {
	if g.sem == nil {
		return nil
	}
	return g.sem.Acquire(ctx, 1)
}

func (g gate) leave() {
	if g.sem != nil {
		g.sem.Release(1)
	}
}
