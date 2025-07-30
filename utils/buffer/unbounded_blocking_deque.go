// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils"
)

var _ BlockingDeque[int] = (*unboundedBlockingDeque[int])(nil)

type BlockingDeque[T any] interface {
	Deque[T]

	// Close and empty the deque.
	Close()
}

// Returns a new unbounded deque with the given initial size.
// Note that the returned deque is always empty -- [initSize] is just
// a hint to prevent unnecessary resizing.
func NewUnboundedBlockingDeque[T any](initSize int) BlockingDeque[T] {
	q := &unboundedBlockingDeque[T]{
		Deque: NewUnboundedDeque[T](initSize),
	}
	q.cond = sync.NewCond(&q.lock)
	return q
}

type unboundedBlockingDeque[T any] struct {
	lock   sync.RWMutex
	cond   *sync.Cond
	closed bool

	Deque[T]
}

// If the deque is closed returns false.
func (q *unboundedBlockingDeque[T]) PushRight(elt T) bool {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	if q.closed {
		return false
	}

	// Add the item to the queue
	q.Deque.PushRight(elt)

	// Signal a waiting thread
	q.cond.Signal()
	return true
}

// If the deque is closed returns false.
func (q *unboundedBlockingDeque[T]) PopRight() (T, bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	for {
		if q.closed {
			return utils.Zero[T](), false
		}
		if q.Deque.Len() != 0 {
			return q.Deque.PopRight()
		}
		q.cond.Wait()
	}
}

func (q *unboundedBlockingDeque[T]) PeekRight() (T, bool) {
	q.lock.RLock()
	defer q.lock.RUnlock()

	if q.closed {
		return utils.Zero[T](), false
	}
	return q.Deque.PeekRight()
}

// If the deque is closed returns false.
func (q *unboundedBlockingDeque[T]) PushLeft(elt T) bool {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	if q.closed {
		return false
	}

	// Add the item to the queue
	q.Deque.PushLeft(elt)

	// Signal a waiting thread
	q.cond.Signal()
	return true
}

// If the deque is closed returns false.
func (q *unboundedBlockingDeque[T]) PopLeft() (T, bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	for {
		if q.closed {
			return utils.Zero[T](), false
		}
		if q.Deque.Len() != 0 {
			return q.Deque.PopLeft()
		}
		q.cond.Wait()
	}
}

func (q *unboundedBlockingDeque[T]) PeekLeft() (T, bool) {
	q.lock.RLock()
	defer q.lock.RUnlock()

	if q.closed {
		return utils.Zero[T](), false
	}
	return q.Deque.PeekLeft()
}

func (q *unboundedBlockingDeque[T]) Index(i int) (T, bool) {
	q.lock.RLock()
	defer q.lock.RUnlock()

	if q.closed {
		return utils.Zero[T](), false
	}
	return q.Deque.Index(i)
}

func (q *unboundedBlockingDeque[T]) Len() int {
	q.lock.RLock()
	defer q.lock.RUnlock()

	if q.closed {
		return 0
	}
	return q.Deque.Len()
}

func (q *unboundedBlockingDeque[T]) List() []T {
	q.lock.RLock()
	defer q.lock.RUnlock()

	if q.closed {
		return nil
	}
	return q.Deque.List()
}

func (q *unboundedBlockingDeque[T]) Close() {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	if q.closed {
		return
	}

	q.Deque = nil

	// Mark the queue as closed
	q.closed = true
	q.cond.Broadcast()
}
