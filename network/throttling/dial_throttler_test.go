// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Test that the DialThrottler returned by NewDialThrottler works
func TestDialThrottler(t *testing.T) {
	require := require.New(t)

	startTime := time.Now()
	// Allows 5 per second
	throttler := NewDialThrottler(5)
	// Use all 5
	for i := 0; i < 5; i++ {
		acquiredChan := make(chan error)
		// Should return immediately because < 5 taken this second
		go func() {
			acquiredChan <- throttler.Acquire(context.Background())
		}()

		select {
		case <-time.After(10 * time.Millisecond):
			require.FailNow("should have acquired immediately")
		case err := <-acquiredChan:
			require.NoError(err)
		}
		close(acquiredChan)
	}

	acquiredChan := make(chan error)
	go func() {
		// Should block because 5 already taken within last second
		acquiredChan <- throttler.Acquire(context.Background())
	}()

	select {
	case <-time.After(25 * time.Millisecond):
	case <-acquiredChan:
		require.FailNow("should not have been able to acquire immediately")
	}

	// Wait until the 6th Acquire() has returned. The time at which
	// that returns should be no more than 1s after the time at which
	// the first Acquire() returned.
	require.NoError(<-acquiredChan)
	// Use 1.05 seconds instead of 1 second to give some "wiggle room"
	// so test doesn't flake
	require.LessOrEqual(time.Since(startTime), 1050*time.Millisecond)
}

// Test that Acquire honors its specification about its context being canceled
func TestDialThrottlerCancel(t *testing.T) {
	require := require.New(t)

	// Allows 5 per second
	throttler := NewDialThrottler(5)
	// Use all 5
	for i := 0; i < 5; i++ {
		acquiredChan := make(chan error)
		// Should return immediately because < 5 taken this second
		go func() {
			acquiredChan <- throttler.Acquire(context.Background())
		}()

		select {
		case <-time.After(10 * time.Millisecond):
			require.FailNow("should have acquired immediately")
		case err := <-acquiredChan:
			require.NoError(err)
		}
	}

	acquiredChan := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the 6th acquire
	cancel()

	go func() {
		acquiredChan <- throttler.Acquire(ctx)
	}()

	select {
	case err := <-acquiredChan:
		require.ErrorIs(err, context.Canceled)
	case <-time.After(10 * time.Millisecond):
		require.FailNow("Acquire should have returned immediately upon context cancellation")
	}
}

// Test that the Throttler return by NewNoThrottler never blocks on Acquire()
func TestNoDialThrottler(t *testing.T) {
	require := require.New(t)

	throttler := NewNoDialThrottler()
	for i := 0; i < 250; i++ {
		startTime := time.Now()
		require.NoError(throttler.Acquire(context.Background())) // Should always immediately return
		require.WithinDuration(time.Now(), startTime, 25*time.Millisecond)
	}
}
