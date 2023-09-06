// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"container/heap"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/maybe"

	"github.com/google/btree"
)

var _ heap.Interface = (*innerHeap)(nil)

type heapItem struct {
	workItem  *workItem
	heapIndex int
}

type innerHeap []*heapItem

// A priority queue of syncWorkItems.
// Note that work item ranges never overlap.
// Supports range merging and priority updating.
// Not safe for concurrent use.
type workHeap struct {
	// Max heap of items by priority.
	// i.e. heap.Pop returns highest priority item.
	innerHeap innerHeap
	// The heap items sorted by range start.
	// A Nothing start is considered to be the smallest.
	sortedItems *btree.BTreeG[*heapItem]
	closed      bool
}

func newWorkHeap() *workHeap {
	return &workHeap{
		sortedItems: btree.NewG(
			2,
			func(a, b *heapItem) bool {
				aNothing := a.workItem.start.IsNothing()
				bNothing := b.workItem.start.IsNothing()
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
				return bytes.Compare(a.workItem.start.Value(), b.workItem.start.Value()) < 0
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

	wrappedItem := &heapItem{workItem: item}

	heap.Push(&wh.innerHeap, wrappedItem)
	wh.sortedItems.ReplaceOrInsert(wrappedItem)
}

// Pops and returns a work item from the heap.
// Returns nil if no work is available or the heap is closed.
func (wh *workHeap) GetWork() *workItem {
	if wh.closed || wh.Len() == 0 {
		return nil
	}
	item := heap.Pop(&wh.innerHeap).(*heapItem)
	wh.sortedItems.Delete(item)
	return item.workItem
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

	var mergedBefore, mergedAfter *heapItem
	searchItem := &heapItem{
		workItem: &workItem{
			start: item.start,
		},
	}

	// Find the item with the greatest start range which is less than [item.start].
	// Note that the iterator function will run at most once, since it always returns false.
	wh.sortedItems.DescendLessOrEqual(
		searchItem,
		func(beforeItem *heapItem) bool {
			if item.localRootID == beforeItem.workItem.localRootID &&
				maybe.Equal(item.start, beforeItem.workItem.end, bytes.Equal) {
				// [beforeItem.start, beforeItem.end] and [item.start, item.end] are
				// merged into [beforeItem.start, item.end]
				beforeItem.workItem.end = item.end
				beforeItem.workItem.priority = math.Max(item.priority, beforeItem.workItem.priority)
				heap.Fix(&wh.innerHeap, beforeItem.heapIndex)
				mergedBefore = beforeItem
			}
			return false
		})

	// Find the item with the smallest start range which is greater than [item.start].
	// Note that the iterator function will run at most once, since it always returns false.
	wh.sortedItems.AscendGreaterOrEqual(
		searchItem,
		func(afterItem *heapItem) bool {
			if item.localRootID == afterItem.workItem.localRootID &&
				maybe.Equal(item.end, afterItem.workItem.start, bytes.Equal) {
				// [item.start, item.end] and [afterItem.start, afterItem.end] are merged into
				// [item.start, afterItem.end].
				afterItem.workItem.start = item.start
				afterItem.workItem.priority = math.Max(item.priority, afterItem.workItem.priority)
				heap.Fix(&wh.innerHeap, afterItem.heapIndex)
				mergedAfter = afterItem
			}
			return false
		})

	// if the new item should be merged with both the item before and the item after,
	// we can combine the before item with the after item
	if mergedBefore != nil && mergedAfter != nil {
		// combine the two ranges
		mergedBefore.workItem.end = mergedAfter.workItem.end
		// remove the second range since it is now covered by the first
		wh.remove(mergedAfter)
		// update the priority
		mergedBefore.workItem.priority = math.Max(mergedBefore.workItem.priority, mergedAfter.workItem.priority)
		heap.Fix(&wh.innerHeap, mergedBefore.heapIndex)
	}

	// nothing was merged, so add new item to the heap
	if mergedBefore == nil && mergedAfter == nil {
		// We didn't merge [item] with an existing one; put it in the heap.
		wh.Insert(item)
	}
}

// Deletes [item] from the heap.
func (wh *workHeap) remove(item *heapItem) {
	heap.Remove(&wh.innerHeap, item.heapIndex)

	wh.sortedItems.Delete(item)
}

func (wh *workHeap) Len() int {
	return wh.innerHeap.Len()
}

// below this line are the implementations required for heap.Interface

func (h innerHeap) Len() int {
	return len(h)
}

func (h innerHeap) Less(i int, j int) bool {
	return h[i].workItem.priority > h[j].workItem.priority
}

func (h innerHeap) Swap(i int, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *innerHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*h = old[0 : n-1]
	return item
}

func (h *innerHeap) Push(x interface{}) {
	item := x.(*heapItem)
	item.heapIndex = len(*h)
	*h = append(*h, item)
}
