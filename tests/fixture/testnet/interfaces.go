// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testnet

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
)

// Defines network capabilities supportable regardless of how a network is orchestrated.
type Network interface {
	GetConfig() NetworkConfig
	GetNodes() []Node
}

// Defines node capabilities supportable regardless of how a network is orchestrated.
type Node interface {
	GetID() ids.NodeID
	GetConfig() NodeConfig
	GetProcessContext() node.NodeProcessContext
}
