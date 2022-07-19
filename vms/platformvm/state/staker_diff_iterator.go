// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"container/heap"
)

var (
	_ StakerDiffIterator = &stakerDiffIterator{}
	_ heap.Interface     = &mutableStakerIterator{}
)

// StakerDiffIterator is an iterator that iterates over the events that will be
// performed on the current staker set.
//
// The ordering of operations is:
// - Staker operations are performed in order of their [NextTime].
// - If operations have the same [NextTime], stakers are first added to the
//   current staker set, then removed.
// - Further ties are broken by *Staker.Less(), returning the lesser staker
//   first.
type StakerDiffIterator interface {
	Next() bool
	// Returns:
	// - The staker that is changing
	// - True if the staker is being added to the current staker set, false if
	//   the staker is being removed from the current staker set
	Value() (*Staker, bool)
	Release()
}

type stakerDiffIterator struct {
	currentIteratorExhausted bool
	currentIterator          *mutableStakerIterator

	pendingIteratorExhausted bool
	pendingIterator          StakerIterator

	modifiedStaker *Staker
	isAdded        bool
}

func NewStakerDiffIterator(currentIterator, pendingIterator StakerIterator) StakerDiffIterator {
	mutableCurrentIterator, currentIteratorExhausted := newMutableStakerIterator(currentIterator)
	return &stakerDiffIterator{
		currentIteratorExhausted: currentIteratorExhausted,
		currentIterator:          mutableCurrentIterator,
		pendingIteratorExhausted: !pendingIterator.Next(),
		pendingIterator:          pendingIterator,
	}
}

func (it *stakerDiffIterator) Next() bool {
	switch {
	case it.currentIteratorExhausted && it.pendingIteratorExhausted:
		return false
	case it.currentIteratorExhausted:
		it.advancePending()
	case it.pendingIteratorExhausted:
		it.advanceCurrent()
	default:
		nextStakerRemoved := it.currentIterator.Value()
		nextStakerAdded := it.pendingIterator.Value()
		// If the next operations share the same time, we default to adding the
		// staker to the current staker set. This means that we default to
		// advancing the pending iterator.
		if nextStakerRemoved.EndTime.Before(nextStakerAdded.StartTime) {
			it.advanceCurrent()
		} else {
			it.advancePending()
		}
	}
	return true
}

func (it *stakerDiffIterator) Value() (*Staker, bool) {
	return it.modifiedStaker, it.isAdded
}

func (it *stakerDiffIterator) Release() {
	it.currentIteratorExhausted = true
	it.currentIterator.Release()
	it.pendingIteratorExhausted = true
	it.pendingIterator.Release()
	it.modifiedStaker = nil
}

func (it *stakerDiffIterator) advanceCurrent() {
	it.modifiedStaker = it.currentIterator.Value()
	it.isAdded = false
	it.currentIteratorExhausted = !it.currentIterator.Next()
}

func (it *stakerDiffIterator) advancePending() {
	it.modifiedStaker = it.pendingIterator.Value()
	it.isAdded = true
	it.pendingIteratorExhausted = !it.pendingIterator.Next()

	toRemove := *it.modifiedStaker
	toRemove.NextTime = toRemove.EndTime
	toRemove.Priority = PendingToCurrentPriorities[toRemove.Priority]
	it.currentIteratorExhausted = false
	it.currentIterator.Add(&toRemove)
}

type mutableStakerIterator struct {
	iteratorExhausted bool
	iterator          StakerIterator

	addedStakerHeap []*Staker

	nextStaker *Staker
}

// TODO: This function returns a partially consumed iterator to make the
//       implementation of [Add] easier. We should figure out a better way to do
//       this.
func newMutableStakerIterator(iterator StakerIterator) (*mutableStakerIterator, bool) {
	hasFirst := iterator.Next()
	var nextStaker *Staker
	if hasFirst {
		nextStaker = iterator.Value()
	}
	return &mutableStakerIterator{
		iteratorExhausted: !iterator.Next(),
		iterator:          iterator,
		nextStaker:        nextStaker,
	}, !hasFirst
}

func (it *mutableStakerIterator) Add(staker *Staker) {
	if it.nextStaker == nil {
		it.nextStaker = staker
		return
	}
	if it.nextStaker.Less(staker) {
		heap.Push(it, staker)
		return
	}
	heap.Push(it, it.nextStaker)
	it.nextStaker = staker
}

func (it *mutableStakerIterator) Next() bool {
	switch {
	case it.iteratorExhausted && len(it.addedStakerHeap) == 0:
		it.nextStaker = nil
		return false
	case it.iteratorExhausted:
		it.nextStaker = it.addedStakerHeap[0]
		heap.Pop(it)
	case len(it.addedStakerHeap) == 0:
		it.nextStaker = it.iterator.Value()
		it.iteratorExhausted = !it.iterator.Next()
	default:
		nextIteratorStaker := it.iterator.Value()
		nextHeapStaker := it.addedStakerHeap[0]
		if nextIteratorStaker.Less(nextHeapStaker) {
			it.nextStaker = nextIteratorStaker
			it.iteratorExhausted = !it.iterator.Next()
		} else {
			it.nextStaker = nextHeapStaker
			heap.Pop(it)
		}
	}
	return true
}

func (it *mutableStakerIterator) Value() *Staker {
	return it.nextStaker
}

func (it *mutableStakerIterator) Release() {
	it.iteratorExhausted = true
	it.iterator.Release()
	it.addedStakerHeap = nil
	it.nextStaker = nil
}

func (it *mutableStakerIterator) Len() int {
	return len(it.addedStakerHeap)
}

func (it *mutableStakerIterator) Less(i, j int) bool {
	return it.addedStakerHeap[i].Less(it.addedStakerHeap[j])
}

func (it *mutableStakerIterator) Swap(i, j int) {
	it.addedStakerHeap[j], it.addedStakerHeap[i] = it.addedStakerHeap[i], it.addedStakerHeap[j]
}

func (it *mutableStakerIterator) Push(value interface{}) {
	it.addedStakerHeap = append(it.addedStakerHeap, value.(*Staker))
}

func (it *mutableStakerIterator) Pop() interface{} {
	newLength := len(it.addedStakerHeap) - 1
	value := it.addedStakerHeap[newLength]
	it.addedStakerHeap[newLength] = nil
	it.addedStakerHeap = it.addedStakerHeap[:newLength]
	return value
}
