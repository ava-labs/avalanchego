// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/graft/simplex/simplex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/ava-labs/avalanchego/graft/simplex/testutil"
)

// TestReplication tests the replication process of a node that
// is behind the rest of the network by less than maxRoundWindow.
func TestBasicReplication(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}

	for i := range 3 * simplex.DefaultMaxRoundWindow {
		testName := fmt.Sprintf("Basic replication_of_%d_blocks", i)

		// lagging node cannot be the leader after node disconnects
		isLaggingNodeLeader := bytes.Equal(simplex.LeaderForRound(nodes, uint64(i)), nodes[3])
		if isLaggingNodeLeader {
			continue
		}

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			testReplication(t, uint64(i), nodes)
		})
	}
}

func testReplication(t *testing.T, startSeq uint64, nodes []common.NodeID) {
	net := testutil.NewControlledNetwork(t, nodes)

	// initiate a network with 4 nodes. one node is behind by startSeq blocks
	storageData := createBlocks(t, nodes, startSeq)
	testEpochConfig := &testutil.TestNodeConfig{
		InitialStorage:     storageData,
		ReplicationEnabled: true,
	}
	normalNode1 := testutil.NewControlledSimplexNode(t, nodes[0], net, testEpochConfig)
	normalNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, testEpochConfig)
	normalNode3 := testutil.NewControlledSimplexNode(t, nodes[2], net, testEpochConfig)
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	})

	require.Equal(t, startSeq, normalNode1.Storage.NumBlocks())
	require.Equal(t, startSeq, normalNode2.Storage.NumBlocks())
	require.Equal(t, startSeq, normalNode3.Storage.NumBlocks())
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	net.StartInstances()
	defer net.StopInstances()
	net.TriggerLeaderBlockBuilder(startSeq)

	// all blocks except the lagging node start at round startSeq, seq startSeq.
	// lagging node starts at round 0, seq 0.
	// this asserts that the lagging node catches up to the latest round
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(startSeq)
	}
}

// TestReplicationAdversarialNode tests the replication process of a node that
// has been sent a different block by one node, however the rest of the network
// notarizes a different block for the same round
func TestReplicationAdversarialNode(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}
	quorum := common.Quorum(len(nodes))
	net := testutil.NewControlledNetwork(t, nodes)

	testEpochConfig := &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	}

	// doubleBlockProposalNode will propose two blocks for the same round
	doubleBlockProposalNode := testutil.NewControlledSimplexNode(t, nodes[0], net, testEpochConfig)
	normalNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, testEpochConfig)
	normalNode3 := testutil.NewControlledSimplexNode(t, nodes[2], net, testEpochConfig)
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	})

	require.Equal(t, uint64(0), doubleBlockProposalNode.Storage.NumBlocks())
	require.Equal(t, uint64(0), normalNode2.Storage.NumBlocks())
	require.Equal(t, uint64(0), normalNode3.Storage.NumBlocks())
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	net.StartInstances()
	defer net.StopInstances()
	doubleBlock := testutil.NewTestBlock(doubleBlockProposalNode.E.Metadata(), emptyBlacklist)
	doubleBlockVote, err := testutil.NewTestVote(doubleBlock, doubleBlockProposalNode.E.ID)
	require.NoError(t, err)
	msg := &common.Message{
		BlockMessage: &common.BlockMessage{
			Block: doubleBlock,
			Vote:  *doubleBlockVote,
		},
	}

	require.NoError(t, laggingNode.E.HandleMessage(msg, doubleBlockProposalNode.E.ID))
	net.Disconnect(laggingNode.E.ID)

	blocks := []common.VerifiedBlock{}
	for i := uint64(0); i < 2; i++ {
		net.TriggerLeaderBlockBuilder(i)

		for j, n := range net.Instances[:3] {
			committed := n.Storage.WaitForBlockCommit(i)
			if j == 0 {
				blocks = append(blocks, committed)
			}
		}
	}

	// lagging node should not have committed the block
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	require.Equal(t, uint64(0), laggingNode.E.Metadata().Round)
	net.Connect(laggingNode.E.ID)

	sigAggr := laggingNode.E.SignatureAggregatorCreator(laggingNode.E.Comm.Validators())
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, blocks[1], nodes[:quorum])
	finalizationMsg := &common.Message{
		Finalization: &finalization,
	}
	require.NoError(t, laggingNode.E.HandleMessage(finalizationMsg, doubleBlockProposalNode.E.ID))

	for i := range 2 {
		lagBlock := laggingNode.Storage.WaitForBlockCommit(uint64(i))
		require.Equal(t, blocks[i], lagBlock)
	}
}

// TestRebroadcastingWithReplication verifies that after network recovery,
// a lagging node and the rest of the network correctly propagate missing
// finalizations and index all blocks.
func TestRebroadcastingWithReplication(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	net := testutil.NewControlledNetwork(t, nodes)

	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, testutil.AllowAllMessages)
		return &testutil.TestNodeConfig{
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))
	// we do not expect the lagging node to build any blocks
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, newNodeConfig(nodes[3]))

	for _, n := range net.Instances {
		require.Equal(t, uint64(0), n.Storage.NumBlocks())
	}

	net.StartInstances()
	defer net.StopInstances()

	net.Disconnect(laggingNode.E.ID)
	numNotarizations := uint64(9)
	missedSeqs := uint64(0)

	// finalization for the first block
	net.TriggerLeaderBlockBuilder(0)
	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			continue
		}
		n.Storage.WaitForBlockCommit(0)
	}

	net.SetAllNodesMessageFilter(denyFinalizationMessages)

	// normal nodes continue to make progress
	for i := uint64(1); i < numNotarizations; i++ {
		emptyRound := bytes.Equal(simplex.LeaderForRound(nodes, i), laggingNode.E.ID)
		if emptyRound {
			net.AdvanceWithoutLeader(i, laggingNode.E.ID)
			missedSeqs++
		} else {
			net.TriggerLeaderBlockBuilder(i)
			for _, n := range net.Instances {
				if n.E.ID.Equals(laggingNode.E.ID) {
					continue
				}
				n.WAL.AssertNotarization(i)
			}
		}
	}

	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			require.Equal(t, uint64(0), n.Storage.NumBlocks())
			require.Equal(t, uint64(0), n.E.Metadata().Round)
			continue
		}

		// assert metadata
		require.Equal(t, numNotarizations, n.E.Metadata().Round)
		require.Equal(t, uint64(1), n.E.Storage.NumBlocks())
	}

	// the lagging node has been asleep, it should be notified blocks are available
	laggingNode.BlockShouldBeBuilt()

	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	net.Connect(laggingNode.E.ID)
	net.TriggerLeaderBlockBuilder(numNotarizations)

	timeout := time.NewTimer(30 * time.Second)
	expectedSeq := numNotarizations - missedSeqs
	for i := uint64(0); i <= expectedSeq; i++ {
		for _, n := range net.Instances {
			for {
				committed := n.Storage.NumBlocks()
				if committed > i {
					break
				}

				// if we haven't indexed, advance the time to trigger rebroadcast/replication timeouts
				select {
				case <-time.After(time.Millisecond * 10):
					for _, n := range net.Instances {
						n.AdvanceTime(2 * simplex.DefaultMaxProposalWaitTime)
					}
					continue
				case <-timeout.C:
					require.Fail(t, "timed out waiting for event")
				}
			}
		}
	}

	for _, n := range net.Instances {
		require.Equal(t, expectedSeq+1, n.Storage.NumBlocks())
	}
}

// TestReplicationEmptyNotarizations ensures a lagging node will properly replicate
// many empty notarizations in a row.
// This test sometimes takes > 30 sec
func TestReplicationEmptyNotarizations(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, {4}, {5}, {6}}

	for endRound := uint64(2); endRound <= 2*simplex.DefaultMaxRoundWindow; endRound++ {
		isLaggingNodeLeader := bytes.Equal(simplex.LeaderForRound(nodes, endRound), nodes[5])
		if isLaggingNodeLeader {
			continue
		}

		testName := fmt.Sprintf("Empty_notarizations_end_round%d", endRound)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			testReplicationEmptyNotarizations(t, nodes, endRound)
		})
	}
}

func testReplicationEmptyNotarizations(t *testing.T, nodes []common.NodeID, endRound uint64) {
	net := testutil.NewControlledNetwork(t, nodes)
	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, testutil.AllowAllMessages)
		return &testutil.TestNodeConfig{
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))
	testutil.NewControlledSimplexNode(t, nodes[3], net, newNodeConfig(nodes[3]))
	testutil.NewControlledSimplexNode(t, nodes[4], net, newNodeConfig(nodes[4]))
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[5], net, newNodeConfig(nodes[5]))

	net.StartInstances()
	defer net.StopInstances()

	net.Disconnect(laggingNode.E.ID)

	net.TriggerLeaderBlockBuilder(0)
	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			continue
		}
		n.Storage.WaitForBlockCommit(0)
	}

	net.SetAllNodesMessageFilter(onlyAllowEmptyRoundMessages)

	// normal nodes continue to make progress
	for i := uint64(1); i < endRound; i++ {
		leader := simplex.LeaderForRound(nodes, i)
		if !leader.Equals(laggingNode.E.ID) {
			net.TriggerLeaderBlockBuilder(i)
		}

		net.AdvanceWithoutLeader(i, laggingNode.E.ID)
	}

	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			require.Equal(t, uint64(0), n.Storage.NumBlocks())
			require.Equal(t, uint64(0), n.E.Metadata().Round)
			continue
		}

		// assert metadata
		require.Equal(t, uint64(endRound), n.E.Metadata().Round)
		require.Equal(t, uint64(1), n.E.Metadata().Seq)
		require.Equal(t, uint64(1), n.E.Storage.NumBlocks())
	}

	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	net.Connect(laggingNode.E.ID)
	net.TriggerLeaderBlockBuilder(endRound)
	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			// maybe lagging node has requested finalizations to a node without it, we may need to resend the request
			for n.Storage.NumBlocks() != 2 {
				time.Sleep(10 * time.Millisecond)
				n.AdvanceTime(2 * simplex.DefaultMaxProposalWaitTime)
			}
			continue
		}
		n.Storage.WaitForBlockCommit(1)
	}

	require.Equal(t, uint64(2), laggingNode.Storage.NumBlocks())
	require.Equal(t, uint64(endRound+1), laggingNode.E.Metadata().Round)
	require.Equal(t, uint64(2), laggingNode.E.Metadata().Seq)
}

// TestReplicationStartsBeforeCurrentRound tests the replication process of a node that
// starts replicating in the middle of the current round.
func TestReplicationStartsBeforeCurrentRound(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}
	quorum := common.Quorum(len(nodes))
	net := testutil.NewControlledNetwork(t, nodes)
	startSeq := uint64(simplex.DefaultMaxRoundWindow + 3)
	storageData := createBlocks(t, nodes, startSeq)
	testEpochConfig := &testutil.TestNodeConfig{
		InitialStorage:     storageData,
		ReplicationEnabled: true,
	}
	normalNode1 := testutil.NewControlledSimplexNode(t, nodes[0], net, testEpochConfig)
	normalNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, testEpochConfig)
	normalNode3 := testutil.NewControlledSimplexNode(t, nodes[2], net, testEpochConfig)
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	})

	firstBlock := storageData[0].VerifiedBlock

	fBytes := firstBlock.Bytes()
	record, err := common.BlockRecord(firstBlock.BlockHeader(), fBytes)
	require.NoError(t, err)
	require.NoError(t, laggingNode.WAL.Append(record))

	sigAggr := laggingNode.E.SignatureAggregatorCreator(laggingNode.E.Comm.Validators())
	firstNotarizationRecord, err := testutil.NewNotarizationRecord(laggingNode.E.Logger, sigAggr, firstBlock, nodes[0:quorum])
	require.NoError(t, err)
	require.NoError(t, laggingNode.WAL.Append(firstNotarizationRecord))

	secondBlock := storageData[1].VerifiedBlock
	sBytes := secondBlock.Bytes()
	record, err = common.BlockRecord(secondBlock.BlockHeader(), sBytes)
	require.NoError(t, err)
	require.NoError(t, laggingNode.WAL.Append(record))

	secondNotarizationRecord, err := testutil.NewNotarizationRecord(laggingNode.E.Logger, sigAggr, secondBlock, nodes[0:quorum])
	require.NoError(t, err)
	require.NoError(t, laggingNode.WAL.Append(secondNotarizationRecord))

	require.Equal(t, startSeq, normalNode1.Storage.NumBlocks())
	require.Equal(t, startSeq, normalNode2.Storage.NumBlocks())
	require.Equal(t, startSeq, normalNode3.Storage.NumBlocks())
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	// Start the lagging node alone so that no messages from the other nodes advance it past its restored round.
	laggingNode.Start()
	require.Equal(t, uint64(2), laggingNode.E.Metadata().Round)

	normalNode3.Start()
	normalNode2.Start()
	normalNode1.Start()
	defer net.StopInstances()

	net.TriggerLeaderBlockBuilder(startSeq)
	for i := uint64(0); i <= startSeq; i++ {
		for _, n := range net.Instances {
			n.Storage.WaitForBlockCommit(startSeq)
		}
	}
}

func TestReplicationFutureFinalization(t *testing.T) {
	// send a block, then simultaneously send a finalization for the block
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	quorum := common.Quorum(len(nodes))

	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	require.Equal(t, md.Round, md.Seq)

	block := bb.GetBuiltBlock()
	block.VerificationDelay = make(chan struct{}) // add a delay to the block verification

	vote, err := testutil.NewTestVote(block, nodes[0])
	require.NoError(t, err)

	err = e.HandleMessage(&common.Message{
		BlockMessage: &common.BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, nodes[0])
	require.NoError(t, err)

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes[0:quorum])
	// send finalization
	err = e.HandleMessage(&common.Message{
		Finalization: &finalization,
	}, nodes[0])
	require.NoError(t, err)

	block.VerificationDelay <- struct{}{} // unblock the block verification

	storedBlock := storage.WaitForBlockCommit(0)
	require.Equal(t, uint64(1), storage.NumBlocks())
	require.Equal(t, block, storedBlock)
}

// TestReplicationAfterNodeDisconnects tests the replication process of a node that
// disconnects from the network and reconnects after the rest of the network has made progress.
//
// All nodes make progress for `startDisconnect` blocks. The lagging node disconnects
// and the rest of the nodes continue to make progress for another `endDisconnect - startDisconnect` blocks.
// The lagging node reconnects and the after the next `finalization` is sent, the lagging node catches up to the latest height.
func TestReplicationAfterNodeDisconnects(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}

	for startDisconnect := uint64(0); startDisconnect <= 5; startDisconnect++ {
		for endDisconnect := uint64(10); endDisconnect <= 20; endDisconnect++ {
			// lagging node cannot be the leader after node disconnects
			isLaggingNodeLeader := bytes.Equal(simplex.LeaderForRound(nodes, endDisconnect), nodes[3])
			if isLaggingNodeLeader {
				continue
			}

			testName := fmt.Sprintf("Disconnect_%d_to_%d", startDisconnect, endDisconnect)

			t.Run(testName, func(t *testing.T) {
				t.Parallel()
				testReplicationAfterNodeDisconnects(t, nodes, startDisconnect, endDisconnect)
			})
		}
	}
}

func testReplicationAfterNodeDisconnects(t *testing.T, nodes []common.NodeID, startDisconnect, endDisconnect uint64) {
	net := testutil.NewControlledNetwork(t, nodes)
	testConfig := &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	}
	normalNode1 := testutil.NewControlledSimplexNode(t, nodes[0], net, testConfig)
	normalNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, testConfig)
	normalNode3 := testutil.NewControlledSimplexNode(t, nodes[2], net, testConfig)
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, testConfig)

	require.Equal(t, uint64(0), normalNode1.Storage.NumBlocks())
	require.Equal(t, uint64(0), normalNode2.Storage.NumBlocks())
	require.Equal(t, uint64(0), normalNode3.Storage.NumBlocks())
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	net.StartInstances()
	defer net.StopInstances()

	for i := uint64(0); i < startDisconnect; i++ {
		net.TriggerLeaderBlockBuilder(i)
		for _, n := range net.Instances {
			n.Storage.WaitForBlockCommit(i)
		}
	}

	// all nodes have committed `startDisconnect` blocks
	for _, n := range net.Instances {
		require.Equal(t, startDisconnect, n.Storage.NumBlocks())
	}

	// lagging node disconnects
	net.Disconnect(laggingNode.E.ID)
	isLaggingNodeLeader := bytes.Equal(simplex.LeaderForRound(nodes, startDisconnect), laggingNode.E.ID)
	if isLaggingNodeLeader {
		net.TriggerLeaderBlockBuilder(startDisconnect)
	}

	missedSeqs := uint64(0)
	// normal nodes continue to make progress
	for i := startDisconnect; i < endDisconnect; i++ {
		emptyRound := bytes.Equal(simplex.LeaderForRound(nodes, i), nodes[3])
		if emptyRound {
			net.AdvanceWithoutLeader(i, laggingNode.E.ID)
			missedSeqs++
		} else {
			net.TriggerLeaderBlockBuilder(i)
			for _, n := range net.Instances[:3] {
				n.Storage.WaitForBlockCommit(i - missedSeqs)
			}
		}
	}

	// all nodes except for lagging node have progressed and committed [endDisconnect - missedSeqs] blocks
	for _, n := range net.Instances[:3] {
		require.Equal(t, endDisconnect-missedSeqs, n.Storage.NumBlocks())
	}
	require.Equal(t, startDisconnect, laggingNode.Storage.NumBlocks())
	require.Equal(t, startDisconnect, laggingNode.E.Metadata().Round)
	// lagging node reconnects
	net.Connect(laggingNode.E.ID)
	net.TriggerLeaderBlockBuilder(endDisconnect)

	var blacklist common.Blacklist
	for _, n := range net.Instances {
		block := n.Storage.WaitForBlockCommit(endDisconnect - missedSeqs)
		blacklist = block.Blacklist()
	}

	for _, n := range net.Instances {
		require.Equal(t, endDisconnect-missedSeqs, n.Storage.NumBlocks()-1)
	}

	if blacklist.IsNodeSuspected(3, uint64(len(nodes))) {
		t.Log("lagging node is blacklisted, cannot continue replication")
		return
	}

	// the lagging node should build a block when triggered if its the leader
	net.TriggerLeaderBlockBuilder(endDisconnect + 1)
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(endDisconnect - missedSeqs + 1)
	}
}

// sendVotesToOneNode allows block messages to be sent to all nodes, and only
// passes vote messages to one node. This will allows that node to notarize the block,
// while the other blocks will timeout
func sendVotesToOneNode(filteredInNode common.NodeID) testutil.MessageFilter {
	return func(msg *common.Message, _, to common.NodeID) bool {
		if msg.VerifiedBlockMessage != nil || msg.BlockMessage != nil {
			return true
		}

		if msg.VoteMessage != nil {
			// this is the lagging node
			if to.Equals(filteredInNode) {
				return true
			}
		}

		return false
	}
}

func TestReplicationStuckInProposingBlock(t *testing.T) {
	var aboutToBuildBlock sync.WaitGroup
	aboutToBuildBlock.Add(2)

	tbb := testutil.NewTestBlockBuilder()
	bb := testutil.NewTestControlledBlockBuilder(t)
	bb.TestBlockBuilder = *tbb
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	blocks := createBlocks(t, nodes, 5)

	quorum := common.Quorum(len(nodes))
	sentMessages := make(chan *common.Message, 100)

	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], &recordingComm{
		Communication: testutil.NewNoopComm(nodes),
		SentMessages:  sentMessages,
	}, bb)

	conf.ReplicationEnabled = true
	l := conf.Logger.(*testutil.TestLogger)
	l.Intercept(func(entry zapcore.Entry) error {
		if strings.Contains(entry.Message, "Scheduling block building") {
			aboutToBuildBlock.Done()
		}
		return nil
	})

	e, err := simplex.NewEpoch(conf)
	e.ReplicationEnabled = true
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	bb.TriggerNewBlock()
	notarizeAndFinalizeRoundWithMetadata(t, e, &bb.TestBlockBuilder, &blocks[0].Finalization.Finalization.ProtocolMetadata)

	gb := storage.WaitForBlockCommit(0)
	require.Equal(t, gb, blocks[0].VerifiedBlock.(*testutil.TestBlock))

	highBlock, _ := blocks[3].VerifiedBlock.(*testutil.TestBlock)

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	highFinalization, _ := testutil.NewFinalizationRecord(t, sigAggr, highBlock, nodes[0:quorum])

	// Trigger the replication process to start by sending a finalization for a block we do not have
	require.NoError(t, e.HandleMessage(&common.Message{
		Finalization: &highFinalization,
	}, nodes[1]))

	// Wait for the replication request to be sent
	for {
		msg := <-sentMessages
		if msg.ReplicationRequest != nil {
			break
		}
	}

	// Drain the block builder channels
	for len(bb.BlockShouldBeBuilt) > 0 {
		select {
		case <-bb.BlockShouldBeBuilt:
		default:
		}
	}

	// Prepare the quorum round answer to be sent as a response to the replication request
	quorumRounds := make([]common.QuorumRound, 0, 4)
	for i := uint64(1); i <= 4; i++ {
		tb := blocks[i].VerifiedBlock.(*testutil.TestBlock)
		finalization := blocks[i].Finalization
		quorumRounds = append(quorumRounds, common.QuorumRound{
			Block:        tb,
			Finalization: &finalization,
		})
	}

	// Respond to the replication request with a block that has a notarization
	replicationResponse := &common.ReplicationResponse{
		LatestRound: &quorumRounds[2],
		Data:        quorumRounds[:3],
	}

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[1]))

	// Wait for the second block to be attempted to be built
	aboutToBuildBlock.Wait()

	// Trigger the replication process to start by sending a finalization for a block we do not have
	require.NoError(t, e.HandleMessage(&common.Message{
		Finalization: &blocks[4].Finalization,
	}, nodes[1]))

	// Wait for the replication request to be sent
	for {
		msg := <-sentMessages
		if msg.ReplicationRequest != nil {
			break
		}
	}

	replicationResponse = &common.ReplicationResponse{
		LatestRound: &quorumRounds[3],
		Data:        quorumRounds[3:],
	}

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[1]))

	storage.WaitForBlockCommit(4)
}

// TestReplicationNodeDiverges tests that a node replicates blocks even if they
// have a stale notarization for a round(i.e. a node notarized a block but the rest of the network
// propagated an empty notarization).
func TestReplicationNodeDiverges(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")

	nodes := []common.NodeID{{1}, {2}, {3}, {4}, {5}, {6}}
	numBlocks := uint64(5)

	net := testutil.NewControlledNetwork(t, nodes)

	nodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, sendVotesToOneNode(nodes[3]))
		return &testutil.TestNodeConfig{
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, nodeConfig(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, nodeConfig(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, nodeConfig(nodes[2]))
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, nodeConfig(nodes[3]))

	// we need at least 6 nodes since the lagging node & leader will not timeout
	testutil.NewControlledSimplexNode(t, nodes[4], net, nodeConfig(nodes[4]))
	testutil.NewControlledSimplexNode(t, nodes[5], net, nodeConfig(nodes[5]))

	net.StartInstances()
	defer net.StopInstances()
	net.TriggerLeaderBlockBuilder(0)

	// because of the message filter, the lagging one will be the only one to notarize the block
	laggingNode.WAL.AssertNotarization(0)
	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			continue
		}
		require.Equal(t, false, n.WAL.ContainsNotarization(0))
	}

	// we disconnect lagging node first so that it doesn't send the notarized block to any other nodes
	net.Disconnect(laggingNode.E.ID)
	net.SetAllNodesMessageFilter(
		// block sending votes from round 0 to ensure all nodes will timeout
		func(msg *common.Message, _, to common.NodeID) bool {
			return msg.VoteMessage == nil || msg.VoteMessage.Vote.Round != 0
		},
	)

	// This function call ensures all nodes will timeout, and
	// receive an empty notarization for round 0(except for lagging).
	net.AdvanceWithoutLeader(0, laggingNode.E.ID)

	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			require.Equal(t, uint64(1), n.E.Metadata().Round)
			require.Equal(t, uint64(1), n.E.Metadata().Seq)
			continue
		}

		require.Equal(t, uint64(0), n.E.Metadata().Seq)
		require.Equal(t, uint64(1), n.E.Metadata().Round)
	}

	// advance [numBlocks] while the lagging node is disconnected
	missedSeqs := uint64(1) // missed the first seq
	for i := uint64(1); i < 1+numBlocks; i++ {
		emptyRound := bytes.Equal(simplex.LeaderForRound(nodes, i), laggingNode.E.ID)
		if emptyRound {
			net.AdvanceWithoutLeader(i, laggingNode.E.ID)
			missedSeqs++
		} else {
			net.TriggerLeaderBlockBuilder(i)
			for _, n := range net.Instances {
				if n.E.ID.Equals(laggingNode.E.ID) {
					continue
				}
				n.Storage.WaitForBlockCommit(i - missedSeqs)
			}
		}
	}

	net.Connect(laggingNode.E.ID)

	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			continue
		}
		testutil.WaitToEnterRound(t, n.E, numBlocks+1)
	}

	// we are in round 6(which means node 1 should be leader(but it is blacklisted))
	net.AdvanceWithoutLeader(1+numBlocks, laggingNode.E.ID)

	net.TriggerLeaderBlockBuilder(numBlocks + 2)
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(numBlocks - missedSeqs + 1)
	}
	assertEqualLedgers(t, net)
}

func assertEqualLedgers(t *testing.T, net *testutil.ControlledInMemoryNetwork) {
	expectedLedger := map[uint64][]byte{}

	for seq := range net.Instances[0].Storage.NumBlocks() {
		block, _, err := net.Instances[0].Storage.Retrieve(seq)
		require.NoError(t, err)
		bytes := block.Bytes()
		expectedLedger[seq] = bytes

	}

	for _, n := range net.Instances {
		actualLedger := map[uint64][]byte{}

		for seq := range n.Storage.NumBlocks() {
			block, _, err := n.Storage.Retrieve(seq)
			require.NoError(t, err)
			bytes := block.Bytes()
			actualLedger[seq] = bytes
		}
		require.Equal(t, expectedLedger, actualLedger)
	}
}

func TestReplicationNotarizationWithoutFinalizations(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}

	for numBlocks := uint64(1); numBlocks <= 3*simplex.DefaultMaxRoundWindow; numBlocks++ {
		// lagging node cannot be the leader after node disconnects
		isLaggingNodeLeader := bytes.Equal(simplex.LeaderForRound(nodes, numBlocks), nodes[3])
		if isLaggingNodeLeader {
			continue
		}

		testName := fmt.Sprintf("NotarizationWithoutFinalization_%d_blocks", numBlocks)

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			testReplicationNotarizationWithoutFinalizations(t, numBlocks, nodes)
		})
	}
}

// TestReplicationNotarizationWithoutFinalizations tests that a lagging node will replicate
// blocks that have notarizations but no finalizations.
func testReplicationNotarizationWithoutFinalizations(t *testing.T, numBlocks uint64, nodes []common.NodeID) {
	net := testutil.NewControlledNetwork(t, nodes)

	onlyAllowBlockProposalsAndNotarizations := func(msg *common.Message, _, to common.NodeID) bool {
		if to.Equals(nodes[3]) {
			return (msg.BlockMessage != nil || msg.VerifiedBlockMessage != nil || msg.Notarization != nil)
		}

		return true
	}

	nodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, onlyAllowBlockProposalsAndNotarizations)
		return &testutil.TestNodeConfig{
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, nodeConfig(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, nodeConfig(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, nodeConfig(nodes[2]))

	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, nodeConfig(nodes[3]))

	for _, n := range net.Instances {
		require.Equal(t, uint64(0), n.Storage.NumBlocks())
	}

	net.StartInstances()
	defer net.StopInstances()

	// normal nodes continue to make progress
	for i := uint64(0); i < uint64(numBlocks); i++ {
		net.TriggerLeaderBlockBuilder(i)
		for _, n := range net.Instances[:3] {
			n.Storage.WaitForBlockCommit(uint64(i))
		}
	}

	laggingNode.WAL.AssertNotarization(numBlocks - 1)
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	require.Equal(t, uint64(numBlocks), laggingNode.E.Metadata().Round)

	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	net.TriggerLeaderBlockBuilder(numBlocks)
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(uint64(numBlocks))
	}
}

func createBlocks(t *testing.T, nodes []common.NodeID, seqCount uint64) []common.VerifiedFinalizedBlock {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	data := make([]common.VerifiedFinalizedBlock, 0, seqCount)
	var prev common.Digest
	for i := uint64(0); i < seqCount; i++ {
		protocolMetadata := common.ProtocolMetadata{
			Seq:   i,
			Round: i,
			Prev:  prev,
		}

		block, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
		require.True(t, ok)
		prev = block.BlockHeader().Digest
		finalization, _ := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{N: len(nodes)}, block, nodes)
		data = append(data, common.VerifiedFinalizedBlock{
			VerifiedBlock: block,
			Finalization:  finalization,
		})
	}
	return data
}

func TestReplicationVerifyNotarization(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}

	// This function takes a QC and makes it that it is signed by only 2 out of 4 nodes,
	// while still having a quorum of signatures.
	corruptQC := func(qc common.QuorumCertificate) common.QuorumCertificate {
		badQC := qc.(testutil.TestQC)
		// Duplicate the last signature
		badQC = append(badQC, badQC[len(badQC)-1])
		// Remove the first signature
		badQC = badQC[1:]

		// Finalization should have 3 signers
		require.Len(t, badQC.Signers(), 3)

		// But all these signers are either the second and third node.
		require.Contains(t, badQC.Signers(), nodes[1])
		require.Contains(t, badQC.Signers(), nodes[2])

		// Not the first or the fourth node.
		require.NotContains(t, badQC.Signers(), nodes[0])
		require.NotContains(t, badQC.Signers(), nodes[3])

		return badQC
	}

	quorum := common.Quorum(len(nodes))
	sentMessages := make(chan *common.Message, 100)

	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], &recordingComm{
		Communication: testutil.NewNoopComm(nodes),
		SentMessages:  sentMessages,
	}, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	require.Equal(t, md.Round, md.Seq)

	block := bb.GetBuiltBlock()

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes[0:quorum])

	// Trigger the replication process to start by sending a finalization for a block we do not have
	require.NoError(t, e.HandleMessage(&common.Message{
		Finalization: &finalization,
	}, nodes[0]))

	// Wait for the replication request to be sent
	for {
		msg := <-sentMessages
		if msg.ReplicationRequest != nil {
			break
		}
	}

	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes[0:quorum])
	require.NoError(t, err)

	// Corrupt the QC
	notarization.QC = corruptQC(notarization.QC)

	// Respond to the replication request with a block that has a notarization
	replicationResponse := &common.ReplicationResponse{
		Data: []common.QuorumRound{
			{
				Block:        block,
				Notarization: &notarization,
			},
		},
	}
	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[0]))

	require.Never(t, func() bool {
		return wal.ContainsNotarization(0)
	}, time.Millisecond*500, time.Millisecond*10, "Did not expect block with a corrupt QC to be written to the WAL")
}

func TestReplicationVerifyEmptyNotarization(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	nodes := []common.NodeID{{1}, {2}, {3}, {4}}

	// This function takes a QC and makes it that it is signed by only 2 out of 4 nodes,
	// while still having a quorum of signatures.
	corruptQC := func(qc common.QuorumCertificate) common.QuorumCertificate {
		badQC := qc.(testutil.TestQC)
		// Duplicate the last signature
		badQC = append(badQC, badQC[len(badQC)-1])
		// Remove the first signature
		badQC = badQC[1:]

		// Finalization should have 3 signers
		require.Len(t, badQC.Signers(), 3)

		// But all these signers are either the second and third node.
		require.Contains(t, badQC.Signers(), nodes[1])
		require.Contains(t, badQC.Signers(), nodes[2])

		// Not the first or the fourth node.
		require.NotContains(t, badQC.Signers(), nodes[0])
		require.NotContains(t, badQC.Signers(), nodes[3])

		return badQC
	}

	quorum := common.Quorum(len(nodes))
	sentMessages := make(chan *common.Message, 100)
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], &recordingComm{
		Communication: testutil.NewNoopComm(nodes),
		SentMessages:  sentMessages,
	}, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	require.Equal(t, md.Round, md.Seq)

	block := bb.GetBuiltBlock()

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes[0:quorum])

	// Trigger the replication process to start by sending a finalization for a block we do not have
	require.NoError(t, e.HandleMessage(&common.Message{
		Finalization: &finalization,
	}, nodes[0]))

	// Wait for the replication request to be sent
	for {
		msg := <-sentMessages
		if msg.ReplicationRequest != nil {
			break
		}
	}

	emptyNotarization := testutil.NewEmptyNotarization(nodes[0:quorum], 0)

	// Corrupt the QC
	emptyNotarization.QC = corruptQC(emptyNotarization.QC)

	// Respond to the replication request with a block that has a notarization
	replicationResponse := &common.ReplicationResponse{
		Data: []common.QuorumRound{
			{
				EmptyNotarization: emptyNotarization,
			},
		},
	}
	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[0]))

	require.Never(t, func() bool {
		return wal.ContainsEmptyNotarization(0)
	}, time.Millisecond*500, time.Millisecond*10, "Did not expect an empty notarization with a corrupt QC to be written to the WAL")
}

// TestReplicationVotesForNotarizations tests that a lagging node will replicate
// finalizations and notarizations. It ensures the node sends finalized votes for rounds
// without finalizations.
func TestReplicationVotesForNotarizations(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}

	numFinalizedBlocks := uint64(5)
	// number of notarized blocks after the finalized blocks
	numNotarizedBlocks := uint64(11)
	net := testutil.NewControlledNetwork(t, nodes)

	storageData := createBlocks(t, nodes, numFinalizedBlocks)

	// almostFinalizeBlocks is a message filter that allows all messages except for finalized votes
	// and finalizations, unless the message is from node 1. This way each node will have 2 finalized votes,
	// which is one short from quorum.
	almostFinalizeBlocks := func(msg *common.Message, from, _ common.NodeID) bool {
		// block finalized votes and finalizations
		if msg.Finalization != nil || msg.FinalizeVote != nil {
			return from.Equals(common.NodeID{1})
		}
		return true
	}

	nodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, almostFinalizeBlocks)
		return &testutil.TestNodeConfig{
			InitialStorage:     storageData,
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	n1 := testutil.NewControlledSimplexNode(t, nodes[0], net, nodeConfig(nodes[0]))
	n2 := testutil.NewControlledSimplexNode(t, nodes[1], net, nodeConfig(nodes[1]))
	adversary := testutil.NewControlledSimplexNode(t, nodes[2], net, nodeConfig(nodes[2]))
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	})

	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			require.Equal(t, uint64(0), n.Storage.NumBlocks())
			continue
		}
		require.Equal(t, numFinalizedBlocks, n.Storage.NumBlocks())
	}

	// lagging node should be disconnected while nodes create notarizations without finalizations
	net.Disconnect(laggingNode.E.ID)

	net.StartInstances()
	defer net.StopInstances()

	missedSeqs := uint64(0)
	// normal nodes continue to make progress
	for round := numFinalizedBlocks; round < numFinalizedBlocks+numNotarizedBlocks; round++ {
		emptyRound := bytes.Equal(simplex.LeaderForRound(nodes, round), laggingNode.E.ID)
		if emptyRound {
			missedSeqs++
			net.AdvanceWithoutLeader(round, laggingNode.E.ID)
		} else {
			net.TriggerLeaderBlockBuilder(round)
			for _, n := range net.Instances {
				if n.E.ID.Equals(laggingNode.E.ID) {
					continue
				}
				n.WAL.AssertNotarization(round)
			}
		}
	}

	// all nodes should be on round [numFinalizedBlocks + numNotarizedBlocks - 1]
	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			require.Equal(t, uint64(0), n.Storage.NumBlocks())
			require.Equal(t, uint64(0), n.E.Metadata().Round)
			continue
		}
		require.Equal(t, numFinalizedBlocks, n.Storage.NumBlocks())
		require.Equal(t, numFinalizedBlocks+numNotarizedBlocks, n.E.Metadata().Round)
	}

	// at this point in time, the adversarial node will disconnect
	// since each node has sent 2 finalized votes, which is one short of a quorum
	// the lagging node will need to replicate the finalizations, and then send votes for notarizations
	net.Disconnect(adversary.E.ID)
	net.Connect(laggingNode.E.ID)
	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)

	// the adversary should not be the leader(to simplify test)
	isAdversaryLeader := bytes.Equal(simplex.LeaderForRound(nodes, numFinalizedBlocks+numNotarizedBlocks), adversary.E.ID)
	require.False(t, isAdversaryLeader)

	// lagging node should not be leader
	isLaggingNodeLeader := bytes.Equal(simplex.LeaderForRound(nodes, numFinalizedBlocks+numNotarizedBlocks), laggingNode.E.ID)
	require.False(t, isLaggingNodeLeader)

	// trigger block building, but we only have 2 connected nodes so the nodes will time out
	net.TriggerLeaderBlockBuilder(numFinalizedBlocks + numNotarizedBlocks)

	// ensure time out on required nodes
	n1.TimeoutOnRound(numFinalizedBlocks + numNotarizedBlocks)
	n2.TimeoutOnRound(numFinalizedBlocks + numNotarizedBlocks)
	require.Equal(t, uint64(0), laggingNode.E.Metadata().Round)

	// when the lagging node times out, it will broadcast an empty vote. The other online nodes will reply with their latest tip which should kickstart replication
	laggingNode.TimeoutOnRound(0)

	expectedNumBlocks := numFinalizedBlocks + numNotarizedBlocks - missedSeqs
	// because the adversarial node is offline, we may need to send replication requests many times
	for {
		time.Sleep(time.Millisecond * 100)
		if laggingNode.Storage.NumBlocks() == expectedNumBlocks {
			break
		}

		laggingNode.AdvanceTime(simplex.DefaultReplicationRequestTimeout)
	}

	for _, n := range net.Instances {
		if n.E.ID.Equals(adversary.E.ID) {
			continue
		}
		n.Storage.WaitForBlockCommit(expectedNumBlocks - 1) // subtract -1 because seq starts at 0
	}

	laggingNode.TimeoutOnRound(numFinalizedBlocks + numNotarizedBlocks)
	for _, n := range net.Instances {
		if n.E.ID.Equals(adversary.E.ID) {
			continue
		}
		testutil.WaitToEnterRound(t, n.E, numFinalizedBlocks+numNotarizedBlocks+1)
		require.True(t, n.WAL.ContainsEmptyNotarization(numFinalizedBlocks+numNotarizedBlocks))
	}
}

// TestReplicationEmptyNotarizations ensures a lagging node will properly replicate
// a tail of empty notarizations.
func TestReplicationEmptyNotarizationsTail(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, {4}, {5}, {6}}

	for endRound := uint64(2); endRound <= 2*simplex.DefaultMaxRoundWindow; endRound++ {
		isLaggingNodeLeader := bytes.Equal(simplex.LeaderForRound(nodes, endRound), nodes[5])
		if isLaggingNodeLeader {
			continue
		}

		testName := fmt.Sprintf("Empty_notarizations_end_round%d", endRound)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			testReplicationEmptyNotarizationsTail(t, nodes, endRound)
		})
	}
}

func testReplicationEmptyNotarizationsTail(t *testing.T, nodes []common.NodeID, endRound uint64) {
	net := testutil.NewControlledNetwork(t, nodes)
	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, testutil.AllowAllMessages)
		return &testutil.TestNodeConfig{
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))
	testutil.NewControlledSimplexNode(t, nodes[3], net, newNodeConfig(nodes[3]))
	testutil.NewControlledSimplexNode(t, nodes[4], net, newNodeConfig(nodes[4]))
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[5], net, newNodeConfig(nodes[5]))

	net.StartInstances()
	defer net.StopInstances()

	net.Disconnect(laggingNode.E.ID)
	net.SetAllNodesMessageFilter(onlyAllowEmptyRoundMessages)

	// normal nodes continue to make progress
	for i := uint64(0); i < endRound; i++ {
		leader := simplex.LeaderForRound(nodes, i)
		if !leader.Equals(laggingNode.E.ID) {
			net.TriggerLeaderBlockBuilder(i)
		}

		net.AdvanceWithoutLeader(i, laggingNode.E.ID)
	}

	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			require.Equal(t, uint64(0), n.Storage.NumBlocks())
			require.Equal(t, uint64(0), n.E.Metadata().Round)
			continue
		}

		// assert metadata
		require.Equal(t, uint64(endRound), n.E.Metadata().Round)
		require.Equal(t, uint64(0), n.E.Metadata().Seq)
		require.Equal(t, uint64(0), n.E.Storage.NumBlocks())
	}

	net.Connect(laggingNode.E.ID)
	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)

	// have the lagging node timeout to trigger replication
	laggingNode.E.AdvanceTime(time.Now().Add(laggingNode.E.MaxProposalWait))

	for _, n := range net.Instances {
		testutil.WaitToEnterRound(t, n.E, endRound)
		require.Equal(t, uint64(endRound), n.E.Metadata().Round)
	}
}

func sendEmptyNotarizationQuorumRounds(emptyNotarizations map[uint64]*common.EmptyNotarization) testutil.MessageFilter {
	return func(msg *common.Message, from, to common.NodeID) bool {
		if msg.VerifiedReplicationResponse != nil {
			newData := make([]common.VerifiedQuorumRound, 0, len(msg.VerifiedReplicationResponse.Data))
			for _, qr := range msg.VerifiedReplicationResponse.Data {
				newQR := common.VerifiedQuorumRound{
					EmptyNotarization: qr.EmptyNotarization,
				}
				if qr.EmptyNotarization == nil {
					newQR.EmptyNotarization = emptyNotarizations[qr.GetRound()]
				}
				newData = append(newData, newQR)
			}
			msg.VerifiedReplicationResponse.Data = newData
		}

		return allowFinalizeVotes(msg, from, to)
	}
}

func allowFinalizeVotes(msg *common.Message, from, to common.NodeID) bool {
	if msg.Finalization != nil || msg.FinalizeVote != nil {
		if to.Equals(common.NodeID{3}) || from.Equals(common.NodeID{3}) {
			return false
		}
	}
	return true
}

// TestReplicationStoresFinalization checks finalizations are stored in the rounds
// map when processing.
func TestReplicationStoresFinalization(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	sentMessages := make(chan *common.Message, 100)

	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[3], &recordingComm{
		Communication: testutil.NewNoopComm(nodes),
		SentMessages:  sentMessages,
	}, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// createBlocks 2 blocks with finalizations
	blocks := createBlocks(t, nodes, 2)
	vote, err := testutil.NewTestVote(blocks[0].VerifiedBlock.(common.Block), nodes[0])
	require.NoError(t, err)
	// send block 1
	require.NoError(t, e.HandleMessage(&common.Message{
		BlockMessage: &common.BlockMessage{
			Block: blocks[0].VerifiedBlock.(common.Block),
			Vote:  *vote,
		},
	}, nodes[0]))

	// Send a finalization for block 2 to trigger replication
	finalization := blocks[1].Finalization
	require.NoError(t, e.HandleMessage(&common.Message{
		Finalization: &finalization,
	}, nodes[1]))

	// Wait for the replication request to be sent
	for {
		msg := <-sentMessages
		if msg.ReplicationRequest != nil {
			break
		}
	}

	// pass in replication response to epoch with both blocks and their finalizations
	quorumRounds := []common.QuorumRound{
		{
			Block:        blocks[0].VerifiedBlock.(common.Block),
			Finalization: &blocks[0].Finalization,
		},
		{
			Block:        blocks[1].VerifiedBlock.(common.Block),
			Finalization: &blocks[1].Finalization,
		},
	}

	replicationResponse := &common.ReplicationResponse{
		Data: quorumRounds,
	}

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[1]))

	// Verify both blocks are committed
	for i := range 2 {
		committed := storage.WaitForBlockCommit(uint64(i))
		require.Equal(t, blocks[i].VerifiedBlock, committed)
	}

	require.Equal(t, uint64(2), storage.NumBlocks())
}

// TestReplicationChain tests that a node can both empty notarizations and notarizations for the same round.
func TestReplicationChain(t *testing.T) {
	// Digest message requests are needed for this test
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	net := testutil.NewControlledNetwork(t, nodes)

	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, allowFinalizeVotes)
		return &testutil.TestNodeConfig{
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	// full nodes operate normally
	fullNode1 := testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	fullNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	fullNode1.Silence()
	fullNode2.Silence()
	// node 3 will not receive finalize votes & finalizations
	blockFinalize3 := testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))
	blockFinalize3.Silence()
	// lagging node is disconnected initially. It initially receives only empty notarizations
	// but then later receives notarizations and must send finalize votes for them
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, newNodeConfig(nodes[3]))
	net.StartInstances()
	defer net.StopInstances()
	net.Disconnect(laggingNode.E.ID)

	emptyNotarizations := make(map[uint64]*common.EmptyNotarization)
	numNotarizations := uint64(8)
	missedNotarizations := uint64(0)
	for i := range numNotarizations {
		// every round has an empty notarization(possible due to timeouts)
		emptyNotarization := testutil.NewEmptyNotarization(nodes, i)
		emptyNotarizations[i] = emptyNotarization

		leader := simplex.LeaderForRound(nodes, i)
		if !leader.Equals(laggingNode.E.ID) {
			net.TriggerLeaderBlockBuilder(i)
			continue
		}

		net.AdvanceWithoutLeader(i, laggingNode.E.ID)
		missedNotarizations++
	}

	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			require.Equal(t, uint64(0), n.Storage.NumBlocks())
			require.Equal(t, uint64(0), n.E.Metadata().Round)
			continue
		}

		n.WAL.AssertNotarization(numNotarizations - 1)
		// assert metadata
		require.Equal(t, numNotarizations, n.E.Metadata().Round)
		require.Equal(t, numNotarizations-missedNotarizations, n.E.Metadata().Seq)
		require.Equal(t, uint64(0), n.E.Storage.NumBlocks())
	}

	// lagging node should not be the leader after reconnect
	leader := simplex.LeaderForRound(nodes, numNotarizations)
	require.NotEqual(t, laggingNode.E.ID, leader)

	net.SetAllNodesMessageFilter(sendEmptyNotarizationQuorumRounds(emptyNotarizations))
	net.Connect(laggingNode.E.ID)
	net.TriggerLeaderBlockBuilder(numNotarizations)

	// now the lagging node should catch up on empty notarizations
	empty := laggingNode.WAL.AssertNotarization(numNotarizations - 1)
	require.Equal(t, empty, common.EmptyNotarizationRecordType)

	// seq should be 0, since filter gave empty notarizations
	require.Equal(t, numNotarizations, laggingNode.E.Metadata().Round)
	require.Equal(t, uint64(0), laggingNode.E.Metadata().Seq)

	// nodes send a block but can't notarize so they time out
	net.SetAllNodesMessageFilter(allowFinalizeVotes)

	for _, n := range net.Instances {
		// the message filter blocks finalizations for blockFinalize3
		if n.E.ID.Equals(blockFinalize3.E.ID) {
			continue
		}

		for {
			numBlocks := n.Storage.NumBlocks()
			if numBlocks >= numNotarizations-missedNotarizations+1 {
				break
			}
			net.AdvanceTime(simplex.DefaultReplicationRequestTimeout)

			// Allow time for messages to be processed
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Sanity check that blockFinalize3 can also catch up once the filter is removed
	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	for {
		numBlocks := blockFinalize3.Storage.NumBlocks()

		if numBlocks >= numNotarizations-missedNotarizations+1 {
			break
		}
		net.AdvanceTime(simplex.DefaultReplicationRequestTimeout)
	}
}

// TestReplicationStartsRoundFromFinalization tests that a replicating node will start monitoring a round after it
// replicates finalizations. It does so by ensuring the node produces an empty vote and completes an
// empty notarization after replicating.
func TestReplicationStartsRoundFromFinalization(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}

	sentMessages := make(chan *common.Message, 100)
	broadcastMessages := make(chan *common.Message, 100)
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], &recordingComm{
		Communication:     testutil.NewNoopComm(nodes),
		SentMessages:      sentMessages,
		BroadcastMessages: broadcastMessages,
	}, bb)
	conf.ReplicationEnabled = true
	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Create blocks that the node needs to replicate
	blocks := createBlocks(t, nodes, 3)

	// Send a finalization for a block we don't have to trigger replication
	lastFinalization := blocks[len(blocks)-1].Finalization

	require.NoError(t, e.HandleMessage(&common.Message{
		Finalization: &lastFinalization,
	}, nodes[1]))

	// Wait for the replication request to be sent
	for {
		msg := <-sentMessages
		if msg.ReplicationRequest != nil {
			break
		}
	}

	// Prepare replication response with all the blocks
	quorumRounds := make([]common.QuorumRound, 0, len(blocks))
	for _, vfb := range blocks {
		tb := vfb.VerifiedBlock.(*testutil.TestBlock)
		quorumRounds = append(quorumRounds, common.QuorumRound{
			Block:        tb,
			Finalization: &vfb.Finalization,
		})
	}

	// Send replication response
	replicationResponse := &common.ReplicationResponse{
		LatestRound: &quorumRounds[len(quorumRounds)-1],
		Data:        quorumRounds,
	}

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[1]))

	// Wait for all blocks to be committed
	for i := range blocks {
		storage.WaitForBlockCommit(uint64(i))
	}

	// Verify all blocks were replicated
	require.Equal(t, uint64(len(blocks)), storage.NumBlocks())
	require.Equal(t, uint64(len(blocks)), e.Metadata().Round)

	emptyBlockMd := common.EmptyVoteMetadata{
		Round: uint64(len(blocks)),
	}

	// the other two nodes send empty votes
	require.NoError(t, e.HandleMessage(&common.Message{
		EmptyVoteMessage: createEmptyVote(emptyBlockMd, nodes[1]),
	}, nodes[1]))

	require.NoError(t, e.HandleMessage(&common.Message{
		EmptyVoteMessage: createEmptyVote(emptyBlockMd, nodes[2]),
	}, nodes[2]))

	// Verify the node sent an empty vote after timeout
	var foundEmptyVote bool
	timeout := time.After(5 * time.Second)
	iteration := 1
messages:
	for {
		select {
		case msg := <-broadcastMessages:
			if msg.EmptyVoteMessage != nil {
				foundEmptyVote = true
				break messages
			}
		case <-timeout:
			break messages
		case <-time.After(10 * time.Millisecond):
			newTime := e.StartTime.Add(
				time.Duration(iteration) * simplex.DefaultEmptyVoteRebroadcastTimeout,
			)
			iteration++
			if len(bb.BlockShouldBeBuilt) == 0 {
				bb.BlockShouldBeBuilt <- struct{}{}
			}
			e.AdvanceTime(newTime)
		}
	}

	require.True(t, foundEmptyVote, "Node should send an empty vote after timeout following replication")
	notarization := wal.AssertNotarization(uint64(len(blocks)))
	require.Equal(t, common.EmptyNotarizationRecordType, notarization)
}

// TestReplicationStartsRoundFromFinalizationWithBlock is a similar test to TestReplicationStartsRoundFromFinalization
// except it checks the replication path when a block already exists in our rounds map for the finalized block.
func TestReplicationStartsRoundFromFinalizationWithBlock(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}

	sentMessages := make(chan *common.Message, 100)
	broadcastMessages := make(chan *common.Message, 100)
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], &recordingComm{
		Communication:     testutil.NewNoopComm(nodes),
		SentMessages:      sentMessages,
		BroadcastMessages: broadcastMessages,
	}, bb)
	conf.ReplicationEnabled = true
	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Create blocks that the node needs to replicate
	blocks := createBlocks(t, nodes, 3)

	// Send a finalization for a block we don't have to trigger replication
	lastBlock := blocks[len(blocks)-1].VerifiedBlock

	// send the block for the round before
	vote, err := testutil.NewTestVote(lastBlock, simplex.LeaderForRound(nodes, lastBlock.BlockHeader().Round))
	require.NoError(t, err)
	err = e.HandleMessage(&common.Message{
		BlockMessage: &common.BlockMessage{
			Vote:  *vote,
			Block: lastBlock.(common.Block),
		},
	}, simplex.LeaderForRound(nodes, lastBlock.BlockHeader().Round))
	require.NoError(t, err)

	// Prepare replication response with all the blocks
	quorumRounds := make([]common.QuorumRound, 0, len(blocks))
	for _, vfb := range blocks {
		tb := vfb.VerifiedBlock.(*testutil.TestBlock)
		quorumRounds = append(quorumRounds, common.QuorumRound{
			Block:        tb,
			Finalization: &vfb.Finalization,
		})
	}

	// Send replication response without last qr
	replicationResponse := &common.ReplicationResponse{
		Data: quorumRounds[0 : len(quorumRounds)-1],
	}

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[1]))

	// Wait for all blocks to be committed
	for i := range blocks[:len(blocks)-1] {
		storage.WaitForBlockCommit(uint64(i))
	}

	// wait for future message to be processed
	wal.AssertBlockProposal(lastBlock.BlockHeader().Round)

	// Send replication response
	replicationResponse = &common.ReplicationResponse{
		Data: []common.QuorumRound{quorumRounds[len(quorumRounds)-1]},
	}

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[1]))

	storage.WaitForBlockCommit(lastBlock.BlockHeader().Seq)

	// Verify all blocks were replicated
	require.Equal(t, uint64(len(blocks)), storage.NumBlocks())
	require.Equal(t, uint64(len(blocks)), e.Metadata().Round)

	emptyBlockMd := common.EmptyVoteMetadata{
		Round: uint64(len(blocks)),
	}

	// the other two nodes send empty votes
	require.NoError(t, e.HandleMessage(&common.Message{
		EmptyVoteMessage: createEmptyVote(emptyBlockMd, nodes[1]),
	}, nodes[1]))

	require.NoError(t, e.HandleMessage(&common.Message{
		EmptyVoteMessage: createEmptyVote(emptyBlockMd, nodes[2]),
	}, nodes[2]))

	// Verify the node sent an empty vote after timeout
	var foundEmptyVote bool
	timeout := time.After(5 * time.Second)
	iteration := 1
messages:
	for {
		select {
		case msg := <-broadcastMessages:
			if msg.EmptyVoteMessage != nil {
				foundEmptyVote = true
				break messages
			}
		case <-timeout:
			break messages
		case <-time.After(10 * time.Millisecond):
			newTime := e.StartTime.Add(
				time.Duration(iteration) * simplex.DefaultEmptyVoteRebroadcastTimeout,
			)
			iteration++
			if len(bb.BlockShouldBeBuilt) == 0 {
				bb.BlockShouldBeBuilt <- struct{}{}
			}
			e.AdvanceTime(newTime)
		}
	}

	require.True(t, foundEmptyVote, "Node should send an empty vote after timeout following replication")
	notarization := wal.AssertNotarization(uint64(len(blocks)))
	require.Equal(t, common.EmptyNotarizationRecordType, notarization)
}

// TestReplicationRequestsWithinMaxRoundWindow ensures that a node that observes a finalization more than MaxRoundWindow
// sequences ahead of its next sequence to commit only requests sequences up to MaxRoundWindow-1 ahead of it
func TestReplicationRequestsWithinMaxRoundWindow(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	sentMessages := make(chan *common.Message, 100)
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], &recordingComm{
		Communication: testutil.NewNoopComm(nodes),
		SentMessages:  sentMessages,
	}, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// observe a finalization more than MaxRoundWindow sequences ahead of us
	seqCount := 2 * conf.MaxRoundWindow
	finalization := createBlocks(t, nodes, seqCount)[seqCount-1].Finalization
	err = e.HandleMessage(&common.Message{Finalization: &finalization}, nodes[0])
	require.NoError(t, err)

	// collect the replication requests sent out in response to the finalization
	requested := make(map[uint64]struct{})
	for len(sentMessages) > 0 {
		msg := <-sentMessages
		if msg.ReplicationRequest == nil {
			continue
		}
		for _, seq := range msg.ReplicationRequest.Seqs {
			requested[seq] = struct{}{}
		}
	}

	// our next sequence to commit is 0, so we may request at most sequences [0, MaxRoundWindow-1]
	require.Len(t, requested, int(conf.MaxRoundWindow))
	for seq := range requested {
		require.Less(t, seq, conf.MaxRoundWindow,
			"requested a sequence more than MaxRoundWindow ahead of the next sequence to commit")
	}
}

func TestLeaderStartsRoundAfterReplicatedQuorumRound(t *testing.T) {
	// This test makes sure that a node that replicates a notarization or a finalization,
	// starts the next round.
	// The test works as follows:
	// The node in the test is the second node in the list of nodes.
	// The first node is the leader of round 0, and the second node is the leader of round 1.
	// The node receives a notarization/finalization for round 0, which it does not have the block for.
	// It will request the block and the notarization/finalization again, which should make it advance to round 1.
	// Since node is the leader of round 1, it should propose a block.
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}

	for _, testCase := range []struct {
		name string
		// quorumRound builds the round 0 quorum round (block + notarization or finalization) and the
		// bare message carrying only the notarization/finalization, which the node receives without the block.
		quorumRound func(t *testing.T, e *simplex.Epoch, block *testutil.TestBlock) (common.QuorumRound, *common.Message)
		// requestsMissingBlock reports whether msg is the request the node sends to fetch the missing block.
		requestsMissingBlock func(msg *common.Message, block *testutil.TestBlock) bool
		// commits is true if replicating the quorum round also commits block 0.
		commits bool
	}{
		{
			name: "notarization",
			quorumRound: func(t *testing.T, e *simplex.Epoch, block *testutil.TestBlock) (common.QuorumRound, *common.Message) {
				validators := e.Comm.Validators()
				quorum := common.Quorum(len(validators))
				sigAggr := e.SignatureAggregatorCreator(validators)
				notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, validators.NodeIDs()[:quorum])
				require.NoError(t, err)
				return common.QuorumRound{Block: block, Notarization: &notarization}, &common.Message{Notarization: &notarization}
			},
			// The notarization path asks for the missing block via a block digest request.
			requestsMissingBlock: func(msg *common.Message, block *testutil.TestBlock) bool {
				return msg.BlockDigestRequest != nil && msg.BlockDigestRequest.Digest == block.BlockHeader().Digest
			},
		},
		{
			name: "finalization",
			quorumRound: func(t *testing.T, e *simplex.Epoch, block *testutil.TestBlock) (common.QuorumRound, *common.Message) {
				validators := e.Comm.Validators()
				quorum := common.Quorum(len(validators))
				sigAggr := e.SignatureAggregatorCreator(validators)
				finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, validators.NodeIDs()[:quorum])
				return common.QuorumRound{Block: block, Finalization: &finalization}, &common.Message{Finalization: &finalization}
			},
			// The finalization path asks for the missing block through the replication path.
			requestsMissingBlock: func(msg *common.Message, block *testutil.TestBlock) bool {
				if msg.ReplicationRequest == nil {
					return false
				}
				for _, seq := range msg.ReplicationRequest.Seqs {
					if seq == block.BlockHeader().Seq {
						return true
					}
				}
				return false
			},
			// A finalization also commits block 0 as the node advances.
			commits: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// nodes[1] leads round 1 but not round 0 - it is the node we drive.
			require.NotEqual(t, nodes[1], simplex.LeaderForRound(nodes, 0))
			require.Equal(t, nodes[1], simplex.LeaderForRound(nodes, 1))

			recordingComm := &recordingComm{
				Communication:     testutil.NewNoopComm(nodes),
				BroadcastMessages: make(chan *common.Message, 100),
				SentMessages:      make(chan *common.Message, 100),
			}

			bb := testutil.NewTestBlockBuilder()
			conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], recordingComm, bb)
			conf.ReplicationEnabled = true

			e, err := simplex.NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)
			require.NoError(t, e.Start())

			// Build a notarized/finalized block B for round 0 (the round led by nodes[0]), which the
			// node under test never received directly.
			blockB := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
			quorumRound, bareMessage := testCase.quorumRound(t, e, blockB)

			// Send the node a notarization/finalization for round 0 without the block itself.
			// Since the node does not have block 0, it will start replication.
			require.NoError(t, e.HandleMessage(bareMessage, nodes[0]))

			// The node asks for the missing block for round 0.
			require.Eventually(t, func() bool {
				for {
					select {
					case msg := <-recordingComm.SentMessages:
						if testCase.requestsMissingBlock(msg, blockB) {
							return true
						}
					default:
						return false
					}
				}
			}, 5*time.Second, 10*time.Millisecond, "node did not request the missing block for round 0")

			// Answer the request with the notarized/finalized round 0 through the replication path,
			// delivering the block together with the notarization/finalization once more.
			require.NoError(t, e.HandleMessage(&common.Message{
				ReplicationResponse: &common.ReplicationResponse{
					Data: []common.QuorumRound{quorumRound},
				},
			}, nodes[0]))

			// The node advances to round 1 off the replicated notarization/finalization.
			require.Eventually(t, func() bool {
				return e.Metadata().Round == 1
			}, 5*time.Second, 10*time.Millisecond, "node did not advance to round 1")

			// Being the leader of round 1, the node must propose a block.
			require.Eventually(t, func() bool {
				for {
					select {
					case msg := <-recordingComm.BroadcastMessages:
						if msg.VerifiedBlockMessage != nil || msg.BlockMessage != nil {
							require.Equal(t, uint64(1), msg.VerifiedBlockMessage.VerifiedBlock.BlockHeader().Round)
							return true
						}
					default:
						return false
					}
				}
			}, 5*time.Second, 50*time.Millisecond, "leader never proposed a block for round 1 after advancing via a replicated quorum round")
		})
	}
}

func replicateSeq(block common.VerifiedFinalizedBlock) *common.Message {
	return &common.Message{ReplicationResponse: &common.ReplicationResponse{
		Data: []common.QuorumRound{{
			Block:        block.VerifiedBlock.(common.Block),
			Finalization: &block.Finalization,
		}},
	}}
}

// TestReplicationRedeliversRestoredFinalization asserts that a node commits a block when a replication
// response redelivers a finalization the node already knows of. This is possible if the finalization was
// recovered from the WAL or previously received in a finalization message.
func TestReplicationRedeliversRestoredFinalization(t *testing.T) {
	ctx := context.Background()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	blocks := createBlocks(t, nodes, 2)

	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), testutil.NewTestBlockBuilder())
	conf.ReplicationEnabled = true
	require.NoError(t, storage.Index(ctx, blocks[0].VerifiedBlock, blocks[0].Finalization))

	// the round is restored holding a finalization whose seq is the next one to commit
	second := blocks[1].VerifiedBlock
	blockRecord, err := common.BlockRecord(second.BlockHeader(), second.Bytes())
	require.NoError(t, err)
	require.NoError(t, wal.Append(blockRecord))
	_, finalizationRecord := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{N: len(nodes)}, second, nodes)
	require.NoError(t, wal.Append(finalizationRecord))

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	require.NoError(t, e.HandleMessage(replicateSeq(blocks[1]), nodes[1]))
	storage.WaitForBlockCommit(1)
}
