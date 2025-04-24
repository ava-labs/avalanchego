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
	metrics          *metrics

	stats struct {
		confirmed uint64
		failed    uint64
	}
	statsMutex sync.RWMutex
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
	t.metrics.InFlightIssuances.Inc()
	t.metrics.InFlightTxs.Inc()
	t.txHashToLastTime[txHash] = t.timeNow()
}

// IssueEnd records a transaction that was issued, but whose final status is
// not yet known.
func (t *Tracker) IssueEnd(txHash common.Hash) {
	t.metrics.InFlightIssuances.Dec()

	start := t.txHashToLastTime[txHash]
	now := t.timeNow()
	diff := now.Sub(start)
	t.metrics.IssuanceTxTimes.Observe(diff.Seconds())
	t.txHashToLastTime[txHash] = t.timeNow()
}

// ObserveConfirmed records a transaction that was confirmed.
func (t *Tracker) ObserveConfirmed(txHash common.Hash) {
	t.metrics.InFlightTxs.Dec()
	t.metrics.Confirmed.Inc()
	issuedTime := t.txHashToLastTime[txHash]
	now := t.timeNow()
	diff := now.Sub(issuedTime)
	t.metrics.ConfirmationTxTimes.Observe(diff.Seconds())
	delete(t.txHashToLastTime, txHash)

	t.statsMutex.Lock()
	t.stats.confirmed++
	t.statsMutex.Unlock()
}

// ObserveFailed records a transaction that failed (e.g. expired)
func (t *Tracker) ObserveFailed(txHash common.Hash) {
	t.metrics.InFlightTxs.Dec()
	t.metrics.Failed.Inc()
	delete(t.txHashToLastTime, txHash)

	t.statsMutex.Lock()
	t.stats.failed++
	t.statsMutex.Unlock()
}

// GetObservedConfirmed returns the number of transactions that the tracker has
// confirmed were accepted.
func (t *Tracker) GetObservedConfirmed() uint64 {
	t.statsMutex.RLock()
	defer t.statsMutex.RUnlock()
	return t.stats.confirmed
}

// GetObservedFailed returns the number of transactions that the tracker has
// confirmed failed.
func (t *Tracker) GetObservedFailed() uint64 {
	t.statsMutex.RLock()
	defer t.statsMutex.RUnlock()
	return t.stats.failed
}
