// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acceptor

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/statesync"

	enginecommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

type Summary = statesync.Summary

var (
	_ Syncer[*Summary] = (*statesync.Syncer)(nil)
	// Paired with a type serving summaries, the [AsyncAcceptor] is a full
	// [adaptor.SyncableVM].
	_ adaptor.SyncableVM[*Summary] = (*struct {
		*AsyncAcceptor[*Summary]
		*statesync.Handler
	})(nil)
)

// fakeSyncer is a [Syncer] with configurable results that records the order
// in which its methods are called.
type fakeSyncer struct {
	skip     bool
	sync     func(context.Context) error // nil returns immediately
	writeErr error

	calls []string
}

func (f *fakeSyncer) ShouldAcceptSummary(*Summary) bool {
	f.calls = append(f.calls, "ShouldAcceptSummary")
	return !f.skip
}

func (f *fakeSyncer) Sync(ctx context.Context, _ *Summary) error {
	f.calls = append(f.calls, "Sync")
	if f.sync == nil {
		return nil
	}
	return f.sync(ctx)
}

func (f *fakeSyncer) WriteSynced(*Summary) error {
	f.calls = append(f.calls, "WriteSynced")
	return f.writeErr
}

func newAcceptor(f *fakeSyncer) *AsyncAcceptor[*Summary] {
	return New(true, func() (Syncer[*Summary], error) {
		return f, nil
	})
}

// blockUntilCanceled is a [fakeSyncer.sync] that stalls like a sync without
// any peers.
func blockUntilCanceled(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func requireSyncDone(t *testing.T, a *AsyncAcceptor[*Summary]) {
	t.Helper()
	msg, err := a.WaitForEvent(t.Context())
	require.NoErrorf(t, err, "%T.WaitForEvent()", a)
	require.Equalf(t, enginecommon.StateSyncDone, msg, "%T.WaitForEvent()", a)
}

func TestAsyncAcceptorStateSyncEnabled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		a := New[*Summary](enabled, nil)
		got, err := a.StateSyncEnabled(t.Context())
		require.NoErrorf(t, err, "%T.StateSyncEnabled()", a)
		require.Equalf(t, enabled, got, "%T.StateSyncEnabled()", a)
	}
}

func TestAsyncAcceptorGetOngoingSyncStateSummary(t *testing.T) {
	a := newAcceptor(&fakeSyncer{})
	_, err := a.GetOngoingSyncStateSummary(t.Context())
	require.ErrorIsf(t, err, database.ErrNotFound, "%T.GetOngoingSyncStateSummary()", a)
}

func TestAsyncAcceptorWaitForEventCanceled(t *testing.T) {
	a := newAcceptor(&fakeSyncer{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := a.WaitForEvent(ctx)
	require.ErrorIsf(t, err, context.Canceled, "%T.WaitForEvent() before any sync", a)
}

func TestAsyncAcceptorAcceptSummary(t *testing.T) {
	var (
		errSync  = errors.New("sync failed")
		errWrite = errors.New("write failed")
	)
	summary := statesync.NewSummary(common.Hash{0xde, 0xad}, 1)

	tests := []struct {
		name      string
		syncer    *fakeSyncer
		wantErr   error
		wantCalls []string
	}{
		{
			name:      "success",
			syncer:    &fakeSyncer{},
			wantCalls: []string{"ShouldAcceptSummary", "Sync", "WriteSynced"},
		},
		{
			name: "sync_error",
			syncer: &fakeSyncer{
				sync: func(context.Context) error { return errSync },
			},
			wantErr:   errSync,
			wantCalls: []string{"ShouldAcceptSummary", "Sync"},
		},
		{
			name:      "write_error",
			syncer:    &fakeSyncer{writeErr: errWrite},
			wantErr:   errWrite,
			wantCalls: []string{"ShouldAcceptSummary", "Sync", "WriteSynced"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAcceptor(tt.syncer)

			// Callers idiomatically cancel the context once AcceptSummary
			// returns, which MUST NOT cancel the sync.
			ctx, cancel := context.WithCancel(t.Context())
			mode, err := a.AcceptSummary(ctx, summary)
			cancel()
			require.NoErrorf(t, err, "%T.AcceptSummary()", a)
			require.Equalf(t, block.StateSyncStatic, mode, "%T.AcceptSummary()", a)

			requireSyncDone(t, a)
			require.ErrorIsf(t, a.SyncError(), tt.wantErr, "%T.SyncError()", a)
			require.Equal(t, tt.wantCalls, tt.syncer.calls, "order of Syncer calls")
			require.NoErrorf(t, a.Shutdown(t.Context()), "%T.Shutdown() after sync", a)
		})
	}
}

func TestAsyncAcceptorIgnoresCallerCancellation(t *testing.T) {
	ctxErr := make(chan error)
	f := &fakeSyncer{
		sync: func(ctx context.Context) error {
			<-ctxErr // the caller's context has been canceled
			return ctx.Err()
		},
	}
	a := newAcceptor(f)

	ctx, cancel := context.WithCancel(t.Context())
	_, err := a.AcceptSummary(ctx, statesync.NewSummary(common.Hash{0xde, 0xad}, 1))
	require.NoErrorf(t, err, "%T.AcceptSummary()", a)
	cancel()
	close(ctxErr)

	requireSyncDone(t, a)
	require.NoErrorf(t, a.SyncError(), "%T.SyncError() after caller canceled", a)
}

func TestAsyncAcceptorSkipped(t *testing.T) {
	summary := statesync.NewSummary(common.Hash{0xde, 0xad}, 1)

	t.Run("syncer_declines", func(t *testing.T) {
		f := &fakeSyncer{skip: true}
		a := newAcceptor(f)

		mode, err := a.AcceptSummary(t.Context(), summary)
		require.NoErrorf(t, err, "%T.AcceptSummary()", a)
		require.Equalf(t, block.StateSyncSkipped, mode, "%T.AcceptSummary()", a)
		require.Equal(t, []string{"ShouldAcceptSummary"}, f.calls, "Syncer calls")
		require.NoErrorf(t, a.Shutdown(t.Context()), "%T.Shutdown() without sync", a)
	})

	t.Run("after_shutdown", func(t *testing.T) {
		f := &fakeSyncer{}
		a := newAcceptor(f)
		require.NoErrorf(t, a.Shutdown(t.Context()), "%T.Shutdown()", a)

		mode, err := a.AcceptSummary(t.Context(), summary)
		require.NoErrorf(t, err, "%T.AcceptSummary()", a)
		require.Equalf(t, block.StateSyncSkipped, mode, "%T.AcceptSummary()", a)
		require.Equal(t, []string{"ShouldAcceptSummary"}, f.calls, "Syncer calls")
	})

	t.Run("constructor_error", func(t *testing.T) {
		errNew := errors.New("no syncer")
		a := New(true, func() (Syncer[*Summary], error) {
			return nil, errNew
		})
		_, err := a.AcceptSummary(t.Context(), summary)
		require.ErrorIsf(t, err, errNew, "%T.AcceptSummary()", a)
	})
}

func TestAsyncAcceptorShutdownCancelsMidSync(t *testing.T) {
	f := &fakeSyncer{sync: blockUntilCanceled}
	a := newAcceptor(f)

	mode, err := a.AcceptSummary(t.Context(), statesync.NewSummary(common.Hash{0xde, 0xad}, 1))
	require.NoErrorf(t, err, "%T.AcceptSummary()", a)
	require.Equalf(t, block.StateSyncStatic, mode, "%T.AcceptSummary()", a)

	require.NoErrorf(t, a.Shutdown(t.Context()), "%T.Shutdown()", a)

	requireSyncDone(t, a)
	require.ErrorIsf(t, a.SyncError(), context.Canceled, "%T.SyncError()", a)
	require.Equal(t, []string{"ShouldAcceptSummary", "Sync"}, f.calls, "Syncer calls")
}

func TestAsyncAcceptorShutdownTimeout(t *testing.T) {
	release := make(chan struct{})
	f := &fakeSyncer{
		sync: func(context.Context) error {
			<-release // ignores cancellation
			return nil
		},
	}
	a := newAcceptor(f)

	_, err := a.AcceptSummary(t.Context(), statesync.NewSummary(common.Hash{0xde, 0xad}, 1))
	require.NoErrorf(t, err, "%T.AcceptSummary()", a)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIsf(t, a.Shutdown(ctx), context.Canceled, "%T.Shutdown() with expired context", a)

	close(release)
	requireSyncDone(t, a)
}
