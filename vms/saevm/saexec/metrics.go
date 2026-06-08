// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import "github.com/prometheus/client_golang/prometheus"

type metrics struct {
	lastExecutedHeight prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		lastExecutedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_executed_height",
			Help: "Height of the latest block that completed async execution.",
		}),
	}
	if err := reg.Register(m.lastExecutedHeight); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *metrics) markExecuted(height uint64) {
	m.lastExecutedHeight.Set(float64(height))
}
