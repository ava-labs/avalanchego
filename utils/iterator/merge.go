// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import (
	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/utils/heap"
)

var _ Iterator[any] = (*merged[any])(nil)

type merged[T any] struct {
	initialized bool
	// heap only contains iterators that have been initialized and are not
	// exhausted.
	heap heap.Queue[Iterator[T]]
}

// Merge returns an iterator that returns all of the elements of [iterators] in
// order.
func Merge[T any](less btree.LessFunc[T], iterators ...Iterator[T]) Iterator[T] {
	// Filter out iterators that are already exhausted.
	i := 0
	for i < len(iterators) {
		it := iterators[i]
		if it.Next() {
			i++
			continue
		}
		it.Release()

		newLength := len(iterators) - 1
		iterators[i] = iterators[newLength]
		iterators[newLength] = nil
		iterators = iterators[:newLength]
	}

	it := &merged[T]{
		heap: heap.QueueOf(
			func(a, b Iterator[T]) bool {
				return less(a.Value(), b.Value())
			},
			iterators...,
		),
	}

	return it
}

func (it *merged[_]) Next() bool {
	if it.heap.Len() == 0 {
		return false
	}

	if !it.initialized {
		// Note that on the first call to Next() (i.e. here) we don't call
		// Next() on the current iterator. This is because we already called
		// Next() on each iterator in Merge.
		it.initialized = true
		return true
	}

	// Update the heap root.
	current, _ := it.heap.Peek()
	if current.Next() {
		// Calling Next() above modifies [current] so we fix the heap.
		it.heap.Fix(0)
		return true
	}

	// The old root is exhausted. Remove it from the heap.
	current.Release()
	it.heap.Pop()
	return it.heap.Len() > 0
}

func (it *merged[T]) Value() T {
	peek, _ := it.heap.Peek()
	return peek.Value()
}

func (it *merged[_]) Release() {
	for it.heap.Len() > 0 {
		removed, _ := it.heap.Pop()
		removed.Release()
	}
}
