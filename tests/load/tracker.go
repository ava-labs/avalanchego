// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	txsIssuedCounter    prometheus.Counter
	txsConfirmedCounter prometheus.Counter

	totalGasUsedCounter prometheus.Counter

	txIssuanceLatency     prometheus.Histogram
	txConfirmationLatency prometheus.Histogram
}

func NewMetrics(namespace string, registry *prometheus.Registry) (*Metrics, error) {
	m := &Metrics{
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
		totalGasUsedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "total_gas_used",
			Help:      "Sum of gas used for all confirmed transactions",
		}),
		txIssuanceLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_issuance_latency",
			Help:      "Latency of issued transactions",
		}),
		txConfirmationLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_confirmation_latecy",
			Help:      "Latency of confirmed transactions",
		}),
	}

	if err := errors.Join(
		registry.Register(m.txsIssuedCounter),
		registry.Register(m.txsConfirmedCounter),
		registry.Register(m.totalGasUsedCounter),
		registry.Register(m.txIssuanceLatency),
		registry.Register(m.txConfirmationLatency),
	); err != nil {
		return nil, err
	}

	return m, nil
}

type Tracker struct {
	lock sync.RWMutex

	txsIssued    uint64
	txsConfirmed uint64

	totalGasUsed uint64

	metrics *Metrics
}

func NewTracker(metrics *Metrics) *Tracker {
	return &Tracker{metrics: metrics}
}

func (t *Tracker) LogIssuance(issuanceDuration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.txsIssued++

	t.metrics.txsIssuedCounter.Add(1)
	t.metrics.txIssuanceLatency.Observe(float64(issuanceDuration))
}

func (t *Tracker) LogConfirmation(receipt *types.Receipt, confirmationDuration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.txsConfirmed++
	t.totalGasUsed += receipt.GasUsed

	t.metrics.txsConfirmedCounter.Add(1)
	t.metrics.txConfirmationLatency.Observe(float64(confirmationDuration))
	t.metrics.totalGasUsedCounter.Add(float64(receipt.GasUsed))
}

func (t *Tracker) TotalGasUsed() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.totalGasUsed
}
