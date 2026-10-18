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
	unsettledGasLimit prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		lastSettledHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_settled_height",
			Help: "Height of the latest block that has settled.",
		}),
		unsettledGasLimit: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "unsettled_gas_limit",
			Help: "Worst-case gas of accepted blocks that have not yet settled.",
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
		reg.Register(m.unsettledGasLimit),
		reg.Register(inMemoryBlocks),
	)
}

// setFrontiers seeds the gauges from the last-settled block and the blocks
// accepted after it.
func (m *metrics) setFrontiers(lastSettled *blocks.Block, unsettled []*blocks.Block) {
	m.lastSettledHeight.Set(float64(lastSettled.Height()))
	var gas float64
	for _, b := range unsettled {
		gas += float64(b.WorstCaseGasUsed())
	}
	m.unsettledGasLimit.Set(gas)
}

func (m *metrics) markAccepted(b *blocks.Block) {
	m.unsettledGasLimit.Add(float64(b.WorstCaseGasUsed()))
}

func (m *metrics) markSettled(b *blocks.Block) {
	m.lastSettledHeight.Set(float64(b.Height()))
	// Subtracting worst-case instead of consumed undoes [metrics.setFrontiers]
	// and [metrics.markAccepted].
	m.unsettledGasLimit.Sub(float64(b.WorstCaseGasUsed()))
}
