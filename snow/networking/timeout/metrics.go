// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	defaultRequestHelpMsg = "time (in ns) spent waiting for a response to this message"
	validatorIDLabel      = "validatorID"
)

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
	cm, err := newChainMetrics(ctx, false)
	if err != nil {
		return fmt.Errorf("couldn't create metrics for chain %s: %w", ctx.ChainID, err)
	}
	m.chainToMetrics[ctx.ChainID] = cm
	return nil
}

// Record that a response of type [op] took [latency]
func (m *metrics) Observe(validatorID ids.ShortID, chainID ids.ID, op message.Op, latency time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()

	cm, exists := m.chainToMetrics[chainID]
	if !exists {
		// TODO should this log an error?
		return
	}
	cm.observe(validatorID, op, latency)
}

// chainMetrics contains message response time metrics for a chain
type chainMetrics struct {
	ctx *snow.ConsensusContext

	messageLatencies map[message.Op]metric.Averager

	summaryEnabled   bool
	messageSummaries map[message.Op]*prometheus.SummaryVec
}

func newChainMetrics(ctx *snow.ConsensusContext, summaryEnabled bool) (*chainMetrics, error) {
	cm := &chainMetrics{
		ctx: ctx,

		messageLatencies: make(map[message.Op]metric.Averager, len(message.ConsensusResponseOps)),

		summaryEnabled:   summaryEnabled,
		messageSummaries: make(map[message.Op]*prometheus.SummaryVec, len(message.ConsensusResponseOps)),
	}

	errs := wrappers.Errs{}
	for _, op := range message.ConsensusResponseOps {
		cm.messageLatencies[op] = metric.NewAveragerWithErrs(
			"lat",
			op.String(),
			defaultRequestHelpMsg,
			ctx.Registerer,
			&errs,
		)

		if !summaryEnabled {
			continue
		}

		summaryName := fmt.Sprintf("%s_peer", op)
		summary := prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Namespace: "lat",
				Name:      summaryName,
				Help:      defaultRequestHelpMsg,
			},
			[]string{validatorIDLabel},
		)
		cm.messageSummaries[op] = summary

		if err := ctx.Registerer.Register(summary); err != nil {
			errs.Add(fmt.Errorf("failed to register %s statistics: %w", summaryName, err))
		}
	}
	return cm, errs.Err
}

func (cm *chainMetrics) observe(validatorID ids.ShortID, op message.Op, latency time.Duration) {
	lat := float64(latency)
	if msg, exists := cm.messageLatencies[op]; exists {
		msg.Observe(lat)
	}

	if !cm.summaryEnabled {
		return
	}

	labels := prometheus.Labels{
		validatorIDLabel: validatorID.String(),
	}

	msg, exists := cm.messageSummaries[op]
	if !exists {
		return
	}

	observer, err := msg.GetMetricWith(labels)
	if err != nil {
		cm.ctx.Log.Warn("failed to get observer with validatorID label due to %s", err)
		return
	}
	observer.Observe(lat)
}
