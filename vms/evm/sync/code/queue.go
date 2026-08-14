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

	// Held for reading across an admit, so close waits out producers already
	// inside one. Closing done is the close, so a waiter cannot be left asleep.
	closeMu sync.RWMutex
	done    chan struct{}

	// Buffered to one, so appends between two drains cost a single wakeup.
	signal chan struct{}
}

func newQueue() *queue {
	return &queue{signal: make(chan struct{}, 1), done: make(chan struct{})}
}

// admit persists hashes through write and queues them as one step against close,
// so a refused call leaves nothing behind.
func (q *queue) admit(hashes []common.Hash, write func() error) error {
	q.closeMu.RLock()
	defer q.closeMu.RUnlock()

	if q.isClosed() {
		return ErrInputClosed
	}
	if err := write(); err != nil {
		return err
	}

	q.enqueue(hashes)
	return nil
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
	// Taking closeMu for writing waits out admits already inside one, so done is
	// only closed once every accepted hash is pending.
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
	// Both in one critical section, so the batcher cannot pair an empty queue
	// from before an append with a closed flag from after it.
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()

	hashes := q.pending
	q.pending = nil
	return hashes, q.isClosed()
}
