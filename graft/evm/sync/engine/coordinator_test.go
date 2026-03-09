// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/message"
)

type coordinatorTestSyncer struct {
	name     string
	id       string
	syncFn   func(context.Context) error
	updateFn func(message.Syncable) error
}

func (s *coordinatorTestSyncer) Sync(ctx context.Context) error {
	if s.syncFn == nil {
		return nil
	}
	return s.syncFn(ctx)
}

func (s *coordinatorTestSyncer) UpdateTarget(target message.Syncable) error {
	if s.updateFn == nil {
		return nil
	}
	return s.updateFn(target)
}

func (s *coordinatorTestSyncer) Name() string { return s.name }
func (s *coordinatorTestSyncer) ID() string   { return s.id }

func TestCoordinator_StateValidation(t *testing.T) {
	co := NewCoordinator(NewSyncerRegistry(), Callbacks{}, WithPivotInterval(1))
	block := newMockBlock(100)
	target := newTestSyncTarget(100)

	// States that reject both operations.
	for _, state := range []State{StateIdle, StateInitializing, StateFinalizing, StateCompleted, StateAborted} {
		co.state.Store(int32(state))
		require.False(t, co.AddBlockOperation(block, OpAccept), "state %d should reject block", state)
		err := co.UpdateSyncTarget(target)
		require.ErrorIs(t, err, errInvalidState, "state %d should reject target update", state)
	}

	// Running: accepts both.
	co.state.Store(int32(StateRunning))
	require.True(t, co.AddBlockOperation(block, OpAccept))
	require.NoError(t, co.UpdateSyncTarget(target))

	// ExecutingBatch: accepts blocks, rejects target updates.
	co.state.Store(int32(StateExecutingBatch))
	require.True(t, co.AddBlockOperation(block, OpAccept))
	err := co.UpdateSyncTarget(target)
	require.ErrorIs(t, err, errInvalidState)

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
	require.Equal(t, uint64(1), co.targetEpoch.Load())

	batch := co.queue.dequeueBatch()
	require.Len(t, batch, 6) // Only 105-110 remain (keep pivot block).
}

func TestCoordinator_UpdateSyncTarget_DoesNotPruneOnFailureAndAborts(t *testing.T) {
	wantErr := errors.New("update failed")
	registry := NewSyncerRegistry()
	require.NoError(t, registry.Register(&coordinatorTestSyncer{
		name: "update-failing",
		id:   "update-failing",
		updateFn: func(message.Syncable) error {
			return wantErr
		},
	}))

	co := NewCoordinator(registry, Callbacks{}, WithPivotInterval(1))
	co.state.Store(int32(StateRunning))
	co.setCommitTarget(newTestSyncTarget(100))

	for i := uint64(100); i <= 110; i++ {
		co.AddBlockOperation(newMockBlock(i), OpAccept)
	}

	err := co.UpdateSyncTarget(newTestSyncTarget(105))
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, StateAborted, co.CurrentState())
	require.Equal(t, uint64(0), co.targetEpoch.Load())
	require.Equal(t, uint64(100), co.getCommitTarget().Height())

	batch := co.queue.dequeueBatch()
	require.Len(t, batch, 11) // Nothing was pruned on failed target fanout.
}

func TestCoordinator_UpdateSyncTarget_SerializesConcurrentCalls(t *testing.T) {
	var (
		inUpdate   atomic.Int32
		concurrent atomic.Bool
	)

	registry := NewSyncerRegistry()
	require.NoError(t, registry.Register(&coordinatorTestSyncer{
		name: "serial-check",
		id:   "serial-check",
		updateFn: func(message.Syncable) error {
			if !inUpdate.CompareAndSwap(0, 1) {
				concurrent.Store(true)
			}
			time.Sleep(10 * time.Millisecond)
			inUpdate.Store(0)
			return nil
		},
	}))

	co := NewCoordinator(registry, Callbacks{}, WithPivotInterval(1))
	co.state.Store(int32(StateRunning))
	co.setCommitTarget(newTestSyncTarget(100))

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		height := uint64(101 + i)
		go func() {
			defer wg.Done()
			errCh <- co.UpdateSyncTarget(newTestSyncTarget(height))
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	require.False(t, concurrent.Load(), "UpdateTarget calls should be serialized")
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
		wantErr := errors.New("syncer failed")
		registry := NewSyncerRegistry()
		require.NoError(t, registry.Register(newMockSyncer("failing", wantErr)))

		co, err := runCoordinator(t, registry, Callbacks{})

		require.ErrorIs(t, err, wantErr)
		require.Equal(t, StateAborted, co.CurrentState())
	})
}

func TestCoordinator_Start_ReplaysDeferredOperationsAfterFinalize(t *testing.T) {
	registry := NewSyncerRegistry()

	started := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, registry.Register(FuncSyncer{
		name: "barrier",
		id:   "barrier",
		fn: func(ctx context.Context) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}))

	done := make(chan error, 1)
	co := NewCoordinator(registry, Callbacks{
		FinalizeVM: func(context.Context, message.Syncable) error { return nil },
		OnDone:     func(err error) { done <- err },
	}, WithPivotInterval(1))
	co.Start(t.Context(), newTestSyncTarget(100))

	<-started
	block := newMockBlock(100)
	require.True(t, co.AddBlockOperation(block, OpAccept))

	close(release)
	require.NoError(t, <-done)
	require.Equal(t, StateCompleted, co.CurrentState())
	require.Equal(t, 1, block.acceptCount)
}

func TestCoordinator_Start_FinalizesWithCommitTarget(t *testing.T) {
	registry := NewSyncerRegistry()

	started := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, registry.Register(&coordinatorTestSyncer{
		name: "barrier",
		id:   "barrier",
		syncFn: func(ctx context.Context) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}))

	done := make(chan error, 1)
	var finalizedTarget message.Syncable
	co := NewCoordinator(registry, Callbacks{
		FinalizeVM: func(_ context.Context, target message.Syncable) error {
			finalizedTarget = target
			return nil
		},
		OnDone: func(err error) {
			done <- err
		},
	}, WithPivotInterval(1))

	initial := newTestSyncTarget(100)
	co.Start(t.Context(), initial)

	<-started
	require.NoError(t, co.UpdateSyncTarget(newTestSyncTarget(105)))
	close(release)

	require.NoError(t, <-done)
	require.Equal(t, uint64(105), finalizedTarget.Height())
	require.Equal(t, StateCompleted, co.CurrentState())
}

func TestCoordinator_Finish_DoesNotOverwriteAbortedState(t *testing.T) {
	co := NewCoordinator(NewSyncerRegistry(), Callbacks{})
	co.state.Store(int32(StateRunning))

	co.finish(nil, errors.New("abort"))
	require.Equal(t, StateAborted, co.CurrentState())

	co.finish(nil, nil)
	require.Equal(t, StateAborted, co.CurrentState())
}

func TestCoordinator_ProcessQueuedBlockOperations(t *testing.T) {
	t.Run("executes queued operations", func(t *testing.T) {
		co := NewCoordinator(NewSyncerRegistry(), Callbacks{})
		co.state.Store(int32(StateRunning))
		co.setCommitTarget(newTestSyncTarget(100))
		co.AddBlockOperation(newMockBlock(100), OpAccept)

		require.NoError(t, co.ProcessQueuedBlockOperations(t.Context()))
		require.Equal(t, StateExecutingBatch, co.CurrentState())
	})

	t.Run("returns error on block operation failure", func(t *testing.T) {
		co := NewCoordinator(NewSyncerRegistry(), Callbacks{})
		co.state.Store(int32(StateRunning))
		co.setCommitTarget(newTestSyncTarget(100))

		failBlock := newMockBlock(100)
		failBlock.acceptErr = errors.New("accept failed")
		co.AddBlockOperation(failBlock, OpAccept)

		err := co.ProcessQueuedBlockOperations(t.Context())
		require.ErrorIs(t, err, errBatchOperationFailed)
	})

	t.Run("fails closed when already aborted", func(t *testing.T) {
		calledFinalize := false
		co := NewCoordinator(NewSyncerRegistry(), Callbacks{
			FinalizeVM: func(context.Context, message.Syncable) error {
				calledFinalize = true
				return nil
			},
		})
		co.state.Store(int32(StateAborted))
		co.setCommitTarget(newTestSyncTarget(100))

		err := co.ProcessQueuedBlockOperations(t.Context())
		require.ErrorIs(t, err, errInvalidState)
		require.False(t, calledFinalize)
		require.Equal(t, StateAborted, co.CurrentState())
	})

	t.Run("requires commit target", func(t *testing.T) {
		co := NewCoordinator(NewSyncerRegistry(), Callbacks{})
		co.state.Store(int32(StateRunning))

		err := co.ProcessQueuedBlockOperations(t.Context())
		require.ErrorIs(t, err, errCommitTargetRequired)
		require.Equal(t, StateAborted, co.CurrentState())
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
	var hashBytes [8]byte
	binary.BigEndian.PutUint64(hashBytes[:], height)
	hash := common.BytesToHash(hashBytes[:])

	var rootBytes [8]byte
	binary.BigEndian.PutUint64(rootBytes[:], height+1)
	root := common.BytesToHash(rootBytes[:])
	return newSyncTarget(hash, root, height)
}
