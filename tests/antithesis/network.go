// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

// Creates a network with the given number of nodes.
func CreateNetwork(nodesCount int) *tmpnet.Network {
	network := tmpnet.NewDefaultNetwork("antithesis-network")
	network.Nodes = tmpnet.NewNodesOrPanic(nodesCount)
	return network
}
