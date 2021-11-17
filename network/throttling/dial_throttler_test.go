// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Test that the DialThrottler returned by NewDialThrottler works
func TestDialThrottler(t *testing.T) {
	startTime := time.Now()
	// Allows 5 per second
	throttler := NewDialThrottler(5)
	// Use all 5
	for i := 0; i < 5; i++ {
		acquiredChan := make(chan struct{}, 1)
		// Should return immediately because < 5 taken this second
		go func() {
			err := throttler.Acquire(context.Background())
			assert.NoError(t, err)
			acquiredChan <- struct{}{}
		}()
		select {
		case <-time.After(10 * time.Millisecond):
			t.Fatal("should have acquired immediately")
		case <-acquiredChan:
		}
		close(acquiredChan)
	}

	acquiredChan := make(chan struct{}, 1)
	go func() {
		// Should block because 5 already taken within last second
		err := throttler.Acquire(context.Background())
		assert.NoError(t, err)
		acquiredChan <- struct{}{}
	}()

	select {
	case <-time.After(25 * time.Millisecond):
		break
	case <-acquiredChan:
		t.Fatal("should not have been able to acquire immediately")
	}

	// Wait until the 6th Acquire() has returned. The time at which
	// that returns should be no more than 1s after the time at which
	// the first Acquire() returned.
	<-acquiredChan
	close(acquiredChan)
	// Use 1.05 seconds instead of 1 second to give some "wiggle room"
	// so test doesn't flake
	if time.Since(startTime) > 1050*time.Millisecond {
		t.Fatal("should not have blocked for so long")
	}
}

// Test that Acquire honors its specification about its context being canceled
func TestDialThrottlerCancel(t *testing.T) {
	// Allows 5 per second
	throttler := NewDialThrottler(5)
	// Use all 5
	for i := 0; i < 5; i++ {
		acquiredChan := make(chan struct{}, 1)
		// Should return immediately because < 5 taken this second
		go func() {
			err := throttler.Acquire(context.Background())
			assert.NoError(t, err)
			acquiredChan <- struct{}{}
		}()
		select {
		case <-time.After(10 * time.Millisecond):
			t.Fatal("should have acquired immediately")
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
		assert.Error(t, err)
		acquiredChan <- struct{}{}
	}()

	// Cancel the 6th acquire
	cancel()
	select {
	case <-acquiredChan:
	case <-time.After(10 * time.Millisecond):
		t.Fatal("Acquire should have returned immediately upon context cancelation")
	}
	close(acquiredChan)
}

// Test that the Throttler return by NewNoThrottler never blocks on Acquire()
func TestNoDialThrottler(t *testing.T) {
	throttler := NewNoDialThrottler()
	for i := 0; i < 250; i++ {
		startTime := time.Now()
		err := throttler.Acquire(context.Background()) // Should always immediately return
		assert.NoError(t, err)
		assert.WithinDuration(t, time.Now(), startTime, 25*time.Millisecond)
	}
}
