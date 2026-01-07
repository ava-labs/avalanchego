// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManager_StartAndCurrent(t *testing.T) {
	t.Parallel()

	m := NewManager("root1")
	s, ok := m.Current()
	require.False(t, ok)
	require.Equal(t, Session[string]{}, s)

	session, ctx := m.Start(t.Context())
	require.Equal(t, ID(1), session.ID)
	require.Equal(t, "root1", session.Target)
	require.NotNil(t, ctx)

	cur, ok := m.Current()
	require.True(t, ok)
	require.Equal(t, session.ID, cur.ID)
	require.Equal(t, session.Target, cur.Target)
}

func TestManager_RequestPivotBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		withEqual     bool
		initialTarget string
		pivotTarget   string

		wantRequested bool
		wantCanceled  bool
	}{
		{
			name:          "pivot_to_new_target_cancels",
			withEqual:     true,
			initialTarget: "root1",
			pivotTarget:   "root2",
			wantRequested: true,
			wantCanceled:  true,
		},
		{
			name:          "pivot_to_same_target_is_noop",
			withEqual:     true,
			initialTarget: "root1",
			pivotTarget:   "root1",
			wantRequested: false,
			wantCanceled:  false,
		},
		{
			name:          "pivot_to_same_target_without_equal_still_requests",
			withEqual:     false,
			initialTarget: "root1",
			pivotTarget:   "root1",
			wantRequested: true,
			wantCanceled:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var opts []Option[string]
			if tt.withEqual {
				opts = append(opts, WithComparableEqual[string]())
			}
			m := NewManager(tt.initialTarget, opts...)
			_, ctx := m.Start(t.Context())

			require.Equal(t, tt.wantRequested, m.RequestPivot(tt.pivotTarget))

			if tt.wantCanceled {
				requireContextCanceledWithCause(t, ctx, ErrPivotRequested)
			} else {
				requireContextNotCanceled(t, ctx)
			}
		})
	}
}

func TestManager_RequestPivotBackToCurrentCancelsPendingRestart(t *testing.T) {
	t.Parallel()

	m := NewManager("root1", WithComparableEqual[string]())
	_, ctx := m.Start(t.Context())

	// Request a pivot away; this should cancel the current session and set pivotPending.
	require.True(t, m.RequestPivot("root2"))
	requireContextCanceledWithCause(t, ctx, ErrPivotRequested)

	// Now "pivot back" to the currently running target. This should cancel the pending
	// restart and align desired without forcing another restart.
	require.False(t, m.RequestPivot("root1"))

	_, _, ok := m.RestartIfPending(t.Context())
	require.False(t, ok)
	require.Equal(t, "root1", m.DesiredTarget())
}

func TestManager_RestartIfPendingUsesLatestTarget(t *testing.T) {
	t.Parallel()

	m := NewManager("root1")
	_, ctx1 := m.Start(t.Context())

	// Two pivots; only latest should win.
	m.RequestPivot("root2")
	m.RequestPivot("root3")

	s2, ctx2, ok := m.RestartIfPending(t.Context())
	require.True(t, ok)
	require.Equal(t, ID(2), s2.ID)
	require.Equal(t, "root3", s2.Target)
	require.NotNil(t, ctx2)

	// No pending pivot now.
	_, _, ok = m.RestartIfPending(t.Context())
	require.False(t, ok)

	// Ensure the current session is updated.
	cur, ok := m.Current()
	require.True(t, ok)
	require.Equal(t, s2.ID, cur.ID)
	require.Equal(t, s2.Target, cur.Target)

	// Ensure old session was canceled (best effort).
	require.True(t, errors.Is(context.Cause(ctx1), ErrPivotRequested) || errors.Is(ctx1.Err(), context.Canceled))
}

func TestManager_RequestPivotNoOpWhenEqual(t *testing.T) {
	t.Parallel()

	m := NewManager("root1", WithComparableEqual[string]())
	_, ctx := m.Start(t.Context())

	// Same target should be treated as no-op.
	require.False(t, m.RequestPivot("root1"))

	// Should not cancel.
	requireContextNotCanceled(t, ctx)

	_, _, ok := m.RestartIfPending(t.Context())
	require.False(t, ok)
}

func TestManager_ConcurrentPivotsCoalesce(t *testing.T) {
	t.Parallel()

	m := NewManager(0)
	_, _ = m.Start(t.Context())

	var wg sync.WaitGroup
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RequestPivot(i)
		}()
	}
	wg.Wait()

	// Desired target should be the last writer's value (nondeterministic across goroutines),
	// but RestartIfPending must start a new session with whatever desired target is final.
	desired := m.DesiredTarget()
	next, _, ok := m.RestartIfPending(t.Context())
	require.True(t, ok)
	require.Equal(t, desired, next.Target)
}

func requireContextCanceledWithCause(t testing.TB, ctx context.Context, want error) {
	t.Helper()
	select {
	case <-ctx.Done():
		cause := context.Cause(ctx)
		require.ErrorIs(t, cause, want)
	case <-time.After(1 * time.Second):
		require.FailNow(t, "timed out waiting for cancellation")
	}
}

func requireContextNotCanceled(t testing.TB, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
		require.FailNow(t, "unexpected cancellation", "cause: %v", context.Cause(ctx))
	default:
	}
}
