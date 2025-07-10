// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"sync"
	"time"
)

const namespace = "load"

// Tracker keeps track of the status of transactions.
// This is thread-safe and can be called in parallel by the issuer(s) or orchestrator.
type Tracker[T TxID] struct {
	lock sync.RWMutex

	outstandingTxs map[T]time.Time

	txsIssued    uint64
	txsConfirmed uint64
	txsFailed    uint64

	metrics *Metrics
}

// NewTracker returns a new Tracker instance which records metrics for the number
// of transactions issued, confirmed, and failed. It also tracks the latency of
// transactions.
func NewTracker[T TxID](metrics *Metrics) *Tracker[T] {
	return &Tracker[T]{
		outstandingTxs: make(map[T]time.Time),
		metrics:        metrics,
	}
}

// GetObservedConfirmed returns the number of transactions that the tracker has
// confirmed were accepted.
func (t *Tracker[_]) GetObservedConfirmed() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.txsConfirmed
}

// GetObservedFailed returns the number of transactions that the tracker has
// confirmed failed.
func (t *Tracker[_]) GetObservedFailed() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.txsFailed
}

// GetObservedIssued returns the number of transactions that the tracker has
// confirmed were issued.
func (t *Tracker[_]) GetObservedIssued() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.txsIssued
}

// Issue records a transaction that was submitted, but whose final status is
// not yet known.
func (t *Tracker[T]) Issue(txID T) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.outstandingTxs[txID] = time.Now()
	t.txsIssued++
	t.metrics.IncIssuedTx()
}

// ObserveConfirmed records a transaction that was confirmed.
func (t *Tracker[T]) ObserveConfirmed(txID T) {
	t.lock.Lock()
	defer t.lock.Unlock()

	startTime := t.outstandingTxs[txID]
	delete(t.outstandingTxs, txID)

	t.txsConfirmed++
	t.metrics.RecordConfirmedTx(float64(time.Since(startTime).Milliseconds()))
}

// ObserveFailed records a transaction that failed (e.g. expired)
func (t *Tracker[T]) ObserveFailed(txID T) {
	t.lock.Lock()
	defer t.lock.Unlock()

	startTime := t.outstandingTxs[txID]
	delete(t.outstandingTxs, txID)

	t.txsFailed++
	t.metrics.RecordFailedTx(float64(time.Since(startTime).Milliseconds()))
}
