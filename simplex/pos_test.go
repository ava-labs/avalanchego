// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

func TestPoS(t *testing.T) {
	weights := map[int]uint64{
		1: 2,
		2: 2,
		3: 5,
		4: 1,
	}

	nodes := []common.NodeID{{1}, {2}, {3}, {4}}

	posSigAggregatorCreator := func(_ []common.Node) common.SignatureAggregator {
		return &testutil.TestSignatureAggregator{
			IsQuorumFunc: func(signatures []common.NodeID) bool {
				var totalWeight uint64
				for _, signer := range signatures {
					totalWeight += weights[int(signer[0])]
				}
				return totalWeight > 6
			},
			AboveByzantineThresholdFunc: func(signatures []common.NodeID) bool {
				var totalWeight uint64
				for _, signer := range signatures {
					totalWeight += weights[int(signer[0])]
				}
				return totalWeight > 3
			},
		}
	}
	testConf := &testutil.TestNodeConfig{SigAggregatorCreator: posSigAggregatorCreator, ReplicationEnabled: true}

	net := testutil.NewControlledNetwork(t, nodes)
	testutil.NewControlledSimplexNode(t, nodes[0], net, testConf)
	testutil.NewControlledSimplexNode(t, nodes[1], net, testConf)
	testutil.NewControlledSimplexNode(t, nodes[2], net, testConf)
	testutil.NewControlledSimplexNode(t, nodes[3], net, testConf)

	net.StartInstances()
	defer net.StopInstances()

	// Totally order 4 blocks, each proposed by a different node

	for seq := uint64(0); seq < 5; seq++ {
		net.TriggerLeaderBlockBuilder(seq)
		for _, n := range net.Instances {
			n.Storage.WaitForBlockCommit(seq)
		}
	}

	for i := range net.Instances {
		require.Equal(t, uint64(5), net.Instances[i].E.Metadata().Round)
	}

	// Next, disconnect the second node which has 20% of the stake
	net.Disconnect(nodes[1])

	net.AdvanceWithoutLeader(5, nodes[1])

	// Re-connect it and ensure it catches up
	net.Connect(nodes[1])

	net.TriggerLeaderBlockBuilder(6)
	for i, n := range net.Instances {
		if i == 1 {
			continue
		}
		n.Storage.WaitForBlockCommit(5)
	}

	net.Instances[1].Storage.WaitForBlockCommit(5)

	net.TriggerLeaderBlockBuilder(7)
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(6)
	}

	net.TriggerLeaderBlockBuilder(8)
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(7)
	}

	// Ensure the leader is the second node
	require.Equal(t, nodes[1], simplex.LeaderForRound(nodes, 9))

	// Take down the first node and the last node, leave only the second and third node.
	net.Disconnect(nodes[0])
	net.Disconnect(nodes[3])

	// At this point the second node is blacklisted, so we skip this round.
	testutil.WaitToEnterRound(t, net.Instances[1].E, 10)
	testutil.WaitToEnterRound(t, net.Instances[2].E, 10)

	net.TriggerLeaderBlockBuilder(10)
	for _, n := range net.Instances {
		if bytes.Equal(n.E.ID, nodes[0]) || bytes.Equal(n.E.ID, nodes[3]) {
			continue
		}
		n.Storage.WaitForBlockCommit(8)
	}

	// Bring up the two nodes back.
	net.Connect(nodes[0])
	net.Connect(nodes[3])

	// Have the nodes advance to round 14 where the leader is 2.

	for net.Instances[1].E.Metadata().Round != 14 && net.Instances[2].E.Metadata().Round != 14 {
		for _, n := range net.Instances {
			if bytes.Equal(n.E.ID, nodes[0]) || bytes.Equal(n.E.ID, nodes[3]) {
				continue
			}
			n.BlockShouldBeBuilt()
			n.AdvanceTime(n.E.MaxProposalWait / 4)
		}

		time.Sleep(time.Millisecond * 100)
	}

	net.TriggerLeaderBlockBuilder(14)
	for _, n := range net.Instances {
		if bytes.Equal(n.E.ID, nodes[0]) || bytes.Equal(n.E.ID, nodes[3]) {
			continue
		}
		n.Storage.WaitForBlockCommit(9)
	}

	// Ensure all nodes including those that were offline, now caught up with the latest round.
	for _, n := range net.Instances {
		testutil.WaitToEnterRound(t, n.E, 15)
	}

	// Now, disconnect the node with the highest stake (node 3) and observe the network is stuck
	net.Disconnect(nodes[2])
	net.TriggerLeaderBlockBuilder(15)

	timedOut := make(map[int]struct{})

	for len(timedOut) < 3 {
		for i, n := range net.Instances {
			if bytes.Equal(n.E.ID, nodes[2]) {
				continue
			}
			n.BlockShouldBeBuilt()
			n.AdvanceTime(n.E.MaxProposalWait / 4)
			if n.WAL.ContainsEmptyVote(15) {
				timedOut[i] = struct{}{}
			}
		}
		time.Sleep(time.Millisecond * 100)
	}

	for _, n := range net.Instances {
		require.False(t, n.WAL.ContainsEmptyNotarization(15))
	}

}
