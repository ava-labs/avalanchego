// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import "github.com/ava-labs/avalanchego/utils"

const defaultInitSize = 32

// An unbounded deque (double-ended queue).
// See https://en.wikipedia.org/wiki/Double-ended_queue
// Not safe for concurrent access.
type Deque[T any] interface {
	// Place an element at the leftmost end of the deque.
	// Returns true if the element was placed in the deque.
	PushLeft(T) bool
	// Place an element at the rightmost end of the deque.
	// Returns true if the element was placed in the deque.
	PushRight(T) bool
	// Remove and return the leftmost element of the deque.
	// Returns false if the deque is empty.
	PopLeft() (T, bool)
	// Remove and return the rightmost element of the deque.
	// Returns false if the deque is empty.
	PopRight() (T, bool)
	// Return the leftmost element of the deque without removing it.
	// Returns false if the deque is empty.
	PeekLeft() (T, bool)
	// Return the rightmost element of the deque without removing it.
	// Returns false if the deque is empty.
	PeekRight() (T, bool)
	// Returns the element at the given index.
	// Returns false if the index is out of bounds.
	// The leftmost element is at index 0.
	Index(int) (T, bool)
	// Returns the number of elements in the deque.
	Len() int
	// Returns the elements in the deque from left to right.
	List() []T
}

// Returns a new unbounded deque with the given initial slice size.
// Note that the returned deque is always empty -- [initSize] is just
// a hint to prevent unnecessary resizing.
func NewUnboundedDeque[T any](initSize int) Deque[T] {
	if initSize < 2 {
		initSize = defaultInitSize
	}
	return &unboundedSliceDeque[T]{
		// Note that [initSize] must be >= 2 to satisfy invariants (1) and (2).
		data:  make([]T, initSize),
		right: 1,
	}
}

// Invariants after each function call and before the first call:
// (1) The next element pushed left will be placed at data[left]
// (2) The next element pushed right will be placed at data[right]
// (3) There are [size] elements in the deque.
type unboundedSliceDeque[T any] struct {
	size, left, right int
	data              []T
}

func (b *unboundedSliceDeque[T]) PushRight(elt T) bool {
	// Invariant (2) says it's safe to place the element without resizing.
	b.data[b.right] = elt
	b.size++
	b.right++
	b.right %= len(b.data)

	b.resize()
	return true
}

func (b *unboundedSliceDeque[T]) PushLeft(elt T) bool {
	// Invariant (1) says it's safe to place the element without resizing.
	b.data[b.left] = elt
	b.size++
	b.left--
	if b.left < 0 {
		b.left = len(b.data) - 1 // Wrap around
	}

	b.resize()
	return true
}

func (b *unboundedSliceDeque[T]) PopLeft() (T, bool) {
	if b.size == 0 {
		return utils.Zero[T](), false
	}
	idx := b.leftmostEltIdx()
	elt := b.data[idx]
	// Zero out to prevent memory leak.
	b.data[idx] = utils.Zero[T]()
	b.size--
	b.left++
	b.left %= len(b.data)
	return elt, true
}

func (b *unboundedSliceDeque[T]) PeekLeft() (T, bool) {
	if b.size == 0 {
		return utils.Zero[T](), false
	}
	idx := b.leftmostEltIdx()
	return b.data[idx], true
}

func (b *unboundedSliceDeque[T]) PopRight() (T, bool) {
	if b.size == 0 {
		return utils.Zero[T](), false
	}
	idx := b.rightmostEltIdx()
	elt := b.data[idx]
	// Zero out to prevent memory leak.
	b.data[idx] = utils.Zero[T]()
	b.size--
	b.right--
	if b.right < 0 {
		b.right = len(b.data) - 1 // Wrap around
	}
	return elt, true
}

func (b *unboundedSliceDeque[T]) PeekRight() (T, bool) {
	if b.size == 0 {
		return utils.Zero[T](), false
	}
	idx := b.rightmostEltIdx()
	return b.data[idx], true
}

func (b *unboundedSliceDeque[T]) Index(idx int) (T, bool) {
	if idx < 0 || idx >= b.size {
		return utils.Zero[T](), false
	}
	leftmostIdx := b.leftmostEltIdx()
	idx = (leftmostIdx + idx) % len(b.data)
	return b.data[idx], true
}

func (b *unboundedSliceDeque[T]) Len() int {
	return b.size
}

func (b *unboundedSliceDeque[T]) List() []T {
	if b.size == 0 {
		return nil
	}

	list := make([]T, b.size)
	leftmostIdx := b.leftmostEltIdx()
	if numCopied := copy(list, b.data[leftmostIdx:]); numCopied < b.size {
		// We copied all of the elements from the leftmost element index
		// to the end of the underlying slice, but we still haven't copied
		// all of the elements, so wrap around and copy the rest.
		copy(list[numCopied:], b.data[:b.right])
	}
	return list
}

func (b *unboundedSliceDeque[T]) leftmostEltIdx() int {
	if b.left == len(b.data)-1 { // Wrap around case
		return 0
	}
	return b.left + 1 // Normal case
}

func (b *unboundedSliceDeque[T]) rightmostEltIdx() int {
	if b.right == 0 {
		return len(b.data) - 1 // Wrap around case
	}
	return b.right - 1 // Normal case
}

func (b *unboundedSliceDeque[T]) resize() {
	if b.size != len(b.data) {
		return
	}
	newData := make([]T, b.size*2)
	leftmostIdx := b.leftmostEltIdx()
	copy(newData, b.data[leftmostIdx:])
	numCopied := len(b.data) - leftmostIdx
	copy(newData[numCopied:], b.data[:b.right])
	b.data = newData
	b.left = len(b.data) - 1
	b.right = b.size
}
