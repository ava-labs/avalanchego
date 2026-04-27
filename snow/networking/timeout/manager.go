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

func NewManager(
	timeoutConfig *timer.AdaptiveTimeoutConfig,
	benchlistMgr benchlist.Manager,
	requestReg prometheus.Registerer,
	responseReg prometheus.Registerer,
) (*Manager, error) {
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

	return &Manager{
		tm:           tm,
		benchlistMgr: benchlistMgr,
		metrics:      m,
	}, nil
}

// Manager manages timeouts for requests sent to peers.
type Manager struct {
	tm           timer.AdaptiveTimeoutManager
	benchlistMgr benchlist.Manager
	metrics      *timeoutMetrics
	stopOnce     sync.Once
}

// Dispatch starts the manager. Must be called before any other method.
// Should be called in a goroutine.
func (m *Manager) Dispatch() {
	m.tm.Dispatch()
}

// TimeoutDuration returns the current timeout duration.
func (m *Manager) TimeoutDuration() time.Duration {
	return m.tm.TimeoutDuration()
}

// IsBenched returns true if messages to [nodeID] regarding [chainID]
// should not be sent over the network and should immediately fail.
func (m *Manager) IsBenched(chainID ids.ID, nodeID ids.NodeID) bool {
	return m.benchlistMgr.IsBenched(chainID, nodeID)
}

// RegisterChain registers the existence of the given chain.
// Must be called before any method calls that use the
// ID of the chain.
func (m *Manager) RegisterChain(ctx *snow.ConsensusContext) error {
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
// time, [timeoutHandler] is executed.
func (m *Manager) RegisterRequest(
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

// RegisterResponse registers that [nodeID] sent us a response of type [op]
// for the given chain. The response corresponds to the given
// [requestID] we sent them. [latency] is the time between us
// sending them the request and receiving their response.
func (m *Manager) RegisterResponse(
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

// RemoveRequest marks that we no longer expect a response to this request.
// Does not modify the timeout.
func (m *Manager) RemoveRequest(requestID ids.RequestID) {
	m.tm.Remove(requestID)
}

// Stop stops the manager.
func (m *Manager) Stop() {
	m.stopOnce.Do(m.tm.Stop)
}
