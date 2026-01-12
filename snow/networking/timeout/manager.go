// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/utils/timer"
)

var _ Manager = (*manager)(nil)

// Manages timeouts for requests sent to peers.
type Manager interface {
	// Start the manager. Must be called before any other method.
	// Should be called in a goroutine.
	Dispatch()
	// TimeoutDuration returns the current timeout duration.
	TimeoutDuration() time.Duration
	// IsBenched returns true if messages to [nodeID] regarding [chainID]
	// should not be sent over the network and should immediately fail.
	IsBenched(nodeID ids.NodeID, chainID ids.ID) bool
	// Register the existence of the given chain.
	// Must be called before any method calls that use the
	// ID of the chain.
	RegisterChain(ctx *snow.ConsensusContext) error
	// RegisterRequest notes that we expect a response of type [op] from
	// [nodeID] for chain [chainID]. If we don't receive a response in
	// time, [timeoutHandler] is executed.
	RegisterRequest(
		nodeID ids.NodeID,
		chainID ids.ID,
		measureLatency bool,
		requestID ids.RequestID,
		timeoutHandler func(),
	)
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
		nodeID ids.NodeID,
		chainID ids.ID,
		requestID ids.RequestID,
		op message.Op,
		latency time.Duration,
	)
	// Mark that we no longer expect a response to this request we sent.
	// Does not modify the timeout.
	RemoveRequest(requestID ids.RequestID)

	// Stops the manager.
	Stop()
}

func NewManager(
	timeoutConfig *timer.AdaptiveTimeoutConfig,
	benchlistMgr benchlist.Manager,
	requestReg prometheus.Registerer,
	responseReg prometheus.Registerer,
) (Manager, error) {
	tm, err := timer.NewAdaptiveTimeoutManager(
		timeoutConfig,
		requestReg,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't create timeout manager: %w", err)
	}

	m, err := newTimeoutMetrics(responseReg)
	if err != nil {
		return nil, fmt.Errorf("couldn't create timeout metrics: %w", err)
	}

	return &manager{
		tm:           tm,
		benchlistMgr: benchlistMgr,
		metrics:      m,
	}, nil
}

type manager struct {
	tm           timer.AdaptiveTimeoutManager
	benchlistMgr benchlist.Manager
	metrics      *timeoutMetrics
	stopOnce     sync.Once
}

func (m *manager) Dispatch() {
	m.tm.Dispatch()
}

func (m *manager) TimeoutDuration() time.Duration {
	return m.tm.TimeoutDuration()
}

// IsBenched returns true if messages to [nodeID] regarding [chainID]
// should not be sent over the network and should immediately fail.
func (m *manager) IsBenched(nodeID ids.NodeID, chainID ids.ID) bool {
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

// RegisterRequest notes that we expect a response of type [op] from
// [nodeID] regarding chain [chainID]. If we don't receive a response in
// time, [timeoutHandler]  is executed.
func (m *manager) RegisterRequest(
	nodeID ids.NodeID,
	chainID ids.ID,
	measureLatency bool,
	requestID ids.RequestID,
	timeoutHandler func(),
) {
	newTimeoutHandler := func() {
		if requestID.Op != byte(message.AppResponseOp) {
			// If the request timed out and wasn't an AppRequest, tell the
			// benchlist manager.
			m.benchlistMgr.RegisterFailure(chainID, nodeID)
		}
		timeoutHandler()
	}
	m.tm.Put(requestID, measureLatency, newTimeoutHandler)
}

// RegisterResponse registers that we received a response from [nodeID]
// regarding the given request ID and chain.
func (m *manager) RegisterResponse(
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID ids.RequestID,
	op message.Op,
	latency time.Duration,
) {
	m.metrics.Observe(chainID, op, latency)
	m.benchlistMgr.RegisterResponse(chainID, nodeID)
	m.tm.Remove(requestID)
}

func (m *manager) RemoveRequest(requestID ids.RequestID) {
	m.tm.Remove(requestID)
}

func (m *manager) RegisterRequestToUnreachableValidator() {
	m.tm.ObserveLatency(m.TimeoutDuration())
}

func (m *manager) Stop() {
	m.stopOnce.Do(m.tm.Stop)
}
