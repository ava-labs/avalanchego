// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/graft/simplex/simplex"
	"github.com/ava-labs/avalanchego/graft/simplex/testutil"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestSimplexRebroadcastFinalizationVotes(t *testing.T) {
	nodes := []common.NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}}
	net := testutil.NewControlledNetwork(t, nodes)

	var allowFinalizeVotes atomic.Bool

	var numFinalizeVotesSent atomic.Uint32

	config := func(from common.NodeID) *testutil.TestNodeConfig {
		return &testutil.TestNodeConfig{
			Comm: testutil.NewTestComm(from, net.BasicInMemoryNetwork, func(msg *common.Message, from common.NodeID, to common.NodeID) bool {
				if msg.Finalization != nil && !allowFinalizeVotes.Load() {
					return false
				}
				if allowFinalizeVotes.Load() && msg.FinalizeVote != nil {
					numFinalizeVotesSent.Add(1)
				}
				return allowFinalizeVotes.Load() || msg.FinalizeVote == nil
			}),
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, config(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, config(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, config(nodes[2]))
	testutil.NewControlledSimplexNode(t, nodes[3], net, config(nodes[3]))

	net.StartInstances()
	defer net.StopInstances()

	lastSeq := uint64(9)

	for seq := uint64(0); seq <= lastSeq; seq++ {
		for _, n := range net.Instances {
			testutil.WaitToEnterRound(t, n.E, seq)
		}
		net.TriggerLeaderBlockBuilder(seq)
		for _, n := range net.Instances {
			n.WAL.AssertNotarization(seq)
		}
	}

	var wg sync.WaitGroup
	wg.Add(len(net.Instances))

	for _, n := range net.Instances {
		go func(n *testutil.ControlledNode) {
			defer wg.Done()
			n.Storage.EnsureNoBlockCommit(t, 0)
		}(n)
	}

	wg.Wait()

	allowFinalizeVotes.Store(true)

	require.Eventually(t, func() bool {
		allHaveFinalized := true
		for _, n := range net.Instances {
			if n.Storage.NumBlocks() < lastSeq+1 {
				allHaveFinalized = false
				break
			}
		}

		if allHaveFinalized {
			return true
		}

		for _, n := range net.Instances {
			n.AdvanceTime(simplex.DefaultFinalizeVoteRebroadcastTimeout / 3)
		}

		return false

	}, time.Second*10, time.Millisecond*100)

	// Close the recorded messages channel. A message sent to this channel will cause a panic.
	finalizeVoteSentCount := numFinalizeVotesSent.Load()

	// Advance the time to make sure we do not continue to send finalize votes.
	for _, n := range net.Instances {
		n.AdvanceTime(simplex.DefaultFinalizeVoteRebroadcastTimeout * 2)
		n.AdvanceTime(simplex.DefaultFinalizeVoteRebroadcastTimeout * 2)
		n.AdvanceTime(simplex.DefaultFinalizeVoteRebroadcastTimeout * 2)
	}

	require.Equal(t, finalizeVoteSentCount, numFinalizeVotesSent.Load(), "no more finalize votes should have been sent")

	// Next, we run the nodes and notarize 100 blocks, and ensure that less than 400 finalize votes were sent.
	previousVoteSentCount := finalizeVoteSentCount // We copy the value just to give it a better name.
	for seq := lastSeq + 1; seq < lastSeq+101; seq++ {
		for _, n := range net.Instances {
			testutil.WaitToEnterRound(t, n.E, seq)
		}
		net.TriggerLeaderBlockBuilder(seq)
		for _, n := range net.Instances {
			n.Storage.WaitForBlockCommit(seq)
			n.AdvanceTime(simplex.DefaultFinalizeVoteRebroadcastTimeout)
		}
	}

	require.LessOrEqual(t, previousVoteSentCount+400, numFinalizeVotesSent.Load())
}

func TestSimplexMultiNodeSimple(t *testing.T) {
	nodes := []common.NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}}
	net := testutil.NewControlledNetwork(t, nodes)
	testutil.NewControlledSimplexNode(t, nodes[0], net, nil)
	testutil.NewControlledSimplexNode(t, nodes[1], net, nil)
	testutil.NewControlledSimplexNode(t, nodes[2], net, nil)
	testutil.NewControlledSimplexNode(t, nodes[3], net, nil)

	net.StartInstances()
	defer net.StopInstances()

	for seq := uint64(0); seq < 10; seq++ {
		net.TriggerLeaderBlockBuilder(seq)
		for _, n := range net.Instances {
			n.Storage.WaitForBlockCommit(seq)
		}
	}
}

func dontVoteFilter(msg *common.Message, _, _ common.NodeID) bool {
	if msg.VoteMessage != nil || msg.EmptyVoteMessage != nil || msg.Notarization != nil || msg.FinalizeVote != nil {
		return false
	}
	return true
}

func TestSimplexMultiNodeBlacklist(t *testing.T) {
	nodes := []common.NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}}
	net := testutil.NewControlledNetwork(t, nodes)
	testEpochConfig := &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	}
	testutil.NewControlledSimplexNode(t, nodes[0], net, testEpochConfig)
	testutil.NewControlledSimplexNode(t, nodes[1], net, testEpochConfig)
	testutil.NewControlledSimplexNode(t, nodes[2], net, testEpochConfig)
	testutil.NewControlledSimplexNode(t, nodes[3], net, testEpochConfig)

	net.StartInstances()
	defer net.StopInstances()

	// Advance to the fourth node's turn by building three blocks
	for seq := 0; seq < 3; seq++ {
		net.TriggerLeaderBlockBuilder(uint64(seq))
		for _, n := range net.Instances {
			n.Storage.WaitForBlockCommit(uint64(seq))
		}
	}

	// The fourth node is disconnected, so the rest should time out on it.
	net.Disconnect(nodes[3])

	for i := range net.Instances[:3] {
		net.Instances[i].BlockShouldBeBuilt()
	}

	for _, n := range net.Instances[:3] {
		testutil.WaitForBlockProposerTimeout(t, n.E, &n.E.StartTime, 3)
	}

	// Build two more blocks, which should blacklist the fourth node.
	// Ensure the fourth node is blacklisted by checking the blacklist on all nodes.
	timedOut := map[uint16]uint64{3: 3}
	emptyBlacklist := common.NewBlacklist(simplex.DefaultMaxBlacklistedRounds)
	accused := emptyBlacklist.Next(common.NodeIDs(nodes).EqualWeightedNodes(), 4, simplex.DefaultMaxBlacklistedRounds, timedOut, nil)
	blacklisted := accused.Next(common.NodeIDs(nodes).EqualWeightedNodes(), 5, simplex.DefaultMaxBlacklistedRounds, timedOut, nil)

	// Build a block, ensure the blacklist contains the fourth node as a suspect.
	net.TriggerLeaderBlockBuilder(uint64(4))
	for _, n := range net.Instances[:3] {
		block := n.Storage.WaitForBlockCommit(uint64(3))
		blacklist := block.Blacklist()
		require.True(t, accused.Equals(&blacklist))
	}

	// Reconnect the fourth node but don't let it un-blacklist itself prematurely.
	net.SetNodeMessageFilter(nodes[3], dontVoteFilter)
	net.Connect(nodes[3])

	// Build another block, ensure the blacklist contains the fourth node as blacklisted.
	net.Instances[1].TriggerNewBlock()
	for _, n := range net.Instances[:3] {
		block := n.Storage.WaitForBlockCommit(uint64(4))
		blacklist := block.Blacklist()
		require.True(t, blacklisted.Equals(&blacklist))
	}

	// Wait for the node to replicate the missing blocks.
	net.Instances[3].TriggerNewBlock()
	block := net.Instances[3].Storage.WaitForBlockCommit(4)
	blacklist := block.Blacklist()
	require.True(t, blacklisted.Equals(&blacklist))

	// Make another block.
	net.Instances[2].TriggerNewBlock()
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(uint64(5))
	}

	// The fourth node is still blacklisted, so it should not be able to propose a block.
	net.Instances[3].TriggerNewBlock() // This shouldn't be here, this is just to side-step a bug.
	for _, n := range net.Instances {
		testutil.WaitForBlockProposerTimeout(t, n.E, &n.E.StartTime, 7)
	}

	// Disconnect the third node to force messages from the fourth node to be taken into account.
	net.Disconnect(nodes[2])
	net.SetNodeMessageFilter(nodes[3], testutil.AllowAllMessages)

	// Make two blocks.
	allButThirdNode := []*testutil.ControlledNode{net.Instances[0], net.Instances[1], net.Instances[3]}
	for i := 0; i < 2; i++ {
		net.Instances[i].TriggerNewBlock()
		for _, n := range allButThirdNode {
			n.BlockShouldBeBuilt()
			n.Storage.WaitForBlockCommit(uint64(6 + i))
		}
	}

	// Skip the third node because it is disconnected.
	for i := range allButThirdNode {
		net.Instances[i].BlockShouldBeBuilt()
	}

	for _, n := range allButThirdNode {
		testutil.WaitForBlockProposerTimeout(t, n.E, &n.E.StartTime, 10)
	}

	// Since the fourth node is still blacklisted, we should skip this round.
	for _, n := range allButThirdNode {
		testutil.WaitForBlockProposerTimeout(t, n.E, &n.E.StartTime, 11)
	}

	var lastBlacklist common.Blacklist

	// Create two blocks
	for i := 0; i < 2; i++ {
		net.Instances[i].TriggerNewBlock()
		for _, n := range allButThirdNode {
			n.BlockShouldBeBuilt()
			block := n.Storage.WaitForBlockCommit(uint64(8 + i))
			lastBlacklist = block.Blacklist()
		}
	}

	// The last node is not suspected anymore, it has been redeemed.
	require.False(t, lastBlacklist.IsNodeSuspected(3, uint64(len(nodes))))

	// The third node will now time out.
	for i := range allButThirdNode {
		net.Instances[i].BlockShouldBeBuilt()
	}

	for _, n := range allButThirdNode {
		testutil.WaitForBlockProposerTimeout(t, n.E, &n.E.StartTime, 14)
	}

	// The fourth node should now be able to propose a block.
	net.Instances[3].TriggerNewBlock()
	for _, n := range allButThirdNode {
		n.BlockShouldBeBuilt()
		block := n.Storage.WaitForBlockCommit(uint64(10))
		lastBlacklist = block.Blacklist()
	}
}

func onlySendBlockProposalsAndVotes(splitNodes []common.NodeID) testutil.MessageFilter {
	return func(m *common.Message, from common.NodeID, to common.NodeID) bool {
		if m.BlockMessage != nil {
			return true
		}
		for _, splitNode := range splitNodes {
			if to.Equals(splitNode) {
				return false
			}
		}
		return true
	}
}

// TestSplitVotes ensures that nodes who have timeout out, while the rest of the network has
// progressed due to notarizations, are able to collect the notarizations and continue
func TestSplitVotes(t *testing.T) {
	nodes := []common.NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}}
	net := testutil.NewControlledNetwork(t, nodes)

	config := func(from common.NodeID) *testutil.TestNodeConfig {
		return &testutil.TestNodeConfig{
			Comm: testutil.NewTestComm(from, net.BasicInMemoryNetwork, onlySendBlockProposalsAndVotes(nodes[2:])),
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, config(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, config(nodes[1]))
	splitNode2 := testutil.NewControlledSimplexNode(t, nodes[2], net, config(nodes[2]))
	splitNode3 := testutil.NewControlledSimplexNode(t, nodes[3], net, config(nodes[3]))

	net.StartInstances()
	defer net.StopInstances()

	net.TriggerLeaderBlockBuilder(0)
	for _, n := range net.Instances {
		n.WAL.AssertBlockProposal(0)
		n.BlockShouldBeBuilt()

		if n.E.ID.Equals(splitNode2.E.ID) || n.E.ID.Equals(splitNode3.E.ID) {
			require.Equal(t, uint64(0), n.E.Metadata().Round)
			testutil.WaitForBlockProposerTimeout(t, n.E, &n.E.StartTime, 0)
			require.False(t, n.WAL.ContainsNotarization(0))
		} else {
			n.WAL.AssertNotarization(0)
			require.Equal(t, uint64(1), n.E.Metadata().Round)
		}
	}

	// allow outstanding messages to be dropped
	time.Sleep(100 * time.Millisecond)

	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)

	time2 := splitNode2.E.StartTime
	time3 := splitNode3.E.StartTime

	for {
		time2 = time2.Add(splitNode2.E.MaxRebroadcastWait / 3)
		splitNode2.E.AdvanceTime(time2)

		time3 = time3.Add(splitNode3.E.MaxRebroadcastWait / 3)
		splitNode3.E.AdvanceTime(time3)
		if splitNode2.WAL.ContainsNotarization(0) && splitNode3.WAL.ContainsNotarization(0) {
			break
		}
	}

	// splitNode3 will receive the notarization from splitNode2
	splitNode2.WAL.AssertNotarization(0)
	splitNode3.WAL.AssertNotarization(0)

	for _, n := range net.Instances {
		require.Equal(t, uint64(0), n.Storage.NumBlocks(), fmt.Sprintf("node %s should not have", n.E.ID))
		require.Equal(t, uint64(1), n.E.Metadata().Round)
		require.Equal(t, uint64(1), n.E.Metadata().Seq)
	}

	// once the new round gets finalized, it will re-broadcast
	// all past notarizations allowing the nodes to index both seq 0 & 1
	net.TriggerLeaderBlockBuilder(1)

	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(0)
		n.Storage.WaitForBlockCommit(1)
		require.Equal(t, uint64(2), n.Storage.NumBlocks())
		require.Equal(t, uint64(2), n.E.Metadata().Round)
		require.Equal(t, uint64(2), n.E.Metadata().Seq)
	}
}

// denyFinalizationMessages blocks any messages that would cause nodes in
// a network to index a block in storage.
func denyFinalizationMessages(msg *common.Message, _, _ common.NodeID) bool {
	if msg.FinalizeVote != nil {
		return false
	}
	if msg.Finalization != nil {
		return false
	}

	return true
}

func onlyAllowEmptyRoundMessages(msg *common.Message, _, _ common.NodeID) bool {
	if msg.EmptyNotarization != nil {
		return true
	}
	if msg.EmptyVoteMessage != nil {
		return true
	}
	return false
}

// TestSimplexMultiNodeBlacklistExpires tests that a node that goes offline is blacklisted, and that it is
// removed from the blacklist by all nodes once it has been blacklisted for more than MaxBlacklistedOrbits,
// even though it is still offline and therefore never redeemed itself.
func TestSimplexMultiNodeBlacklistExpires(t *testing.T) {
	nodes := []common.NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}}
	net := testutil.NewControlledNetwork(t, nodes)

	// A node may remain blacklisted for a single orbit.
	const maxBlacklistedRounds = 4

	// The chain starts from GenesisMaxBlacklistedRounds, and only moves towards the local configuration
	// by a single orbit per block, so the nodes start from a finalized block of round 0 whose blacklist
	// already has the configured max blacklisted orbits.
	genesis, ok := testutil.NewTestBlockBuilder().BuildBlock(context.Background(), common.ProtocolMetadata{},
		common.NewBlacklist(maxBlacklistedRounds))
	require.True(t, ok)
	genesisFinalization, _ := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{N: len(nodes)}, genesis, nodes)

	testEpochConfig := &testutil.TestNodeConfig{
		ReplicationEnabled:   true,
		MaxBlacklistedRounds: maxBlacklistedRounds,
		InitialStorage:       []common.VerifiedFinalizedBlock{{VerifiedBlock: genesis, Finalization: genesisFinalization}},
	}
	for _, node := range nodes {
		testutil.NewControlledSimplexNode(t, node, net, testEpochConfig)
	}

	net.StartInstances()
	defer net.StopInstances()

	const offlineNodeIndex = 3
	onlineNodes := net.Instances[:offlineNodeIndex]

	// buildBlock lets the leader of the given round build the block with the given sequence,
	// waits for the online nodes to commit it, and returns its blacklist.
	buildBlock := func(round, seq uint64) common.Blacklist {
		net.TriggerLeaderBlockBuilder(round)
		var blacklist common.Blacklist
		for _, n := range onlineNodes {
			block := n.Storage.WaitForBlockCommit(seq)
			require.Equal(t, round, block.BlockHeader().Round)
			blacklist = block.Blacklist()
		}
		return blacklist
	}

	// timeoutOnOfflineLeader lets the online nodes time out on the offline leader of the given round.
	timeoutOnOfflineLeader := func(round uint64) {
		require.Equal(t, nodes[offlineNodeIndex], simplex.LeaderForRound(nodes, round))
		for _, n := range onlineNodes {
			testutil.WaitToEnterRound(t, n.E, round)
			n.BlockShouldBeBuilt()
		}
		for _, n := range onlineNodes {
			testutil.WaitForBlockProposerTimeout(t, n.E, &n.E.StartTime, round)
		}
	}

	// skipBlacklistedLeader asserts that the online nodes do not wait for the blacklisted leader
	// of the given round to propose a block. Time is not advanced, so the round can only be
	// empty notarized if the nodes immediately vote for an empty block.
	skipBlacklistedLeader := func(round uint64) {
		require.Equal(t, nodes[offlineNodeIndex], simplex.LeaderForRound(nodes, round))
		for _, n := range onlineNodes {
			testutil.WaitToEnterRound(t, n.E, round)
			n.BlockShouldBeBuilt()
		}
		for _, n := range onlineNodes {
			require.Eventually(t, func() bool {
				return n.WAL.ContainsEmptyVote(round) || n.WAL.ContainsEmptyNotarization(round)
			}, time.Minute, 10*time.Millisecond, "node %s waited for the blacklisted leader of round %d", n.E.ID, round)
		}
	}

	// Rounds 1-2: ordinary blocks proposed by nodes 1 and 2, on top of the finalized block of round 0.
	for seq := uint64(1); seq < 3; seq++ {
		buildBlock(seq, seq)
	}

	// The last node goes offlineNodeIndex for the rest of the test.
	net.Disconnect(nodes[offlineNodeIndex])

	// Round 3: the online nodes time out on the offlineNodeIndex node.
	timeoutOnOfflineLeader(3)

	// The blacklists the online nodes are expected to build, as the offline node timed out in round 3.
	emptyBlacklist := common.NewBlacklist(testEpochConfig.MaxBlacklistedRounds)
	timedOut := map[uint16]uint64{offlineNodeIndex: 3}
	accused := emptyBlacklist.Next(common.NodeIDs(nodes).EqualWeightedNodes(), 4, testEpochConfig.MaxBlacklistedRounds, timedOut, nil)
	blacklisted := accused.Next(common.NodeIDs(nodes).EqualWeightedNodes(), 5, testEpochConfig.MaxBlacklistedRounds, timedOut, nil)

	// Round 4: node 0 accuses the offline node of timing out in its orbit 1.
	blacklist := buildBlock(4, 3)
	require.False(t, blacklist.IsNodeSuspected(offlineNodeIndex, uint64(len(nodes))))
	require.True(t, accused.Equals(&blacklist), "expected %s, got %s", accused.String(), blacklist.String())

	// Round 5: node 1 accuses the offline node as well, which is then blacklisted with f+1 accusations in its orbit 1.
	blacklist = buildBlock(5, 4)
	require.True(t, blacklist.IsNodeSuspected(offlineNodeIndex, uint64(len(nodes))))
	require.True(t, blacklisted.Equals(&blacklist), "expected %s, got %s", blacklisted.String(), blacklist.String())

	// Round 6: node 2 carries over the blacklist.
	blacklist = buildBlock(6, 5)
	require.True(t, blacklist.IsNodeSuspected(offlineNodeIndex, uint64(len(nodes))))

	// Round 7: the offline node's turn, in its orbit 2. It is blacklisted, so it is skipped.
	// It has now been blacklisted for a single orbit, but the blacklist is only garbage collected
	// when a block is built, so it is still skipped in this round.
	skipBlacklistedLeader(7)

	// Round 8: node 0 builds a block, which garbage collects the offline node, although it never redeemed itself.
	blacklist = buildBlock(8, 6)
	require.False(t, blacklist.IsNodeSuspected(offlineNodeIndex, uint64(len(nodes))))
	require.True(t, emptyBlacklist.Equals(&blacklist), "expected an empty blacklist, got %s", blacklist.String())

	// Rounds 9 and 10: nodes 1 and 2 carry over the empty blacklist.
	for round, seq := uint64(9), uint64(7); round <= 10; round, seq = round+1, seq+1 {
		blacklist = buildBlock(round, seq)
		require.True(t, emptyBlacklist.Equals(&blacklist), "round %d: expected an empty blacklist, got %s", round, blacklist.String())
	}

	// Round 11: the offline node's turn. It is no longer blacklisted, so the online nodes wait for its block
	// rather than skip it, and only vote for an empty block once they time out on it.
	for _, n := range onlineNodes {
		testutil.WaitToEnterRound(t, n.E, 11)
		n.BlockShouldBeBuilt()
	}

	// Since we don't advance the time, the online nodes should not vote for an empty block just yet.
	// This ensures that the offline node is no longer blacklisted, and that the online nodes are waiting for it to propose a block.
	require.Never(t, func() bool {
		for _, n := range onlineNodes {
			if n.WAL.ContainsEmptyVote(11) {
				return true
			}
		}
		return false
	}, 500*time.Millisecond, 10*time.Millisecond, "the online nodes skipped the offlineNodeIndex node although it is no longer blacklisted")

	// Now we can timeout on the offline node and finish the test.
	timeoutOnOfflineLeader(11)
}
