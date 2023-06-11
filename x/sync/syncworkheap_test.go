// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// Tests heap.Interface methods Push, Pop, Swap, Len, Less.
func Test_SyncWorkHeap_InnerHeap(t *testing.T) {
	require := require.New(t)

	lowPriorityItem := &heapItem{
		workItem: &syncWorkItem{
			start:       []byte{1},
			end:         []byte{2},
			priority:    lowPriority,
			LocalRootID: ids.GenerateTestID(),
		},
	}

	mediumPriorityItem := &heapItem{
		workItem: &syncWorkItem{
			start:       []byte{3},
			end:         []byte{4},
			priority:    medPriority,
			LocalRootID: ids.GenerateTestID(),
		},
	}

	highPriorityItem := &heapItem{
		workItem: &syncWorkItem{
			start:       []byte{5},
			end:         []byte{6},
			priority:    highPriority,
			LocalRootID: ids.GenerateTestID(),
		},
	}

	h := innerHeap{}
	require.Zero(h.Len())

	// Note we're calling Push and Pop on the heap directly,
	// not using heap.Push and heap.Pop.
	h.Push(lowPriorityItem)
	// Heap has [lowPriorityItem]
	require.Equal(1, h.Len())
	require.Equal(lowPriorityItem, h[0])

	got := h.Pop()
	// Heap has []
	require.Equal(lowPriorityItem, got)
	require.Zero(h.Len())

	h.Push(lowPriorityItem)
	h.Push(mediumPriorityItem)
	// Heap has [lowPriorityItem, mediumPriorityItem]
	require.Equal(2, h.Len())
	require.Equal(lowPriorityItem, h[0])
	require.Equal(mediumPriorityItem, h[1])

	got = h.Pop()
	// Heap has [lowPriorityItem]
	require.Equal(mediumPriorityItem, got)
	require.Equal(1, h.Len())

	got = h.Pop()
	// Heap has []
	require.Equal(lowPriorityItem, got)
	require.Zero(h.Len())

	h.Push(mediumPriorityItem)
	h.Push(lowPriorityItem)
	h.Push(highPriorityItem)
	// Heap has [mediumPriorityItem, lowPriorityItem, highPriorityItem]
	require.Equal(mediumPriorityItem, h[0])
	require.Equal(lowPriorityItem, h[1])
	require.Equal(highPriorityItem, h[2])

	h.Swap(0, 1)
	// Heap has [lowPriorityItem, mediumPriorityItem, highPriorityItem]
	require.Equal(lowPriorityItem, h[0])
	require.Equal(mediumPriorityItem, h[1])
	require.Equal(highPriorityItem, h[2])

	h.Swap(1, 2)
	// Heap has [lowPriorityItem, highPriorityItem, mediumPriorityItem]
	require.Equal(lowPriorityItem, h[0])
	require.Equal(highPriorityItem, h[1])
	require.Equal(mediumPriorityItem, h[2])

	h.Swap(0, 2)
	// Heap has [mediumPriorityItem, highPriorityItem, lowPriorityItem]
	require.Equal(mediumPriorityItem, h[0])
	require.Equal(highPriorityItem, h[1])
	require.Equal(lowPriorityItem, h[2])
	require.False(h.Less(0, 1))
	require.True(h.Less(1, 0))
	require.True(h.Less(1, 2))
	require.False(h.Less(2, 1))
	require.True(h.Less(0, 2))
	require.False(h.Less(2, 0))
}

// Tests Insert and GetWork
func Test_SyncWorkHeap_Insert_GetWork(t *testing.T) {
	require := require.New(t)
	h := newSyncWorkHeap()

	lowPriorityItem := &syncWorkItem{
		start:       []byte{4},
		end:         []byte{5},
		priority:    lowPriority,
		LocalRootID: ids.GenerateTestID(),
	}
	mediumPriorityItem := &syncWorkItem{
		start:       []byte{0},
		end:         []byte{1},
		priority:    medPriority,
		LocalRootID: ids.GenerateTestID(),
	}
	highPriorityItem := &syncWorkItem{
		start:       []byte{2},
		end:         []byte{3},
		priority:    highPriority,
		LocalRootID: ids.GenerateTestID(),
	}
	h.Insert(highPriorityItem)
	h.Insert(mediumPriorityItem)
	h.Insert(lowPriorityItem)
	require.Equal(3, h.Len())

	// Ensure [sortedItems] is in right order.
	got := []*syncWorkItem{}
	h.sortedItems.Ascend(
		func(i *heapItem) bool {
			got = append(got, i.workItem)
			return true
		},
	)
	require.Equal(
		[]*syncWorkItem{mediumPriorityItem, highPriorityItem, lowPriorityItem},
		got,
	)

	// Ensure priorities are in right order.
	gotItem := h.GetWork()
	require.Equal(highPriorityItem, gotItem)
	gotItem = h.GetWork()
	require.Equal(mediumPriorityItem, gotItem)
	gotItem = h.GetWork()
	require.Equal(lowPriorityItem, gotItem)
	gotItem = h.GetWork()
	require.Nil(gotItem)

	require.Zero(h.Len())
}

func Test_SyncWorkHeap_remove(t *testing.T) {
	require := require.New(t)

	h := newSyncWorkHeap()

	lowPriorityItem := &syncWorkItem{
		start:       []byte{0},
		end:         []byte{1},
		priority:    lowPriority,
		LocalRootID: ids.GenerateTestID(),
	}

	mediumPriorityItem := &syncWorkItem{
		start:       []byte{2},
		end:         []byte{3},
		priority:    medPriority,
		LocalRootID: ids.GenerateTestID(),
	}

	highPriorityItem := &syncWorkItem{
		start:       []byte{4},
		end:         []byte{5},
		priority:    highPriority,
		LocalRootID: ids.GenerateTestID(),
	}

	h.Insert(lowPriorityItem)

	wrappedLowPriorityItem := h.innerHeap[0]
	h.remove(wrappedLowPriorityItem)

	require.Zero(h.Len())
	require.Empty(h.innerHeap)
	require.Zero(h.sortedItems.Len())

	h.Insert(lowPriorityItem)
	h.Insert(mediumPriorityItem)
	h.Insert(highPriorityItem)

	wrappedhighPriorityItem := h.innerHeap[0]
	require.Equal(highPriorityItem, wrappedhighPriorityItem.workItem)
	h.remove(wrappedhighPriorityItem)
	require.Equal(2, h.Len())
	require.Len(h.innerHeap, 2)
	require.Equal(2, h.sortedItems.Len())
	require.Zero(h.innerHeap[0].heapIndex)
	require.Equal(mediumPriorityItem, h.innerHeap[0].workItem)

	wrappedMediumPriorityItem := h.innerHeap[0]
	require.Equal(mediumPriorityItem, wrappedMediumPriorityItem.workItem)
	h.remove(wrappedMediumPriorityItem)
	require.Equal(1, h.Len())
	require.Len(h.innerHeap, 1)
	require.Equal(1, h.sortedItems.Len())
	require.Zero(h.innerHeap[0].heapIndex)
	require.Equal(lowPriorityItem, h.innerHeap[0].workItem)

	wrappedLowPriorityItem = h.innerHeap[0]
	require.Equal(lowPriorityItem, wrappedLowPriorityItem.workItem)
	h.remove(wrappedLowPriorityItem)
	require.Zero(h.Len())
	require.Empty(h.innerHeap)
	require.Zero(h.sortedItems.Len())
}

func Test_SyncWorkHeap_Merge_Insert(t *testing.T) {
	// merge with range before
	syncHeap := newSyncWorkHeap()

	syncHeap.MergeInsert(&syncWorkItem{start: nil, end: []byte{63}})
	require.Equal(t, 1, syncHeap.Len())

	syncHeap.MergeInsert(&syncWorkItem{start: []byte{127}, end: []byte{192}})
	require.Equal(t, 2, syncHeap.Len())

	syncHeap.MergeInsert(&syncWorkItem{start: []byte{193}, end: nil})
	require.Equal(t, 3, syncHeap.Len())

	syncHeap.MergeInsert(&syncWorkItem{start: []byte{63}, end: []byte{126}, priority: lowPriority})
	require.Equal(t, 3, syncHeap.Len())

	// merge with range after
	syncHeap = newSyncWorkHeap()

	syncHeap.MergeInsert(&syncWorkItem{start: nil, end: []byte{63}})
	require.Equal(t, 1, syncHeap.Len())

	syncHeap.MergeInsert(&syncWorkItem{start: []byte{127}, end: []byte{192}})
	require.Equal(t, 2, syncHeap.Len())

	syncHeap.MergeInsert(&syncWorkItem{start: []byte{193}, end: nil})
	require.Equal(t, 3, syncHeap.Len())

	syncHeap.MergeInsert(&syncWorkItem{start: []byte{64}, end: []byte{127}, priority: lowPriority})
	require.Equal(t, 3, syncHeap.Len())

	// merge both sides at the same time
	syncHeap = newSyncWorkHeap()

	syncHeap.MergeInsert(&syncWorkItem{start: nil, end: []byte{63}})
	require.Equal(t, 1, syncHeap.Len())

	syncHeap.MergeInsert(&syncWorkItem{start: []byte{127}, end: nil})
	require.Equal(t, 2, syncHeap.Len())

	syncHeap.MergeInsert(&syncWorkItem{start: []byte{63}, end: []byte{127}, priority: lowPriority})
	require.Equal(t, 1, syncHeap.Len())
}
