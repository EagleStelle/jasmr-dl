package downloader

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// Result records the outcome of one job. Paths holds more than one file where
// a stream was split into chapters.
type Result struct {
	Job   Job
	Paths []string
	Err   error
}

// Run downloads concurrency files at once, each split into ranged pieces. Every
// file's pieces draw on one budget, so the requests in flight hold to the
// ceiling however the files are arranged behind it.
//
// A failed job does not cancel its siblings, so one dead link cannot abandon a
// good album. Cancellation still propagates.
func (d *Downloader) Run(ctx context.Context, jobs []Job, concurrency int) []Result {
	if len(jobs) > 0 && concurrency > len(jobs) {
		concurrency = len(jobs)
	}
	if concurrency < 1 {
		concurrency = 1
	}
	d.budget = semaphore.NewWeighted(int64(d.connections()))

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
