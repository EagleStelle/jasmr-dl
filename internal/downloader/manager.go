package downloader

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Result records the outcome of one job. Paths holds more than one file where
// a stream was split into chapters.
type Result struct {
	Job   Job
	Paths []string
	Err   error
}

// Run downloads jobs, as many at once as the budget allows, each split into
// ranged pieces drawing on that same budget. Every post in a run shares one, so
// the files and the requests in flight hold to their ceilings however many
// posts are arranged behind them.
//
// A failed job does not cancel its siblings, so one dead link cannot abandon a
// good album. Cancellation still propagates.
func (d *Downloader) Run(ctx context.Context, jobs []Job) []Result {
	results := make([]Result, len(jobs))

	g, gctx := errgroup.WithContext(ctx)

	var mu sync.Mutex

	for i, job := range jobs {
		g.Go(func() error {
			// Small stagger so a run does not open every connection in
			// the same millisecond. Before the slot, not holding one.
			err := sleepCtx(gctx, jitter())
			if err == nil {
				err = d.Budget.files.enter(gctx)
			}
			if err != nil {
				mu.Lock()
				results[i] = Result{Job: job, Err: err}
				mu.Unlock()
				return nil
			}
			defer d.Budget.files.leave()

			paths, err := d.Download(gctx, job)

			mu.Lock()
			results[i] = Result{Job: job, Paths: paths, Err: err}
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait() // workers never return an error; failures live in results
	return results
}

func jitter() time.Duration {
	return time.Duration(rand.Intn(400)) * time.Millisecond
}
