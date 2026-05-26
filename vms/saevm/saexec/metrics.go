// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"github.com/prometheus/client_golang/prometheus"
)

// LastExecutedHeightName names the gauge for the height of the last block
// that completed async execution.
const LastExecutedHeightName = "last_executed_height"

type metrics struct {
	lastExecutedHeight prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		lastExecutedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: LastExecutedHeightName,
			Help: "Height of the latest block that completed async execution.",
		}),
	}
	return m, reg.Register(m.lastExecutedHeight)
}

func (m *metrics) markExecuted(height uint64) {
	m.lastExecutedHeight.Set(float64(height))
}
