// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"container/list"
	"context"
	"sync"
)

type waiter struct {
	ready chan<- struct{} // Closed when semaphore acquired.
}

// newSemaphore creates a new semaphore with the given
// maximum count for concurrent access.
func newSemaphore(n int32) *semaphore {
	w := &semaphore{max: n, cur: n}
	return w
}

// Weighted provides a way to bound concurrent access to a resource.
// The callers can request access with a given weight.
type semaphore struct {
	max     int32
	cur     int32
	mu      sync.Mutex
	waiters list.List
}

// Acquire acquires the semaphore, blocking until resources
// are available or ctx is done. On success, returns nil. On failure, returns
// ctx.Err() and leaves the semaphore unchanged.
//
// If ctx is already done, Acquire may still succeed without blocking.
func (s *semaphore) Acquire(ctx context.Context) error {
	s.mu.Lock()
	if s.cur > 0 && s.waiters.Len() == 0 {
		s.cur--
		s.mu.Unlock()
		return nil
	}

	ready := make(chan struct{})
	w := waiter{ready: ready}
	elem := s.waiters.PushBack(w)
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		err := ctx.Err()
		s.mu.Lock()
		defer s.mu.Unlock()
		select {
		case <-ready:
			// Acquired the semaphore after we were canceled.  Rather than trying to
			// fix up the queue, just pretend we didn't notice the cancellation.
			return nil
		default:
			isFront := s.waiters.Front() == elem
			s.waiters.Remove(elem)
			// If we're at the front and there're extra tokens left, notify other waiters.
			if isFront && s.cur > 0 {
				s.notifyWaiters()
			}
			return err
		}
	case <-ready:
		return nil
	}
}

// TryAcquire acquires the semaphore with a weight of n without blocking.
// On success, returns true. On failure, returns false and leaves the semaphore unchanged.
func (s *semaphore) TryAcquire() bool {
	if s.cur == 0 || s.waiters.Len() > 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	success := s.cur > 0 && s.waiters.Len() == 0
	if success {
		s.cur--
	}
	return success
}

// Release releases the semaphore
func (s *semaphore) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cur++
	if s.cur > s.max {
		panic("semaphore: released more than held")
	}
	s.notifyWaiters()
}

func (s *semaphore) notifyWaiters() {
	next := s.waiters.Front()
	for next != nil && s.cur > 0 {
		w := next.Value.(waiter)
		s.cur--
		s.waiters.Remove(next)
		close(w.ready)
		next = s.waiters.Front()
	}
}
