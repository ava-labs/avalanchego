// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/prometheus/client_golang/prometheus"
)

var _ Manager = &manager{}

// Manages timeouts for requests sent to peers.
type Manager interface {
	// Start the manager. Must be called before any other method.
	// Should be called in a goroutine.
	Dispatch()
	// TimeoutDuration returns the current timeout duration.
	TimeoutDuration() time.Duration
	// IsBenched returns true if messages to [nodeID] regarding [chainID]
	// should not be sent over the network and should immediately fail.
	IsBenched(nodeID ids.ShortID, chainID ids.ID) bool
	// Register the existence of the given chain.
	// Must be called before any method calls that use the
	// ID of the chain.
	RegisterChain(ctx *snow.ConsensusContext) error
	// RegisterRequest notes that we expect a response of type [op] from
	// [nodeID] for chain [chainID]. If we don't receive a response in
	// time, [timeoutHandler] is executed.
	RegisterRequest(
		nodeID ids.ShortID,
		chainID ids.ID,
		op message.Op,
		requestID ids.ID,
		timeoutHandler func(),
	) (time.Time, bool)
	// Registers that we would have sent a request to a validator but they
	// are unreachable because they are benched or because of network conditions
	// (e.g. we're not connected), so we didn't send the query. For the sake
	// of calculating the average latency and network timeout, we act as
	// though we sent the validator a request and it timed out.
	RegisterRequestToUnreachableValidator()
	// Registers that [nodeID] sent us a response of type [op]
	// for the given chain. The response corresponds to the given
	// requestID we sent them. [latency] is the time between us
	// sending them the request and receiving their response.
	RegisterResponse(
		nodeID ids.ShortID,
		chainID ids.ID,
		requestID ids.ID,
		op message.Op,
		latency time.Duration,
	)
	// Mark that we no longer expect a response to this request we sent.
	// Does not modify the timeout.
	RemoveRequest(requestID ids.ID)
}

func NewManager(
	timeoutConfig *timer.AdaptiveTimeoutConfig,
	benchlistMgr benchlist.Manager,
	metricsNamespace string,
	metricsRegister prometheus.Registerer,
) (Manager, error) {
	m := &manager{
		benchlistMgr: benchlistMgr,
	}
	return m, m.tm.Initialize(timeoutConfig, metricsNamespace, metricsRegister)
}

type manager struct {
	tm           timer.AdaptiveTimeoutManager
	benchlistMgr benchlist.Manager
	metrics      metrics
}

func (m *manager) Dispatch() {
	m.tm.Dispatch()
}

func (m *manager) TimeoutDuration() time.Duration {
	return m.tm.TimeoutDuration()
}

func (m *manager) IsBenched(nodeID ids.ShortID, chainID ids.ID) bool {
	return m.benchlistMgr.IsBenched(nodeID, chainID)
}

func (m *manager) RegisterChain(ctx *snow.ConsensusContext) error {
	if err := m.metrics.RegisterChain(ctx); err != nil {
		return fmt.Errorf("couldn't register timeout metrics for chain %s: %w", ctx.ChainID, err)
	}
	if err := m.benchlistMgr.RegisterChain(ctx); err != nil {
		return fmt.Errorf("couldn't register chain %s with benchlist manager: %w", ctx.ChainID, err)
	}
	return nil
}

func (m *manager) RegisterRequest(
	nodeID ids.ShortID,
	chainID ids.ID,
	op message.Op,
	requestID ids.ID,
	timeoutHandler func(),
) (time.Time, bool) {
	newTimeoutHandler := func() {
		// If this request timed out, tell the benchlist manager
		m.benchlistMgr.RegisterFailure(chainID, nodeID)
		timeoutHandler()
	}
	return m.tm.Put(requestID, op, newTimeoutHandler), true
}

func (m *manager) RegisterResponse(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID ids.ID,
	op message.Op,
	latency time.Duration,
) {
	m.metrics.Observe(nodeID, chainID, op, latency)
	m.benchlistMgr.RegisterResponse(chainID, nodeID)
	m.tm.Remove(requestID)
}

func (m *manager) RemoveRequest(requestID ids.ID) {
	m.tm.Remove(requestID)
}

func (m *manager) RegisterRequestToUnreachableValidator() {
	m.tm.ObserveLatency(m.TimeoutDuration())
}
