// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interfaces

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"

	validatorsstateinterfaces "github.com/ava-labs/subnet-evm/plugin/evm/validators/state/interfaces"
)

type PausableManager interface {
	uptime.Manager
	validatorsstateinterfaces.StateCallbackListener
	IsPaused(nodeID ids.NodeID) bool
}
