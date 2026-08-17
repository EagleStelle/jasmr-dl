package downloader

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// Result records the outcome of one job.
type Result struct {
	Job  Job
	Path string
	Err  error
}

// Run downloads concurrency files at once, each split across ranged chunks.
//
// A failed job does not cancel its siblings, so one dead link cannot abandon a
// good album. Cancellation still propagates.
func (d *Downloader) Run(ctx context.Context, jobs []Job, concurrency int) []Result {
	if len(jobs) > 0 && concurrency > len(jobs) {
		concurrency = len(jobs)
	}
	concurrency, d.chunks = connectionPlan(concurrency)
	d.budget = semaphore.NewWeighted(maxConnections)

	results := make([]Result, len(jobs))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	var mu sync.Mutex

	for i, job := range jobs {
		g.Go(func() error {
			// Small stagger so a run does not open every connection in
			// the same millisecond.
			if err := sleepCtx(gctx, jitter()); err != nil {
				mu.Lock()
				results[i] = Result{Job: job, Err: err}
				mu.Unlock()
				return nil
			}

			path, err := d.Download(gctx, job)

			mu.Lock()
			results[i] = Result{Job: job, Path: path, Err: err}
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
