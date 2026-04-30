// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

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

func TestBlockQueue_RemoveThroughHeight(t *testing.T) {
	q := newBlockQueue()

	// Enqueue blocks at heights 100-110.
	for i := uint64(100); i <= 110; i++ {
		q.enqueue(newMockBlock(i), OpAccept)
	}

	// Remove blocks at or below height 105 (commit-target block included).
	q.removeThroughHeight(105)

	// Only blocks strictly above 105 should remain (106, 107, 108, 109, 110).
	batch := q.dequeueBatch()
	require.Len(t, batch, 5)
	require.Equal(t, uint64(106), batch[0].block.GetEthBlock().NumberU64())
}

func TestBlockQueue_OperationDedupSemantics(t *testing.T) {
	tests := []struct {
		name       string
		enqueueOps []BlockOperationType
		wantOps    []BlockOperationType
	}{
		{
			name:       "dedupe verify duplicates",
			enqueueOps: []BlockOperationType{OpVerify, OpVerify},
			wantOps:    []BlockOperationType{OpVerify},
		},
		{
			name:       "allow different operations for same block",
			enqueueOps: []BlockOperationType{OpVerify, OpAccept},
			wantOps:    []BlockOperationType{OpVerify, OpAccept},
		},
		{
			name:       "do not dedupe accept/reject",
			enqueueOps: []BlockOperationType{OpAccept, OpAccept, OpReject, OpReject},
			wantOps:    []BlockOperationType{OpAccept, OpAccept, OpReject, OpReject},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newBlockQueue()
			b := newMockBlock(100)

			for _, op := range tt.enqueueOps {
				// Enqueue should report deferred even when deduping verify operations.
				require.True(t, q.enqueue(b, op))
			}

			batch := q.dequeueBatch()
			require.Len(t, batch, len(tt.wantOps))
			for i, wantOp := range tt.wantOps {
				require.Equal(t, wantOp, batch[i].operation)
			}
		})
	}
}

func TestBlockQueue_VerifyCanBeRequeuedAfterCleanup(t *testing.T) {
	tests := []struct {
		name          string
		forgetCurrent bool
		pruneBelow    uint64
	}{
		{
			name:          "after forget",
			forgetCurrent: true,
		},
		{
			name:       "after prune drop",
			pruneBelow: 101,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newBlockQueue()
			b := newMockBlock(100)
			require.True(t, q.enqueue(b, OpVerify))

			if tt.forgetCurrent {
				batch := q.dequeueBatch()
				require.Len(t, batch, 1)
				require.Equal(t, OpVerify, batch[0].operation)
				q.forget(batch)
			}
			if tt.pruneBelow != 0 {
				// Drop blocks strictly below 101, so our block at height 100 is pruned.
				q.removeBelowHeight(tt.pruneBelow)
			}
			require.Empty(t, q.dequeueBatch())

			// Verify should be enqueueable again after cleanup.
			require.True(t, q.enqueue(b, OpVerify))
			batch := q.dequeueBatch()
			require.Len(t, batch, 1)
			require.Equal(t, OpVerify, batch[0].operation)
		})
	}
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
