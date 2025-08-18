// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

// A priority queue of syncWorkItems.
// Note that work item ranges never overlap.
// Supports range merging and priority updating.
// Not safe for concurrent use.
type workHeap struct {
	// Max heap of items by priority.
	// i.e. heap.Pop returns highest priority item.
	innerHeap heap.Set[*workItem]
	// The heap items sorted by range start.
	// A Nothing start is considered to be the smallest.
	sortedItems *btree.BTreeG[*workItem]
	closed      bool
}

func newWorkHeap() *workHeap {
	return &workHeap{
		innerHeap: heap.NewSet[*workItem](func(a, b *workItem) bool {
			return a.priority > b.priority
		}),
		sortedItems: btree.NewG(
			2,
			func(a, b *workItem) bool {
				aNothing := a.start.IsNothing()
				bNothing := b.start.IsNothing()
				if aNothing {
					// [a] is Nothing, so if [b] is Nothing, they're equal.
					// Otherwise, [b] is greater.
					return !bNothing
				}
				if bNothing {
					// [a] has a value and [b] doesn't so [a] is greater.
					return false
				}
				// [a] and [b] both contain values. Compare the values.
				return bytes.Compare(a.start.Value(), b.start.Value()) < 0
			},
		),
	}
}

// Marks the heap as closed.
func (wh *workHeap) Close() {
	wh.closed = true
}

// Adds a new [item] into the heap. Will not merge items, unlike MergeInsert.
func (wh *workHeap) Insert(item *workItem) {
	if wh.closed {
		return
	}

	wh.innerHeap.Push(item)
	wh.sortedItems.ReplaceOrInsert(item)
}

// Pops and returns a work item from the heap.
// Returns nil if no work is available or the heap is closed.
func (wh *workHeap) GetWork() *workItem {
	if wh.closed || wh.Len() == 0 {
		return nil
	}
	item, _ := wh.innerHeap.Pop()
	wh.sortedItems.Delete(item)
	return item
}

// Insert the item into the heap, merging it with existing items
// that share a boundary and root ID.
// e.g. if the heap contains a work item with range
// [0,10] and then [10,20] is inserted, we will merge the two
// into a single work item with range [0,20].
// e.g. if the heap contains work items [0,10] and [20,30],
// and we add [10,20], we will merge them into [0,30].
func (wh *workHeap) MergeInsert(item *workItem) {
	if wh.closed {
		return
	}

	var mergedBefore, mergedAfter *workItem
	searchItem := &workItem{
		start: item.start,
	}

	// Find the item with the greatest start range which is less than [item.start].
	// Note that the iterator function will run at most once, since it always returns false.
	wh.sortedItems.DescendLessOrEqual(
		searchItem,
		func(beforeItem *workItem) bool {
			if item.localRootID == beforeItem.localRootID &&
				maybe.Equal(item.start, beforeItem.end, bytes.Equal) {
				// [beforeItem.start, beforeItem.end] and [item.start, item.end] are
				// merged into [beforeItem.start, item.end]
				beforeItem.end = item.end
				beforeItem.priority = max(item.priority, beforeItem.priority)
				wh.innerHeap.Fix(beforeItem)
				mergedBefore = beforeItem
			}
			return false
		})

	// Find the item with the smallest start range which is greater than [item.start].
	// Note that the iterator function will run at most once, since it always returns false.
	wh.sortedItems.AscendGreaterOrEqual(
		searchItem,
		func(afterItem *workItem) bool {
			if item.localRootID == afterItem.localRootID &&
				maybe.Equal(item.end, afterItem.start, bytes.Equal) {
				// [item.start, item.end] and [afterItem.start, afterItem.end] are merged into
				// [item.start, afterItem.end].
				afterItem.start = item.start
				afterItem.priority = max(item.priority, afterItem.priority)
				wh.innerHeap.Fix(afterItem)
				mergedAfter = afterItem
			}
			return false
		})

	// if the new item should be merged with both the item before and the item after,
	// we can combine the before item with the after item
	if mergedBefore != nil && mergedAfter != nil {
		// combine the two ranges
		mergedBefore.end = mergedAfter.end
		// remove the second range since it is now covered by the first
		wh.remove(mergedAfter)
		// update the priority
		mergedBefore.priority = max(mergedBefore.priority, mergedAfter.priority)
		wh.innerHeap.Fix(mergedBefore)
	}

	// nothing was merged, so add new item to the heap
	if mergedBefore == nil && mergedAfter == nil {
		// We didn't merge [item] with an existing one; put it in the heap.
		wh.Insert(item)
	}
}

// Deletes [item] from the heap.
func (wh *workHeap) remove(item *workItem) {
	wh.innerHeap.Remove(item)
	wh.sortedItems.Delete(item)
}

func (wh *workHeap) Len() int {
	return wh.innerHeap.Len()
}
