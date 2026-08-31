// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefetch

import (
	"sync"

	"github.com/ava-labs/libevm/core/state"
)

// WithConcurrentWorkers sets the maximum number of goroutines that trie
// prefetching uses to load nodes.
func WithConcurrentWorkers(prefetchers int) state.PrefetcherOption {
	// libevm calls the constructor one time for each prefetcher, and shares the
	// pool between that prefetcher's tries, so the option is reusable.
	return state.WithWorkerPool(func() state.WorkerPool {
		return newBoundedWorkers(prefetchers)
	})
}

type boundedWorkers struct {
	workerSpawner      chan struct{}
	outstandingWorkers sync.WaitGroup

	work      chan func()
	workClose sync.Once
}

// newBoundedWorkers returns a pool that starts a maximum of count goroutines.
func newBoundedWorkers(count int) *boundedWorkers {
	return &boundedWorkers{
		workerSpawner: make(chan struct{}, count),
		work:          make(chan func()),
	}
}

// startWorker starts a goroutine. The goroutine runs f, then runs more work
// until [boundedWorkers.Done] closes the work channel.
func (b *boundedWorkers) startWorker(f func()) {
	b.outstandingWorkers.Go(func() {
		f()
		for f := range b.work {
			f()
		}
	})
}

// Execute runs f on an idle goroutine. If no goroutine is idle, Execute starts
// a new one, or waits if the pool is at its limit.
//
// Do not call Execute after [boundedWorkers.Done]. Execute can panic.
func (b *boundedWorkers) Execute(f func()) {
	// Ensure we feed idle workers first
	select {
	case b.work <- f:
		return
	default:
	}

	// Fallback to waiting for an idle worker or allocating
	// a new worker (if we aren't yet at max concurrency)
	select {
	case b.work <- f:
	case b.workerSpawner <- struct{}{}:
		b.startWorker(f)
	}
}

// Done returns after all enqueued work finishes and all goroutines exit.
//
// Done is safe to be called multiple times, but MUST be called after ALL calls
// to [boundedWorkers.Execute] have returned.
func (b *boundedWorkers) Done() {
	b.workClose.Do(func() {
		close(b.work)
	})
	b.outstandingWorkers.Wait()
}
