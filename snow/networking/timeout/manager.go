// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type request struct {
	// When this request was registered with the timeout manager
	time.Time
	// The type of request that was made
	constants.MsgType
}

// Manager registers and fires timeouts for the snow API.
type Manager struct {
	lock sync.Mutex
	// Tells the time. Can be faked for testing.
	clock        timer.Clock
	tm           timer.AdaptiveTimeoutManager
	benchlistMgr benchlist.Manager
	executor     timer.Executor
	metrics      metrics
	// Unique-ified request ID --> Time and type of message made
	requests map[ids.ID]request
}

// Initialize this timeout manager.
func (m *Manager) Initialize(timeoutConfig *timer.AdaptiveTimeoutConfig, benchlist benchlist.Manager) error {
	m.benchlistMgr = benchlist
	m.requests = map[ids.ID]request{}
	return m.tm.Initialize(timeoutConfig)
}

// Dispatch ...
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

// RegisterChain ...
func (m *Manager) RegisterChain(ctx *snow.Context, namespace string) error {
	if err := m.metrics.RegisterChain(ctx, namespace); err != nil {
		return fmt.Errorf("couldn't register timeout metrics for chain %s: %w", ctx.ChainID, err)
	}
	if err := m.benchlistMgr.RegisterChain(ctx, namespace); err != nil {
		return fmt.Errorf("couldn't register chain %s with benchlist manager: %w", ctx.ChainID, err)
	}
	return nil
}

// RegisterRequests notes that we sent a request of type [msgType] to [validatorID]
// regarding chain [chainID]. If we don't receive a response in time, [timeoutHandler]
// is executed.
func (m *Manager) RegisterRequest(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	register bool,
	msgType constants.MsgType,
	timeoutHandler func(),
) (time.Time, bool) {
	uniqueRequestID := createRequestID(validatorID, chainID, requestID)
	m.lock.Lock()
	m.requests[uniqueRequestID] = request{Time: time.Now(), MsgType: msgType}
	m.lock.Unlock()
	m.benchlistMgr.RegisterQuery(chainID, validatorID, requestID, msgType)
	newTimeoutHandler := func() {
		// If this request timed out, tell the benchlist manager
		m.benchlistMgr.QueryFailed(chainID, validatorID, requestID)
		timeoutHandler()
	}
	return m.tm.Put(uniqueRequestID, newTimeoutHandler), true
}

// RegisterResponse registers that we received a response from [validatorID]
// regarding the given request ID and chain.
func (m *Manager) RegisterResponse(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	uniqueRequestID := createRequestID(validatorID, chainID, requestID)
	m.lock.Lock()
	request, ok := m.requests[uniqueRequestID]
	if !ok {
		// This message is not in response to a request we made
		// TODO add a log here
		m.lock.Unlock()
		return
	}
	delete(m.requests, uniqueRequestID)
	latency := m.clock.Time().Sub(request.Time)
	m.metrics.observe(chainID, request.MsgType, latency)
	m.lock.Unlock()
	m.benchlistMgr.RegisterResponse(chainID, validatorID, requestID)
	m.tm.Remove(createRequestID(validatorID, chainID, requestID))
}

func createRequestID(validatorID ids.ShortID, chainID ids.ID, requestID uint32) ids.ID {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.IntLen)}
	p.PackInt(requestID)
	return hashing.ByteArraysToHash256Array(validatorID.Bytes(), chainID[:], p.Bytes)
}
