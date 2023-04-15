// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"container/heap"
)

var (
	_ StakerIterator = (*mergedIterator)(nil)
	_ heap.Interface = (*mergedIterator)(nil)
)

type mergedIterator struct {
	initialized bool
	// heap only contains iterators that have been initialized and are not
	// exhausted.
	heap []StakerIterator
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
		heap: stakers,
	}

	heap.Init(it)
	return it
}

func (it *mergedIterator) Next() bool {
	if len(it.heap) == 0 {
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
	current := it.heap[0]
	if current.Next() {
		// Calling Next() above modifies [current] so we fix the heap.
		heap.Fix(it, 0)
		return true
	}

	// The old root is exhausted. Remove it from the heap.
	current.Release()
	heap.Pop(it)
	return len(it.heap) > 0
}

func (it *mergedIterator) Value() *Staker {
	return it.heap[0].Value()
}

// When Release() returns, Release() has been called on each element of
// [stakers].
func (it *mergedIterator) Release() {
	for _, it := range it.heap {
		it.Release()
	}
	it.heap = nil
}

// Returns the number of sub-iterators in [it].
func (it *mergedIterator) Len() int {
	return len(it.heap)
}

func (it *mergedIterator) Less(i, j int) bool {
	return it.heap[i].Value().Less(it.heap[j].Value())
}

func (it *mergedIterator) Swap(i, j int) {
	it.heap[j], it.heap[i] = it.heap[i], it.heap[j]
}

// Push is never actually used - but we need it to implement heap.Interface.
func (it *mergedIterator) Push(value interface{}) {
	it.heap = append(it.heap, value.(StakerIterator))
}

func (it *mergedIterator) Pop() interface{} {
	newLength := len(it.heap) - 1
	value := it.heap[newLength]
	it.heap[newLength] = nil
	it.heap = it.heap[:newLength]
	return value
}
