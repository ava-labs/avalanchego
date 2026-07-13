// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	updateFrequency  = 1 * time.Minute
	leafRateHalfLife = 1 * time.Minute
	epsilon          = 1e-6 // added to avoid division by 0
)

// trieSyncStats tracks leaves and tries synced and periodically logs an ETA.
type trieSyncStats struct {
	log  logging.Logger
	lock sync.Mutex

	lastUpdated       time.Time
	leavesRate        safemath.Averager
	leavesSinceUpdate uint64

	triesRemaining int
	triesSynced    int

	// remainingLeaves is the estimated leaves left per in-progress segment.
	remainingLeaves map[*stateSegment]uint64
}

func newTrieSyncStats(log logging.Logger) *trieSyncStats {
	return &trieSyncStats{
		log:             log,
		lastUpdated:     time.Now(),
		remainingLeaves: make(map[*stateSegment]uint64),
	}
}

// incLeaves records count leaves synced for segment and periodically logs an ETA.
func (t *trieSyncStats) incLeaves(segment *stateSegment, count, remaining uint64) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.leavesSinceUpdate += count
	t.remainingLeaves[segment] = remaining

	now := time.Now()
	if sinceUpdate := now.Sub(t.lastUpdated); sinceUpdate > updateFrequency {
		t.updateETA(sinceUpdate, now)
		t.lastUpdated = now
		t.leavesSinceUpdate = 0
	}
}

// trieDone records a completed trie and drops its segments from the estimate.
func (t *trieSyncStats) trieDone(root common.Hash) {
	t.lock.Lock()
	defer t.lock.Unlock()

	for segment := range t.remainingLeaves {
		if segment.trie.root == root {
			delete(t.remainingLeaves, segment)
		}
	}
	t.triesSynced++
	t.triesRemaining--
}

// setTriesRemaining records how many storage tries remain after the account trie.
func (t *trieSyncStats) setTriesRemaining(triesRemaining int) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.triesRemaining = triesRemaining
}

// estimateSegmentsInProgressTime estimates when the in-progress segments finish,
// from the one with the most remaining leaves.
func (t *trieSyncStats) estimateSegmentsInProgressTime() time.Duration {
	if len(t.remainingLeaves) == 0 {
		return 0
	}

	maxLeaves := uint64(0)
	for _, leaves := range t.remainingLeaves {
		if leaves > maxLeaves {
			maxLeaves = leaves
		}
	}
	perThreadRate := (t.leavesRate.Read() + epsilon) / float64(len(t.remainingLeaves))
	return time.Duration(float64(maxLeaves)/perThreadRate) * time.Second
}

// updateETA refreshes the leaf rate and logs the current ETA.
func (t *trieSyncStats) updateETA(sinceUpdate time.Duration, now time.Time) {
	leavesRate := float64(t.leavesSinceUpdate) / sinceUpdate.Seconds()
	if t.leavesRate == nil {
		t.leavesRate = safemath.NewAverager(leavesRate, leafRateHalfLife, now)
	} else {
		t.leavesRate.Observe(leavesRate, now)
	}

	leavesTime := t.estimateSegmentsInProgressTime()
	if t.triesSynced == 0 {
		// Storage trie count is unknown until the account trie completes, so
		// report the account-trie ETA on its own.
		t.log.Info("state sync: syncing account trie", zap.String("ETA", roundETA(leavesTime)))
		return
	}

	t.log.Info(
		"state sync: syncing storage tries",
		zap.Int("triesRemaining", t.triesRemaining),
		zap.String("ETA", roundETA(leavesTime)),
	)
}

// roundETA rounds d to a minute and trims the trailing "0s", returning "<1m"
// when it rounds to zero.
func roundETA(d time.Duration) string {
	str := strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
	if len(str) == 0 {
		return "<1m"
	}
	return str
}
