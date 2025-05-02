// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	// Issued is the number of issued transactions.
	Issued prometheus.Counter
	// Confirmed is the number of confirmed transactions.
	Confirmed prometheus.Counter
	// Failed is the number of failed transactions.
	Failed prometheus.Counter
	// ConfirmationTxTimes is the summary of the quantiles of individual confirmation tx times.
	// Failed transactions do not show in this metric.
	ConfirmationTxTimes prometheus.Summary

	registry PrometheusRegistry
}

type PrometheusRegistry interface {
	prometheus.Registerer
	prometheus.Gatherer
}

func newMetrics(registry PrometheusRegistry) *metrics {
	issued := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_issued",
		Help: "Number of issued transactions",
	})
	confirmed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_confirmed",
		Help: "Number of confirmed transactions",
	})
	failed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_failed",
		Help: "Number of failed transactions",
	})
	confirmationTxTimes := prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "tx_confirmation_time",
		Help:       "Individual Tx Confirmation Times for a Load Test",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})

	registry.MustRegister(issued, confirmed, failed,
		confirmationTxTimes)

	return &metrics{
		registry:            registry,
		Issued:              issued,
		Confirmed:           confirmed,
		Failed:              failed,
		ConfirmationTxTimes: confirmationTxTimes,
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
