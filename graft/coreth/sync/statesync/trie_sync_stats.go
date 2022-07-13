// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"strings"
	"sync"
	"time"

	utils_math "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	updateFrequency  = 1 * time.Minute
	leafRateHalfLife = 1 * time.Minute
	epsilon          = 1e-6 // added to avoid division by 0
)

// trieSyncStats keeps track of the total number of leafs and tries
// completed during a sync.
type trieSyncStats struct {
	lock sync.Mutex

	lastUpdated time.Time
	leafsRate   utils_math.Averager

	triesRemaining   int
	triesSynced      int
	triesStartTime   time.Time
	leafsSinceUpdate uint64

	remainingLeafs map[*trieSegment]uint64

	// metrics
	totalLeafs     metrics.Counter
	triesSegmented metrics.Counter
	leafsRateGauge metrics.Gauge
}

func newTrieSyncStats() *trieSyncStats {
	now := time.Now()
	return &trieSyncStats{
		remainingLeafs: make(map[*trieSegment]uint64),
		lastUpdated:    now,

		// metrics
		totalLeafs:     metrics.GetOrRegisterCounter("state_sync_total_leafs", nil),
		leafsRateGauge: metrics.GetOrRegisterGauge("state_sync_leafs_per_second", nil),
		triesSegmented: metrics.GetOrRegisterCounter("state_sync_tries_segmented", nil),
	}
}

// incTriesSegmented increases the metric for segmented tries.
func (t *trieSyncStats) incTriesSegmented() {
	t.triesSegmented.Inc(1) // safe to be called concurrently
}

// incLeafs takes a lock and adds [count] to the total number of leafs synced.
// periodically outputs a log message with the number of leafs and tries.
func (t *trieSyncStats) incLeafs(segment *trieSegment, count uint64, remaining uint64) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.totalLeafs.Inc(int64(count))
	t.leafsSinceUpdate += count
	t.remainingLeafs[segment] = remaining

	now := time.Now()
	sinceUpdate := now.Sub(t.lastUpdated)
	if sinceUpdate > updateFrequency {
		t.updateETA(sinceUpdate, now)
		t.lastUpdated = now
		t.leafsSinceUpdate = 0
	}
}

// estimateSegmentsInProgressTime retrns the ETA for all trie segments
// in progress to finish (uses the one with most remaining leafs to estimate).
func (t *trieSyncStats) estimateSegmentsInProgressTime() time.Duration {
	if len(t.remainingLeafs) == 0 {
		// if there are no tries in progress, return 0
		return 0
	}

	maxLeafs := uint64(0)
	for _, leafs := range t.remainingLeafs {
		if leafs > maxLeafs {
			maxLeafs = leafs
		}
	}
	perThreadLeafsRate := (t.leafsRate.Read() + epsilon) / float64(len(t.remainingLeafs))
	return time.Duration(float64(maxLeafs)/perThreadLeafsRate) * time.Second
}

// trieDone takes a lock and adds one to the total number of tries synced.
func (t *trieSyncStats) trieDone(root common.Hash) {
	t.lock.Lock()
	defer t.lock.Unlock()

	for segment := range t.remainingLeafs {
		if segment.trie.root == root {
			delete(t.remainingLeafs, segment)
		}
	}

	t.triesSynced++
	t.triesRemaining--
}

// updateETA calculates and logs and ETA based on the number of leafs
// currently in progress and the number of tries remaining.
// assumes lock is held.
func (t *trieSyncStats) updateETA(sinceUpdate time.Duration, now time.Time) {
	leafsRate := float64(t.leafsSinceUpdate) / sinceUpdate.Seconds()
	if t.leafsRate == nil {
		t.leafsRate = utils_math.NewAverager(leafsRate, leafRateHalfLife, now)
	} else {
		t.leafsRate.Observe(leafsRate, now)
	}
	t.leafsRateGauge.Update(int64(t.leafsRate.Read()))

	leafsTime := t.estimateSegmentsInProgressTime()
	if t.triesSynced == 0 {
		// provide a separate ETA for the account trie syncing step since we
		// don't know the total number of storage tries yet.
		log.Info("state sync: syncing account trie", "ETA", roundETA(leafsTime))
		return
	}

	triesTime := now.Sub(t.triesStartTime) * time.Duration(t.triesRemaining) / time.Duration(t.triesSynced)
	log.Info(
		"state sync: syncing storage tries",
		"triesRemaining", t.triesRemaining,
		"ETA", roundETA(leafsTime+triesTime), // TODO: should we use max instead of sum?
	)
}

func (t *trieSyncStats) setTriesRemaining(triesRemaining int) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.triesRemaining = triesRemaining
	t.triesStartTime = time.Now()
}

// roundETA rounds [d] to a minute and chops off the "0s" suffix
// returns "<1m" if [d] rounds to 0 minutes.
func roundETA(d time.Duration) string {
	str := d.Round(time.Minute).String()
	str = strings.TrimSuffix(str, "0s")
	if len(str) == 0 {
		return "<1m"
	}
	return str
}
