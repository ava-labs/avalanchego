// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

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
	// Sampled at scrape time rather than via a setter like lastSettledHeight:
	// the count changes through GC finalizers, with no event to update on.
	inMemoryBlocks := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "in_memory_blocks",
			Help: "Number of SAE blocks still live in memory (created but not yet garbage collected).",
		},
		func() float64 {
			return float64(blocks.InMemoryBlockCount())
		},
	)
	return m, errors.Join(
		reg.Register(m.lastSettledHeight),
		reg.Register(inMemoryBlocks),
	)
}

func (m *metrics) markSettled(height uint64) {
	m.lastSettledHeight.Set(float64(height))
}
