// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	// Confirmed is the number of confirmed transactions.
	Confirmed prometheus.Counter
	// Failed is the number of failed transactions.
	Failed prometheus.Counter
	// InFlightIssuances is the number of transactions that are being issued, from issuance start
	// to issuance end.
	InFlightIssuances prometheus.Gauge
	// InFlightTxs is the number of transactions that are in-flight, from issuance start
	// to confirmation end or failure.
	InFlightTxs prometheus.Gauge
	// IssuanceTxTimes is the summary of the quantiles of individual issuance tx times.
	IssuanceTxTimes prometheus.Summary
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
	confirmed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_confirmed",
		Help: "Number of confirmed transactions",
	})
	failed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_failed",
		Help: "Number of failed transactions",
	})
	inFlightIssuances := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tx_in_flight_issuances",
		Help: "Number of transactions in flight issuances",
	})
	inFlightTxs := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tx_in_flight_txs",
		Help: "Number of transactions in flight",
	})
	issuanceTxTimes := prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "tx_issuance_time",
		Help:       "Individual Tx Issuance Times for a Load Test",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
	confirmationTxTimes := prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "tx_confirmation_time",
		Help:       "Individual Tx Confirmation Times for a Load Test",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})

	registry.MustRegister(confirmed, failed, inFlightIssuances, inFlightTxs,
		issuanceTxTimes, confirmationTxTimes)

	return &metrics{
		registry:            registry,
		Confirmed:           confirmed,
		InFlightIssuances:   inFlightIssuances,
		InFlightTxs:         inFlightTxs,
		IssuanceTxTimes:     issuanceTxTimes,
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
