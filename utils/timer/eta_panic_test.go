// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"testing"
	"time"
)

// TestEmptyTrackerDefensiveProgramming tests that ETA tracker methods handle
// improperly initialized trackers gracefully without panicking
func TestEmptyTrackerDefensiveProgramming(t *testing.T) {
	t.Run("add sample to empty tracker", func(t *testing.T) {
		// Create tracker with empty samples slice (API misuse)
		tracker := &EtaTracker{
			samples:        make([]Sample, 0),
			samplePosition: 0,
			totalSamples:   0,
			slowdownFactor: 1.0,
		}

		// Should not panic, should return nil gracefully
		eta, pct := tracker.AddSample(1000, 10000, time.Now())

		if eta != nil {
			t.Errorf("Expected nil ETA for empty tracker, got %v", *eta)
		}

		if pct != 0.0 {
			t.Errorf("Expected 0%% for empty tracker, got %.2f%%", pct)
		}
	})

	t.Run("restore samples to empty tracker", func(t *testing.T) {
		// Create tracker with empty samples slice
		tracker := &EtaTracker{
			samples:        make([]Sample, 0),
			samplePosition: 0,
			totalSamples:   0,
			slowdownFactor: 1.0,
		}

		samples := []Sample{
			{Completed: 1000, Timestamp: time.Now()},
			{Completed: 2000, Timestamp: time.Now().Add(time.Second)},
		}

		// Should not panic, should return gracefully
		tracker.RestoreSamples(samples)

		// Verify nothing was restored
		if tracker.totalSamples != 0 {
			t.Errorf("Expected totalSamples=0 for empty tracker, got %d", tracker.totalSamples)
		}
	})

	t.Run("get samples from empty tracker", func(t *testing.T) {
		// Create tracker with empty samples slice but non-zero totalSamples (corrupt state)
		tracker := &EtaTracker{
			samples:        make([]Sample, 0),
			samplePosition: 0,
			totalSamples:   5, // Inconsistent!
			slowdownFactor: 1.0,
		}

		// Should not panic, should return nil gracefully
		samples := tracker.GetSamples()

		if samples != nil {
			t.Errorf("Expected nil samples for empty tracker, got %v", samples)
		}
	})

	t.Run("properly initialized tracker works", func(t *testing.T) {
		// Properly initialized tracker should work fine
		tracker := NewEtaTracker(5, 1.0)
		now := time.Now()

		// Add samples normally
		tracker.AddSample(0, 1000, now)
		tracker.AddSample(100, 1000, now.Add(time.Second))
		tracker.AddSample(200, 1000, now.Add(2*time.Second))

		// Export
		samples := tracker.GetSamples()
		if len(samples) != 3 {
			t.Errorf("Expected 3 samples, got %d", len(samples))
		}

		// Restore to new tracker
		newTracker := NewEtaTracker(5, 1.0)
		newTracker.RestoreSamples(samples)

		// Add another sample - should calculate ETA
		eta, pct := newTracker.AddSample(300, 1000, now.Add(3*time.Second))
		if eta == nil {
			t.Error("Expected ETA after proper initialization and restoration")
		}
		if pct < 25 || pct > 35 {
			t.Errorf("Expected progress around 30%%, got %.2f%%", pct)
		}
	})
}
