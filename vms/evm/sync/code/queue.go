// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"sync"

	"github.com/ava-labs/libevm/common"
)

// queue hands hashes from producers to the batcher, which drains it directly.
// Appending never waits on the batcher.
type queue struct {
	pendingMu sync.Mutex
	pending   []common.Hash

	closeMu sync.RWMutex  // held across AddCode's write and across take(), so close waits out both
	done    chan struct{} // closing it is the close, so a waiter cannot be left asleep

	signal chan struct{} // buffered to one, so appends between two drains cost a single wakeup
}

func newQueue() *queue {
	return &queue{
		signal: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
}

// enter reports whether the queue still takes hashes.
func (q *queue) enter() bool {
	q.closeMu.RLock()
	if q.isClosed() {
		q.closeMu.RUnlock()
		return false
	}
	return true
}

// exit releases what a successful enter held. Call it once per true enter.
func (q *queue) exit() {
	q.closeMu.RUnlock()
}

// enqueue appends unconditionally. The caller holds closeMu, or runs before any
// producer does, as recovery is.
func (q *queue) enqueue(hashes []common.Hash) {
	if len(hashes) == 0 {
		return
	}

	q.pendingMu.Lock()
	q.pending = append(q.pending, hashes...)
	q.pendingMu.Unlock()

	q.wake()
}

// close stops taking hashes. Pending hashes still drain, and it is idempotent.
func (q *queue) close() {
	// Taking closeMu for writing waits out any producer already inside enter.
	q.closeMu.Lock()
	defer q.closeMu.Unlock()

	// Authoritative under the write lock, so no one can close concurrently.
	if q.isClosed() {
		return
	}
	close(q.done)
}

// wait blocks until there may be work, or ctx ends. A wakeup is not a promise
// of work, since another drain may already have taken it.
func (q *queue) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.signal:
	case <-q.done:
	}
	return nil
}

func (q *queue) isClosed() bool {
	select {
	case <-q.done:
		return true
	default:
		return false
	}
}

func (q *queue) wake() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

// take empties the queue and reports whether input is closed. Closed with
// nothing pending is the batcher's stop condition.
func (q *queue) take() ([]common.Hash, bool) {
	// Held across the same section as enter, so every read of pending and
	// done is gated by closeMu the same way.
	q.closeMu.RLock()
	defer q.closeMu.RUnlock()

	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()

	hashes := q.pending
	q.pending = nil
	return hashes, q.isClosed()
}
