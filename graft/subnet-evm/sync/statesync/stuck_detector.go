// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/log"
)

const (
	// Thresholds for stuck detection
	zeroRateTimeout     = 10 * time.Minute // No leafs fetched
	noTrieTimeout       = 15 * time.Minute // No trie completed (reduced further for faster detection)
	maxRetriesThreshold = 1000             // Excessive retries
	checkInterval       = 1 * time.Minute  // How often to check

	// Progress velocity thresholds
	minLeafsPerMinute      = 100             // Minimum expected leaf fetch rate
	slowProgressTimeout    = 10 * time.Minute // How long to tolerate slow progress (reduced from 15)
	emergencySlowThreshold = 10              // Emergency: < 10 leafs/min
	emergencySlowTimeout   = 5 * time.Minute // Emergency timeout for critically slow progress
)

// StuckDetector monitors state sync progress and detects when sync has stalled.
// It tracks multiple indicators: leaf fetch rate, trie completion rate, and retry count.
type StuckDetector struct {
	stats                  *trieSyncStats
	lastLeafCount          atomic.Uint64
	lastTrieCount          atomic.Uint64
	lastLeafUpdate         atomic.Value // time.Time
	lastTrieUpdate         atomic.Value // time.Time
	retryCount             atomic.Uint64
	stuckChan              chan struct{}
	stopChan               chan struct{}
	slowProgressStart      atomic.Value // time.Time - when slow progress was first detected
	emergencySlowStart     atomic.Value // time.Time - when critically slow progress was detected
	lastVelocityCheck      atomic.Value // time.Time
	lastVelocityCount      atomic.Uint64 // leaf count at last velocity check
}

// NewStuckDetector creates a new stuck detector for monitoring state sync progress.
// Must be called before Start() to ensure proper initialization of atomic values.
func NewStuckDetector(stats *trieSyncStats) *StuckDetector {
	sd := &StuckDetector{
		stats:     stats,
		stuckChan: make(chan struct{}, 1),
		stopChan:  make(chan struct{}),
	}
	// Timestamps will be initialized in Start() to avoid timing issues
	return sd
}

// Start begins monitoring in a background goroutine.
func (sd *StuckDetector) Start(ctx context.Context) {
	if sd == nil {
		log.Error("CRITICAL: StuckDetector.Start() called on nil detector")
		return
	}
	if sd.stats == nil {
		log.Error("CRITICAL: StuckDetector.stats is nil")
		return
	}

	// Initialize timestamps and counters at start time
	now := time.Now()
	sd.lastLeafUpdate.Store(now)
	sd.lastTrieUpdate.Store(now)
	sd.lastVelocityCheck.Store(now)
	currentLeafCount := uint64(sd.stats.totalLeafs.Snapshot().Count())
	sd.lastLeafCount.Store(currentLeafCount)
	sd.lastVelocityCount.Store(currentLeafCount)
	triesSynced, _ := sd.stats.getProgress()
	sd.lastTrieCount.Store(uint64(triesSynced))

	log.Info("Stuck detector initialization complete, starting monitor goroutine",
		"initialLeafCount", currentLeafCount,
		"initialTriesSynced", triesSynced)

	go sd.monitorLoop(ctx)
}

// monitorLoop periodically checks for stuck conditions.
func (sd *StuckDetector) monitorLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("CRITICAL: Stuck detector goroutine panicked", "panic", r)
		}
	}()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	log.Info("Stuck detector monitoring started")

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		log.Warn("Stuck detector context cancelled before first check", "err", ctx.Err())
		return
	default:
		// Context is still active, proceed
	}

	checkCount := 0
	for {
		select {
		case <-ticker.C:
			checkCount++
			stuck := sd.checkIfStuck()

			// Log status every 5 checks (5 minutes) for visibility
			if checkCount%5 == 0 {
				triesSynced, triesRemaining := sd.stats.getProgress()
				log.Info("Stuck detector status",
					"checkCount", checkCount,
					"stuck", stuck,
					"triesSynced", triesSynced,
					"triesRemaining", triesRemaining,
					"totalLeafs", sd.stats.totalLeafs.Snapshot().Count())
			}

			if stuck {
				log.Warn("State sync appears stuck, triggering fallback to block sync")
				select {
				case sd.stuckChan <- struct{}{}:
				default:
					// Channel already has signal
				}
				return
			}
		case <-ctx.Done():
			log.Info("Stuck detector stopped: context cancelled")
			return
		case <-sd.stopChan:
			log.Info("Stuck detector stopped: stop signal")
			return
		}
	}
}

// checkIfStuck evaluates multiple criteria to detect if state sync has stalled.
func (sd *StuckDetector) checkIfStuck() bool {
	defer func() {
		if r := recover(); r != nil {
			log.Error("CRITICAL: checkIfStuck panicked", "panic", r)
		}
	}()

	now := time.Now()

	// Check 1: Track leaf progress
	currentLeafCount := uint64(sd.stats.totalLeafs.Snapshot().Count())
	lastLeafCount := sd.lastLeafCount.Load()
	leafsProgressing := currentLeafCount != lastLeafCount

	var leafStuckDuration time.Duration
	if leafsProgressing {
		sd.lastLeafCount.Store(currentLeafCount)
		sd.lastLeafUpdate.Store(now)
	} else {
		lastUpdate := sd.lastLeafUpdate.Load().(time.Time)
		leafStuckDuration = now.Sub(lastUpdate)
		if leafStuckDuration > zeroRateTimeout {
			log.Error("Stuck detected: No leafs fetched in 10 minutes",
				"lastLeafCount", lastLeafCount)
			return true
		}
	}

	// Check 2: Track trie completion progress
	triesSynced, triesRemaining := sd.stats.getProgress()
	currentTrieCount := uint64(triesSynced)
	lastTrieCount := sd.lastTrieCount.Load()
	triesProgressing := currentTrieCount != lastTrieCount

	if triesProgressing {
		sd.lastTrieCount.Store(currentTrieCount)
		sd.lastTrieUpdate.Store(now)
	} else {
		lastUpdate := sd.lastTrieUpdate.Load().(time.Time)
		trieStuckDuration := now.Sub(lastUpdate)

		// If no trie completed in 30 minutes, something is wrong
		// Even if leafs are trickling in, a trie should eventually complete
		if trieStuckDuration > noTrieTimeout {
			log.Error("Stuck detected: No trie completed in 30 minutes",
				"triesRemaining", triesRemaining,
				"trieStuckDuration", trieStuckDuration.Round(time.Second),
				"leafsProgressing", leafsProgressing)
			return true
		}
	}

	// Check 3: Excessive retries without progress
	retries := sd.retryCount.Load()
	if retries > maxRetriesThreshold {
		log.Error("Stuck detected: Excessive retries without progress",
			"retryCount", retries)
		return true
	}

	// Check 4: Progress velocity - is progress happening but too slowly?
	lastVelocityCheck := sd.lastVelocityCheck.Load()
	if lastVelocityCheck != nil {
		lastCheckTime := lastVelocityCheck.(time.Time)
		timeSinceLastCheck := now.Sub(lastCheckTime)

		if timeSinceLastCheck >= checkInterval {
			lastVelocityCount := sd.lastVelocityCount.Load()
			var leafsSinceLastCheck int64
			if currentLeafCount >= lastVelocityCount {
				leafsSinceLastCheck = int64(currentLeafCount - lastVelocityCount)
			} else {
				// Handle counter overflow or reset
				leafsSinceLastCheck = 0
			}

			if leafsSinceLastCheck > 0 {
				leafsPerMinute := float64(leafsSinceLastCheck) / timeSinceLastCheck.Minutes()

				log.Debug("Velocity check",
					"leafsSinceLastCheck", leafsSinceLastCheck,
					"timeSinceLastCheck", timeSinceLastCheck.Round(time.Second),
					"leafsPerMinute", int(leafsPerMinute),
					"minExpected", minLeafsPerMinute)

				// Emergency check: critically slow progress
				if leafsPerMinute < emergencySlowThreshold {
					emergencySlowStart := sd.emergencySlowStart.Load()
					if emergencySlowStart == nil {
						sd.emergencySlowStart.Store(now)
						log.Error("CRITICALLY slow sync progress detected - emergency timer started",
							"leafsPerMinute", int(leafsPerMinute),
							"emergencyThreshold", emergencySlowThreshold,
							"willTriggerIn", emergencySlowTimeout)
					} else {
						emergencyDuration := now.Sub(emergencySlowStart.(time.Time))
						if emergencyDuration > emergencySlowTimeout {
							log.Error("Stuck detected: CRITICALLY slow progress for extended period",
								"leafsPerMinute", int(leafsPerMinute),
								"emergencyThreshold", emergencySlowThreshold,
								"emergencyDuration", emergencyDuration.Round(time.Second))
							return true
						}
					}
				} else {
					// Reset emergency tracking if above critical threshold
					sd.emergencySlowStart.Store(nil)
				}

				// Normal slow progress check
				if leafsPerMinute < minLeafsPerMinute {
					// Progress is too slow
					slowProgressStart := sd.slowProgressStart.Load()
					if slowProgressStart == nil {
						// First time detecting slow progress
						sd.slowProgressStart.Store(now)
						log.Warn("Slow sync progress detected - starting slow progress timer",
							"leafsPerMinute", int(leafsPerMinute),
							"minExpected", minLeafsPerMinute,
							"willTriggerIn", slowProgressTimeout)
					} else {
						// Check how long we've been slow
						slowDuration := now.Sub(slowProgressStart.(time.Time))
						log.Warn("Slow sync progress continues",
							"leafsPerMinute", int(leafsPerMinute),
							"minExpected", minLeafsPerMinute,
							"slowDuration", slowDuration.Round(time.Second),
							"timeout", slowProgressTimeout)

						if slowDuration > slowProgressTimeout {
							log.Error("Stuck detected: Progress too slow for extended period",
								"leafsPerMinute", int(leafsPerMinute),
								"minExpected", minLeafsPerMinute,
								"slowDuration", slowDuration.Round(time.Second))
							return true
						}
					}
				} else {
					// Progress is acceptable, reset slow progress tracking
					slowProgressStart := sd.slowProgressStart.Load()
					if slowProgressStart != nil {
						log.Info("Sync progress recovered to acceptable rate",
							"leafsPerMinute", int(leafsPerMinute))
						sd.slowProgressStart.Store(nil)
					}
				}
			} else {
				// No progress at all - this will be caught by Check 1 (zero rate timeout)
				log.Debug("No leafs fetched since last velocity check",
					"lastVelocityCount", lastVelocityCount,
					"currentLeafCount", currentLeafCount)
			}

			// Update velocity tracking
			sd.lastVelocityCheck.Store(now)
			sd.lastVelocityCount.Store(currentLeafCount)
		}
	}

	return false
}

// RecordRetry increments the retry counter. Should be called when a request fails and is retried.
func (sd *StuckDetector) RecordRetry() {
	sd.retryCount.Add(1)
}

// ResetRetries resets the retry counter. Should be called after a successful request.
func (sd *StuckDetector) ResetRetries() {
	sd.retryCount.Store(0)
}

// StuckChannel returns a channel that will receive a signal when stuck is detected.
func (sd *StuckDetector) StuckChannel() <-chan struct{} {
	return sd.stuckChan
}

// Stop terminates the monitoring goroutine.
func (sd *StuckDetector) Stop() {
	close(sd.stopChan)
}
