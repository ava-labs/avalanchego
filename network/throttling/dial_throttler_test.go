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
		acquiredChan := make(chan struct{}, 1)
		// Should return immediately because < 5 taken this second
		go func() {
			require.NoError(throttler.Acquire(context.Background()))
			acquiredChan <- struct{}{}
		}()
		select {
		case <-time.After(10 * time.Millisecond):
			require.FailNow("should have acquired immediately")
		case <-acquiredChan:
		}
		close(acquiredChan)
	}

	acquiredChan := make(chan struct{}, 1)
	go func() {
		// Should block because 5 already taken within last second
		require.NoError(throttler.Acquire(context.Background()))
		acquiredChan <- struct{}{}
	}()

	select {
	case <-time.After(25 * time.Millisecond):
	case <-acquiredChan:
		require.FailNow("should not have been able to acquire immediately")
	}

	// Wait until the 6th Acquire() has returned. The time at which
	// that returns should be no more than 1s after the time at which
	// the first Acquire() returned.
	<-acquiredChan
	close(acquiredChan)
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
		acquiredChan := make(chan struct{}, 1)
		// Should return immediately because < 5 taken this second
		go func() {
			require.NoError(throttler.Acquire(context.Background()))
			acquiredChan <- struct{}{}
		}()
		select {
		case <-time.After(10 * time.Millisecond):
			require.FailNow("should have acquired immediately")
		case <-acquiredChan:
		}
		close(acquiredChan)
	}

	acquiredChan := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Should block because 5 already taken within last second
		err := throttler.Acquire(ctx)
		// Should error because we call cancel() below
		require.ErrorIs(err, context.Canceled)
		acquiredChan <- struct{}{}
	}()

	// Cancel the 6th acquire
	cancel()
	select {
	case <-acquiredChan:
	case <-time.After(10 * time.Millisecond):
		require.FailNow("Acquire should have returned immediately upon context cancellation")
	}
	close(acquiredChan)
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
