// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Manager registers and fires timeouts for the snow API.
type Manager struct {
	tm        timer.AdaptiveTimeoutManager
	benchlist benchlist.Manager
	executor  timer.Executor
}

// Initialize this timeout manager.
func (m *Manager) Initialize(timeoutConfig *timer.AdaptiveTimeoutConfig, benchlist benchlist.Manager) error {
	m.benchlist = benchlist
	m.executor.Initialize()
	return m.tm.Initialize(timeoutConfig)
}

// Dispatch ...
func (m *Manager) Dispatch() {
	go m.executor.Dispatch()
	m.tm.Dispatch()
}

// RegisterChain ...
func (m *Manager) RegisterChain(ctx *snow.Context, namespace string) {
	m.benchlist.RegisterChain(ctx, namespace)
}

// Register request to time out unless Manager.Cancel is called
// before the timeout duration passes, with the same request parameters.
func (m *Manager) Register(validatorID ids.ShortID, chainID ids.ID, requestID uint32, register bool, msgType constants.MsgType, timeout func()) (time.Time, bool) {
	if register {
		if ok := m.benchlist.RegisterQuery(chainID, validatorID, requestID, msgType); !ok {
			m.executor.Add(timeout)
			return time.Time{}, false
		}
	}
	return m.tm.Put(createRequestID(validatorID, chainID, requestID), func() {
		m.benchlist.QueryFailed(chainID, validatorID, requestID) // Benchlist ignores QueryFailed if it was not registered
		timeout()
	}), true
}

// Cancel request timeout with the specified parameters.
func (m *Manager) Cancel(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	m.benchlist.RegisterResponse(chainID, validatorID, requestID)
	m.tm.Remove(createRequestID(validatorID, chainID, requestID))
}

func createRequestID(validatorID ids.ShortID, chainID ids.ID, requestID uint32) ids.ID {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.IntLen)}
	p.PackInt(requestID)

	return ids.NewID(hashing.ByteArraysToHash256Array(validatorID.Bytes(), chainID.Bytes(), p.Bytes))
}
