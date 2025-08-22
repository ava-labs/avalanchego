// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interfaces

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"

	"github.com/ava-labs/avalanchego/vms/evm/plugin/validators/state"
)

type PausableManager interface {
	uptime.Manager
	state.StateCallbackListener
	IsPaused(nodeID ids.NodeID) bool
}
