// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	// Summary of the quantiles of Individual Issuance Tx Times
	IssuanceTxTimes prometheus.Summary
	// Summary of the quantiles of Individual Confirmation Tx Times
	ConfirmationTxTimes prometheus.Summary
	// Summary of the quantiles of Individual Issuance To Confirmation Tx Times
	IssuanceToConfirmationTxTimes prometheus.Summary
}

// NewMetrics creates and returns a Metrics and registers it with a Collector
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		IssuanceTxTimes: prometheus.NewSummary(prometheus.SummaryOpts{
			Name:       "tx_issuance_time",
			Help:       "Individual Tx Issuance Times for a Load Test",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
		ConfirmationTxTimes: prometheus.NewSummary(prometheus.SummaryOpts{
			Name:       "tx_confirmation_time",
			Help:       "Individual Tx Confirmation Times for a Load Test",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
		IssuanceToConfirmationTxTimes: prometheus.NewSummary(prometheus.SummaryOpts{
			Name:       "tx_issuance_to_confirmation_time",
			Help:       "Individual Tx Issuance To Confirmation Times for a Load Test",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
	}
	reg.MustRegister(m.IssuanceTxTimes)
	reg.MustRegister(m.ConfirmationTxTimes)
	reg.MustRegister(m.IssuanceToConfirmationTxTimes)
	return m
}
