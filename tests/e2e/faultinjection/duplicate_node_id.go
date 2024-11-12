// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package faultinjection

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ = ginkgo.Describe("Duplicate node handling", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should ensure that a given Node ID (i.e. staking keypair) can be used at most once on a network", func() {
		network := e2e.GetEnv(tc).GetNetwork()

		tc.By("creating new node")
		node1 := e2e.AddEphemeralNode(tc, network, tmpnet.FlagsMap{})
		e2e.WaitForHealthy(tc, node1)

		tc.By("checking that the new node is connected to its peers")
		checkConnectedPeers(tc, network.Nodes, node1)

		tc.By("creating a second new node with the same staking keypair as the first new node")
		node1Flags := node1.Flags
		node2Flags := tmpnet.FlagsMap{
			config.StakingTLSKeyContentKey: node1Flags[config.StakingTLSKeyContentKey],
			config.StakingCertContentKey:   node1Flags[config.StakingCertContentKey],
			// Construct a unique data dir to ensure the two nodes' data will be stored
			// separately. Usually the dir name is the node ID but in this one case the nodes have
			// the same node ID.
			config.DataDirKey: fmt.Sprintf("%s-second", node1Flags[config.DataDirKey]),
		}
		node2 := e2e.AddEphemeralNode(tc, network, node2Flags)

		tc.By("checking that the second new node fails to become healthy before timeout")
		err := tmpnet.WaitForHealthy(tc.DefaultContext(), node2)
		require.ErrorIs(err, context.DeadlineExceeded)

		tc.By("stopping the first new node")
		require.NoError(node1.Stop(tc.DefaultContext()))

		tc.By("checking that the second new node becomes healthy within timeout")
		e2e.WaitForHealthy(tc, node2)

		tc.By("checking that the second new node is connected to its peers")
		checkConnectedPeers(tc, network.Nodes, node2)

		// A bootstrap check was already performed by the second node.
	})
})

// Check that a new node is connected to existing nodes and vice versa
func checkConnectedPeers(tc tests.TestContext, existingNodes []*tmpnet.Node, newNode *tmpnet.Node) {
	require := require.New(tc)

	// Collect the node ids of the new node's peers
	infoClient := info.NewClient(newNode.URI)
	peers, err := infoClient.Peers(tc.DefaultContext())
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
		peers, err := infoClient.Peers(tc.DefaultContext())
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
