// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
)

const (
	chainLabel = "chain"
	opLabel    = "op"
)

var opLabels = []string{chainLabel, opLabel}

type timeoutMetrics struct {
	messages         *prometheus.CounterVec // chain + op
	messageLatencies *prometheus.GaugeVec   // chain + op

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
	}
	return m, errors.Join(
		reg.Register(m.messages),
		reg.Register(m.messageLatencies),
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
