// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"github.com/prometheus/client_golang/prometheus"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
)

// lastSettledHeightName names the gauge for the height of the last settled
// block.
const lastSettledHeightName = "last_settled_height"

type metrics struct {
	registry          *prometheus.Registry
	lastSettledHeight prometheus.Gauge
}

func newMetrics(snowMetrics apimetrics.MultiGatherer) (*metrics, error) {
	reg, err := apimetrics.MakeAndRegister(snowMetrics, "sae")
	if err != nil {
		return nil, err
	}

	m := &metrics{
		registry: reg,
		lastSettledHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: lastSettledHeightName,
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
