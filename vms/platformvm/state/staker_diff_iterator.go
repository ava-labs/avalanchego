// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ StakerDiffIterator         = (*stakerDiffIterator)(nil)
	_ iterator.Iterator[*Staker] = (*mutableStakerIterator)(nil)
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
	pendingIterator          iterator.Iterator[*Staker]

	modifiedStaker *Staker
	isAdded        bool
}

func NewStakerDiffIterator(currentIterator, pendingIterator iterator.Iterator[*Staker]) StakerDiffIterator {
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
	iterator          iterator.Iterator[*Staker]
	heap              heap.Queue[*Staker]
}

func newMutableStakerIterator(iterator iterator.Iterator[*Staker]) *mutableStakerIterator {
	return &mutableStakerIterator{
		iteratorExhausted: !iterator.Next(),
		iterator:          iterator,
		heap:              heap.NewQueue((*Staker).Less),
	}
}

// Add should not be called until after Next has been called at least once.
func (it *mutableStakerIterator) Add(staker *Staker) {
	it.heap.Push(staker)
}

func (it *mutableStakerIterator) Next() bool {
	// The only time the heap should be empty - is when the iterator is
	// exhausted or uninitialized.
	if it.heap.Len() > 0 {
		it.heap.Pop()
	}

	// If the iterator is exhausted, the only elements left to iterate over are
	// in the heap.
	if it.iteratorExhausted {
		return it.heap.Len() > 0
	}

	// If the heap doesn't contain the next staker to return, we need to move
	// the next element from the iterator into the heap.
	nextIteratorStaker := it.iterator.Value()
	peek, ok := it.heap.Peek()
	if !ok || nextIteratorStaker.Less(peek) {
		it.Add(nextIteratorStaker)
		it.iteratorExhausted = !it.iterator.Next()
	}
	return true
}

func (it *mutableStakerIterator) Value() *Staker {
	peek, _ := it.heap.Peek()
	return peek
}

func (it *mutableStakerIterator) Release() {
	it.iteratorExhausted = true
	it.iterator.Release()
	it.heap = heap.NewQueue((*Staker).Less)
}
