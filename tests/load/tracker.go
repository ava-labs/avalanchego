// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const namespace = "load"

// Tracker keeps track of the status of transactions.
// This is thread-safe and can be called in parallel by the issuer(s) or orchestrator.
type Tracker[T comparable] struct {
	lock sync.RWMutex

	outstandingTxs map[T]time.Time

	txsIssued    uint64
	txsConfirmed uint64
	txsFailed    uint64

	// metrics
	txsIssuedCounter    prometheus.Counter
	txsConfirmedCounter prometheus.Counter
	txsFailedCounter    prometheus.Counter
	txLatency           prometheus.Histogram
}

// NewTracker returns a new Tracker instance which records metrics for the number
// of transactions issued, confirmed, and failed. It also tracks the latency of
// transactions.
func NewTracker[T comparable](reg *prometheus.Registry) (*Tracker[T], error) {
	tracker := &Tracker[T]{
		outstandingTxs: make(map[T]time.Time),
		txsIssuedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "txs_issued",
			Help:      "Number of transactions issued",
		}),
		txsConfirmedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "txs_confirmed",
			Help:      "Number of transactions confirmed",
		}),
		txsFailedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "txs_failed",
			Help:      "Number of transactions failed",
		}),
		txLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_latency",
			Help:      "Latency of transactions",
		}),
	}

	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(tracker.txsIssuedCounter),
		reg.Register(tracker.txsConfirmedCounter),
		reg.Register(tracker.txsFailedCounter),
		reg.Register(tracker.txLatency),
	)
	return tracker, errs.Err
}

// GetObservedConfirmed returns the number of transactions that the tracker has
// confirmed were accepted.
func (p *Tracker[T]) GetObservedConfirmed() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.txsConfirmed
}

// GetObservedFailed returns the number of transactions that the tracker has
// confirmed failed.
func (p *Tracker[T]) GetObservedFailed() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.txsFailed
}

// GetObservedIssued returns the number of transactions that the tracker has
// confirmed were issued.
func (p *Tracker[T]) GetObservedIssued() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.txsIssued
}

// Issue records a transaction that was submitted, but whose final status is
// not yet known.
func (p *Tracker[T]) Issue(tx T) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.outstandingTxs[tx] = time.Now()
	p.txsIssued++
	p.txsIssuedCounter.Inc()
}

// ObserveConfirmed records a transaction that was confirmed.
func (p *Tracker[T]) ObserveConfirmed(tx T) {
	p.lock.Lock()
	defer p.lock.Unlock()

	startTime := p.outstandingTxs[tx]
	delete(p.outstandingTxs, tx)

	p.txsConfirmed++
	p.txsConfirmedCounter.Inc()
	p.txLatency.Observe(float64(time.Since(startTime).Milliseconds()))
}

// ObserveFailed records a transaction that failed (e.g. expired)
func (p *Tracker[T]) ObserveFailed(tx T) {
	p.lock.Lock()
	defer p.lock.Unlock()

	startTime := p.outstandingTxs[tx]
	delete(p.outstandingTxs, tx)

	p.txsFailed++
	p.txsFailedCounter.Inc()
	p.txLatency.Observe(float64(time.Since(startTime).Milliseconds()))
}
