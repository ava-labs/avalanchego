// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"encoding/binary"
	"math"
	"time"
)

// ProgressFromHash returns the progress out of MaxUint64 assuming [b] is a key
// in a uniformly distributed sequence that is being iterated lexicographically.
func ProgressFromHash(b []byte) uint64 {
	// binary.BigEndian.Uint64 will panic if the input length is less than 8, so
	// pad 0s as needed.
	var progress [8]byte
	copy(progress[:], b)
	return binary.BigEndian.Uint64(progress[:])
}

// A sample represents a completed amount and the timestamp of the sample
type sample struct {
	completed uint64
	timestamp time.Time
}

// EtaTracker tracks the ETA of a job
type EtaTracker struct {
	samples        []sample
	samplePosition uint8
	maxSamples     uint8
	totalSamples   uint64
	slowdownFactor float64
}

// NewEtaTracker creates a new EtaTracker with the given maximum number of samples
// and a slowdown factor. The slowdown factor is a multiplier that is added to the ETA
// based on the percentage completed.
//
// The adjustment works as follows:
//   - At 0% progress: ETA is multiplied by slowdownFactor
//   - At 100% progress: ETA is the raw estimate (no adjustment)
//   - Between 0% and 100%: Adjustment decreases linearly with progress
//
// Example: With slowdownFactor = 2.0:
//   - At 0% progress: ETA = raw_estimate * 2.0
//   - At 50% progress: ETA = raw_estimate * 1.5
//   - At 100% progress: ETA = raw_estimate * 1.0
//
// If maxSamples is less than 1, it will default to 5
func NewEtaTracker(maxSamples uint8, slowdownFactor float64) *EtaTracker {
	if maxSamples < 1 {
		maxSamples = 5
	}
	return &EtaTracker{
		samples:        make([]sample, maxSamples),
		samplePosition: 0,
		maxSamples:     maxSamples,
		totalSamples:   0,
		slowdownFactor: slowdownFactor,
	}
}

// AddSample adds a sample to the EtaTracker
// It returns the remaining time to complete the target and the percent complete
// The returned values are rounded to the nearest second and 2 decimal places respectively
// This function can return a nil time.Duration indicating that there are not yet enough
// samples to calculate an accurate ETA.
//
// The first sample should be at 0% progress to establish a baseline
func (t *EtaTracker) AddSample(completed uint64, target uint64, timestamp time.Time) (remaining *time.Duration, percentComplete float64) {
	sample := sample{
		completed: completed,
		timestamp: timestamp,
	}
	// save the oldest sample; this will not be used if we don't have enough samples
	t.samples[t.samplePosition] = sample
	t.samplePosition = (t.samplePosition + 1) % t.maxSamples
	t.totalSamples++

	// If we don't have enough samples, return nil
	if t.totalSamples < uint64(t.maxSamples) {
		return nil, 0.0
	}

	oldestSample := t.samples[t.samplePosition]

	// Calculate the time and progress since the oldest sample
	timeSinceOldest := sample.timestamp.Sub(oldestSample.timestamp)
	progressSinceOldest := sample.completed - oldestSample.completed

	// Check if target is already completed or exceeded
	if sample.completed >= target {
		zeroDuration := time.Duration(0)
		percentComplete := float64(sample.completed) / float64(target)
		roundedPercentComplete := math.Round(percentComplete*10000) / 100 // Return percentage (0.0 to 100.0)
		return &zeroDuration, roundedPercentComplete
	}

	if timeSinceOldest == 0 {
		return nil, 0.0
	}
	rate := float64(progressSinceOldest) / float64(timeSinceOldest)
	if rate == 0 {
		return nil, 0.0
	}

	remainingProgress := target - sample.completed

	actualPercentComplete := float64(sample.completed) / float64(target)
	roundedScaledPercentComplete := math.Round(actualPercentComplete*10000) / 100

	duration := float64(remainingProgress) / rate
	adjustment := t.slowdownFactor - (t.slowdownFactor-1.0)*actualPercentComplete

	adjustedDuration := duration * adjustment
	eta := time.Duration(adjustedDuration)
	roundedEta := eta.Round(time.Second)
	return &roundedEta, roundedScaledPercentComplete
}

// EstimateETA calculates ETA from start time and current progress.
//
// Deprecated: use EtaTracker instead
func EstimateETA(startTime time.Time, progress, end uint64) time.Duration {
	timeSpent := time.Since(startTime)

	percentExecuted := float64(progress) / float64(end)
	estimatedTotalDuration := time.Duration(float64(timeSpent) / percentExecuted)
	eta := estimatedTotalDuration - timeSpent
	return eta.Round(time.Second)
}
