// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"container/heap"
)

var (
	_ StakerIterator = &multiIterator{}
	_ heap.Interface = &multiIterator{}
)

type multiIterator struct {
	initialized bool
	heap        []StakerIterator
}

// Returns an iterator that returns the elements of [stakers] in order.
func NewMultiIterator(stakers ...StakerIterator) StakerIterator {
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

	it := &multiIterator{
		heap: stakers,
	}

	heap.Init(it)
	return it
}

func (it *multiIterator) Next() bool {
	if len(it.heap) == 0 {
		return false
	}

	if !it.initialized {
		// We call Next() on each iterator in NewMultiIterator.
		// Note that on the first call to Next() (i.e. here) we
		// don't call Next() on the current iterator.
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

func (it *multiIterator) Value() *Staker {
	return it.heap[0].Value()
}

// When Release() returns, Release() has been called on each element of [stakers].
func (it *multiIterator) Release() {
	for _, it := range it.heap {
		it.Release()
	}
	it.heap = nil
}

// Returns the number of sub-iterators in [it].
func (it *multiIterator) Len() int {
	return len(it.heap)
}

func (it *multiIterator) Less(i, j int) bool {
	return it.heap[i].Value().Less(it.heap[j].Value())
}

func (it *multiIterator) Swap(i, j int) {
	it.heap[j], it.heap[i] = it.heap[i], it.heap[j]
}

func (it *multiIterator) Push(value interface{}) {
	it.heap = append(it.heap, value.(StakerIterator))
}

func (it *multiIterator) Pop() interface{} {
	newLength := len(it.heap) - 1
	value := it.heap[newLength]
	it.heap[newLength] = nil
	it.heap = it.heap[:newLength]
	return value
}
