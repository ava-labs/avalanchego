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

// Manager registers and fires timeouts for the snow API.
type Manager struct {
	tm           timer.AdaptiveTimeoutManager
	benchlistMgr benchlist.Manager
	metrics      metrics
}

// Initialize this timeout manager.
func (m *Manager) Initialize(
	timeoutConfig *timer.AdaptiveTimeoutConfig,
	benchlistMgr benchlist.Manager,
	metricsNamespace string,
	metricsRegister prometheus.Registerer,
) error {
	m.benchlistMgr = benchlistMgr
	return m.tm.Initialize(timeoutConfig, metricsNamespace, metricsRegister)
}

func (m *Manager) Dispatch() {
	m.tm.Dispatch()
}

// TimeoutDuration returns the current network timeout duration
func (m *Manager) TimeoutDuration() time.Duration {
	return m.tm.TimeoutDuration()
}

// IsBenched returns true if messages to [validatorID] regarding [chainID]
// should not be sent over the network and should immediately fail.
func (m *Manager) IsBenched(validatorID ids.ShortID, chainID ids.ID) bool {
	return m.benchlistMgr.IsBenched(validatorID, chainID)
}

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
// [validatorID] regarding chain [chainID]. If we don't receive a response in
// time, [timeoutHandler]  is executed.
func (m *Manager) RegisterRequest(
	validatorID ids.ShortID,
	chainID ids.ID,
	op message.Op,
	uniqueRequestID ids.ID,
	timeoutHandler func(),
) (time.Time, bool) {
	newTimeoutHandler := func() {
		// If this request timed out, tell the benchlist manager
		m.benchlistMgr.RegisterFailure(chainID, validatorID)
		timeoutHandler()
	}
	return m.tm.Put(uniqueRequestID, op, newTimeoutHandler), true
}

// RegisterResponse registers that we received a response from [validatorID]
// regarding the given request ID and chain.
func (m *Manager) RegisterResponse(
	validatorID ids.ShortID,
	chainID ids.ID,
	uniqueRequestID ids.ID,
	op message.Op,
	latency time.Duration,
) {
	m.metrics.Observe(validatorID, chainID, op, latency)
	m.benchlistMgr.RegisterResponse(chainID, validatorID)
	m.tm.Remove(uniqueRequestID)
}

// RemoveRequest clears the request with the provided ID.
func (m *Manager) RemoveRequest(uniqueRequestID ids.ID) {
	m.tm.Remove(uniqueRequestID)
}

// RegisterRequestToUnreachableValidator registers that we would have sent
// a query to a validator but they are unreachable because they are bench
// or because of network conditions (e.g. we're not connected), so we didn't
// send the query. For the sake// of calculating the average latency and
// network timeout, we act as though we sent the validator a request and it timed out.
func (m *Manager) RegisterRequestToUnreachableValidator() {
	m.tm.ObserveLatency(m.TimeoutDuration())
}
