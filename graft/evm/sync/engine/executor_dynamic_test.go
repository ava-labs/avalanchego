// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/message"
)

type noopAcceptor struct{}

func (noopAcceptor) AcceptSync(context.Context, message.Syncable) error { return nil }

type targetTrackingSyncer struct {
	name      string
	id        string
	updateErr error
	updates   int
}

func (*targetTrackingSyncer) Sync(context.Context) error { return nil }
func (s *targetTrackingSyncer) Name() string             { return s.name }
func (s *targetTrackingSyncer) ID() string               { return s.id }
func (s *targetTrackingSyncer) UpdateTarget(message.Syncable) error {
	s.updates++
	return s.updateErr
}

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
			syncer := &targetTrackingSyncer{
				name:      "target-tracker",
				id:        "target-tracker",
				updateErr: tt.updateErr,
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
				require.Equal(t, wantUpdates, syncer.updates)

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
			require.NoError(t, registry.Register(&targetTrackingSyncer{name: "target-tracker", id: "target-tracker"}))

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
