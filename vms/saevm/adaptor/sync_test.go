// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adaptor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	errShould = errors.New("should failed")

	_ SyncableVM[stubSummary] = (*stubSyncableVM)(nil)
)

type stubSummary struct{}

func (stubSummary) ID() ids.ID     { return ids.Empty }
func (stubSummary) Bytes() []byte  { return nil }
func (stubSummary) Height() uint64 { return 1 }

// stubSyncableVM lets each test choose the ShouldAcceptSummary result; Sync
// records whether it ran.
type stubSyncableVM struct {
	should    bool
	shouldErr error
	synced    chan struct{}
}

func (vm *stubSyncableVM) StateSyncEnabled(context.Context) (bool, error) { return true, nil }
func (vm *stubSyncableVM) GetLastStateSummary(context.Context) (stubSummary, error) {
	return stubSummary{}, nil
}
func (vm *stubSyncableVM) GetOngoingSyncStateSummary(context.Context) (stubSummary, error) {
	return stubSummary{}, nil
}
func (vm *stubSyncableVM) GetStateSummary(context.Context, uint64) (stubSummary, error) {
	return stubSummary{}, nil
}
func (vm *stubSyncableVM) ParseStateSummary(context.Context, []byte) (stubSummary, error) {
	return stubSummary{}, nil
}
func (vm *stubSyncableVM) ShouldAcceptSummary(context.Context, stubSummary) (bool, error) {
	return vm.should, vm.shouldErr
}
func (vm *stubSyncableVM) Sync(context.Context, stubSummary) error {
	close(vm.synced)
	return nil
}

// TestSummaryAcceptModes checks the [block.StateSyncMode] mapping performed
// by [Summary.Accept].
func TestSummaryAcceptModes(t *testing.T) {
	tests := []struct {
		name       string
		vm         *stubSyncableVM
		stopRunner bool
		wantMode   block.StateSyncMode
		wantErr    error
		wantSynced bool
	}{
		{
			name:       "should_accept_starts_sync",
			vm:         &stubSyncableVM{should: true},
			wantMode:   block.StateSyncStatic,
			wantSynced: true,
		},
		{
			name:     "should_not_accept_skips",
			vm:       &stubSyncableVM{should: false},
			wantMode: block.StateSyncSkipped,
		},
		{
			name:     "should_error_propagates",
			vm:       &stubSyncableVM{should: true, shouldErr: errShould},
			wantMode: block.StateSyncSkipped,
			wantErr:  errShould,
		},
		{
			name:       "stopped_runner_skips",
			vm:         &stubSyncableVM{should: true},
			stopRunner: true,
			wantMode:   block.StateSyncSkipped,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.vm.synced = make(chan struct{})
			runner := NewRunner()
			if tt.stopRunner {
				require.NoErrorf(t, runner.Shutdown(t.Context()), "%T.Shutdown()", runner)
			}

			syncer := ConvertStateSync[stubSummary](tt.vm, runner)
			s, err := syncer.GetLastStateSummary(t.Context())
			require.NoErrorf(t, err, "%T.GetLastStateSummary()", syncer)

			mode, err := s.Accept(t.Context())
			require.ErrorIsf(t, err, tt.wantErr, "%T.Accept()", s)
			require.Equalf(t, tt.wantMode, mode, "%T.Accept() mode", s)

			if tt.wantSynced {
				_, err := runner.WaitForEvent(t.Context())
				require.NoErrorf(t, err, "%T.WaitForEvent()", runner)
				select {
				case <-tt.vm.synced:
				default:
					t.Fatal("Accept() returned StateSyncStatic but Sync never ran")
				}
			}
		})
	}
}
