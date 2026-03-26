// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/message"
)

type noopAcceptor struct{}

func (noopAcceptor) AcceptSync(context.Context, message.Syncable) error { return nil }

func TestDynamicExecutor_OnBlockAccepted(t *testing.T) {
	updateErr := errors.New("update failed")
	tests := []struct {
		name      string
		state     State
		updateErr error
		wantErr   error
	}{
		{
			name:      "running updates target successfully",
			state:     StateRunning,
			updateErr: nil,
			wantErr:   nil,
		},
		{
			name:      "running returns target update failure",
			state:     StateRunning,
			updateErr: updateErr,
			wantErr:   updateErr,
		},
		{
			name:      "executing batch skips target update",
			state:     StateExecutingBatch,
			updateErr: nil,
			wantErr:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updates := 0
			syncer := FuncSyncer{
				name: "target-tracker",
				updateFn: func(message.Syncable) error {
					updates++
					return tt.updateErr
				},
			}
			registry := NewSyncerRegistry()
			require.NoError(t, registry.Register(syncer))

			executor := newDynamicExecutor(registry, noopAcceptor{}, 1)
			executor.coordinator.state.Store(int32(tt.state))

			deferred, err := executor.OnBlockAccepted(newMockBlock(100))
			require.ErrorIs(t, err, tt.wantErr)

			if tt.state == StateExecutingBatch {
				// During batch replay, blocks are not re-enqueued to avoid
				// infinite re-entrancy loops.
				require.False(t, deferred)
				require.Empty(t, executor.coordinator.queue.dequeueBatch())
			} else {
				require.True(t, deferred)
				wantUpdates := 0
				if tt.state == StateRunning {
					wantUpdates = 1
				}
				require.Equal(t, wantUpdates, updates)

				if tt.updateErr != nil {
					require.Equal(t, StateAborted, executor.coordinator.CurrentState())
				} else {
					require.Equal(t, tt.state, executor.coordinator.CurrentState())
				}

				queued := executor.coordinator.queue.dequeueBatch()
				require.Len(t, queued, 1)
				require.Equal(t, OpAccept, queued[0].operation)
			}
		})
	}
}

func TestDynamicExecutor_FullPivotCycleWithBlockAcceptance(t *testing.T) {
	// End-to-end test: blocks arrive via OnBlockAccepted while syncers are
	// running, triggering a pivot. After syncers finish, batch replay
	// executes the surviving blocks.
	var started sync.WaitGroup
	started.Add(1)
	release := make(chan struct{})

	registry := NewSyncerRegistry()
	require.NoError(t, registry.Register(NewBarrierSyncer("syncer", &started, release)))

	done := make(chan error, 1)
	executor := newDynamicExecutor(registry, noopAcceptor{}, 1)
	go func() {
		done <- executor.Execute(t.Context(), newTestSyncTarget(100))
	}()

	started.Wait()

	// Simulate consensus accepting blocks while sync runs.
	// With pivotInterval=1, every block triggers a pivot, so the final
	// commit target will be 110 (the last block accepted).
	blocks := make(map[uint64]*mockEthBlockWrapper)
	for i := uint64(100); i <= 110; i++ {
		b := newMockBlock(i)
		blocks[i] = b
		deferred, err := executor.OnBlockAccepted(b)
		require.NoError(t, err)
		require.True(t, deferred, "block %d should be deferred", i)
	}

	require.Equal(t, uint64(110), executor.coordinator.getCommitTarget().Height())

	// Release syncers to complete.
	close(release)
	require.NoError(t, <-done)

	require.Equal(t, StateCompleted, executor.CurrentState())

	// Each pivot pruned blocks below the new target. With pivotInterval=1,
	// only the last block (110) survives.
	requireBlocksNotReplayed(t, blocks, 100, 109)
	requireBlocksReplayed(t, blocks, 110, 110)
}

func TestDynamicExecutor_OnBlockRejectedAndVerified(t *testing.T) {
	tests := []struct {
		name   string
		state  State
		call   func(*dynamicExecutor, EthBlockWrapper) (bool, error)
		wantOp BlockOperationType
	}{
		{
			name:   "reject is deferred",
			state:  StateRunning,
			call:   (*dynamicExecutor).OnBlockRejected,
			wantOp: OpReject,
		},
		{
			name:   "verify is deferred",
			state:  StateRunning,
			call:   (*dynamicExecutor).OnBlockVerified,
			wantOp: OpVerify,
		},
		{
			name:  "reject during batch replay is not deferred",
			state: StateExecutingBatch,
			call:  (*dynamicExecutor).OnBlockRejected,
		},
		{
			name:  "verify during batch replay is not deferred",
			state: StateExecutingBatch,
			call:  (*dynamicExecutor).OnBlockVerified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSyncerRegistry()
			require.NoError(t, registry.Register(FuncSyncer{name: "target-tracker"}))

			executor := newDynamicExecutor(registry, noopAcceptor{}, 1)
			executor.coordinator.state.Store(int32(tt.state))

			deferred, err := tt.call(executor, newMockBlock(100))
			require.NoError(t, err)

			if tt.state == StateExecutingBatch {
				require.False(t, deferred)
				require.Empty(t, executor.coordinator.queue.dequeueBatch())
			} else {
				require.True(t, deferred)
				queued := executor.coordinator.queue.dequeueBatch()
				require.Len(t, queued, 1)
				require.Equal(t, tt.wantOp, queued[0].operation)
			}
		})
	}
}
