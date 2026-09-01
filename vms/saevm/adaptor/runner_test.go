// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adaptor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var errSync = errors.New("sync failed")

// TestRunnerLifecycle checks the happy path: Start runs fn, WaitForEvent
// returns StateSyncDone once fn finishes, and Err returns fn's result.
func TestRunnerLifecycle(t *testing.T) {
	tests := []struct {
		name    string
		syncErr error
	}{
		{name: "sync_succeeds", syncErr: nil},
		{name: "sync_fails", syncErr: errSync},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRunner()
			require.Truef(t, r.Start(func(context.Context) error {
				return tt.syncErr
			}), "%T.Start()", r)

			msg, err := r.WaitForEvent(t.Context())
			require.NoErrorf(t, err, "%T.WaitForEvent()", r)
			// StateSyncDone is returned even on sync failure: the engine has
			// no failure message, the error surfaces via Err at SetState.
			require.Equalf(t, common.StateSyncDone, msg, "%T.WaitForEvent()", r)
			require.ErrorIsf(t, r.Err(), tt.syncErr, "%T.Err()", r)
		})
	}
}

// TestRunnerStartOnce checks that a second Start is refused and does not run
// its function.
func TestRunnerStartOnce(t *testing.T) {
	r := NewRunner()
	release := make(chan struct{})
	require.Truef(t, r.Start(func(context.Context) error {
		<-release
		return nil
	}), "%T.Start() first call", r)

	require.Falsef(t, r.Start(func(context.Context) error {
		t.Error("second Start ran its function")
		return nil
	}), "%T.Start() second call", r)

	close(release)
	_, err := r.WaitForEvent(t.Context())
	require.NoErrorf(t, err, "%T.WaitForEvent()", r)
}

// TestRunnerStartAfterShutdown checks that Shutdown prevents future syncs.
func TestRunnerStartAfterShutdown(t *testing.T) {
	r := NewRunner()
	require.NoErrorf(t, r.Shutdown(t.Context()), "%T.Shutdown() with no sync", r)
	require.Falsef(t, r.Start(func(context.Context) error {
		t.Error("Start after Shutdown ran its function")
		return nil
	}), "%T.Start() after Shutdown", r)
}

// TestRunnerWaitForEventCanceled checks that WaitForEvent respects its
// context while a sync is still running.
func TestRunnerWaitForEventCanceled(t *testing.T) {
	r := NewRunner()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	require.Truef(t, r.Start(func(context.Context) error {
		<-release
		return nil
	}), "%T.Start()", r)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	msg, err := r.WaitForEvent(ctx)
	require.ErrorIsf(t, err, context.Canceled, "%T.WaitForEvent() canceled", r)
	require.Equalf(t, common.Message(0), msg, "%T.WaitForEvent() canceled", r)
}

// TestRunnerShutdownCancelsSync checks that Shutdown cancels the sync's
// context and waits for the goroutine to exit.
func TestRunnerShutdownCancelsSync(t *testing.T) {
	r := NewRunner()
	require.Truef(t, r.Start(func(ctx context.Context) error {
		<-ctx.Done() // stall until canceled, as a peerless sync would
		return context.Cause(ctx)
	}), "%T.Start()", r)

	require.NoErrorf(t, r.Shutdown(t.Context()), "%T.Shutdown()", r)

	msg, err := r.WaitForEvent(t.Context())
	require.NoErrorf(t, err, "%T.WaitForEvent() after Shutdown", r)
	require.Equalf(t, common.StateSyncDone, msg, "%T.WaitForEvent() after Shutdown", r)
	require.ErrorIsf(t, r.Err(), context.Canceled, "%T.Err() after Shutdown", r)
}

// TestRunnerShutdownCtxExpired checks that Shutdown returns the context's
// error if the sync goroutine does not exit in time.
func TestRunnerShutdownCtxExpired(t *testing.T) {
	r := NewRunner()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	require.Truef(t, r.Start(func(context.Context) error {
		<-release // ignores cancellation
		return nil
	}), "%T.Start()", r)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIsf(t, r.Shutdown(ctx), context.Canceled, "%T.Shutdown() with expired ctx", r)
}
