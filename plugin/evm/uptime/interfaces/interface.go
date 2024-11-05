// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interfaces

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	validatorsinterfaces "github.com/ava-labs/subnet-evm/plugin/evm/validators/interfaces"
)

type PausableManager interface {
	uptime.Manager
	validatorsinterfaces.StateCallbackListener
	IsPaused(nodeID ids.NodeID) bool
}
