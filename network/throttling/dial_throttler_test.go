// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// Test that the DialThrottler returned by NewDialThrottler works
func TestDialThrottler(t *testing.T) {
	require := require.New(t)

	startTime := time.Now()
	// Allows 5 per second
	throttler := NewDialThrottler(5)
	// Use all 5
	for i := 0; i < 5; i++ {
		eg := errgroup.Group{}
		acquiredChan := make(chan struct{})
		// Should return immediately because < 5 taken this second
		eg.Go(func() error {
			defer close(acquiredChan)
			return throttler.Acquire(context.Background())
		})

		select {
		case <-time.After(10 * time.Millisecond):
			require.FailNow("should have acquired immediately")
		case <-acquiredChan:
			require.NoError(eg.Wait())
		}
		close(acquiredChan)
	}

	eg := errgroup.Group{}
	acquiredChan := make(chan struct{}, 1)
	eg.Go(func() error {
		defer close(acquiredChan)
		// Should block because 5 already taken within last second
		return throttler.Acquire(context.Background())
	})

	select {
	case <-time.After(25 * time.Millisecond):
	case <-acquiredChan:
		require.FailNow("should not have been able to acquire immediately")
	}

	// Wait until the 6th Acquire() has returned. The time at which
	// that returns should be no more than 1s after the time at which
	// the first Acquire() returned.
	<-acquiredChan
	require.NoError(eg.Wait())
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
		eg := errgroup.Group{}
		acquiredChan := make(chan struct{})
		// Should return immediately because < 5 taken this second
		eg.Go(func() error {
			defer close(acquiredChan)
			return throttler.Acquire(context.Background())
		})

		select {
		case <-time.After(10 * time.Millisecond):
			require.FailNow("should have acquired immediately")
		case <-acquiredChan:
		}
	}

	eg := errgroup.Group{}
	acquiredChan := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the 6th acquire
	cancel()

	eg.Go(func() error {
		defer close(acquiredChan)
		// Should block because 5 already taken within last second
		return throttler.Acquire(ctx)
	})

	select {
	case <-acquiredChan:
		err := eg.Wait()
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
