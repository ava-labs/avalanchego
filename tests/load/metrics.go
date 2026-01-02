// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	txsIssuedCounter      prometheus.Counter
	txIssuanceLatency     prometheus.Histogram
	txConfirmationLatency prometheus.Histogram
	txTotalLatency        prometheus.Histogram
}

func newMetrics(namespace string, registry *prometheus.Registry) (metrics, error) {
	m := metrics{
		txsIssuedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "txs_issued",
			Help:      "Number of transactions issued",
		}),
		txIssuanceLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_issuance_latency",
			Help:      "Issuance latency of transactions",
		}),
		txConfirmationLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_confirmation_latency",
			Help:      "Confirmation latency of transactions",
		}),
		txTotalLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_total_latency",
			Help:      "Total latency of transactions",
		}),
	}

	if err := errors.Join(
		registry.Register(m.txsIssuedCounter),
		registry.Register(m.txIssuanceLatency),
		registry.Register(m.txConfirmationLatency),
		registry.Register(m.txTotalLatency),
	); err != nil {
		return metrics{}, err
	}

	return m, nil
}

func (m metrics) issue(d time.Duration) {
	m.txsIssuedCounter.Inc()
	m.txIssuanceLatency.Observe(float64(d.Milliseconds()))
}

func (m metrics) accept(confirmationDuration time.Duration, totalDuration time.Duration) {
	m.txConfirmationLatency.Observe(float64(confirmationDuration.Milliseconds()))
	m.txTotalLatency.Observe(float64(totalDuration.Milliseconds()))
}
