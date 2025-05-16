// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	txsIssuedCounter    prometheus.Counter
	txsConfirmedCounter prometheus.Counter
	txsFailedCounter    prometheus.Counter
	txLatency           prometheus.Histogram
}

func NewMetrics(registry *prometheus.Registry) (*Metrics, error) {
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

	if err := errors.Join(
		registry.Register(m.txsIssuedCounter),
		registry.Register(m.txsConfirmedCounter),
		registry.Register(m.txsFailedCounter),
		registry.Register(m.txLatency),
	); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Metrics) IncIssuedTx() {
	m.txsIssuedCounter.Inc()
}

func (m *Metrics) RecordConfirmedTx(latencyMS float64) {
	m.txsConfirmedCounter.Inc()
	m.txLatency.Observe(latencyMS)
}

func (m *Metrics) RecordFailedTx(latencyMS float64) {
	m.txsFailedCounter.Inc()
	m.txLatency.Observe(latencyMS)
}
