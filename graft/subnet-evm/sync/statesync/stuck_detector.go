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
	// Reduced timeouts to detect stuck state BEFORE 6-minute crash
	zeroRateTimeout     = 5 * time.Minute  // REDUCED from 10min - No leafs fetched
	noTrieTimeout       = 8 * time.Minute  // REDUCED from 15min - No trie completed
	maxRetriesThreshold = 1000             // Excessive retries
	checkInterval       = 30 * time.Second // REDUCED from 1min - More frequent checks

	// Progress velocity thresholds
	minLeafsPerMinute      = 100
	slowProgressTimeout    = 6 * time.Minute  // REDUCED from 10min - Slow progress tolerance
	emergencySlowThreshold = 10               // Emergency: < 10 leafs/min
	emergencySlowTimeout   = 3 * time.Minute  // REDUCED from 5min - Emergency timeout

	// Code sync specific timeouts (even faster since code sync failures are peer-related)
	codeSyncZeroProgressTimeout = 2 * time.Minute // Faster detection for code sync phase
)

// StuckDetector monitors state sync progress and detects when sync has stalled.
// It tracks multiple indicators: leaf fetch rate, trie completion rate, and retry count.
type StuckDetector struct {
	stats                  *trieSyncStats
	started                atomic.Bool   // prevents multiple Start() calls
	lastLeafCount          atomic.Uint64
	lastTrieCount          atomic.Uint64
	lastLeafUpdate         atomic.Value // *time.Time
	lastTrieUpdate         atomic.Value // *time.Time
	retryCount             atomic.Uint64
	stuckChan              chan struct{}
	stopChan               chan struct{}
	slowProgressStart      atomic.Value // *time.Time - when slow progress was first detected (nil = not slow)
	emergencySlowStart     atomic.Value // *time.Time - when critically slow progress was detected (nil = not slow)
	lastVelocityCheck      atomic.Value // *time.Time
	lastVelocityCount      atomic.Uint64 // leaf count at last velocity check
	codeSyncPhaseStart     atomic.Value  // *time.Time - when code sync phase started (nil = not in code sync)
	inCodeSyncPhase        atomic.Bool   // true if currently in code sync phase

	// Track storage tries waiting for code sync (specific failure mode)
	// Enables faster stuck detection when storage is complete but code sync failing
	storageTriesWaitingStart atomic.Value // *time.Time - when storage tries started waiting for code
	storageTriesWaiting      atomic.Bool  // true if storage tries are waiting for code sync
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
// Safe to call multiple times - only the first call will start monitoring.
func (sd *StuckDetector) Start(ctx context.Context) {
	if sd == nil {
		log.Error("CRITICAL: StuckDetector.Start() called on nil detector")
		return
	}
	if sd.stats == nil {
		log.Error("CRITICAL: StuckDetector.stats is nil")
		return
	}

	// Atomically check and set started flag to prevent multiple goroutines
	if !sd.started.CompareAndSwap(false, true) {
		log.Warn("StuckDetector.Start() called multiple times - ignoring duplicate call")
		return
	}

	// Initialize timestamps and counters at start time
	now := time.Now()
	sd.lastLeafUpdate.Store(&now)
	sd.lastTrieUpdate.Store(&now)
	sd.lastVelocityCheck.Store(&now)
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

	// Safety check: if Start() was never called, we can't detect stuck state
	if !sd.started.Load() {
		log.Warn("checkIfStuck called before Start() - cannot detect stuck state")
		return false
	}

	now := time.Now()

	// Check 1: Track leaf progress
	currentLeafCount := uint64(sd.stats.totalLeafs.Snapshot().Count())
	lastLeafCount := sd.lastLeafCount.Load()
	leafsProgressing := currentLeafCount != lastLeafCount

	var leafStuckDuration time.Duration
	if leafsProgressing {
		sd.lastLeafCount.Store(currentLeafCount)
		nowCopy := now
		sd.lastLeafUpdate.Store(&nowCopy)
	} else {
		lastUpdatePtr := sd.lastLeafUpdate.Load().(*time.Time)
		if lastUpdatePtr != nil {
			leafStuckDuration = now.Sub(*lastUpdatePtr)
			if leafStuckDuration > zeroRateTimeout {
				log.Error("Stuck detected: No leafs fetched in 10 minutes",
					"lastLeafCount", lastLeafCount)
				return true
			}
		}
	}

	// Check 2: Track trie completion progress
	triesSynced, triesRemaining := sd.stats.getProgress()
	currentTrieCount := uint64(triesSynced)
	lastTrieCount := sd.lastTrieCount.Load()
	triesProgressing := currentTrieCount != lastTrieCount

	if triesProgressing {
		sd.lastTrieCount.Store(currentTrieCount)
		nowCopy := now
		sd.lastTrieUpdate.Store(&nowCopy)
	} else {
		lastUpdatePtr := sd.lastTrieUpdate.Load().(*time.Time)
		if lastUpdatePtr != nil {
			trieStuckDuration := now.Sub(*lastUpdatePtr)

			// If no trie completed in 15 minutes, something is wrong
			// Even if leafs are trickling in, a trie should eventually complete
			if trieStuckDuration > noTrieTimeout {
				log.Error("Stuck detected: No trie completed in 15 minutes",
					"triesRemaining", triesRemaining,
					"trieStuckDuration", trieStuckDuration.Round(time.Second),
					"leafsProgressing", leafsProgressing)
				return true
			}
		}
	}

	// Check 3: Excessive retries without progress
	retries := sd.retryCount.Load()
	if retries > maxRetriesThreshold {
		log.Error("Stuck detected: Excessive retries without progress",
			"retryCount", retries)
		return true
	}

	// Check 3.5: Code sync phase timeout (faster detection for peer unavailability)
	if sd.inCodeSyncPhase.Load() {
		codeSyncStartPtr := sd.codeSyncPhaseStart.Load()
		if codeSyncStartPtr != nil {
			codeSyncStart := codeSyncStartPtr.(*time.Time)
			if codeSyncStart != nil {
				codeSyncDuration := now.Sub(*codeSyncStart)

				// If in code sync phase and no progress for 2 minutes, likely peers don't have data
				if !leafsProgressing && codeSyncDuration > codeSyncZeroProgressTimeout {
					log.Error("Stuck detected: Code sync phase timeout - peers likely don't have code data",
						"codeSyncDuration", codeSyncDuration.Round(time.Second),
						"timeout", codeSyncZeroProgressTimeout,
						"lastLeafCount", currentLeafCount)
					return true
				}

				// Log progress during code sync
				if codeSyncDuration > 30*time.Second {
					log.Debug("Code sync in progress",
						"duration", codeSyncDuration.Round(time.Second),
						"leafsProgressing", leafsProgressing,
						"timeout", codeSyncZeroProgressTimeout)
				}
			}
		}
	}

	// Check 3.6: Storage tries waiting for code sync timeout
	// This is a specific failure mode where storage tries completed but can't commit
	// because code sync is failing. We use a faster timeout (3 min) because we know
	// storage is complete and only code sync is blocking.
	if sd.storageTriesWaiting.Load() {
		waitingStartPtr := sd.storageTriesWaitingStart.Load()
		if waitingStartPtr != nil {
			waitingStart := waitingStartPtr.(*time.Time)
			if waitingStart != nil {
				waitingDuration := now.Sub(*waitingStart)

				// Faster timeout: 3 minutes (vs 5-8 min general timeout)
				// We know storage is done, so this is purely a code sync issue
				const storageWaitingTimeout = 3 * time.Minute

				if !leafsProgressing && waitingDuration > storageWaitingTimeout {
					triesSynced, triesRemaining := sd.stats.getProgress()
					log.Error("Stuck detected: Storage tries blocked by code sync failures",
						"waitingDuration", waitingDuration.Round(time.Second),
						"timeout", storageWaitingTimeout,
						"triesSynced", triesSynced,
						"triesRemaining", triesRemaining,
						"lastLeafCount", currentLeafCount)
					return true
				}

				// Log waiting status every minute to show we're monitoring
				if waitingDuration.Truncate(time.Minute) != (waitingDuration - 30*time.Second).Truncate(time.Minute) {
					triesSynced, triesRemaining := sd.stats.getProgress()
					log.Warn("Storage tries still waiting for code sync",
						"duration", waitingDuration.Round(time.Second),
						"timeout", storageWaitingTimeout,
						"triesSynced", triesSynced,
						"triesRemaining", triesRemaining,
						"leafsProgressing", leafsProgressing)
				}
			}
		}
	}

	// Check 4: Progress velocity - is progress happening but too slowly?
	lastVelocityCheckPtr := sd.lastVelocityCheck.Load().(*time.Time)
	if lastVelocityCheckPtr != nil {
		timeSinceLastCheck := now.Sub(*lastVelocityCheckPtr)

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
					emergencySlowStartPtr := sd.emergencySlowStart.Load()
					if emergencySlowStartPtr == nil {
						nowCopy := now
						sd.emergencySlowStart.Store(&nowCopy)
						log.Error("CRITICALLY slow sync progress detected - emergency timer started",
							"leafsPerMinute", int(leafsPerMinute),
							"emergencyThreshold", emergencySlowThreshold,
							"willTriggerIn", emergencySlowTimeout)
					} else {
						emergencyStart := emergencySlowStartPtr.(*time.Time)
						emergencyDuration := now.Sub(*emergencyStart)
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
					if sd.emergencySlowStart.Load() != nil {
						sd.emergencySlowStart = atomic.Value{} // Reset to zero value
					}
				}

				// Normal slow progress check
				if leafsPerMinute < minLeafsPerMinute {
					// Progress is too slow
					slowProgressStartPtr := sd.slowProgressStart.Load()
					if slowProgressStartPtr == nil {
						// First time detecting slow progress
						nowCopy := now
						sd.slowProgressStart.Store(&nowCopy)
						log.Warn("Slow sync progress detected - starting slow progress timer",
							"leafsPerMinute", int(leafsPerMinute),
							"minExpected", minLeafsPerMinute,
							"willTriggerIn", slowProgressTimeout)
					} else {
						// Check how long we've been slow
						slowStart := slowProgressStartPtr.(*time.Time)
						slowDuration := now.Sub(*slowStart)
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
					if sd.slowProgressStart.Load() != nil {
						log.Info("Sync progress recovered to acceptable rate",
							"leafsPerMinute", int(leafsPerMinute))
						sd.slowProgressStart = atomic.Value{} // Reset to zero value
					}
				}
			} else {
				// No progress at all - this will be caught by Check 1 (zero rate timeout)
				log.Debug("No leafs fetched since last velocity check",
					"lastVelocityCount", lastVelocityCount,
					"currentLeafCount", currentLeafCount)
			}

			// Update velocity tracking
			nowCopy := now
			sd.lastVelocityCheck.Store(&nowCopy)
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

// EnterCodeSyncPhase marks the beginning of code sync phase for faster stuck detection.
// Code sync failures (peers not having data) should be detected faster than trie sync issues.
func (sd *StuckDetector) EnterCodeSyncPhase() {
	if sd.inCodeSyncPhase.CompareAndSwap(false, true) {
		now := time.Now()
		sd.codeSyncPhaseStart.Store(&now)
		log.Info("Entered code sync phase - using faster stuck detection",
			"timeout", codeSyncZeroProgressTimeout)
	}
}

// ExitCodeSyncPhase marks the end of code sync phase.
func (sd *StuckDetector) ExitCodeSyncPhase() {
	if sd.inCodeSyncPhase.CompareAndSwap(true, false) {
		sd.codeSyncPhaseStart.Store((*time.Time)(nil))
		log.Info("Exited code sync phase")
	}
}

// NotifyStorageTriesWaitingForCode indicates storage tries are waiting for code sync.
// This enables faster stuck detection for the specific failure mode where storage completes
// but code sync is failing due to peer unavailability.
func (sd *StuckDetector) NotifyStorageTriesWaitingForCode() {
	if sd.storageTriesWaiting.CompareAndSwap(false, true) {
		now := time.Now()
		sd.storageTriesWaitingStart.Store(&now)
		log.Warn("Storage tries waiting for code sync - enabling faster stuck detection",
			"timeout", 3*time.Minute)
	}
}

// ClearStorageTriesWaiting clears the storage tries waiting state.
// Called when code sync completes or sync is cancelled.
func (sd *StuckDetector) ClearStorageTriesWaiting() {
	if sd.storageTriesWaiting.CompareAndSwap(true, false) {
		sd.storageTriesWaitingStart.Store((*time.Time)(nil))
		log.Info("Storage tries no longer waiting for code sync")
	}
}

// StuckChannel returns a channel that will receive a signal when stuck is detected.
func (sd *StuckDetector) StuckChannel() <-chan struct{} {
	return sd.stuckChan
}

// Stop terminates the monitoring goroutine.
func (sd *StuckDetector) Stop() {
	close(sd.stopChan)
}
