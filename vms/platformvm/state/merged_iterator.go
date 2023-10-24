// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "github.com/ava-labs/avalanchego/utils/heap"

var _ StakerIterator = (*mergedIterator)(nil)

type mergedIterator struct {
	initialized bool
	// heap only contains iterators that have been initialized and are not
	// exhausted.
	heap heap.Queue[StakerIterator]
}

// Returns an iterator that returns all of the elements of [stakers] in order.
func NewMergedIterator(stakers ...StakerIterator) StakerIterator {
	// Filter out iterators that are already exhausted.
	i := 0
	for i < len(stakers) {
		staker := stakers[i]
		if staker.Next() {
			i++
			continue
		}
		staker.Release()

		newLength := len(stakers) - 1
		stakers[i] = stakers[newLength]
		stakers[newLength] = nil
		stakers = stakers[:newLength]
	}

	it := &mergedIterator{
		heap: heap.QueueOf(
			func(a, b StakerIterator) bool {
				return a.Value().Less(b.Value())
			},
			stakers...,
		),
	}

	return it
}

func (it *mergedIterator) Next() bool {
	if it.heap.Len() == 0 {
		return false
	}

	if !it.initialized {
		// Note that on the first call to Next() (i.e. here) we don't call
		// Next() on the current iterator. This is because we already called
		// Next() on each iterator in NewMergedIterator.
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

func (it *mergedIterator) Value() *Staker {
	peek, _ := it.heap.Peek()
	return peek.Value()
}

func (it *mergedIterator) Release() {
	for it.heap.Len() > 0 {
		removed, _ := it.heap.Pop()
		removed.Release()
	}
}

func (it *mergedIterator) Len() int {
	return it.heap.Len()
}
