// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package metrics provides the metrics shared by the SAE chain
// implementations.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics reports SAE chain implementation metrics.
type Metrics struct {
	minBlockDelay prometheus.Gauge
}

// New registers the metrics with reg.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		minBlockDelay: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "min_block_delay_seconds",
			Help: "ACP-226 minimum block delay, in seconds.",
		}),
	}
	return m, reg.Register(m.minBlockDelay)
}

// SetMinBlockDelay records the ACP-226 minimum block delay.
func (m *Metrics) SetMinBlockDelay(d time.Duration) {
	m.minBlockDelay.Set(d.Seconds())
}
