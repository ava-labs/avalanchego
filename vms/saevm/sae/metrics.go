// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import "github.com/prometheus/client_golang/prometheus"

// LastSettledHeightName names the gauge for the height of the last settled
// block.
const LastSettledHeightName = "last_settled_height"

type metrics struct {
	lastSettledHeight prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		lastSettledHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: LastSettledHeightName,
			Help: "Height of the latest block that has settled.",
		}),
	}
	return m, reg.Register(m.lastSettledHeight)
}

func (m *metrics) markSettled(height uint64) {
	m.lastSettledHeight.Set(float64(height))
}
