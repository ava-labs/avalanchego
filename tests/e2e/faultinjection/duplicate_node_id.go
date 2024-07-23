// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package faultinjection

import (
	"context"
	"fmt"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/set"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("Duplicate node handling", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("should ensure that a given Node ID (i.e. staking keypair) can be used at most once on a network", func() {
		network := e2e.Env.GetNetwork()

		ginkgo.By("creating new node")
		node1 := e2e.AddEphemeralNode(network, tmpnet.FlagsMap{})
		e2e.WaitForHealthy(node1)

		ginkgo.By("checking that the new node is connected to its peers")
		checkConnectedPeers(network.Nodes, node1)

		ginkgo.By("creating a second new node with the same staking keypair as the first new node")
		node1Flags := node1.Flags
		node2Flags := tmpnet.FlagsMap{
			config.StakingTLSKeyContentKey: node1Flags[config.StakingTLSKeyContentKey],
			config.StakingCertContentKey:   node1Flags[config.StakingCertContentKey],
			// Construct a unique data dir to ensure the two nodes' data will be stored
			// separately. Usually the dir name is the node ID but in this one case the nodes have
			// the same node ID.
			config.DataDirKey: fmt.Sprintf("%s-second", node1Flags[config.DataDirKey]),
		}
		node2 := e2e.AddEphemeralNode(network, node2Flags)

		ginkgo.By("checking that the second new node fails to become healthy before timeout")
		err := tmpnet.WaitForHealthy(e2e.DefaultContext(), node2)
		require.ErrorIs(err, context.DeadlineExceeded)

		ginkgo.By("stopping the first new node")
		require.NoError(node1.Stop(e2e.DefaultContext()))

		ginkgo.By("checking that the second new node becomes healthy within timeout")
		e2e.WaitForHealthy(node2)

		ginkgo.By("checking that the second new node is connected to its peers")
		checkConnectedPeers(network.Nodes, node2)

		// A bootstrap check was already performed by the second node.
	})
})

// Check that a new node is connected to existing nodes and vice versa
func checkConnectedPeers(existingNodes []*tmpnet.Node, newNode *tmpnet.Node) {
	require := require.New(ginkgo.GinkgoT())

	// Collect the node ids of the new node's peers
	infoClient := info.NewClient(newNode.URI)
	peers, err := infoClient.Peers(e2e.DefaultContext())
	require.NoError(err)
	peerIDs := set.NewSet[ids.NodeID](len(existingNodes))
	for _, peer := range peers {
		peerIDs.Add(peer.ID)
	}

	for _, existingNode := range existingNodes {
		// Check that the existing node is a peer of the new node
		require.True(peerIDs.Contains(existingNode.NodeID))

		// Check that the new node is a peer
		infoClient := info.NewClient(existingNode.URI)
		peers, err := infoClient.Peers(e2e.DefaultContext())
		require.NoError(err)
		isPeer := false
		for _, peer := range peers {
			if peer.ID == newNode.NodeID {
				isPeer = true
				break
			}
		}
		require.True(isPeer)
	}
}
