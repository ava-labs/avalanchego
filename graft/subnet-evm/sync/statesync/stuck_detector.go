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
	noTrieTimeout       = 30 * time.Minute // No trie completed
	maxRetriesThreshold = 1000             // Excessive retries
	checkInterval       = 1 * time.Minute  // How often to check
)

// StuckDetector monitors state sync progress and detects when sync has stalled.
// It tracks multiple indicators: leaf fetch rate, trie completion rate, and retry count.
type StuckDetector struct {
	stats          *trieSyncStats
	lastLeafCount  atomic.Uint64
	lastTrieCount  atomic.Uint64
	lastLeafUpdate atomic.Value // time.Time
	lastTrieUpdate atomic.Value // time.Time
	retryCount     atomic.Uint64
	stuckChan      chan struct{}
	stopChan       chan struct{}
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
	// Initialize timestamps and counters at start time
	now := time.Now()
	sd.lastLeafUpdate.Store(now)
	sd.lastTrieUpdate.Store(now)
	sd.lastLeafCount.Store(sd.stats.totalLeafs.Count())
	triesSynced, _ := sd.stats.getProgress()
	sd.lastTrieCount.Store(uint64(triesSynced))

	go sd.monitorLoop(ctx)
}

// monitorLoop periodically checks for stuck conditions.
func (sd *StuckDetector) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if sd.checkIfStuck() {
				log.Warn("State sync appears stuck, triggering fallback to block sync")
				select {
				case sd.stuckChan <- struct{}{}:
				default:
					// Channel already has signal
				}
				return
			}
		case <-ctx.Done():
			return
		case <-sd.stopChan:
			return
		}
	}
}

// checkIfStuck evaluates multiple criteria to detect if state sync has stalled.
func (sd *StuckDetector) checkIfStuck() bool {
	now := time.Now()

	// Check 1: Track leaf progress
	currentLeafCount := sd.stats.totalLeafs.Count()
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
