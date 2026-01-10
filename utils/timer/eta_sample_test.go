// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"testing"
	"time"
)

// TestSampleExportRestore tests that ETA samples can be exported and restored correctly
func TestSampleExportRestore(t *testing.T) {
	// Create tracker with 5 samples
	tracker := NewEtaTracker(5, 1.2)

	// Add some samples
	now := time.Now()
	tracker.AddSample(1000, 10000, now)
	tracker.AddSample(2000, 10000, now.Add(time.Second))
	tracker.AddSample(3000, 10000, now.Add(2*time.Second))

	// Export samples
	exported := tracker.GetSamples()

	// Verify we got samples
	if len(exported) != 3 {
		t.Fatalf("Expected 3 samples, got %d", len(exported))
	}

	// Verify samples are in order (oldest first)
	if exported[0].Completed != 1000 {
		t.Errorf("Expected first sample completed=1000, got %d", exported[0].Completed)
	}
	if exported[1].Completed != 2000 {
		t.Errorf("Expected second sample completed=2000, got %d", exported[1].Completed)
	}
	if exported[2].Completed != 3000 {
		t.Errorf("Expected third sample completed=3000, got %d", exported[2].Completed)
	}

	// Create new tracker and restore samples
	newTracker := NewEtaTracker(5, 1.2)
	newTracker.RestoreSamples(exported)

	// Add another sample and verify ETA calculation works
	eta, pct := newTracker.AddSample(4000, 10000, now.Add(3*time.Second))

	// Verify ETA is calculated (not nil)
	if eta == nil {
		t.Error("Expected ETA to be calculated after restore, got nil")
	}

	// Verify progress percentage
	if pct < 35 || pct > 45 {
		t.Errorf("Expected progress around 40%%, got %.2f%%", pct)
	}
}

// TestSampleRestoreMoreThanCapacity tests restoring more samples than buffer capacity
func TestSampleRestoreMoreThanCapacity(t *testing.T) {
	// Create tracker with capacity 3
	tracker := NewEtaTracker(3, 1.0)

	// Create 5 samples
	now := time.Now()
	samples := []Sample{
		{Completed: 1000, Timestamp: now},
		{Completed: 2000, Timestamp: now.Add(time.Second)},
		{Completed: 3000, Timestamp: now.Add(2 * time.Second)},
		{Completed: 4000, Timestamp: now.Add(3 * time.Second)},
		{Completed: 5000, Timestamp: now.Add(4 * time.Second)},
	}

	// Restore (should keep only the 3 most recent)
	tracker.RestoreSamples(samples)

	// Add one more sample to verify it uses the restored data
	eta, pct := tracker.AddSample(6000, 10000, now.Add(5*time.Second))

	// Verify ETA is calculated
	if eta == nil {
		t.Error("Expected ETA to be calculated, got nil")
	}

	// Verify progress
	if pct < 55 || pct > 65 {
		t.Errorf("Expected progress around 60%%, got %.2f%%", pct)
	}
}

// TestSampleEmptyRestore tests restoring with empty sample list
func TestSampleEmptyRestore(t *testing.T) {
	tracker := NewEtaTracker(5, 1.0)

	// Restore empty list (should be no-op)
	tracker.RestoreSamples([]Sample{})

	// Verify tracker still works
	now := time.Now()
	eta, _ := tracker.AddSample(1000, 10000, now)

	// Should not have ETA yet (need more samples)
	if eta != nil {
		t.Error("Expected no ETA with single sample, but got one")
	}
}

// TestSampleExportEmpty tests exporting when no samples exist
func TestSampleExportEmpty(t *testing.T) {
	tracker := NewEtaTracker(5, 1.0)

	// Export with no samples
	samples := tracker.GetSamples()

	// Should return nil or empty
	if samples != nil && len(samples) > 0 {
		t.Errorf("Expected nil or empty samples, got %d samples", len(samples))
	}
}
