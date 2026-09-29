// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	minBlockDelay prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		minBlockDelay: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "min_block_delay_seconds",
			Help: "ACP-226 minimum block delay, in seconds.",
		}),
	}
	return m, reg.Register(m.minBlockDelay)
}

func (m *metrics) setMinBlockDelay(d time.Duration) {
	m.minBlockDelay.Set(d.Seconds())
}
