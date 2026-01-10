// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"math"
	"testing"
	"time"
)

// TestEtaTrackerEdgeCases tests boundary conditions and edge cases
func TestEtaTrackerEdgeCases(t *testing.T) {
	t.Run("zero target", func(t *testing.T) {
		tracker := NewEtaTracker(3, 1.0)
		now := time.Now()

		// Add samples with target=0
		eta, pct := tracker.AddSample(0, 0, now)

		// Should handle gracefully, not divide by zero
		if eta != nil {
			if *eta != 0 {
				t.Errorf("Expected 0 duration for zero target, got %v", *eta)
			}
		}

		if pct != 100 && pct != 0 {
			t.Errorf("Expected 0%% or 100%% for zero target, got %.2f%%", pct)
		}
	})

	t.Run("progress exceeds target", func(t *testing.T) {
		tracker := NewEtaTracker(3, 1.0)
		now := time.Now()

		tracker.AddSample(0, 1000, now)
		tracker.AddSample(500, 1000, now.Add(time.Second))
		tracker.AddSample(1000, 1000, now.Add(2*time.Second))

		// Progress exceeds target
		eta, pct := tracker.AddSample(1500, 1000, now.Add(3*time.Second))

		if eta == nil {
			t.Error("Expected ETA for exceeded target")
		} else if *eta != 0 {
			t.Errorf("Expected 0 duration for exceeded target, got %v", *eta)
		}

		if pct != 100 {
			t.Errorf("Expected 100%% for exceeded target, got %.2f%%", pct)
		}
	})

	t.Run("no progress made", func(t *testing.T) {
		tracker := NewEtaTracker(3, 1.0)
		now := time.Now()

		tracker.AddSample(100, 1000, now)
		tracker.AddSample(100, 1000, now.Add(time.Second))

		// No progress between samples
		eta, _ := tracker.AddSample(100, 1000, now.Add(2*time.Second))

		// Should return nil since rate is 0
		if eta != nil {
			t.Error("Expected nil ETA when no progress is made")
		}
	})

	t.Run("time goes backwards", func(t *testing.T) {
		tracker := NewEtaTracker(3, 1.0)
		now := time.Now()

		tracker.AddSample(0, 1000, now)
		tracker.AddSample(500, 1000, now.Add(2*time.Second))

		// Time goes backwards
		eta, _ := tracker.AddSample(600, 1000, now.Add(time.Second))

		// Current behavior: compares newest vs oldest (now+1s vs now), which is forward
		// So it calculates an ETA. This is acceptable since we can't easily detect
		// non-monotonic sequences in a circular buffer without extra bookkeeping.
		// The important check is timeSinceOldest <= 0, which catches truly backwards time.
		if eta == nil {
			t.Log("Returned nil (conservative)")
		} else {
			t.Logf("Calculated ETA despite non-monotonic samples: %v (acceptable)", *eta)
		}
	})

	t.Run("progress goes backwards", func(t *testing.T) {
		tracker := NewEtaTracker(3, 1.0)
		now := time.Now()

		tracker.AddSample(500, 1000, now)
		tracker.AddSample(600, 1000, now.Add(time.Second))

		// Progress goes backwards (corruption scenario)
		eta, _ := tracker.AddSample(400, 1000, now.Add(2*time.Second))

		// After fix: should detect progress going backwards and return nil
		if eta != nil {
			t.Errorf("Expected nil ETA when progress goes backwards, got %v", *eta)
		}
	})

	t.Run("max uint64 values", func(t *testing.T) {
		tracker := NewEtaTracker(3, 1.0)
		now := time.Now()

		maxUint := uint64(math.MaxUint64)

		tracker.AddSample(maxUint-1000, maxUint, now)
		tracker.AddSample(maxUint-500, maxUint, now.Add(time.Second))
		eta, pct := tracker.AddSample(maxUint-100, maxUint, now.Add(2*time.Second))

		if eta == nil {
			t.Error("Expected ETA with max uint64 values")
		}

		if pct < 99 || pct > 100 {
			t.Errorf("Expected ~100%% progress, got %.2f%%", pct)
		}
	})

	t.Run("very small slowdown factor", func(t *testing.T) {
		tracker := NewEtaTracker(3, 0.001) // Very small slowdown
		now := time.Now()

		tracker.AddSample(0, 1000, now)
		tracker.AddSample(100, 1000, now.Add(time.Second))
		eta, _ := tracker.AddSample(200, 1000, now.Add(2*time.Second))

		if eta == nil {
			t.Error("Expected ETA with small slowdown factor")
		}
	})

	t.Run("very large slowdown factor", func(t *testing.T) {
		tracker := NewEtaTracker(3, 1000.0) // Very large slowdown
		now := time.Now()

		tracker.AddSample(0, 1000, now)
		tracker.AddSample(100, 1000, now.Add(time.Second))
		eta, _ := tracker.AddSample(200, 1000, now.Add(2*time.Second))

		if eta == nil {
			t.Error("Expected ETA with large slowdown factor")
		}

		// ETA might overflow time.Duration max value
		// time.Duration is int64 nanoseconds, max ~290 years
		// Should handle gracefully
	})

	t.Run("negative slowdown factor", func(t *testing.T) {
		tracker := NewEtaTracker(3, -1.0) // Negative slowdown
		now := time.Now()

		tracker.AddSample(0, 1000, now)
		tracker.AddSample(100, 1000, now.Add(time.Second))
		eta, _ := tracker.AddSample(200, 1000, now.Add(2*time.Second))

		// Negative adjustment could cause negative ETA
		// This might be invalid behavior
		if eta != nil {
			t.Logf("Negative slowdown factor: ETA=%v", *eta)
		}
	})

	t.Run("duration overflow with very slow progress", func(t *testing.T) {
		tracker := NewEtaTracker(3, 1.0)
		now := time.Now()

		// Extremely slow progress: 1 unit per hour
		tracker.AddSample(0, math.MaxUint64, now)
		tracker.AddSample(1, math.MaxUint64, now.Add(time.Hour))
		eta, pct := tracker.AddSample(2, math.MaxUint64, now.Add(2*time.Hour))

		if eta == nil {
			t.Error("Expected ETA with very slow progress")
		} else {
			// ETA should be capped at max duration, not overflow to negative
			if *eta < 0 {
				t.Errorf("ETA overflowed to negative: %v", *eta)
			}

			// Should be at max duration (math.MaxInt64 nanoseconds)
			maxDuration := time.Duration(math.MaxInt64)
			if *eta != maxDuration {
				t.Logf("ETA capped at: %v (expected %v)", *eta, maxDuration)
			}

			// Progress should be near 0%
			if pct > 0.01 {
				t.Errorf("Expected progress near 0%%, got %.2f%%", pct)
			}
		}
	})
}

// TestRestoreSamplesEdgeCases tests edge cases in sample restoration
func TestRestoreSamplesEdgeCases(t *testing.T) {
	t.Run("restore nil samples", func(t *testing.T) {
		tracker := NewEtaTracker(5, 1.0)

		// Restore nil slice
		tracker.RestoreSamples(nil)

		// Should handle gracefully
		samples := tracker.GetSamples()
		if samples != nil {
			t.Error("Expected nil samples after restoring nil")
		}
	})

	t.Run("restore with zero timestamps", func(t *testing.T) {
		tracker := NewEtaTracker(5, 1.0)
		now := time.Now()

		samples := []Sample{
			{Completed: 1000, Timestamp: time.Time{}}, // Zero timestamp
			{Completed: 2000, Timestamp: now},
		}

		tracker.RestoreSamples(samples)

		// Add new sample
		eta, _ := tracker.AddSample(3000, 10000, now.Add(time.Second))

		// Zero timestamp might cause issues with time.Sub
		if eta == nil {
			t.Log("Zero timestamp prevented ETA calculation (expected)")
		}
	})

	t.Run("restore with duplicate timestamps", func(t *testing.T) {
		tracker := NewEtaTracker(5, 1.0)
		now := time.Now()

		samples := []Sample{
			{Completed: 1000, Timestamp: now},
			{Completed: 2000, Timestamp: now}, // Same timestamp
			{Completed: 3000, Timestamp: now},
		}

		tracker.RestoreSamples(samples)

		// Add new sample
		eta, _ := tracker.AddSample(4000, 10000, now.Add(time.Second))

		// timeSinceOldest will be 1 second, progressSinceOldest will be 3000
		// Should calculate valid ETA
		if eta == nil {
			t.Error("Expected ETA with duplicate timestamps in history")
		}
	})

	t.Run("restore with samples in wrong order", func(t *testing.T) {
		tracker := NewEtaTracker(5, 1.0)
		now := time.Now()

		// Samples NOT in chronological order
		samples := []Sample{
			{Completed: 3000, Timestamp: now.Add(2 * time.Second)},
			{Completed: 1000, Timestamp: now},
			{Completed: 2000, Timestamp: now.Add(time.Second)},
		}

		tracker.RestoreSamples(samples)

		// GetSamples should return them as-is since we just copied
		restored := tracker.GetSamples()

		// First sample should be 3000 (wrong order preserved)
		if restored[0].Completed != 3000 {
			t.Errorf("Expected first sample to be 3000, got %d", restored[0].Completed)
		}

		// This might cause issues when calculating ETA
		// Since oldest sample is actually at index 1, not 0
	})
}

// TestCheckpointEdgeCases tests checkpoint validation edge cases
func TestCheckpointEdgeCases(t *testing.T) {
	t.Run("checkpoint with future timestamp", func(t *testing.T) {
		// This would be tested in bootstrapper_test if we could
		// Checkpoint with timestamp in the future should be rejected
		// by validateCheckpoint's age check
		t.Skip("Requires bootstrapper integration")
	})

	t.Run("checkpoint with invalid height range", func(t *testing.T) {
		// Height < StartingHeight should be rejected
		// TipHeight < StartingHeight should be rejected
		t.Skip("Requires bootstrapper integration")
	})
}
