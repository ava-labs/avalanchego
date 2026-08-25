// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefetch

import (
	"sync"

	"github.com/ava-labs/libevm/core/state"
)

type boundedWorkers struct {
	workerSpawner      chan struct{}
	outstandingWorkers sync.WaitGroup

	work      chan func()
	workClose sync.Once
}

// newBoundedWorkers returns an instance of [boundedWorkers] that
// will spawn up to count goroutines.
func newBoundedWorkers(count int) *boundedWorkers {
	return &boundedWorkers{
		workerSpawner: make(chan struct{}, count),
		work:          make(chan func()),
	}
}

// startWorker creates a new goroutine to execute [f] immediately and then keeps the goroutine
// alive to continue executing new work.
func (b *boundedWorkers) startWorker(f func()) {
	b.outstandingWorkers.Add(1)

	go func() {
		defer b.outstandingWorkers.Done()

		if f != nil {
			f()
		}
		for f := range b.work {
			f()
		}
	}()
}

// Execute the given function on an existing goroutine waiting for more work or
// a new goroutine.
//
// Execute must not be called after Done, otherwise it might panic.
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
// Done can only be called after ALL calls to [Execute] have returned.
//
// It is safe to call Done multiple times but not safe to call [Execute]
// after [Done] has been called.
func (b *boundedWorkers) Done() {
	b.workClose.Do(func() {
		close(b.work)
	})
	b.outstandingWorkers.Wait()
}

// WithConcurrentWorkers sets the maximum number of goroutines that trie
// prefetching uses to load nodes.
func WithConcurrentWorkers(prefetchers int) state.PrefetcherOption {
	pool := newBoundedWorkers(prefetchers)
	return state.WithWorkerPools(func() state.WorkerPool { return pool })
}
