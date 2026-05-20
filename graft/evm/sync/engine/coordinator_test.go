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

func TestCoordinator_UpdateSyncTarget_SerializesConcurrentCalls(t *testing.T) {
	var (
		inUpdate   atomic.Int32
		concurrent atomic.Bool
	)

	registry := NewSyncerRegistry()
	require.NoError(t, registry.Register(FuncSyncer{
		name: "serial-check",
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
		co.AddBlockOperation(newMockBlock(101), OpAccept)

		require.NoError(t, co.ProcessQueuedBlockOperations(t.Context()))
		require.Equal(t, StateExecutingBatch, co.CurrentState())
	})

	t.Run("returns error on block operation failure", func(t *testing.T) {
		co := NewCoordinator(NewSyncerRegistry(), Callbacks{})
		co.state.Store(int32(StateRunning))
		co.setCommitTarget(newTestSyncTarget(100))

		failBlock := newMockBlock(101)
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

func TestCoordinator_PivotCycleBlockReplay(t *testing.T) {
	tests := []struct {
		name             string
		initialHeight    uint64
		blockLo, blockHi uint64
		pivots           []uint64
		wantFinalHeight  uint64
		wantReplayedLo   uint64
		wantReplayedHi   uint64
	}{
		{
			name:            "single pivot prunes below target",
			initialHeight:   500,
			blockLo:         490,
			blockHi:         510,
			pivots:          []uint64{505},
			wantFinalHeight: 505,
			wantReplayedLo:  506,
			wantReplayedHi:  510,
		},
		{
			name:            "two pivots advance commit target",
			initialHeight:   100,
			blockLo:         100,
			blockHi:         200,
			pivots:          []uint64{150, 180},
			wantFinalHeight: 180,
			wantReplayedLo:  181, // commit-target block (180) is pruned before replay (handled by FinalizeVM)
			wantReplayedHi:  200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var started sync.WaitGroup
			started.Add(1)
			release := make(chan struct{})

			registry := NewSyncerRegistry()
			require.NoError(t, registry.Register(NewBarrierSyncer("syncer", &started, release)))

			done := make(chan error, 1)
			var finalizedTarget message.Syncable
			co := NewCoordinator(registry, Callbacks{
				FinalizeVM: func(_ context.Context, target message.Syncable) error {
					finalizedTarget = target
					return nil
				},
				OnDone: func(err error) { done <- err },
			}, WithPivotInterval(1))

			co.Start(t.Context(), newTestSyncTarget(tt.initialHeight))
			started.Wait()

			blocks := enqueueBlockRange(t, co, tt.blockLo, tt.blockHi)

			for i, h := range tt.pivots {
				require.NoError(t, co.UpdateSyncTarget(newTestSyncTarget(h)))
				require.Equal(t, uint64(i+1), co.targetEpoch.Load())
			}

			close(release)
			require.NoError(t, <-done)

			require.NotNil(t, finalizedTarget)
			require.Equal(t, tt.wantFinalHeight, finalizedTarget.Height())
			require.Equal(t, StateCompleted, co.CurrentState())

			if tt.blockLo < tt.wantReplayedLo {
				requireBlocksNotReplayed(t, blocks, tt.blockLo, tt.wantReplayedLo-1)
			}
			requireBlocksReplayed(t, blocks, tt.wantReplayedLo, tt.wantReplayedHi)
		})
	}
}

func TestCoordinator_UpdateTargetFailureAborts(t *testing.T) {
	wantErr := errors.New("syncer rejected update")

	var started sync.WaitGroup
	started.Add(1)
	release := make(chan struct{})

	registry := NewSyncerRegistry()
	// Register a barrier syncer that also fails on UpdateTarget.
	syncer := NewBarrierSyncer("failing-update", &started, release)
	syncer.updateFn = func(message.Syncable) error { return wantErr }
	require.NoError(t, registry.Register(syncer))

	done := make(chan error, 1)
	calledFinalize := false
	co := NewCoordinator(registry, Callbacks{
		FinalizeVM: func(context.Context, message.Syncable) error {
			calledFinalize = true
			return nil
		},
		OnDone: func(err error) { done <- err },
	}, WithPivotInterval(1))

	co.Start(t.Context(), newTestSyncTarget(100))
	started.Wait()

	// Enqueue blocks so we can verify they're NOT replayed after abort.
	blocks := enqueueBlockRange(t, co, 100, 110)

	// UpdateSyncTarget should fail and abort the coordinator.
	err := co.UpdateSyncTarget(newTestSyncTarget(105))
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, StateAborted, co.CurrentState())

	// The syncer is still blocked on the barrier. Release it so the
	// coordinator can finish. The syncer will see context cancellation
	// because abort cancels the coordinator context.
	close(release)
	err = <-done
	require.ErrorIs(t, err, wantErr)

	// FinalizeVM should NOT have been called since the coordinator aborted.
	require.False(t, calledFinalize, "FinalizeVM should not be called after abort")

	// No blocks should have been replayed.
	requireBlocksNotReplayed(t, blocks, 100, 110)

	// Further state transitions should be rejected.
	require.Equal(t, StateAborted, co.CurrentState())
	err = co.UpdateSyncTarget(newTestSyncTarget(200))
	require.ErrorIs(t, err, errInvalidState)
}

// enqueueBlockRange creates mock blocks for the inclusive range [lo, hi] and
// enqueues them as OpAccept on the coordinator. Returns the block map.
func enqueueBlockRange(t *testing.T, co *Coordinator, lo, hi uint64) map[uint64]*mockEthBlockWrapper {
	t.Helper()
	blocks := make(map[uint64]*mockEthBlockWrapper, hi-lo+1)
	for i := lo; i <= hi; i++ {
		b := newMockBlock(i)
		blocks[i] = b
		require.True(t, co.AddBlockOperation(b, OpAccept))
	}
	return blocks
}

// requireBlocksReplayed asserts that every block in [lo, hi] was accepted exactly once.
func requireBlocksReplayed(t *testing.T, blocks map[uint64]*mockEthBlockWrapper, lo, hi uint64) {
	t.Helper()
	for i := lo; i <= hi; i++ {
		require.Equal(t, 1, blocks[i].acceptCount, "block %d should have been replayed", i)
	}
}

// requireBlocksNotReplayed asserts that every block in [lo, hi] was never accepted.
func requireBlocksNotReplayed(t *testing.T, blocks map[uint64]*mockEthBlockWrapper, lo, hi uint64) {
	t.Helper()
	for i := lo; i <= hi; i++ {
		require.Equal(t, 0, blocks[i].acceptCount, "block %d should have been pruned", i)
	}
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
