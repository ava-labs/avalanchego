// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import "errors"

var (
	_ Queue[struct{}] = (*boundedQueue[struct{}])(nil)

	errInvalidMaxSize = errors.New("maxSize must be greater than 0")
)

// A FIFO queue.
type Queue[T any] interface {
	// Pushes [elt] onto the queue.
	// If the queue is full, the oldest element is evicted to make space.
	Push(T)
	// Pops the oldest element from the queue.
	// Returns false if the queue is empty.
	Pop() (T, bool)
	// Returns the oldest element without removing it.
	// Returns false if the queue is empty.
	Peek() (T, bool)
	// Returns the element at the given index without removing it.
	// Index(0) returns the oldest element.
	// Index(Len() - 1) returns the newest element.
	// Returns false if there is no element at that index.
	Index(int) (T, bool)
	// Returns the number of elements in the queue.
	Len() int
	// Returns the queue elements from oldest to newest.
	// This is an O(n) operation and should be used sparingly.
	List() []T
}

// Keeps up to [maxSize] entries in an ordered buffer
// and calls [onEvict] on any item that is evicted.
// Not safe for concurrent use.
type boundedQueue[T any] struct {
	deque   Deque[T]
	maxSize int
	onEvict func(T)
}

// Returns a new bounded, non-blocking queue that holds up to [maxSize] elements.
// When an element is evicted, [onEvict] is called with the evicted element.
// If [onEvict] is nil, this is a no-op.
// [maxSize] must be >= 1.
// Not safe for concurrent use.
func NewBoundedQueue[T any](maxSize int, onEvict func(T)) (Queue[T], error) {
	if maxSize < 1 {
		return nil, errInvalidMaxSize
	}
	return &boundedQueue[T]{
		deque:   NewUnboundedDeque[T](maxSize + 1), // +1 so we never resize
		maxSize: maxSize,
		onEvict: onEvict,
	}, nil
}

func (b *boundedQueue[T]) Push(elt T) {
	if b.deque.Len() == b.maxSize {
		evicted, _ := b.deque.PopLeft()
		if b.onEvict != nil {
			b.onEvict(evicted)
		}
	}
	_ = b.deque.PushRight(elt)
}

func (b *boundedQueue[T]) Pop() (T, bool) {
	return b.deque.PopLeft()
}

func (b *boundedQueue[T]) Peek() (T, bool) {
	return b.deque.PeekLeft()
}

func (b *boundedQueue[T]) Index(i int) (T, bool) {
	return b.deque.Index(i)
}

func (b *boundedQueue[T]) Len() int {
	return b.deque.Len()
}

func (b *boundedQueue[T]) List() []T {
	return b.deque.List()
}
