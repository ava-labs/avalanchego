// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils"
)

var _ UnboundedBlockingQueue[int] = &unboundedBlockingQueue[int]{}

type UnboundedBlockingQueue[T any] interface {
	UnboundedQueue[T]

	// Close and empty the queue.
	Close()
}

// Returns a new unbounded queue with the given initial size.
func NewUnboundedBlockingQueue[T any](queue UnboundedQueue[T]) UnboundedBlockingQueue[T] {
	q := &unboundedBlockingQueue[T]{
		UnboundedQueue: queue,
	}
	q.cond = sync.NewCond(&q.lock)
	return q
}

type unboundedBlockingQueue[T any] struct {
	lock   sync.RWMutex
	cond   *sync.Cond
	closed bool

	UnboundedQueue[T]
}

// Enqueue pushes element in to the queue.
// If the queue is closed returns false.
func (q *unboundedBlockingQueue[T]) Enqueue(elt T) bool {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	if q.closed {
		return false
	}

	// Add the item to the queue
	q.UnboundedQueue.Enqueue(elt)

	// Signal a waiting thread
	q.cond.Signal()
	return true
}

// It will hold the lock and wait if there is no element in the queue.
// Returns false if the queue is closed.
func (q *unboundedBlockingQueue[T]) Dequeue() (T, bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	for {
		if q.closed {
			return utils.Zero[T](), false
		}
		if q.UnboundedQueue.Len() != 0 {
			return q.UnboundedQueue.Dequeue()
		}
		q.cond.Wait()
	}
}

func (q *unboundedBlockingQueue[T]) PeekHead() (T, bool) {
	q.lock.RLock()
	defer q.lock.RUnlock()

	if q.closed {
		return utils.Zero[T](), false
	}
	return q.UnboundedQueue.PeekHead()
}

func (q *unboundedBlockingQueue[T]) PeekTail() (T, bool) {
	q.lock.RLock()
	defer q.lock.RUnlock()

	if q.closed {
		return utils.Zero[T](), false
	}
	return q.UnboundedQueue.PeekTail()
}

func (q *unboundedBlockingQueue[T]) Len() int {
	q.lock.RLock()
	defer q.lock.RUnlock()

	if q.closed {
		return 0
	}
	return q.UnboundedQueue.Len()
}

func (q *unboundedBlockingQueue[T]) Close() {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	if q.closed {
		return
	}

	q.UnboundedQueue = nil

	// Mark the queue as closed
	q.closed = true
	q.cond.Broadcast()
}
