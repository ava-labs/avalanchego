// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

// Tests Insert and GetWork
func Test_WorkHeap_Insert_GetWork(t *testing.T) {
	require := require.New(t)
	h := newWorkHeap()

	lowPriorityItem := &workItem{
		start:       maybe.Some([]byte{4}),
		end:         maybe.Some([]byte{5}),
		priority:    lowPriority,
		localRootID: ids.GenerateTestID(),
	}
	mediumPriorityItem := &workItem{
		start:       maybe.Some([]byte{0}),
		end:         maybe.Some([]byte{1}),
		priority:    medPriority,
		localRootID: ids.GenerateTestID(),
	}
	highPriorityItem := &workItem{
		start:       maybe.Some([]byte{2}),
		end:         maybe.Some([]byte{3}),
		priority:    highPriority,
		localRootID: ids.GenerateTestID(),
	}
	h.Insert(highPriorityItem)
	h.Insert(mediumPriorityItem)
	h.Insert(lowPriorityItem)
	require.Equal(3, h.Len())

	// Ensure [sortedItems] is in right order.
	got := []*workItem{}
	h.sortedItems.Ascend(
		func(i *workItem) bool {
			got = append(got, i)
			return true
		},
	)
	require.Equal(
		[]*workItem{mediumPriorityItem, highPriorityItem, lowPriorityItem},
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

func Test_WorkHeap_remove(t *testing.T) {
	require := require.New(t)

	h := newWorkHeap()

	lowPriorityItem := &workItem{
		start:       maybe.Some([]byte{0}),
		end:         maybe.Some([]byte{1}),
		priority:    lowPriority,
		localRootID: ids.GenerateTestID(),
	}

	mediumPriorityItem := &workItem{
		start:       maybe.Some([]byte{2}),
		end:         maybe.Some([]byte{3}),
		priority:    medPriority,
		localRootID: ids.GenerateTestID(),
	}

	highPriorityItem := &workItem{
		start:       maybe.Some([]byte{4}),
		end:         maybe.Some([]byte{5}),
		priority:    highPriority,
		localRootID: ids.GenerateTestID(),
	}

	h.Insert(lowPriorityItem)

	wrappedLowPriorityItem, ok := h.innerHeap.Peek()
	require.True(ok)
	h.remove(wrappedLowPriorityItem)

	require.Zero(h.Len())
	require.Zero(h.sortedItems.Len())

	h.Insert(lowPriorityItem)
	h.Insert(mediumPriorityItem)
	h.Insert(highPriorityItem)

	wrappedhighPriorityItem, ok := h.innerHeap.Peek()
	require.True(ok)
	require.Equal(highPriorityItem, wrappedhighPriorityItem)
	h.remove(wrappedhighPriorityItem)
	require.Equal(2, h.Len())
	require.Equal(2, h.sortedItems.Len())
	got, ok := h.innerHeap.Peek()
	require.True(ok)
	require.Equal(mediumPriorityItem, got)

	wrappedMediumPriorityItem, ok := h.innerHeap.Peek()
	require.True(ok)
	require.Equal(mediumPriorityItem, wrappedMediumPriorityItem)
	h.remove(wrappedMediumPriorityItem)
	require.Equal(1, h.Len())
	require.Equal(1, h.sortedItems.Len())
	got, ok = h.innerHeap.Peek()
	require.True(ok)
	require.Equal(lowPriorityItem, got)

	wrappedLowPriorityItem, ok = h.innerHeap.Peek()
	require.True(ok)
	require.Equal(lowPriorityItem, wrappedLowPriorityItem)
	h.remove(wrappedLowPriorityItem)
	require.Zero(h.Len())
	require.Zero(h.sortedItems.Len())
}

func Test_WorkHeap_Merge_Insert(t *testing.T) {
	// merge with range before
	syncHeap := newWorkHeap()

	syncHeap.MergeInsert(&workItem{start: maybe.Nothing[[]byte](), end: maybe.Some([]byte{63})})
	require.Equal(t, 1, syncHeap.Len())

	syncHeap.MergeInsert(&workItem{start: maybe.Some([]byte{127}), end: maybe.Some([]byte{192})})
	require.Equal(t, 2, syncHeap.Len())

	syncHeap.MergeInsert(&workItem{start: maybe.Some([]byte{193}), end: maybe.Nothing[[]byte]()})
	require.Equal(t, 3, syncHeap.Len())

	syncHeap.MergeInsert(&workItem{start: maybe.Some([]byte{63}), end: maybe.Some([]byte{126}), priority: lowPriority})
	require.Equal(t, 3, syncHeap.Len())

	// merge with range after
	syncHeap = newWorkHeap()

	syncHeap.MergeInsert(&workItem{start: maybe.Nothing[[]byte](), end: maybe.Some([]byte{63})})
	require.Equal(t, 1, syncHeap.Len())

	syncHeap.MergeInsert(&workItem{start: maybe.Some([]byte{127}), end: maybe.Some([]byte{192})})
	require.Equal(t, 2, syncHeap.Len())

	syncHeap.MergeInsert(&workItem{start: maybe.Some([]byte{193}), end: maybe.Nothing[[]byte]()})
	require.Equal(t, 3, syncHeap.Len())

	syncHeap.MergeInsert(&workItem{start: maybe.Some([]byte{64}), end: maybe.Some([]byte{127}), priority: lowPriority})
	require.Equal(t, 3, syncHeap.Len())

	// merge both sides at the same time
	syncHeap = newWorkHeap()

	syncHeap.MergeInsert(&workItem{start: maybe.Nothing[[]byte](), end: maybe.Some([]byte{63})})
	require.Equal(t, 1, syncHeap.Len())

	syncHeap.MergeInsert(&workItem{start: maybe.Some([]byte{127}), end: maybe.Nothing[[]byte]()})
	require.Equal(t, 2, syncHeap.Len())

	syncHeap.MergeInsert(&workItem{start: maybe.Some([]byte{63}), end: maybe.Some([]byte{127}), priority: lowPriority})
	require.Equal(t, 1, syncHeap.Len())
}

func TestWorkHeapMergeInsertRandom(t *testing.T) {
	var (
		require   = require.New(t)
		seed      = time.Now().UnixNano()
		rand      = rand.New(rand.NewSource(seed))
		numRanges = 1_000
		bounds    = [][]byte{}
		rootID    = ids.GenerateTestID()
	)
	t.Logf("seed: %d", seed)

	// Create start and end bounds
	for i := 0; i < numRanges; i++ {
		bound := make([]byte, 32)
		_, _ = rand.Read(bound)
		bounds = append(bounds, bound)
	}
	slices.SortFunc(bounds, bytes.Compare)

	// Note that start < end for all ranges.
	// It is possible but extremely unlikely that
	// two elements of [bounds] are equal.
	ranges := []workItem{}
	for i := 0; i < numRanges/2; i++ {
		start := bounds[i*2]
		end := bounds[i*2+1]
		ranges = append(ranges, workItem{
			start:    maybe.Some(start),
			end:      maybe.Some(end),
			priority: lowPriority,
			// Note they all share the same root ID.
			localRootID: rootID,
		})
	}
	// Set beginning of first range to Nothing.
	ranges[0].start = maybe.Nothing[[]byte]()
	// Set end of last range to Nothing.
	ranges[len(ranges)-1].end = maybe.Nothing[[]byte]()

	setup := func() *workHeap {
		// Insert all the ranges into the heap.
		h := newWorkHeap()
		for i, r := range ranges {
			require.Equal(i, h.Len())
			rCopy := r
			h.MergeInsert(&rCopy)
		}
		return h
	}

	{
		// Case 1: Merging an item with the range before and after
		h := setup()
		// Keep merging ranges until there's only one range left.
		for i := 0; i < len(ranges)-1; i++ {
			// Merge ranges[i] with ranges[i+1]
			h.MergeInsert(&workItem{
				start:       ranges[i].end,
				end:         ranges[i+1].start,
				priority:    lowPriority,
				localRootID: rootID,
			})
			require.Equal(len(ranges)-i-1, h.Len())
		}
		got := h.GetWork()
		require.True(got.start.IsNothing())
		require.True(got.end.IsNothing())
	}

	{
		// Case 2: Merging an item with the range before
		h := setup()
		for i := 0; i < len(ranges)-1; i++ {
			// Extend end of ranges[i]
			newEnd := slices.Clone(ranges[i].end.Value())
			newEnd = append(newEnd, 0)
			h.MergeInsert(&workItem{
				start:       ranges[i].end,
				end:         maybe.Some(newEnd),
				priority:    lowPriority,
				localRootID: rootID,
			})

			// Shouldn't cause number of elements to change
			require.Equal(len(ranges), h.Len())

			start := ranges[i].start
			if i == 0 {
				start = maybe.Nothing[[]byte]()
			}
			// Make sure end is updated
			got, ok := h.sortedItems.Get(&workItem{
				start: start,
			})
			require.True(ok)
			require.Equal(newEnd, got.end.Value())
		}
	}

	{
		// Case 3: Merging an item with the range after
		h := setup()
		for i := 1; i < len(ranges); i++ {
			// Extend start of ranges[i]
			newStartBytes := slices.Clone(ranges[i].start.Value())
			newStartBytes = newStartBytes[:len(newStartBytes)-1]
			newStart := maybe.Some(newStartBytes)

			h.MergeInsert(&workItem{
				start:       newStart,
				end:         ranges[i].start,
				priority:    lowPriority,
				localRootID: rootID,
			})

			// Shouldn't cause number of elements to change
			require.Equal(len(ranges), h.Len())

			// Make sure start is updated
			got, ok := h.sortedItems.Get(&workItem{
				start: newStart,
			})
			require.True(ok)
			require.Equal(newStartBytes, got.start.Value())
		}
	}
}
