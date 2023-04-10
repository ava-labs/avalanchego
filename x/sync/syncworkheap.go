// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"container/heap"

	"github.com/google/btree"
)

var _ heap.Interface = &syncWorkHeap{}

type heapItem struct {
	workItem  *syncWorkItem
	heapIndex int
}

// A priority queue of syncWorkItems.
// Note that work item ranges never overlap.
// Supports range merging and priority updating.
// Not safe for concurrent use.
type syncWorkHeap struct {
	// Max heap of items by priority.
	// i.e. heap.Pop returns highest priority item.
	priorityHeap []*heapItem
	// The heap items sorted by range start.
	// A nil start is considered to be the smallest.
	sortedItems *btree.BTreeG[*heapItem]
	closed      bool
}

func newSyncWorkHeap(maxSize int) *syncWorkHeap {
	return &syncWorkHeap{
		priorityHeap: make([]*heapItem, 0, maxSize),
		sortedItems: btree.NewG(
			2,
			func(a, b *heapItem) bool {
				if a.workItem == nil {
					return b.workItem != nil
				}
				if b.workItem == nil {
					return false
				}
				return bytes.Compare(a.workItem.start, b.workItem.start) < 0
			},
		),
	}
}

// Marks the heap as closed.
func (wh *syncWorkHeap) Close() {
	wh.closed = true
}

// Adds a new [item] into the heap. Will not merge items, unlike MergeInsert.
func (wh *syncWorkHeap) Insert(item *syncWorkItem) {
	if wh.closed {
		return
	}

	heap.Push(wh, &heapItem{workItem: item})
}

// Pops and returns a work item from the heap.
// Returns nil if no work is available or the heap is closed.
func (wh *syncWorkHeap) GetWork() *syncWorkItem {
	if wh.closed || wh.Len() == 0 {
		return nil
	}
	return heap.Pop(wh).(*heapItem).workItem
}

// Insert the item into the heap, merging it with existing items
// that share a boundary and root ID.
// e.g. if the heap contains a work item with range
// [0,10] and then [10,20] is inserted, we will merge the two
// into a single work item with range [0,20].
// e.g. if the heap contains work items [0,10] and [20,30],
// and we add [10,20], we will merge them into [0,30].
func (wh *syncWorkHeap) MergeInsert(item *syncWorkItem) {
	if wh.closed {
		return
	}

	var mergedRange *heapItem

	// Find the item with the greatest start range which is less than [item.start].
	// Note that the iterator function will run at most once, since it always returns false.
	wh.sortedItems.DescendLessOrEqual(
		&heapItem{
			workItem: &syncWorkItem{
				start: item.start,
			},
		},
		func(beforeItem *heapItem) bool {
			if item.LocalRootID == beforeItem.workItem.LocalRootID && bytes.Equal(beforeItem.workItem.end, item.start) {
				// [beforeItem.start, beforeItem.end] and [item.start, item.end] are
				// merged into [beforeItem.start, item.end]
				beforeItem.workItem.end = item.end
				mergedRange = beforeItem
			}
			return false
		})

	// Find the item with the smallest start range which is greater than [item.start].
	// Note that the iterator function will run at most once, since it always returns false.
	wh.sortedItems.AscendGreaterOrEqual(
		&heapItem{
			workItem: &syncWorkItem{
				start: item.start,
			},
		},
		func(afterItem *heapItem) bool {
			if item.LocalRootID == afterItem.workItem.LocalRootID && bytes.Equal(afterItem.workItem.start, item.end) {
				if mergedRange != nil {
					// [beforeItem.start, item.end] and [afterItem.start, afterItem.end] are merged
					// into [beforeItem.start, afterItem.end].
					// Modify [mergedRange] and remove [afterItem] since [mergedRange] now contains the entire
					// range that was covered by [afterItem].
					wh.remove(afterItem)
					mergedRange.workItem.end = afterItem.workItem.end
					if afterItem.workItem.priority > mergedRange.workItem.priority {
						mergedRange.workItem.priority = afterItem.workItem.priority
						heap.Fix(wh, mergedRange.heapIndex)
					}
				} else {
					// [item.start, item.end] and [afterItem.start, afterItem.end] are merged into
					// [item.start, afterItem.end].
					afterItem.workItem.start = item.start
					mergedRange = afterItem
				}
			}
			return false
		})

	if mergedRange != nil {
		// We merged [item] with at least one existing item.
		if item.priority > mergedRange.workItem.priority {
			mergedRange.workItem.priority = item.priority
			// Priority was updated; fix position in the heap.
			heap.Fix(wh, mergedRange.heapIndex)
		}
	} else {
		// We didn't merge [item] with an existing one; put it in the heap.
		heap.Push(wh, &heapItem{workItem: item})
	}
}

// Deletes [item] from the heap.
func (wh *syncWorkHeap) remove(item *heapItem) {
	oldIndex := item.heapIndex
	newLength := len(wh.priorityHeap) - 1

	// swap with last item, delete item, then fix heap if required
	wh.Swap(newLength, item.heapIndex)
	wh.priorityHeap[newLength] = nil
	wh.priorityHeap = wh.priorityHeap[:newLength]

	// the item was already the last item, so nothing needs to be fixed
	if oldIndex != newLength {
		heap.Fix(wh, oldIndex)
	}
	wh.sortedItems.Delete(item)
}

// below this line are the implementations required for heap.Interface

func (wh *syncWorkHeap) Len() int {
	return len(wh.priorityHeap)
}

func (wh *syncWorkHeap) Less(i int, j int) bool {
	return wh.priorityHeap[i].workItem.priority > wh.priorityHeap[j].workItem.priority
}

func (wh *syncWorkHeap) Swap(i int, j int) {
	wh.priorityHeap[i], wh.priorityHeap[j] = wh.priorityHeap[j], wh.priorityHeap[i]
	wh.priorityHeap[i].heapIndex = i
	wh.priorityHeap[j].heapIndex = j
}

func (wh *syncWorkHeap) Pop() interface{} {
	newLength := len(wh.priorityHeap) - 1
	value := wh.priorityHeap[newLength]
	wh.priorityHeap[newLength] = nil
	wh.priorityHeap = wh.priorityHeap[:newLength]
	wh.sortedItems.Delete(value)
	return value
}

func (wh *syncWorkHeap) Push(x interface{}) {
	item := x.(*heapItem)
	item.heapIndex = len(wh.priorityHeap)
	wh.priorityHeap = append(wh.priorityHeap, item)
	wh.sortedItems.ReplaceOrInsert(item)
}
