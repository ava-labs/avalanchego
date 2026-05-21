// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// Exported metric names for the SAE lifecycle bucket. Cross-package callers
// (tests, alert/dashboard wiring) should reference these constants rather
// than the raw strings.
const (
	LastExecutedHeightName = "last_executed_height"
	LastSettledHeightName  = "last_settled_height"
)

// metrics holds the SAE block-lifecycle gauges. Both live here — the lowest
// package that updates a member of the bucket — so one site owns
// registration. Execution events update last_executed_height in-package;
// settlement updates last_settled_height through [Executor.MarkSettled].
type metrics struct {
	lastExecutedHeight prometheus.Gauge
	lastSettledHeight  prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		lastExecutedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: LastExecutedHeightName,
			Help: "Height of the latest block that completed async execution.",
		}),
		lastSettledHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: LastSettledHeightName,
			Help: "Height of the latest block that has settled.",
		}),
	}
	return m, errors.Join(
		reg.Register(m.lastExecutedHeight),
		reg.Register(m.lastSettledHeight),
	)
}

func (m *metrics) markExecuted(height uint64) {
	m.lastExecutedHeight.Set(float64(height))
}

func (m *metrics) markSettled(height uint64) {
	m.lastSettledHeight.Set(float64(height))
}
