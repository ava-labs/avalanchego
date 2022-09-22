// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import "github.com/ava-labs/avalanchego/utils"

const defaultInitSize = 32

// An unbounded queue.
// Not safe for concurrent access.
type UnboundedQueue[T any] interface {
	Enqueue(T)
	// Returns false if the queue is empty.
	Dequeue() (T, bool)
	// Returns the oldest element without removing it.
	// Returns false if the queue is empty.
	PeekHead() (T, bool)
	// Returns the newest without removing it.
	// Returns false if the queue is empty.
	PeekTail() (T, bool)
	Len() int
}

// Returns a new unbounded queue with the given initial slice size.
// Note that the returned queue is always empty -- [initSize] is just
// a hint to prevent unnecessary resizing.
func NewUnboundedSliceQueue[T any](initSize int) UnboundedQueue[T] {
	if initSize <= 0 {
		initSize = defaultInitSize
	}
	return &unboundedSliceQueue[T]{
		// Note that [initSize] must be > 0 to satisfy invariant (5).
		data: make([]T, initSize),
	}
}

// Invariants after each function call and before the first call:
// (1) If head == tail then the queue is empty
// (2) If head < tail then the queue is data[head:tail]
// (3) If head > tail then the queue is data[head:len(data)] + data[0:tail]
// (4) The next element to be dequeued is data[head]
// (5) The next element will be enqueued at data[tail]
// (6) There are [size] elements in the queue.
type unboundedSliceQueue[T any] struct {
	size, head, tail int
	data             []T
}

func (b *unboundedSliceQueue[T]) Enqueue(elt T) {
	// Invariant (5) says it's safe to place the element without resizing.
	b.data[b.tail] = elt
	b.size++
	b.tail++
	b.tail %= len(b.data)

	if b.head != b.tail {
		return
	}
	// Invariant (1) says if the head and the tail are equal then the queue is empty.
	// It isn't -- we just enqueued an element -- so we need to resize to honor invariant (1).
	newData := make([]T, b.size*2)
	copy(newData, b.data[b.head:])
	numCopied := len(b.data) - b.head
	copy(newData[numCopied:], b.data[:b.tail])
	b.data = newData
	b.head = 0
	b.tail = b.size
}

func (b *unboundedSliceQueue[T]) Dequeue() (T, bool) {
	if b.size == 0 {
		return utils.Zero[T](), false
	}
	elt := b.data[b.head]
	// Zero out to prevent memory leak.
	b.data[b.head] = utils.Zero[T]()
	b.size--
	b.head++
	b.head %= len(b.data)
	return elt, true
}

func (b *unboundedSliceQueue[T]) PeekHead() (T, bool) {
	if b.size == 0 {
		return utils.Zero[T](), false
	}
	return b.data[b.head], true
}

func (b *unboundedSliceQueue[T]) PeekTail() (T, bool) {
	if b.size == 0 {
		return utils.Zero[T](), false
	}
	if b.tail == 0 {
		return b.data[len(b.data)-1], true
	}
	return b.data[b.tail-1], true
}

func (b *unboundedSliceQueue[T]) Len() int {
	return b.size
}
