// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	// issued is the number of issued transactions.
	issued prometheus.Counter
	// confirmed is the number of confirmed transactions.
	confirmed prometheus.Counter
	// failed is the number of failed transactions.
	failed prometheus.Counter
	// txLatency is a histogram of individual tx times.
	// Failed transactions do not show in this metric.
	txLatency prometheus.Histogram

	registry PrometheusRegistry
}

type PrometheusRegistry interface {
	prometheus.Registerer
	prometheus.Gatherer
}

func newMetrics(registry PrometheusRegistry) *metrics {
	issued := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "txs_issued",
		Help: "Number of issued transactions",
	})
	confirmed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "txs_confirmed",
		Help: "Number of confirmed transactions",
	})
	failed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "txs_failed",
		Help: "Number of failed transactions",
	})
	txLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "tx_latency",
		Help: "Latency of transactions",
	})

	registry.MustRegister(issued, confirmed, failed,
		txLatency)

	return &metrics{
		registry:  registry,
		issued:    issued,
		confirmed: confirmed,
		failed:    failed,
		txLatency: txLatency,
	}
}

func (m *metrics) String() string {
	s := "Metrics:\n"

	metricFamilies, err := m.registry.Gather()
	if err != nil {
		return s + "failed to gather metrics: " + err.Error() + "\n"
	}

	for _, mf := range metricFamilies {
		metrics := mf.GetMetric()
		for _, metric := range metrics {
			s += fmt.Sprintf("Name: %s, Type: %s, Description: %s, Values: %s\n",
				mf.GetName(), mf.GetType().String(), mf.GetHelp(), metric.String())
		}
	}

	return s
}
