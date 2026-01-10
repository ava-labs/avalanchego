// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

// A Sample represents a completed amount and the timestamp of the Sample
type Sample struct {
	Completed uint64
	Timestamp time.Time
}

// EtaTracker tracks the ETA of a job
type EtaTracker struct {
	samples        []Sample
	samplePosition uint8
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
		samples:        make([]Sample, maxSamples),
		samplePosition: 0,
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
func (t *EtaTracker) AddSample(completed uint64, target uint64, timestamp time.Time) (*time.Duration, float64) {
	// Guard against empty tracker (defensive programming)
	if len(t.samples) == 0 {
		// Tracker was not properly initialized, cannot add samples
		return nil, 0.0
	}

	s := Sample{
		Completed: completed,
		Timestamp: timestamp,
	}
	// save the oldest sample; this will not be used if we don't have enough samples
	maxSamples := len(t.samples)
	t.samples[t.samplePosition] = s
	t.samplePosition = (t.samplePosition + 1) % uint8(maxSamples)
	t.totalSamples++

	// If we don't have enough samples, return nil
	if t.totalSamples < uint64(maxSamples) {
		return nil, 0.0
	}

	oldestSample := t.samples[t.samplePosition]

	// Calculate the time and progress since the oldest sample
	timeSinceOldest := s.Timestamp.Sub(oldestSample.Timestamp)

	// Guard against progress going backwards (corruption/clock skew)
	if s.Completed < oldestSample.Completed {
		// Progress decreased, likely corruption or restoring wrong samples
		return nil, 0.0
	}

	progressSinceOldest := s.Completed - oldestSample.Completed

	// Check if target is already completed or exceeded
	if s.Completed >= target {
		zeroDuration := time.Duration(0)
		return &zeroDuration, 100
	}

	// Guard against time going backwards or no time elapsed
	if timeSinceOldest <= 0 {
		return nil, 0.0
	}

	// Guard against no progress made
	if progressSinceOldest == 0 {
		return nil, 0.0
	}

	rate := float64(progressSinceOldest) / float64(timeSinceOldest)

	remainingWork := target - s.Completed

	actualPercentComplete := float64(s.Completed) / float64(target)
	// scale to 0.00 to 100.00
	roundedScaledPercentComplete := math.Round(actualPercentComplete*10000) / 100

	duration := float64(remainingWork) / rate
	adjustment := t.slowdownFactor - (t.slowdownFactor-1.0)*actualPercentComplete

	adjustedDuration := duration * adjustment

	// Guard against time.Duration overflow (max ~290 years)
	// time.Duration is int64 nanoseconds, max value is math.MaxInt64
	const maxDuration = float64(math.MaxInt64)
	if adjustedDuration > maxDuration {
		// ETA is too large to represent, cap at maximum duration
		maxEta := time.Duration(math.MaxInt64)
		return &maxEta, roundedScaledPercentComplete
	}

	eta := time.Duration(adjustedDuration)
	roundedEta := eta.Round(time.Second)
	return &roundedEta, roundedScaledPercentComplete
}

// GetSamples exports the current samples for persistence.
// Returns a copy of the samples in the order they were added (oldest first).
func (t *EtaTracker) GetSamples() []Sample {
	if t.totalSamples == 0 {
		return nil
	}

	// Guard against empty tracker (defensive programming)
	if len(t.samples) == 0 {
		return nil
	}

	// Determine how many samples we actually have
	numSamples := int(min(t.totalSamples, uint64(len(t.samples))))
	result := make([]Sample, numSamples)

	if t.totalSamples < uint64(len(t.samples)) {
		// Haven't filled the buffer yet, samples are in order from 0 to samplePosition-1
		copy(result, t.samples[:numSamples])
	} else {
		// Buffer is full, need to copy in order: [samplePosition..end] + [0..samplePosition)
		firstPartLen := len(t.samples) - int(t.samplePosition)
		copy(result, t.samples[t.samplePosition:])
		copy(result[firstPartLen:], t.samples[:t.samplePosition])
	}

	return result
}

// RestoreSamples restores the EtaTracker state from persisted samples.
// The samples should be in chronological order (oldest first).
func (t *EtaTracker) RestoreSamples(samples []Sample) {
	if len(samples) == 0 {
		return
	}

	// Guard against empty tracker (defensive programming)
	if len(t.samples) == 0 {
		// Tracker was not properly initialized, cannot restore
		return
	}

	// Limit to available buffer size
	numToRestore := int(min(uint64(len(samples)), uint64(len(t.samples))))

	// Copy the most recent samples (keep the newest data)
	startIdx := len(samples) - numToRestore
	copy(t.samples, samples[startIdx:])

	// Update state
	// Set samplePosition to next write location
	t.samplePosition = uint8(numToRestore % len(t.samples))

	// Set totalSamples strategically:
	// - If we restored enough samples (>= 2), set to (maxSamples - 1)
	//   so the next AddSample will trigger ETA calculation immediately
	// - Otherwise, set to actual restored count
	if numToRestore >= 2 {
		t.totalSamples = uint64(len(t.samples)) - 1
	} else {
		t.totalSamples = uint64(numToRestore)
	}
}

func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
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
