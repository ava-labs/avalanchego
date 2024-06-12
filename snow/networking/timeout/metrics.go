// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
)

const (
	chainLabel = "chain"
	opLabel    = "op"
)

var opLabels = []string{chainLabel, opLabel}

type timeoutMetrics struct {
	messages                  *prometheus.CounterVec   // chain + op
	messageLatencies          *prometheus.GaugeVec     // chain + op
	messageLatenciesHistogram *prometheus.HistogramVec // chain + op

	lock           sync.RWMutex
	chainIDToAlias map[ids.ID]string
}

func newTimeoutMetrics(reg prometheus.Registerer) (*timeoutMetrics, error) {
	m := &timeoutMetrics{
		messages: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "messages",
				Help: "number of responses",
			},
			opLabels,
		),
		messageLatencies: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "message_latencies",
				Help: "message latencies (ns)",
			},
			opLabels,
		),
		chainIDToAlias: make(map[ids.ID]string),
		messageLatenciesHistogram: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "message_latencies_histogram",
				Help: "message latencies histogram (ns)",
				Buckets: []float64{
					float64(10 * time.Millisecond),
					float64(50 * time.Millisecond),
					float64(100 * time.Millisecond),
					float64(150 * time.Millisecond),
					float64(200 * time.Millisecond),
					float64(250 * time.Millisecond),
					float64(300 * time.Millisecond),
					float64(350 * time.Millisecond),
					float64(400 * time.Millisecond),
					float64(450 * time.Millisecond),
					float64(500 * time.Millisecond),
					float64(750 * time.Millisecond),
					float64(time.Second),
					float64(1500 * time.Millisecond),
					float64(2 * time.Second),
				},
			},
			opLabels,
		),
	}
	return m, utils.Err(
		reg.Register(m.messages),
		reg.Register(m.messageLatencies),
		reg.Register(m.messageLatenciesHistogram),
	)
}

func (m *timeoutMetrics) RegisterChain(ctx *snow.ConsensusContext) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.chainIDToAlias[ctx.ChainID] = ctx.PrimaryAlias
	return nil
}

// Record that a response of type [op] took [latency]
func (m *timeoutMetrics) Observe(chainID ids.ID, op message.Op, latency time.Duration) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	labels := prometheus.Labels{
		chainLabel: m.chainIDToAlias[chainID],
		opLabel:    op.String(),
	}
	m.messages.With(labels).Inc()
	m.messageLatencies.With(labels).Add(float64(latency))
}
