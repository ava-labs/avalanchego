// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlockQueue_EnqueueAndDequeue(t *testing.T) {
	q := newBlockQueue()

	// Nil block should be rejected.
	require.False(t, q.enqueue(nil, OpAccept))

	// Enqueue blocks.
	for i := uint64(100); i < 105; i++ {
		require.True(t, q.enqueue(newMockBlock(i), OpAccept))
	}

	// Dequeue returns all in FIFO order and clears queue.
	batch := q.dequeueBatch()
	require.Len(t, batch, 5)
	for i, op := range batch {
		require.Equal(t, uint64(100+i), op.block.GetEthBlock().NumberU64())
	}

	// Queue is now empty.
	require.Empty(t, q.dequeueBatch())
}

func TestBlockQueue_RemoveBelowHeight(t *testing.T) {
	q := newBlockQueue()

	// Enqueue blocks at heights 100-110.
	for i := uint64(100); i <= 110; i++ {
		q.enqueue(newMockBlock(i), OpAccept)
	}

	// Remove blocks at or below height 105.
	q.removeBelowHeight(105)

	// Only blocks > 105 should remain (106, 107, 108, 109, 110).
	batch := q.dequeueBatch()
	require.Len(t, batch, 5)
	require.Equal(t, uint64(106), batch[0].block.GetEthBlock().NumberU64())
}

func TestBlockQueue_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	q := newBlockQueue()
	const numGoroutines = 10
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numOps; i++ {
				q.enqueue(newMockBlock(uint64(id*numOps+i)), OpAccept)
			}
		}(g)
	}

	wg.Wait()

	// All operations should have been enqueued.
	batch := q.dequeueBatch()
	require.Len(t, batch, numGoroutines*numOps)
}
