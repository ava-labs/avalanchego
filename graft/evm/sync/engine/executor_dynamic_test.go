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
			require.True(t, deferred)
			require.ErrorIs(t, err, tt.wantErr)
			wantUpdates := 0
			if tt.state == StateRunning {
				wantUpdates = 1
			}
			require.Equal(t, wantUpdates, syncer.updates)

			queued := executor.coordinator.queue.dequeueBatch()
			require.Len(t, queued, 1)
			require.Equal(t, OpAccept, queued[0].operation)
		})
	}
}

func TestDynamicExecutor_OnBlockRejectedAndVerified(t *testing.T) {
	tests := []struct {
		name   string
		call   func(*dynamicExecutor, EthBlockWrapper) (bool, error)
		wantOp BlockOperationType
	}{
		{
			name:   "reject is deferred",
			call:   (*dynamicExecutor).OnBlockRejected,
			wantOp: OpReject,
		},
		{
			name:   "verify is deferred",
			call:   (*dynamicExecutor).OnBlockVerified,
			wantOp: OpVerify,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSyncerRegistry()
			require.NoError(t, registry.Register(&targetTrackingSyncer{name: "target-tracker", id: "target-tracker"}))

			executor := newDynamicExecutor(registry, noopAcceptor{}, 1)
			executor.coordinator.state.Store(int32(StateRunning))

			deferred, err := tt.call(executor, newMockBlock(100))
			require.True(t, deferred)
			require.NoError(t, err)

			queued := executor.coordinator.queue.dequeueBatch()
			require.Len(t, queued, 1)
			require.Equal(t, tt.wantOp, queued[0].operation)
		})
	}
}
