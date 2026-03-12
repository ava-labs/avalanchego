// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/sync/session"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

func newTestSessionedQueue(t *testing.T, opts ...SessionedQueueOption) *SessionedQueue {
	t.Helper()
	db := rawdb.NewMemoryDatabase()
	q, err := NewSessionedQueue(db, nil, opts...)
	require.NoError(t, err)
	return q
}

// assertSessionBoundaryInvariants drains the event channel and verifies:
//   - at most one active session at a time
//   - no duplicate session starts
//   - session end only for the active session
//   - code hash events only within an active, non-ended session
func assertSessionBoundaryInvariants(t *testing.T, events <-chan Event) {
	t.Helper()
	var (
		started = make(map[session.ID]struct{})
		ended   = make(map[session.ID]struct{})
		active  session.ID
	)
	for ev := range events {
		switch ev.Type {
		case EventSessionStart:
			require.Zero(t, active, "concurrent active sessions: active=%d new=%d", active, ev.SessionID)
			require.NotContains(t, started, ev.SessionID, "duplicate session start")
			started[ev.SessionID] = struct{}{}
			active = ev.SessionID
		case EventSessionEnd:
			require.Equal(t, active, ev.SessionID)
			require.Contains(t, started, ev.SessionID)
			ended[ev.SessionID] = struct{}{}
			active = 0
		case EventCodeHash:
			require.Equal(t, active, ev.SessionID)
			require.Contains(t, started, ev.SessionID)
			require.NotContains(t, ended, ev.SessionID)
		}
	}
}

func TestSessionedQueue_StartAndPivotEmitsSessionBoundaries(t *testing.T) {
	t.Parallel()

	q := newTestSessionedQueue(t, WithSessionedCapacity(10))

	sid1, err := q.Start(common.Hash{1})
	require.NoError(t, err)
	require.NotZero(t, sid1)

	sid2, changed, err := q.PivotTo(common.Hash{2})
	require.NoError(t, err)
	require.True(t, changed)
	require.NotEqual(t, sid1, sid2)

	e1 := <-q.Events()
	require.Equal(t, EventSessionStart, e1.Type)
	require.Equal(t, sid1, e1.SessionID)

	e2 := <-q.Events()
	require.Equal(t, EventSessionEnd, e2.Type)
	require.Equal(t, sid1, e2.SessionID)

	e3 := <-q.Events()
	require.Equal(t, EventSessionStart, e3.Type)
	require.Equal(t, sid2, e3.SessionID)
}

func TestSessionedQueue_PivotClearsCodeToFetchMarkers(t *testing.T) {
	t.Parallel()

	db := rawdb.NewMemoryDatabase()
	q, err := NewSessionedQueue(db, nil, WithSessionedCapacity(10))
	require.NoError(t, err)

	_, err = q.Start(common.Hash{1})
	require.NoError(t, err)

	h1 := crypto.Keccak256Hash([]byte("code-1"))
	require.NoError(t, q.AddCode(t.Context(), []common.Hash{h1}))

	it := customrawdb.NewCodeToFetchIterator(db)
	require.True(t, it.Next())
	it.Release()

	_, changed, err := q.PivotTo(common.Hash{2})
	require.NoError(t, err)
	require.True(t, changed)

	it = customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()
	require.False(t, it.Next())
	require.NoError(t, it.Error())
}

func TestSessionedQueue_StartFailsCleanlyAfterFinalize(t *testing.T) {
	t.Parallel()

	q := newTestSessionedQueue(t)
	require.NoError(t, q.Finalize())

	_, err := q.Start(common.Hash{1})
	require.ErrorIs(t, err, errFailedToFinalizeCodeQueue)
}

func TestSessionedQueue_PivotToSameRootIsNoop(t *testing.T) {
	t.Parallel()

	q := newTestSessionedQueue(t, WithSessionedCapacity(10))

	sid, err := q.Start(common.Hash{1})
	require.NoError(t, err)

	next, changed, err := q.PivotTo(common.Hash{1})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, sid, next)

	ev := <-q.Events()
	require.Equal(t, EventSessionStart, ev.Type)
	require.Equal(t, sid, ev.SessionID)
	select {
	case ev := <-q.Events():
		require.FailNowf(t, "unexpected extra event", "event=%+v", ev)
	default:
	}
}

func TestSessionedQueue_PivotToFailsFastOnBackpressure(t *testing.T) {
	t.Parallel()

	q := newTestSessionedQueue(t,
		WithSessionedCapacity(1),
		WithSessionBoundarySendTimeout(25*time.Millisecond),
	)

	_, err := q.Start(common.Hash{1})
	require.NoError(t, err)

	start := time.Now()
	_, _, err = q.PivotTo(common.Hash{2})
	require.ErrorIs(t, err, errSessionBoundarySendTimeout)
	require.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestSessionedQueue_ConcurrentAddCodeAndPivot_BoundariesHold(t *testing.T) {
	t.Parallel()

	q := newTestSessionedQueue(t, WithSessionedCapacity(20_000))

	_, err := q.Start(common.Hash{1})
	require.NoError(t, err)

	const (
		numPivots = 50
		numAdds   = 500
	)

	var wg sync.WaitGroup
	pivotErrCh := make(chan error, numPivots)
	addErrCh := make(chan error, numAdds)
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := range numPivots {
			_, _, err := q.PivotTo(common.Hash{byte(i + 2)})
			pivotErrCh <- err
		}
	}()

	go func() {
		defer wg.Done()
		for i := range numAdds {
			h := crypto.Keccak256Hash([]byte{byte(i), byte(i >> 8)})
			addErrCh <- q.AddCode(t.Context(), []common.Hash{h})
		}
	}()

	wg.Wait()
	close(pivotErrCh)
	close(addErrCh)
	for err := range pivotErrCh {
		require.NoError(t, err)
	}
	for err := range addErrCh {
		require.NoError(t, err)
	}

	require.NoError(t, q.Finalize())
	assertSessionBoundaryInvariants(t, q.Events())
}

func TestSessionedQueue_ConcurrentAddCodeAndFinalize_CloseSafe(t *testing.T) {
	t.Parallel()

	q := newTestSessionedQueue(t, WithSessionedCapacity(10_000))

	_, err := q.Start(common.Hash{1})
	require.NoError(t, err)

	errCh := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		for i := range 10_000 {
			h := crypto.Keccak256Hash([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
			if err := q.AddCode(t.Context(), []common.Hash{h}); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()

	<-started
	require.NoError(t, q.Finalize())
	errAdd := <-errCh

	if errAdd != nil {
		require.ErrorIs(t, errAdd, errFailedToFinalizeCodeQueue)
	}

	for range q.Events() {
	}
}
