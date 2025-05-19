// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"sync"
	"sync/atomic"
)

type BoundedWorkers struct {
	workerCount        atomic.Int32
	workerSpawner      chan struct{}
	outstandingWorkers sync.WaitGroup

	work      chan func()
	workClose sync.Once
}

// NewBoundedWorkers returns an instance of [BoundedWorkers] that
// will spawn up to [max] goroutines.
func NewBoundedWorkers(max int) *BoundedWorkers {
	return &BoundedWorkers{
		workerSpawner: make(chan struct{}, max),
		work:          make(chan func()),
	}
}

// startWorker creates a new goroutine to execute [f] immediately and then keeps the goroutine
// alive to continue executing new work.
func (b *BoundedWorkers) startWorker(f func()) {
	b.workerCount.Add(1)
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

// Execute the given function on an existing goroutine waiting for more work, a new goroutine,
// or return if the context is canceled.
//
// Execute must not be called after Wait, otherwise it might panic.
func (b *BoundedWorkers) Execute(f func()) {
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

// Wait returns after all enqueued work finishes and all goroutines to exit.
// Wait returns the number of workers that were spawned during the run.
//
// Wait can only be called after ALL calls to [Execute] have returned.
//
// It is safe to call Wait multiple times but not safe to call [Execute]
// after [Wait] has been called.
func (b *BoundedWorkers) Wait() int {
	b.workClose.Do(func() {
		close(b.work)
	})
	b.outstandingWorkers.Wait()
	return int(b.workerCount.Load())
}
