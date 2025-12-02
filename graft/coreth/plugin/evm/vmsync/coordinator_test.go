// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
)

func TestCoordinator_StateValidation(t *testing.T) {
	co := NewCoordinator(NewSyncerRegistry(), Callbacks{}, WithPivotInterval(1))
	block := newMockBlock(100)
	target := newTestSyncTarget(100)

	// States that reject both operations.
	for _, state := range []State{StateIdle, StateInitializing, StateFinalizing, StateCompleted, StateAborted} {
		co.state.Store(int32(state))
		require.False(t, co.AddBlockOperation(block, OpAccept), "state %d should reject block", state)
		require.ErrorIs(t, co.UpdateSyncTarget(target), errInvalidState, "state %d should reject target update", state)
	}

	// Running: accepts both.
	co.state.Store(int32(StateRunning))
	require.True(t, co.AddBlockOperation(block, OpAccept))
	require.NoError(t, co.UpdateSyncTarget(target))

	// ExecutingBatch: accepts blocks, rejects target updates.
	co.state.Store(int32(StateExecutingBatch))
	require.True(t, co.AddBlockOperation(block, OpAccept))
	require.ErrorIs(t, co.UpdateSyncTarget(target), errInvalidState)

	// Nil block is always rejected.
	co.state.Store(int32(StateRunning))
	require.False(t, co.AddBlockOperation(nil, OpAccept))
}

func TestCoordinator_UpdateSyncTarget_RemovesStaleBlocks(t *testing.T) {
	co := NewCoordinator(NewSyncerRegistry(), Callbacks{}, WithPivotInterval(1))
	co.state.Store(int32(StateRunning))

	for i := uint64(100); i <= 110; i++ {
		co.AddBlockOperation(newMockBlock(i), OpAccept)
	}

	require.NoError(t, co.UpdateSyncTarget(newTestSyncTarget(105)))

	batch := co.queue.dequeueBatch()
	require.Len(t, batch, 5) // Only 106-110 remain.
}

func TestCoordinator_Lifecycle(t *testing.T) {
	t.Run("completes successfully", func(t *testing.T) {
		registry := NewSyncerRegistry()
		require.NoError(t, registry.Register(newMockSyncer("test", nil)))

		co, err := runCoordinator(t, registry, Callbacks{
			FinalizeVM: func(context.Context, message.Syncable) error { return nil },
		})

		require.NoError(t, err)
		require.Equal(t, StateCompleted, co.CurrentState())
	})

	t.Run("aborts on syncer error", func(t *testing.T) {
		expectedErr := errors.New("syncer failed")
		registry := NewSyncerRegistry()
		require.NoError(t, registry.Register(newMockSyncer("failing", expectedErr)))

		co, err := runCoordinator(t, registry, Callbacks{})

		require.ErrorIs(t, err, expectedErr)
		require.Equal(t, StateAborted, co.CurrentState())
	})
}

func TestCoordinator_ProcessQueuedBlockOperations(t *testing.T) {
	t.Run("executes queued operations", func(t *testing.T) {
		co := NewCoordinator(NewSyncerRegistry(), Callbacks{})
		co.state.Store(int32(StateRunning))
		co.target.Store(newTestSyncTarget(100))
		co.AddBlockOperation(newMockBlock(100), OpAccept)

		require.NoError(t, co.ProcessQueuedBlockOperations(t.Context()))
		require.Equal(t, StateExecutingBatch, co.CurrentState())
	})

	t.Run("returns error on block operation failure", func(t *testing.T) {
		co := NewCoordinator(NewSyncerRegistry(), Callbacks{})
		co.state.Store(int32(StateRunning))
		co.target.Store(newTestSyncTarget(100))

		failBlock := newMockBlock(100)
		failBlock.acceptErr = errors.New("accept failed")
		co.AddBlockOperation(failBlock, OpAccept)

		err := co.ProcessQueuedBlockOperations(t.Context())
		require.ErrorIs(t, err, errBatchOperationFailed)
	})
}

// runCoordinator starts a coordinator and waits for completion.
func runCoordinator(t *testing.T, registry *SyncerRegistry, cbs Callbacks) (*Coordinator, error) {
	t.Helper()

	var (
		errDone error
		wg      sync.WaitGroup
	)
	wg.Add(1)

	cbs.OnDone = func(err error) {
		errDone = err
		wg.Done()
	}

	co := NewCoordinator(registry, cbs)
	co.Start(t.Context(), newTestSyncTarget(100))
	wg.Wait()

	return co, errDone
}

func newTestSyncTarget(height uint64) message.Syncable {
	hash := common.BytesToHash([]byte{byte(height)})
	root := common.BytesToHash([]byte{byte(height + 1)})
	return newSyncTarget(hash, root, height)
}
