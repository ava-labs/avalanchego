// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"github.com/ava-labs/avalanchego/tests/fixture/pchain"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

// NewPChainNode returns the P-Chain client of one node, labeled by node ID.
func NewPChainNode(nodeURI tmpnet.NodeURI) pchain.Node {
	return pchain.NewNode(nodeURI.NodeID, nodeURI.URI)
}

// NewPChainNodes returns every running, non-ephemeral node.
func NewPChainNodes(network *tmpnet.Network) []pchain.Node {
	nodeURIs := network.GetNodeURIs()
	nodes := make([]pchain.Node, len(nodeURIs))
	for i, nodeURI := range nodeURIs {
		nodes[i] = NewPChainNode(nodeURI)
	}
	return nodes
}
