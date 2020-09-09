// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"time"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/hashing"
	"github.com/ava-labs/avalanche-go/utils/timer"
	"github.com/ava-labs/avalanche-go/utils/wrappers"
)

// Manager registers and fires timeouts for the snow API.
type Manager struct{ tm timer.AdaptiveTimeoutManager }

// Initialize this timeout manager.
func (m *Manager) Initialize(config *timer.AdaptiveTimeoutConfig) error {
	return m.tm.Initialize(config)
}

// Dispatch ...
func (m *Manager) Dispatch() { m.tm.Dispatch() }

// Register request to time out unless Manager.Cancel is called
// before the timeout duration passes, with the same request parameters.
func (m *Manager) Register(validatorID ids.ShortID, chainID ids.ID, requestID uint32, timeout func()) time.Time {
	return m.tm.Put(createRequestID(validatorID, chainID, requestID), timeout)
}

// Cancel request timeout with the specified parameters.
func (m *Manager) Cancel(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	m.tm.Remove(createRequestID(validatorID, chainID, requestID))
}

func createRequestID(validatorID ids.ShortID, chainID ids.ID, requestID uint32) ids.ID {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.IntLen)}
	p.PackInt(requestID)

	return ids.NewID(hashing.ByteArraysToHash256Array(validatorID.Bytes(), chainID.Bytes(), p.Bytes))
}
