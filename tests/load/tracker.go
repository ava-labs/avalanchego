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
func (p *Tracker[_]) GetObservedConfirmed() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.txsConfirmed
}

// GetObservedFailed returns the number of transactions that the tracker has
// confirmed failed.
func (p *Tracker[_]) GetObservedFailed() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.txsFailed
}

// GetObservedIssued returns the number of transactions that the tracker has
// confirmed were issued.
func (p *Tracker[_]) GetObservedIssued() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.txsIssued
}

// Issue records a transaction that was submitted, but whose final status is
// not yet known.
func (p *Tracker[T]) Issue(txID T) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.outstandingTxs[txID] = time.Now()
	p.txsIssued++
	p.metrics.IncIssuedTx()
}

// ObserveConfirmed records a transaction that was confirmed.
func (p *Tracker[T]) ObserveConfirmed(txID T) {
	p.lock.Lock()
	defer p.lock.Unlock()

	startTime := p.outstandingTxs[txID]
	delete(p.outstandingTxs, txID)

	p.txsConfirmed++
	p.metrics.RecordConfirmedTx(float64(time.Since(startTime).Milliseconds()))
}

// ObserveFailed records a transaction that failed (e.g. expired)
func (p *Tracker[T]) ObserveFailed(txID T) {
	p.lock.Lock()
	defer p.lock.Unlock()

	startTime := p.outstandingTxs[txID]
	delete(p.outstandingTxs, txID)

	p.txsFailed++
	p.metrics.RecordFailedTx(float64(time.Since(startTime).Milliseconds()))
}
