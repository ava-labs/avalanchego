// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
)

const (
	responseNamespace = "response"
	opLabel           = "op"
)

var opLabels = []string{opLabel}

type metrics struct {
	lock           sync.Mutex
	chainToMetrics map[ids.ID]*chainMetrics
}

func (m *metrics) RegisterChain(ctx *snow.ConsensusContext) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.chainToMetrics == nil {
		m.chainToMetrics = map[ids.ID]*chainMetrics{}
	}
	if _, exists := m.chainToMetrics[ctx.ChainID]; exists {
		return fmt.Errorf("chain %s has already been registered", ctx.ChainID)
	}
	cm, err := newChainMetrics(ctx.Registerer)
	if err != nil {
		return fmt.Errorf("couldn't create metrics for chain %s: %w", ctx.ChainID, err)
	}
	m.chainToMetrics[ctx.ChainID] = cm
	return nil
}

// Record that a response of type [op] took [latency]
func (m *metrics) Observe(chainID ids.ID, op message.Op, latency time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()

	cm, exists := m.chainToMetrics[chainID]
	if !exists {
		// TODO should this log an error?
		return
	}
	cm.observe(op, latency)
}

// chainMetrics contains message response time metrics for a chain
type chainMetrics struct {
	messages         *prometheus.CounterVec // op
	messageLatencies *prometheus.GaugeVec   // op
}

func newChainMetrics(reg prometheus.Registerer) (*chainMetrics, error) {
	cm := &chainMetrics{
		messages: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: responseNamespace,
				Name:      "messages",
				Help:      "number of responses",
			},
			opLabels,
		),
		messageLatencies: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: responseNamespace,
				Name:      "message_latencies",
				Help:      "message latencies (ns)",
			},
			opLabels,
		),
	}
	return cm, utils.Err(
		reg.Register(cm.messages),
		reg.Register(cm.messageLatencies),
	)
}

func (cm *chainMetrics) observe(op message.Op, latency time.Duration) {
	labels := prometheus.Labels{
		opLabel: op.String(),
	}
	cm.messages.With(labels).Inc()
	cm.messageLatencies.With(labels).Add(float64(latency))
}
