// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/blacklist"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Manager registers and fires timeouts for the snow API.
type Manager struct {
	tm        timer.AdaptiveTimeoutManager
	blacklist blacklist.Manager
}

// Initialize this timeout manager.
func (m *Manager) Initialize(timeoutConfig *timer.AdaptiveTimeoutConfig, blacklist blacklist.Manager) error {
	m.blacklist = blacklist
	return m.tm.Initialize(timeoutConfig)
}

// Dispatch ...
func (m *Manager) Dispatch() { m.tm.Dispatch() }

// Register request to time out unless Manager.Cancel is called
// before the timeout duration passes, with the same request parameters.
func (m *Manager) Register(validatorID ids.ShortID, chainID ids.ID, requestID uint32, timeout func()) (time.Time, bool) {
	if ok := m.blacklist.RegisterQuery(chainID, validatorID, requestID); !ok {
		timeout() // TODO use executor to execute asynchronously
		return time.Time{}, false
	}
	return m.tm.Put(createRequestID(validatorID, chainID, requestID), func() {
		m.blacklist.QueryFailed(chainID, validatorID, requestID)
		timeout()
	}), true
}

// Cancel request timeout with the specified parameters.
func (m *Manager) Cancel(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	m.blacklist.RegisterResponse(chainID, validatorID, requestID)
	m.tm.Remove(createRequestID(validatorID, chainID, requestID))
}

func createRequestID(validatorID ids.ShortID, chainID ids.ID, requestID uint32) ids.ID {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.IntLen)}
	p.PackInt(requestID)

	return ids.NewID(hashing.ByteArraysToHash256Array(validatorID.Bytes(), chainID.Bytes(), p.Bytes))
}
