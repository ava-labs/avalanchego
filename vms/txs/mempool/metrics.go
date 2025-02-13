// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var _ Metrics = (*metrics)(nil)

type metrics struct {
	numTxs               prometheus.Gauge
	bytesAvailableMetric prometheus.Gauge
}

func NewMetrics(namespace string, registerer prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		numTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "Number of transactions in the mempool",
		}),
		bytesAvailableMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "bytes_available",
			Help:      "Number of bytes of space currently available in the mempool",
		}),
	}

	err := errors.Join(
		registerer.Register(m.numTxs),
		registerer.Register(m.bytesAvailableMetric),
	)

	return m, err
}

func (m *metrics) Update(numTxs, bytesAvailable int) {
	m.numTxs.Set(float64(numTxs))
	m.bytesAvailableMetric.Set(float64(bytesAvailable))
}
