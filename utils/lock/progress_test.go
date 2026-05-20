// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProgressSubscriptionInitialProgressUnblocksWaiterImmediately(t *testing.T) {
	ps := NewProgressSubscription[int](10)

	// Waiting for a value below the initial progress should return immediately.
	done := make(chan struct{})
	go func() {
		require.NoError(t, ps.WaitForProgress(t.Context(), 5))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatal("WaitForProgress should have returned immediately")
	}
}

func TestProgressSubscriptionWaiterBlocksUntilProgressAdvances(t *testing.T) {
	ps := NewProgressSubscription(0)

	var wg sync.WaitGroup
	wg.Add(1)

	done := make(chan struct{})
	go func() {
		defer wg.Done()
		require.NoError(t, ps.WaitForProgress(t.Context(), 5))
		close(done)
	}()

	// The goroutine should be blocked since progress is 0.
	select {
	case <-done:
		t.Fatal("WaitForProgress should not have returned yet")
	case <-time.After(50 * time.Millisecond):
	}

	ps.SetProgress(10)

	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatal("WaitForProgress should have returned after SetProgress")
	}

	wg.Wait()
}

func TestProgressSubscriptionContextCancellationUnblocksWaiter(t *testing.T) {
	ps := NewProgressSubscription(0)

	ctx, cancel := context.WithCancel(t.Context())

	var wg sync.WaitGroup
	wg.Add(1)

	done := make(chan struct{})
	go func() {
		defer wg.Done()
		err := ps.WaitForProgress(ctx, 5)
		require.ErrorIs(t, err, context.Canceled)
		close(done)
	}()

	// The goroutine should be blocked.
	select {
	case <-done:
		t.Fatal("WaitForProgress should not have returned yet")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatal("WaitForProgress should have returned after context cancellation")
	}

	wg.Wait()
}

func TestProgressSubscriptionMultipleWaitersUnblockedBySingleSetProgress(t *testing.T) {
	ps := NewProgressSubscription(0)

	const numWaiters = 5

	var wg sync.WaitGroup
	wg.Add(numWaiters)

	done := make([]chan struct{}, numWaiters)
	for i := range numWaiters {
		done[i] = make(chan struct{})
	}

	for i := range numWaiters {
		go func() {
			defer wg.Done()
			require.NoError(t, ps.WaitForProgress(t.Context(), i))
			close(done[i])
		}()
	}

	// Give goroutines time to start waiting.
	time.Sleep(50 * time.Millisecond)

	ps.SetProgress(numWaiters)

	for i := range numWaiters {
		select {
		case <-done[i]:
		case <-time.After(time.Minute):
			t.Fatal("waiter should have been unblocked")
		}
	}

	wg.Wait()
}

func TestProgressSubscriptionEqualProgressBlocks(t *testing.T) {
	ps := NewProgressSubscription[int](5)

	done := make(chan struct{})
	go func() {
		require.NoError(t, ps.WaitForProgress(t.Context(), 5))
		close(done)
	}()

	// Waiting for exactly the current progress should block (condition is pos >= progress).
	select {
	case <-done:
		t.Fatal("WaitForProgress should block when pos == progress")
	case <-time.After(50 * time.Millisecond):
	}

	ps.SetProgress(6)

	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatal("WaitForProgress should have returned after progress exceeded pos")
	}
}
