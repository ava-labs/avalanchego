// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"container/heap"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ StakerDiffIterator = (*stakerDiffIterator)(nil)
	_ StakerIterator     = (*mutableStakerIterator)(nil)
	_ heap.Interface     = (*mutableStakerIterator)(nil)
)

// StakerDiffIterator is an iterator that iterates over the events that will be
// performed on the current staker set.
//
// There are two event types affecting current staker set, removal of an
// existing staker and addition of a new staker from the pending set.
//
// The ordering of operations is:
//   - Staker operations are performed in order of their [NextTime].
//   - If operations have the same [NextTime], stakers are first added to the
//     current staker set, then removed.
//   - Further ties are broken by *Staker.Less(), returning the lesser staker
//     first.
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
	mutableCurrentIterator := newMutableStakerIterator(currentIterator)
	return &stakerDiffIterator{
		currentIteratorExhausted: !mutableCurrentIterator.Next(),
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
	toRemove.Priority = txs.PendingToCurrentPriorities[toRemove.Priority]
	it.currentIteratorExhausted = false
	it.currentIterator.Add(&toRemove)
}

type mutableStakerIterator struct {
	iteratorExhausted bool
	iterator          StakerIterator
	heap              []*Staker
}

func newMutableStakerIterator(iterator StakerIterator) *mutableStakerIterator {
	return &mutableStakerIterator{
		iteratorExhausted: !iterator.Next(),
		iterator:          iterator,
	}
}

// Add should not be called until after Next has been called at least once.
func (it *mutableStakerIterator) Add(staker *Staker) {
	heap.Push(it, staker)
}

func (it *mutableStakerIterator) Next() bool {
	// The only time the heap should be empty - is when the iterator is
	// exhausted or uninitialized.
	if len(it.heap) > 0 {
		heap.Pop(it)
	}

	// If the iterator is exhausted, the only elements left to iterate over are
	// in the heap.
	if it.iteratorExhausted {
		return len(it.heap) > 0
	}

	// If the heap doesn't contain the next staker to return, we need to move
	// the next element from the iterator into the heap.
	nextIteratorStaker := it.iterator.Value()
	if len(it.heap) == 0 || nextIteratorStaker.Less(it.heap[0]) {
		it.Add(nextIteratorStaker)
		it.iteratorExhausted = !it.iterator.Next()
	}
	return true
}

func (it *mutableStakerIterator) Value() *Staker {
	return it.heap[0]
}

func (it *mutableStakerIterator) Release() {
	it.iteratorExhausted = true
	it.iterator.Release()
	it.heap = nil
}

func (it *mutableStakerIterator) Len() int {
	return len(it.heap)
}

func (it *mutableStakerIterator) Less(i, j int) bool {
	return it.heap[i].Less(it.heap[j])
}

func (it *mutableStakerIterator) Swap(i, j int) {
	it.heap[j], it.heap[i] = it.heap[i], it.heap[j]
}

func (it *mutableStakerIterator) Push(value interface{}) {
	it.heap = append(it.heap, value.(*Staker))
}

func (it *mutableStakerIterator) Pop() interface{} {
	newLength := len(it.heap) - 1
	value := it.heap[newLength]
	it.heap[newLength] = nil
	it.heap = it.heap[:newLength]
	return value
}
