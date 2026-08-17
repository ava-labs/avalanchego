// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"sync"

	"github.com/ava-labs/libevm/common"
)

// queue hands hashes from producers to the batcher, which drains it directly.
// Reserving an intake slot waits, appending a reserved one does not.
type queue struct {
	bound int // ceiling on pending plus reserved

	pendingMu sync.Mutex
	pending   []common.Hash
	reserved  int           // promised to a producer that has not appended yet
	room      chan struct{} // closed and replaced under pendingMu, so a drain wakes every waiter

	closeMu sync.RWMutex  // held across AddCode's write and across take(), so close waits out both
	done    chan struct{} // closing it is the close, so a waiter cannot be left asleep

	signal chan struct{} // buffered to one, so appends between two drains cost a single wakeup
}

func newQueue(bound int) *queue {
	return &queue{
		bound:  bound,
		room:   make(chan struct{}),
		signal: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
}

// reserve holds n intake slots, waiting until they are free. It returns nil
// holding them or an error holding nothing, and pairs with one release of n.
func (q *queue) reserve(ctx context.Context, n int) error {
	// Nothing to hold, and recovery can leave pending above the bound.
	if n == 0 {
		return nil
	}

	for {
		taken, room := q.tryReserve(n)
		if taken {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.done:
			return ErrInputClosed
		case <-room:
		}
	}
}

// tryReserve is reserve without the wait. Handle and check share one critical
// section, or a drain landing between them is a lost wakeup.
func (q *queue) tryReserve(n int) (bool, <-chan struct{}) {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()

	if q.hasRoomFor(n) {
		q.reserved += n
		return true, nil
	}
	return false, q.room
}

// hasRoomFor reports whether n slots are free. The caller holds pendingMu.
func (q *queue) hasRoomFor(n int) bool {
	// A call over the bound would wait on room that can never appear. Both
	// counters, since a reservation never shows up in pending.
	if n > q.bound {
		return len(q.pending) == 0 && q.reserved == 0
	}
	return len(q.pending)+q.reserved+n <= q.bound
}

// signalRoom releases every waiter. The caller holds pendingMu.
func (q *queue) signalRoom() {
	close(q.room)
	q.room = make(chan struct{})
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

// enqueue appends hashes and hands back the slots they came from, in one critical
// section so occupancy never dips. Recovery passes zero, holding none.
func (q *queue) enqueue(hashes []common.Hash, slots int) {
	q.pendingMu.Lock()
	// Clamped, so a stray release cannot compound into a higher ceiling.
	q.reserved = max(0, q.reserved-slots)
	q.pending = append(q.pending, hashes...)
	// Holding more slots than it queued frees the difference, and only this
	// signals it, since nothing else knows the surplus existed.
	if slots > len(hashes) {
		q.signalRoom()
	}
	q.pendingMu.Unlock()

	// An empty call queued nothing to wake the batcher for.
	if len(hashes) > 0 {
		q.wake()
	}
}

// release hands back n slots, queueing nothing.
func (q *queue) release(n int) {
	q.enqueue(nil, n)
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
	// Only a drain that removed something frees a slot.
	if len(hashes) > 0 {
		q.signalRoom()
	}
	return hashes, q.isClosed()
}
