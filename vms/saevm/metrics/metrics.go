// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// Metrics holds SAE Prometheus collectors and provides semantic update methods.
type Metrics struct {
	LastExecutedHeight prometheus.Gauge
	LastSettledHeight  prometheus.Gauge
}

// New constructs and registers SAE metrics.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		LastExecutedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_executed_height",
			Help: "Height of the latest block that completed async execution.",
		}),
		LastSettledHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_settled_height",
			Help: "Height of the latest block that has settled.",
		}),
	}

	return m, errors.Join(
		reg.Register(m.LastExecutedHeight),
		reg.Register(m.LastSettledHeight),
	)
}

// MarkBlockExecuted updates metrics for a block that completed async execution.
func (m *Metrics) MarkBlockExecuted(block *blocks.Block) {
	m.LastExecutedHeight.Set(float64(block.Height()))
}

// MarkBlockSettled updates metrics for a block that has settled.
func (m *Metrics) MarkBlockSettled(block *blocks.Block) {
	m.LastSettledHeight.Set(float64(block.Height()))
}
