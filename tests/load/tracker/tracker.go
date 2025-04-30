// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"time"

	"github.com/ava-labs/libevm/common"
)

type Tracker struct {
	timeNow          func() time.Time
	txHashToLastTime map[common.Hash]time.Time
	lastBlockNumber  uint64
	lastBlockTime    time.Time
	metrics          *metrics

	stats struct {
		confirmed        uint64
		failed           uint64
		durationPerBlock []time.Duration
	}
	mutex sync.Mutex
}

// New creates a new Tracker instance.
// The tracker should then be started usign [Tracker.Start] and stopped
// using [Tracker.Stop].
func New(registry PrometheusRegistry) *Tracker {
	return &Tracker{
		timeNow:          time.Now,
		txHashToLastTime: make(map[common.Hash]time.Time),
		metrics:          newMetrics(registry),
	}
}

// IssueStart records a transaction that is being issued.
func (t *Tracker) IssueStart(txHash common.Hash) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.metrics.InFlightIssuances.Inc()
	t.metrics.InFlightTxs.Inc()
	t.txHashToLastTime[txHash] = t.timeNow()
}

// IssueEnd records a transaction that was issued, but whose final status is
// not yet known.
func (t *Tracker) IssueEnd(txHash common.Hash) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.metrics.InFlightIssuances.Dec()
	start := t.txHashToLastTime[txHash]
	now := t.timeNow()
	diff := now.Sub(start)
	t.metrics.IssuanceTxTimes.Observe(diff.Seconds())
	t.txHashToLastTime[txHash] = t.timeNow()
}

// ObserveConfirmed records a transaction that was confirmed.
func (t *Tracker) ObserveConfirmed(txHash common.Hash) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.metrics.InFlightTxs.Dec()
	t.metrics.Confirmed.Inc()
	issuedTime := t.txHashToLastTime[txHash]
	now := t.timeNow()
	diff := now.Sub(issuedTime)
	t.metrics.ConfirmationTxTimes.Observe(diff.Seconds())
	delete(t.txHashToLastTime, txHash)
	t.stats.confirmed++
}

// ObserveFailed records a transaction that failed (e.g. expired)
func (t *Tracker) ObserveFailed(txHash common.Hash) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.metrics.InFlightTxs.Dec()
	t.metrics.Failed.Inc()
	delete(t.txHashToLastTime, txHash)
	t.stats.failed++
}

// ObserveBlock records a new block with the given number.
// Note it ignores the first block, to avoid misleading metrics due to the
// absence of information on when the previous block was created.
func (t *Tracker) ObserveBlock(number uint64) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	now := t.timeNow()
	if t.lastBlockNumber == 0 {
		t.lastBlockTime = now
		t.lastBlockNumber = number
		return
	}

	if number == t.lastBlockNumber {
		// No new block. This can happen when polling the node periodically.
		return
	}

	// Usually numberDiff should be 1, but it may happen it is bigger, especially
	// when polling the node periodically instead of using a subscription.
	numberDiff := number - t.lastBlockNumber
	timeDiff := now.Sub(t.lastBlockTime)
	durationPerBlock := timeDiff / time.Duration(numberDiff)
	t.stats.durationPerBlock = append(t.stats.durationPerBlock, durationPerBlock)
	t.metrics.BlockTimes.Observe(durationPerBlock.Seconds())
	t.lastBlockTime = now
	t.lastBlockNumber = number
}

// GetObservedConfirmed returns the number of transactions that the tracker has
// confirmed were accepted.
func (t *Tracker) GetObservedConfirmed() uint64 {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.stats.confirmed
}

// GetObservedFailed returns the number of transactions that the tracker has
// confirmed failed.
func (t *Tracker) GetObservedFailed() uint64 {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.stats.failed
}

// GetAverageDurationPerBlock returns the average duration per block.
func (t *Tracker) GetAverageDurationPerBlock() time.Duration {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	var average time.Duration
	for _, durationPerBlock := range t.stats.durationPerBlock {
		average += durationPerBlock
	}
	return average / time.Duration(len(t.stats.durationPerBlock))
}
