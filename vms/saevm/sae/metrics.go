// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import "github.com/prometheus/client_golang/prometheus"

type metrics struct {
	lastSettledHeight prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		lastSettledHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_settled_height",
			Help: "Height of the latest block that has settled.",
		}),
	}
	if err := reg.Register(m.lastSettledHeight); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *metrics) markSettled(height uint64) {
	m.lastSettledHeight.Set(float64(height))
}
