// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package session

import (
	"context"
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

	started, ctx := m.Start(t.Context())
	require.Equal(t, ID(1), started.ID)
	require.Equal(t, "root1", started.Target)
	require.NotNil(t, ctx)

	cur, ok := m.Current()
	require.True(t, ok)
	require.Equal(t, started, cur)
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
			name:          "pivot to new target cancels",
			withEqual:     true,
			initialTarget: "root1",
			pivotTarget:   "root2",
			wantRequested: true,
			wantCanceled:  true,
		},
		{
			name:          "pivot to same target is noop",
			withEqual:     true,
			initialTarget: "root1",
			pivotTarget:   "root1",
			wantRequested: false,
			wantCanceled:  false,
		},
		{
			name:          "without equal func, same target still requests",
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

	require.True(t, m.RequestPivot("root2"))
	requireContextCanceledWithCause(t, ctx, ErrPivotRequested)

	require.False(t, m.RequestPivot("root1"))
	_, _, ok := m.RestartIfPending(t.Context())
	require.False(t, ok)
	require.Equal(t, "root1", m.DesiredTarget())
}

func TestManager_RestartIfPendingUsesLatestTarget(t *testing.T) {
	t.Parallel()

	m := NewManager("root1")
	_, ctx1 := m.Start(t.Context())

	m.RequestPivot("root2")
	m.RequestPivot("root3")

	s2, ctx2, ok := m.RestartIfPending(t.Context())
	require.True(t, ok)
	require.Equal(t, ID(2), s2.ID)
	require.Equal(t, "root3", s2.Target)
	require.NotNil(t, ctx2)

	_, _, ok = m.RestartIfPending(t.Context())
	require.False(t, ok)

	cur, ok := m.Current()
	require.True(t, ok)
	require.Equal(t, s2, cur)
	requireContextCanceledWithCause(t, ctx1, ErrPivotRequested)
}

func TestManager_ConcurrentPivotsCoalesce(t *testing.T) {
	t.Parallel()

	m := NewManager(0)
	_, _ = m.Start(t.Context())

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RequestPivot(i + 1)
		}()
	}
	wg.Wait()

	desired := m.DesiredTarget()
	next, _, ok := m.RestartIfPending(t.Context())
	require.True(t, ok)
	require.Equal(t, desired, next.Target)
}

func TestManager_StartPivotRace_NoMissedPivot(t *testing.T) {
	t.Parallel()

	type result struct {
		session Session[string]
		ctx     context.Context
	}

	for range 200 {
		m := NewManager("root1", WithComparableEqual[string]())

		started := make(chan result, 1)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			s, ctx := m.Start(t.Context())
			started <- result{session: s, ctx: ctx}
		}()
		go func() {
			defer wg.Done()
			m.RequestPivot("root2")
		}()

		wg.Wait()
		res := <-started

		require.Equal(t, "root2", m.DesiredTarget())
		switch res.session.Target {
		case "root1":
			requireContextCanceledWithCause(t, res.ctx, ErrPivotRequested)
			next, _, ok := m.RestartIfPending(t.Context())
			require.True(t, ok)
			require.Equal(t, "root2", next.Target)
		case "root2":
			// Start observed desired target after pivot request.
		default:
			require.FailNowf(t, "unexpected target", "got %q", res.session.Target)
		}
	}
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
