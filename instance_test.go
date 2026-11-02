// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"
	"time"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	metadata "github.com/ava-labs/simplex/msm"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// TestValidatorIndexes tests that a validator indexes and accepts a new block sent by the network
// It is the only validator, so it will build and finalize its own block.
func TestValidatorIndexes(t *testing.T) {
	validator := newNodeMapping(1)

	genesisSet := []metadata.NodeBLSMapping{validator}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	node := network.addNode(validator.NodeID[:]).sync()

	isValidator, epochsReplicated := node.role()
	require.True(t, epochsReplicated, "a node already at the latest validator set has no epochs to replicate")
	require.True(t, isValidator)

	network.acceptNewBlock()
}

// emptyVoteRecorder signals the first empty vote broadcast and drops all other traffic.
type emptyVoteRecorder struct {
	got chan struct{}
}

func (r *emptyVoteRecorder) Broadcast(msg *common.Message) {
	if msg.EmptyVoteMessage != nil {
		select {
		case r.got <- struct{}{}:
		default:
		}
	}
}

func (r *emptyVoteRecorder) Send(*common.Message, common.NodeID) {}

func TestEpochInvokesMSMWaitForPendingBlock(t *testing.T) {
	// Two validators, but only one is instantiated. Our node has the smaller ID so it sorts to
	// index 0 and is a non-leader for round 1 (LeaderForRound picks index 1%2). The other
	// validator is the round leader but is never created, so no block is ever proposed.
	ourValidator := newNodeMapping(1)
	leader := newNodeMapping(2)
	genesisSet := []metadata.NodeBLSMapping{ourValidator, leader}

	pChain := newTestPChain(genesisSet)
	nodes := pChain.GenesisValidatorSet().Nodes()
	common.SortNodes(nodes)
	require.NotEqual(t, common.NodeID(ourValidator.NodeID[:]), simplex.LeaderForRound(nodes.NodeIDs(), 1))

	storage := newTestStorageWithGenesis(t)

	vm := newBlockBuilderVM(storage, newPendingBlockSignal())
	recorder := &emptyVoteRecorder{got: make(chan struct{}, 1)}
	wc := &walCreator{t: t}

	inst := NewInstance(Config{
		LastNonSimplexInnerBlock: genesisBlock,
		ParameterConfig:          paramConfig,
		PlatformChain:            pChain,
		Broadcaster:              recorder,
		Sender:                   recorder,
		CryptoOps:                &testCryptoOps{},
		WalCreator:               wc.createWAL,
		Storage:                  storage,
		Logger:                   testutil.MakeLogger(t, 1),
		VM:                       vm,
		ICMETransition:           noopICMTransition,
		ID:                       ourValidator.NodeID[:],
	})

	require.NoError(t, inst.Start(t.Context()))
	t.Cleanup(inst.Stop)

	select {
	case <-recorder.got:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "node never broadcast an empty vote, so the Epoch did not drive the MSM's WaitForPendingBlock")
	}
}

// TestNonValidatorSyncs asserts a node outside the validator set syncs the chain when added
// to the network, and stays a non-validator once it has epochsReplicated.
func TestNonValidatorSyncs(t *testing.T) {
	validator := newNodeMapping(1)
	genesisSet := []metadata.NodeBLSMapping{validator}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	network.addNode(validator.NodeID[:])

	network.acceptNewBlock()

	nonValidator := newNodeMapping(2)
	node := network.addNode(nonValidator.NodeID[:])
	network.acceptNewBlock()
	node.sync()

	// ensure we replicated epochs and are not a validator
	isValidator, epochsReplicated := node.role()
	require.True(t, epochsReplicated)
	require.False(t, isValidator)
}

// TestNonValidatorBecomesValidator tests that an upcoming validator becomes a validator
// when an epoch change they are following is sealed. The non-validator must contribute it's approval in
// order to do so.
func TestNonValidatorBecomesValidator(t *testing.T) {
	validator := newNodeMapping(1)

	genesisSet := []metadata.NodeBLSMapping{validator}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	network.addNode(validator.NodeID[:]).sync()

	network.acceptNewBlock()

	// The non-validator node syncs the accepted blocks and then contributes to the next blocks
	upcomingValidator := newNodeMapping(2)
	network.addNode(upcomingValidator.NodeID[:]).sync()

	// initiate an epoch change
	newValidatorSet := metadata.NodeBLSMappings{validator, upcomingValidator}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)

	network.waitUntilSealingBlock(newValidatorSet.Nodes())

	// a normal block should be signed by both the validators now
	_, finalization := network.acceptNewBlock()
	assertExpectedNodeIds(t, finalization.QC.Signers(), newValidatorSet.NodeIDs())
}

// TestInstanceDropsMessagesBeforeStart asserts that a message delivered before Start
// is dropped without error. A validator broadcasts the finalization only after
// indexing the block, so a node created on that commit can receive it before starting.
func TestInstanceDropsMessagesBeforeStart(t *testing.T) {
	validator := newNodeMapping(1)
	pChain := newTestPChain([]metadata.NodeBLSMapping{validator})
	instance := NewInstance(Config{
		LastNonSimplexInnerBlock: genesisBlock,
		PlatformChain:            pChain,
		Storage:                  newTestStorageWithGenesis(t),
		Logger:                   testutil.MakeLogger(t, 1),
		ID:                       validator.NodeID[:],
	})

	msg := &common.Message{Finalization: &common.Finalization{}}
	require.NoError(t, instance.HandleMessage(msg, validator.NodeID[:]))
}

// TestNonValidatorThresholdFollowsPChainTip asserts a non-validator counts sealing block votes
// against the validator set at the current P-chain tip. The tip grows while the node is offline,
// raising the threshold to two, instead of the one that was required when the non-validator joined.
func TestNonValidatorThresholdFollowsPChainTip(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)
	targetNode := newNodeMapping(42)

	pChain := newTestPChain(metadata.NodeBLSMappings{v1})
	network := newNetwork(t, pChain)
	network.addNode(v1.NodeID[:])
	network.addNode(v2.NodeID[:]).sync()
	network.addNode(v3.NodeID[:]).sync()

	// produce a few sealing blocks
	twoValidators := metadata.NodeBLSMappings{v1, v2}
	pChain.setValidatorSetAt(10, twoValidators)
	pChain.advanceHeight(10)
	network.waitUntilSealingBlock(twoValidators.Nodes())

	threeValidators := metadata.NodeBLSMappings{v1, v2, v3}
	pChain.setValidatorSetAt(20, threeValidators)
	pChain.advanceHeight(20)
	network.waitUntilSealingBlock(threeValidators.Nodes())

	// only v1 answers the target node from here on
	network.setOffline(v2.NodeID[:])
	network.setOffline(v3.NodeID[:])

	// our nonValidator joins when a threshold of votes for tip = 1
	network.setOffline(targetNode.NodeID[:])
	nonValidator := network.addNode(targetNode.NodeID[:])

	// Now the threshold of votes for tip is F(5)+1 = 2.
	fiveValidators := metadata.NodeBLSMappings{v1, v2, v3, newNodeMapping(4), newNodeMapping(5)}
	pChain.setValidatorSetAt(30, fiveValidators)
	pChain.advanceHeight(30)
	network.setOnline(targetNode.NodeID[:])

	// the next sealing block request goes out after simplex.DefaultReplicationRequestTimeout
	require.Never(t, func() bool {
		_, epochsReplicated := nonValidator.role()
		return epochsReplicated
	}, simplex.DefaultReplicationRequestTimeout+2*time.Second, 100*time.Millisecond)

	// Now we have 2 validators online so we should reach a threshold and finish replication.
	network.setOnline(v2.NodeID[:])
	require.Eventually(t, func() bool {
		_, epochsReplicated := nonValidator.role()
		return epochsReplicated
	}, 2*simplex.DefaultReplicationRequestTimeout, 100*time.Millisecond)
}

func TestHonestLeaderBuildsOnFutureDatedParent(t *testing.T) {
	// Four validators, of which the Byzantine one is never instantiated: the honest three are a
	// quorum on their own. It has the largest ID, so it sorts last.
	honest := []metadata.NodeBLSMapping{newNodeMapping(1), newNodeMapping(2), newNodeMapping(3)}
	byzantine := newNodeMapping(4)
	validators := append(metadata.NodeBLSMappings{byzantine}, honest...)

	pChain := newTestPChain(validators)
	network := newNetwork(t, pChain)
	var nodes []*node
	for _, v := range honest {
		nodes = append(nodes, network.addNode(v.NodeID[:]))
	}
	network.sync()

	validatorIDs := network.epochValidatorSet.NodeIDs()
	byzantineID := common.NodeID(byzantine.NodeID[:])

	// Accept honest blocks until the Byzantine validator leads the round after the tip.
	var parent metadata.StateMachineBlock
	for {
		blk, _ := network.acceptNewBlock()
		parent = blk.(*ParsedBlock).StateMachineBlock
		if simplex.LeaderForRound(validatorIDs, parent.Metadata.SimplexProtocolMetadata.Round+1).Equals(byzantineID) {
			break
		}
	}

	// The Byzantine leader proposes a block that is valid in every respect, but stamped 5 seconds
	// ahead of the honest validators' clocks, which is within the default time skew limit.
	futureTime := time.Now().Add(5 * time.Second)
	futureDated := metadata.StateMachineBlock{
		InnerBlock: &testInnerBlock{Height_: network.seq, TS: futureTime, Payload: []byte("future-dated")},
		Metadata: metadata.StateMachineMetadata{
			Timestamp: uint64(futureTime.UnixMilli()),
			SimplexProtocolMetadata: common.ProtocolMetadata{
				Epoch: parent.Metadata.SimplexProtocolMetadata.Epoch,
				Round: parent.Metadata.SimplexProtocolMetadata.Round + 1,
				Seq:   network.seq,
				Prev:  parent.Digest(),
			},
			SimplexBlacklist: parent.Metadata.SimplexBlacklist,
			SimplexEpochInfo: metadata.SimplexEpochInfo{
				PChainReferenceHeight: parent.Metadata.SimplexEpochInfo.PChainReferenceHeight,
				PrevVMBlockSeq:        parent.Metadata.SimplexProtocolMetadata.Seq,
			},
			PChainHeight: parent.Metadata.PChainHeight,
		},
	}

	vote := common.ToBeSignedVote{BlockHeader: (&ParsedBlock{StateMachineBlock: futureDated.Clone()}).BlockHeader()}
	sig, err := vote.Sign(&testCryptoOps{})
	require.NoError(t, err)
	for _, n := range nodes {
		require.NoError(t, n.inst.HandleMessage(&common.Message{
			BlockMessage: &common.BlockMessage{
				Block: &ParsedBlock{StateMachineBlock: futureDated.Clone()},
				Vote:  common.Vote{Vote: vote, Signature: common.Signature{Signer: byzantineID, Value: sig}},
			},
		}, byzantineID))
	}

	network.seq++

	network.sync()
	committed, _, err := nodes[0].storage.GetBlock(network.seq - 1)
	require.NoError(t, err)
	require.Equal(t, futureDated.Digest(), committed.Digest())

	// The next round is led by an honest validator. Its block should be accepted well before
	// the honest validators' clocks reach the parent's timestamp.
	require.False(t, simplex.LeaderForRound(validatorIDs, futureDated.Metadata.SimplexProtocolMetadata.Round+1).Equals(byzantineID))
	network.pending.addPendingBlock()
	for _, n := range nodes {
		require.Eventually(t, func() bool {
			return n.storage.NumBlocks() > network.seq
		}, 3*time.Second, 10*time.Millisecond,
			"node %s did not commit an honest block on top of a future-dated parent", n.id)
	}

	child, _, err := nodes[0].storage.GetBlock(network.seq)
	require.NoError(t, err)
	require.GreaterOrEqual(t, child.Metadata.Timestamp, futureDated.Metadata.Timestamp)
}

// TestValidator_ValidatorSetNotChanged tests that a P-chain height increase
// that does not have a unique validator set, does not create a new epoch
func TestValidator_ValidatorSetNotChanged(t *testing.T) {
	validator := newNodeMapping(1)

	genesisSet := []metadata.NodeBLSMapping{validator}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	node := network.addNode(validator.NodeID[:]).sync()

	firstBlock, _ := network.acceptNewBlock()

	// initiate an epoch change
	pChain.setValidatorSetAt(10, []metadata.NodeBLSMapping{validator})
	pChain.advanceHeight(10)

	blockCount := node.storage.NumBlocks()
	// potential time to propose blocks (if any)
	require.Never(t, func() bool {
		return node.storage.NumBlocks() != blockCount
	}, time.Second, 100*time.Millisecond, "no new block should have been proposed")

	secondBlock, _ := network.acceptNewBlock()
	require.Equal(t, uint64(1), secondBlock.BlockHeader().Epoch)
	require.Equal(t, firstBlock.BlockHeader().Seq+1, secondBlock.BlockHeader().Seq)
}

// TestValidatorValidatorSetDecreased tests that an epoch with two validators
// is reduced to one, when the pchain height notes a validator is leaving.
func TestValidatorValidatorSetDecreased(t *testing.T) {
	validatorMapping := newNodeMapping(1)
	leavingValidatorMapping := newNodeMapping(2)

	genesisSet := []metadata.NodeBLSMapping{validatorMapping, leavingValidatorMapping}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)

	// starting from storage holding the first simplex block avoids
	// blocking on a sync that needs a quorum online
	network.addNode(validatorMapping.NodeID[:])
	network.addNode(leavingValidatorMapping.NodeID[:]).sync()

	block, _ := network.acceptNewBlock()
	require.Equal(t, uint64(2), block.BlockHeader().Seq)

	// initiate an epoch change
	newValidatorSet := metadata.NodeBLSMappings{validatorMapping}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)

	sealing := network.waitUntilSealingBlock(newValidatorSet.Nodes())

	block, _ = network.acceptNewBlock()
	require.Equal(t, sealing.BlockHeader().Seq+1, block.BlockHeader().Seq)
}

// TestInstanceOfflineDuringTransition asserts an epoch transition completes while a
// current validator is offline. The online quorum batches approvals and finalizes the
// sealing block, and the new epoch finalizes blocks without the offline node.
func TestInstanceOfflineDuringTransition(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)
	offline := newNodeMapping(4)

	genesisSet := []metadata.NodeBLSMapping{v1, v2, v3, offline}
	nodeIDs := metadata.NodeBLSMappings(genesisSet).NodeIDs()
	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)

	node1 := network.addNode(v1.NodeID[:])
	network.addNode(v2.NodeID[:])
	network.addNode(v3.NodeID[:])
	network.sync()

	// using seq should be fine since we have no empty blocks
	leader := simplex.LeaderForRound(pChain.GenesisValidatorSet().NodeIDs(), network.seq)
	for !leader.Equals(offline.NodeID[:]) {
		network.acceptNewBlock()
		leader = simplex.LeaderForRound(nodeIDs, node1.inst.e.Metadata().Round)
	}

	// initiate an epoch change when the offline node is the leader
	newValidatorSet := []metadata.NodeBLSMapping{v1, v2, v3}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)

	network.waitUntilSealingBlock(metadata.NodeBLSMappings(newValidatorSet).Nodes())
}

// TestNonValidatorStaysNonValidator ensures that a non-validator advances through different epoch changes.
func TestNonValidatorStaysNonValidator(t *testing.T) {
	targetNode := newNodeMapping(42)

	// case 1: epoch change is not highest and we are NOT in the validator1 set
	// case 2: epoch change is not highest, and we are in the validator1 set
	// case 3: epoch change is highest, and we are in the validator1 set
	// case 4: epoch change is highest, and we are NOT in the validator1 set
	validator1 := newNodeMapping(1)

	genesisSet := []metadata.NodeBLSMapping{validator1}
	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	network.addNode(validator1.NodeID[:])

	validator2 := newNodeMapping(2)
	validator3 := newNodeMapping(3)
	validator4 := newNodeMapping(4)
	network.addNode(validator2.NodeID[:]).sync()
	network.addNode(validator3.NodeID[:]).sync()
	network.addNode(validator4.NodeID[:]).sync()

	// we should have a quorum without the target node to create this epoch change
	targetNodeNotInMiddleEpoch := metadata.NodeBLSMappings{validator1, validator2, validator3}
	targetNodeInMiddleEpoch := metadata.NodeBLSMappings{validator1, validator2, validator3, targetNode}
	targetNodeInHighestEpoch := metadata.NodeBLSMappings{validator1, validator2, validator3, validator4, targetNode}

	// set the pchain heights
	pChain.setValidatorSetAt(10, targetNodeNotInMiddleEpoch)
	pChain.setValidatorSetAt(20, targetNodeInMiddleEpoch)
	pChain.setValidatorSetAt(30, targetNodeInHighestEpoch)

	pChain.advanceHeight(10)
	network.waitUntilSealingBlock(targetNodeNotInMiddleEpoch.Nodes())

	pChain.advanceHeight(20)
	network.waitUntilSealingBlock(targetNodeInMiddleEpoch.Nodes())

	// Occasionally, this will offset the proposer of the transition block to be the offline target node
	// Therefore, if we don't have a mechanism to skip offline leaders during the transition phase, this test will hang
	network.acceptNewBlock()

	pChain.advanceHeight(30)
	network.waitUntilSealingBlock(targetNodeInHighestEpoch.Nodes())

	// the target node should join now
	network.addNode(targetNode.NodeID[:]).sync()

	// the target node must sign within a bounded number of blocks, otherwise it never rejoined
	const maxBlocksUntilTargetSigns = 10
	var signed bool
	for i := 0; i < maxBlocksUntilTargetSigns && !signed; i++ {
		_, finalization := network.acceptNewBlock()
		signed = common.NodeIDs(finalization.QC.Signers()).IndexOf(targetNode.NodeID[:]) != -1
	}
	require.True(t, signed, "target node never signed a finalization within %d blocks", maxBlocksUntilTargetSigns)
}

// TestInstanceValidatorSkipsAnEpoch tests that a validator stops and starts being a validator
// It boots up as a non-validator then syncs to the highest epoch where it is a validator,
// then it is no longer a validator, and finally it is
func TestInstanceValidatorSkipsAnEpoch(t *testing.T) {
	validator := newNodeMapping(1)

	genesisSet := []metadata.NodeBLSMapping{validator}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	network.addNode(validator.NodeID[:]).sync()

	// The non-validator node syncs the accepted blocks and then contributes to the next blocks
	onOffValidator := newNodeMapping(2)
	network.addNode(onOffValidator.NodeID[:]).sync()

	// initiate an epoch change
	newValidatorSet := metadata.NodeBLSMappings{validator, onOffValidator}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)

	network.waitUntilSealingBlock(newValidatorSet.Nodes())

	newValidatorSet = metadata.NodeBLSMappings{validator}
	pChain.setValidatorSetAt(20, newValidatorSet)
	pChain.advanceHeight(20)
	network.waitUntilSealingBlock(newValidatorSet.Nodes())

	// accept a new block to ensure both nodes are still syncing the chain
	network.acceptNewBlock()

	// initiate the final epoch change
	newValidatorSet = metadata.NodeBLSMappings{validator, onOffValidator}
	pChain.setValidatorSetAt(30, newValidatorSet)
	pChain.advanceHeight(30)

	network.waitUntilSealingBlock(newValidatorSet.Nodes())
}

func TestInstanceRestartsAfterZeroBlock(t *testing.T) {
	validator := newNodeMapping(1)
	pChain := newTestPChain([]metadata.NodeBLSMapping{validator})
	network := newNetwork(t, pChain)

	// The lone validator builds and commits the zero block on its own.
	node := network.addNode(validator.NodeID[:]).sync()

	zeroBlock, _, err := node.storage.GetBlock(1)
	require.NoError(t, err)
	require.Equal(t, metadata.BlockTypeZero, zeroBlock.Type())
	require.Nil(t, zeroBlock.InnerBlock)

	node.restart()

	// The restarted node keeps the chain going.
	network.acceptNewBlock()
}

// TestInstanceGarbageCollectsWAL asserts a validator garbage collects its WAL every
// walGCInterval blocks, so the WAL never holds more than about walGCInterval rounds of records.
func TestInstanceGarbageCollectsWAL(t *testing.T) {
	validator := newNodeMapping(1)
	pChain := newTestPChain([]metadata.NodeBLSMapping{validator})
	network := newNetwork(t, pChain)

	// We make the WAL size tiny, so we make more WAL files
	params := paramConfig
	params.WALMaxSizeBytes = 10
	node := network.addNodeWithConfig(validator.NodeID[:], nodeConfig{params: &params}).sync()

	for range 100 {
		network.acceptNewBlock()

		// Each round writes two records, and the garbage collection a block triggers
		// may not have run yet when its commit is reported.
		records := node.walRecords()
		require.NotEmpty(t, records)
		require.LessOrEqual(t, len(records), 2*(walGCInterval+1))
	}
}

// TestInstanceClearsWALAfterSealingBlock asserts that once a sealing block is indexed,
// the WAL holds no records of the sealed epoch, only those of the epoch that follows it.
func TestInstanceClearsWALAfterSealingBlock(t *testing.T) {
	validatorMapping := newNodeMapping(1)
	leavingValidatorMapping := newNodeMapping(2)

	pChain := newTestPChain([]metadata.NodeBLSMapping{validatorMapping, leavingValidatorMapping})
	network := newNetwork(t, pChain)

	node := network.addNode(validatorMapping.NodeID[:])
	network.addNode(leavingValidatorMapping.NodeID[:]).sync()

	network.acceptNewBlock()
	require.NotEmpty(t, node.walRecords())

	newValidatorSet := metadata.NodeBLSMappings{validatorMapping}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)

	sealing := network.waitUntilSealingBlock(newValidatorSet.Nodes())
	network.acceptNewBlock()

	// The epoch that follows the sealing block is numbered by the sealing block's sequence.
	records := node.walRecords()
	require.NotEmpty(t, records)
	fromNewEpoch, err := common.RecordsFromEpoch(records, sealing.BlockHeader().Seq)
	require.NoError(t, err)
	require.Equal(t, records, fromNewEpoch, "WAL holds records of the sealed epoch")
}

// TestInstanceEmptiesWALOfLeavingValidator asserts a validator that leaves the validator set
// empties its WAL upon indexing the sealing block, except for a Telock it may propose before it is stopped,
// and as a non-validator writes no new records.
func TestInstanceEmptiesWALOfLeavingValidator(t *testing.T) {
	validatorMapping := newNodeMapping(1)
	leavingValidatorMapping := newNodeMapping(2)

	pChain := newTestPChain([]metadata.NodeBLSMapping{validatorMapping, leavingValidatorMapping})
	network := newNetwork(t, pChain)

	network.addNode(validatorMapping.NodeID[:])
	leavingNode := network.addNode(leavingValidatorMapping.NodeID[:]).sync()

	network.acceptNewBlock()
	require.NotEmpty(t, leavingNode.walRecords())

	newValidatorSet := metadata.NodeBLSMappings{validatorMapping}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)

	network.waitUntilSealingBlock(newValidatorSet.Nodes())

	// isTelock reports whether the given WAL record is a block record of a Telock.
	isTelock := func(record []byte) bool {
		_, blockBytes, err := common.ParseBlockRecord(record)
		if err != nil {
			return false
		}
		var rawBlock metadata.RawBlock
		if err := rawBlock.UnmarshalCanoto(blockBytes); err != nil {
			return false
		}
		block := metadata.StateMachineBlock{Metadata: rawBlock.Metadata}
		return block.Type() == metadata.BlockTypeTelock
	}

	// The garbage collection may not have run yet when the sealing block's commit is reported.
	// The epoch may propose a Telock for the round after the sealing block before it is stopped,
	// and its record is written after the garbage collection, so only Telocks may remain.
	require.Eventually(t, func() bool {
		for _, record := range leavingNode.walRecords() {
			if !isTelock(record) {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)

	// A non-validator must not add to the WAL.
	remaining := leavingNode.walRecords()
	network.acceptNewBlock()
	require.Equal(t, remaining, leavingNode.walRecords())
}

func TestInstanceDoubleStartFails(t *testing.T) {
	validator := newNodeMapping(1)
	genesisSet := []metadata.NodeBLSMapping{validator}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	node := network.addNode(validator.NodeID[:])
	require.ErrorIs(t, node.inst.Start(t.Context()), errAlreadyStarted)
}

// TestNonValidatorSkipsMSMVerification proves that a non-validator does not use the MSM to
// verify blocks: it commits a finalized block whose state machine transition is invalid.
func TestNonValidatorSkipsMSMVerification(t *testing.T) {
	validator := newNodeMapping(1)
	genesisValidatorSet := []metadata.NodeBLSMapping{validator}
	pChain := newTestPChain(genesisValidatorSet)

	nonValidator := newNodeMapping(2)
	network := newNetwork(t, pChain)
	network.addNode(validator.NodeID[:]).sync()
	nonValidatorNode := network.addNode(nonValidator.NodeID[:]).sync()

	parent, _, err := nonValidatorNode.storage.GetBlock(1)
	require.NoError(t, err)

	// The non-validator requires a  threshold of F(1)+1 responses for the first sealing block.
	// Otherwise it will block non-replication messages.
	sealing := &ParsedBlock{StateMachineBlock: parent.Clone()}
	sealingFinalization, _ := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{N: 1}, sealing, []common.NodeID{validator.NodeID[:]})
	require.NoError(t, nonValidatorNode.inst.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			LatestSeq: &common.QuorumRound{Block: sealing, Finalization: &sealingFinalization},
		},
	}, validator.NodeID[:]))

	_, epochsReplicated := nonValidatorNode.role()
	require.True(t, epochsReplicated)

	// A block whose only defect is its state machine transition: its timestamp precedes its
	// parent's.
	invalid := metadata.StateMachineBlock{
		InnerBlock: &testInnerBlock{Height_: 2, TS: time.Now(), Payload: []byte("invalid")},
		Metadata: metadata.StateMachineMetadata{
			Timestamp:               parent.Metadata.Timestamp - 1,
			SimplexProtocolMetadata: common.ProtocolMetadata{Epoch: 1, Round: 2, Seq: 2, Prev: common.Digest(parent.Digest())},
		},
	}

	// Hand the non-validator that block, finalized over its digest.
	block := &ParsedBlock{StateMachineBlock: invalid.Clone()}
	finalization, _ := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{N: 1}, block, []common.NodeID{validator.NodeID[:]})
	require.NoError(t, nonValidatorNode.inst.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			Data: []common.QuorumRound{{Block: block, Finalization: &finalization}},
		},
	}, validator.NodeID[:]))

	// It commits the block its state machine would have rejected.
	nonValidatorNode.storage.WaitForBlockCommit(2)
	committed, _, err := nonValidatorNode.storage.GetBlock(2)
	require.NoError(t, err)
	require.Equal(t, invalid.Digest(), committed.Digest())
}

// TestValidatorSkipsMSMVerificationWhenReplicating proves that a lagging validator does not
// use the MSM to verify blocks it replicates, as they carry a QC. It checks a notarized and
// a finalized block.
func TestValidatorSkipsMSMVerificationWhenReplicating(t *testing.T) {
	for _, tt := range []struct {
		name string
		// quorumRound wraps the replicated block with a QC.
		quorumRound func(t *testing.T, block *ParsedBlock, signers []common.NodeID) common.QuorumRound
		// requireReplicated asserts the lagging node replicated the block.
		requireReplicated func(t *testing.T, laggingNode *node, block metadata.StateMachineBlock)
	}{
		{
			name: "notarization",
			quorumRound: func(t *testing.T, block *ParsedBlock, signers []common.NodeID) common.QuorumRound {
				notarization, err := testutil.NewNotarization(testutil.MakeLogger(t, 0), &testutil.TestSignatureAggregator{N: len(signers)}, block, signers)
				require.NoError(t, err)
				return common.QuorumRound{Block: block, Notarization: &notarization}
			},
			// A notarized block is not committed but notarized: the node persists the
			// notarization to its WAL, which it only reaches after the block verified.
			requireReplicated: func(t *testing.T, laggingNode *node, block metadata.StateMachineBlock) {
				round := block.Metadata.SimplexProtocolMetadata.Round
				require.Eventually(t, func() bool {
					return laggingNode.wals.containsNotarization(round)
				}, 10*time.Second, 10*time.Millisecond, "no notarization for round %d was persisted to the WAL", round)
				require.Equal(t, block.Metadata.SimplexProtocolMetadata.Seq, laggingNode.storage.NumBlocks(), "a notarized block should not have been committed")
			},
		},
		{
			name: "finalization",
			quorumRound: func(t *testing.T, block *ParsedBlock, signers []common.NodeID) common.QuorumRound {
				finalization, _ := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{N: len(signers)}, block, signers)
				return common.QuorumRound{Block: block, Finalization: &finalization}
			},
			// A finalized block is committed.
			requireReplicated: func(t *testing.T, laggingNode *node, block metadata.StateMachineBlock) {
				seq := block.Metadata.SimplexProtocolMetadata.Seq
				laggingNode.storage.WaitForBlockCommit(seq)
				committed, _, err := laggingNode.storage.GetBlock(seq)
				require.NoError(t, err)
				require.Equal(t, block.Digest(), committed.Digest())
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Two validators, so a quorum is both of them: with its peer absent, the lagging
			// validator can neither build a block nor empty notarize a round on its own.
			lagging := newNodeMapping(1)
			peer := newNodeMapping(2)
			validators := metadata.NodeBLSMappings{lagging, peer}
			pChain := newTestPChain(validators)

			// The lagging validator holds genesis plus epoch 1's defining block, and lives on
			// a network of its own, so the replication response below is the only way it can
			// learn the block at the round it sits on.

			network := newNetwork(t, pChain)
			network.addNode(peer.NodeID[:])
			laggingNode := network.addNode(lagging.NodeID[:])
			network.sync()

			parent, _, err := laggingNode.storage.GetBlock(1)
			require.NoError(t, err)
			// A block whose only defect is its state machine transition: its timestamp
			// precedes its parent's.
			invalid := metadata.StateMachineBlock{
				InnerBlock: &testInnerBlock{Height_: 2, TS: time.Now(), Payload: []byte("invalid")},
				Metadata: metadata.StateMachineMetadata{
					Timestamp:               parent.Metadata.Timestamp - 1,
					SimplexProtocolMetadata: common.ProtocolMetadata{Epoch: 1, Round: 2, Seq: 2, Prev: common.Digest(parent.Digest())},
				},
			}

			// Hand the lagging validator that block wrapped in a QC.
			block := &ParsedBlock{StateMachineBlock: invalid.Clone()}
			quorumRound := tt.quorumRound(t, block, validators.NodeIDs())
			require.NoError(t, laggingNode.inst.HandleMessage(&common.Message{
				ReplicationResponse: &common.ReplicationResponse{Data: []common.QuorumRound{quorumRound}},
			}, peer.NodeID[:]))

			// It replicates the block its state machine would have rejected.
			tt.requireReplicated(t, laggingNode, invalid)
		})
	}
}

// TestValidatorIndexes tests that a validator indexes and accepts a new block sent by the network
// It is the only validator, so it will build and finalize its own block.
func TestValidatorRequestsGenesis(t *testing.T) {
	validatorID := newNodeMapping(1)
	nonValidatorID := newNodeMapping(2)

	genesisSet := []metadata.NodeBLSMapping{validatorID}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	validator := network.addNode(validatorID.NodeID[:]).sync()

	// replication request for seq 0 (genesis)
	msg := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs: []uint64{0},
		},
	}

	require.NoError(t, validator.inst.HandleMessage(msg, nonValidatorID.NodeID[:]))
}

func TestValidatorSetsMetadataFromSnowman(t *testing.T) {
	validatorID := newNodeMapping(1)
	numNonSimplexBlocks := uint64(10)
	genesisSet := []metadata.NodeBLSMapping{validatorID}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	network.seq = numNonSimplexBlocks
	storage := newTestStorage()

	var lastBlock avalanchego.VMBlock
	for i := range numNonSimplexBlocks {
		md := common.ProtocolMetadata{
			Epoch: 0, // non-simplex don't have an epoch
			Round: 0, // non-simplex blocks don't have rounds
			Seq:   uint64(i),
		}
		innerBlock := &testInnerBlock{Height_: uint64(i), TS: time.Now(), Payload: []byte{byte(i)}}
		pb := &ParsedBlock{StateMachineBlock: metadata.StateMachineBlock{InnerBlock: innerBlock, Metadata: metadata.StateMachineMetadata{
			SimplexProtocolMetadata: md,
		}}}
		require.NoError(t, storage.Index(t.Context(), pb, common.Finalization{}))
		lastBlock = innerBlock
	}

	config := nodeConfig{
		storage:             storage,
		lastNonSimplexBlock: lastBlock,
	}

	network.addNodeWithConfig(validatorID.NodeID[:], config).sync()

	block, _ := network.acceptNewBlock()
	require.Equal(t, numNonSimplexBlocks, block.BlockHeader().Epoch)
	require.Equal(t, uint64(1), block.BlockHeader().Round)
	require.Equal(t, numNonSimplexBlocks, block.BlockHeader().Seq)
}

// TestInstanceZeroBlockAfterPreSimplexBlocks brings up a network whose ledger already holds pre-Simplex
// blocks beyond the genesis block, and asserts that the zero block the network commits chains to the
// last non-Simplex block through that block's inner digest, at the sequence right above it.
func TestInstanceZeroBlockAfterPreSimplexBlocks(t *testing.T) {
	// The ledger holds pre-Simplex blocks from height 0 (genesis) to lastNonSimplexHeight.
	const lastNonSimplexHeight = uint64(3)
	zeroBlockSeq := lastNonSimplexHeight + 1

	// Timestamps lie in the past because the zero block carries over the last non-Simplex block's timestamp.
	preSimplexBlocks := make([]*testInnerBlock, 0, lastNonSimplexHeight+1)
	for h := uint64(0); h <= lastNonSimplexHeight; h++ {
		preSimplexBlocks = append(preSimplexBlocks, &testInnerBlock{
			Height_: h,
			TS:      time.Now().Add(-time.Duration(lastNonSimplexHeight-h+1) * time.Second),
			Payload: append([]byte("pre-simplex block "), byte('0'+h)),
		})
	}
	lastNonSimplexBlock := preSimplexBlocks[lastNonSimplexHeight]

	// Two validators, so neither can commit the zero block alone: the leader's proposal is only
	// committed once the other node has verified it against its own copy of the pre-Simplex chain.
	nodeA := newNodeMapping(1)
	nodeB := newNodeMapping(2)
	pChain := newTestPChain(metadata.NodeBLSMappings{nodeA, nodeB})

	network := newNetwork(t, pChain)
	// The zero block is built automatically at zeroBlockSeq; the first block the network asks for follows it.
	network.seq = zeroBlockSeq + 1

	for _, id := range []common.NodeID{nodeA.NodeID[:], nodeB.NodeID[:]} {
		network.addNodeWithConfig(id, nodeConfig{
			storage:             newTestStorageWithPreSimplexBlocks(t, preSimplexBlocks...),
			lastNonSimplexBlock: lastNonSimplexBlock,
		})
	}

	// Both nodes commit the zero block, then a couple of ordinary Simplex blocks on top of it.
	network.sync()
	network.acceptNewBlock()
	network.acceptNewBlock()

	for _, n := range network.nodesSnapshot() {
		zeroBlock, _, err := n.storage.GetBlock(zeroBlockSeq)
		require.NoError(t, err)
		require.Equal(t, metadata.BlockTypeZero, zeroBlock.Type())
		require.Nil(t, zeroBlock.InnerBlock)

		md := zeroBlock.Metadata.SimplexProtocolMetadata
		require.Equal(t, common.Digest(lastNonSimplexBlock.Digest()), md.Prev,
			"zero block must point to the last non-Simplex block by its inner digest")
		require.Equal(t, zeroBlockSeq, md.Seq)
	}
}

// TestValidatorReplicatesEpochsDuringTransition asserts a validator replicates epochs when
// its latest epoch is mid-transition. i.e. the latest pchain validator set disagrees
// with the validators latest index validator set.
func TestValidatorReplicatesEpochsDuringTransition(t *testing.T) {
	ourNodeMapping := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)
	v4 := newNodeMapping(4)
	futureValidator := newNodeMapping(5)
	genesisValidatorSet := metadata.NodeBLSMappings{ourNodeMapping, v2, v3, v4}
	futureValidatorSet := metadata.NodeBLSMappings{ourNodeMapping, v2, v3, v4, futureValidator}

	// The P-chain moved on to a set that contains ourNode, but no sealing block for it has
	// been indexed, so our indexed set and the latest set disagree.
	pChain := newTestPChain(genesisValidatorSet)

	network := newNetwork(t, pChain)
	network.addNode(futureValidator.NodeID[:])
	network.addNode(v2.NodeID[:])
	network.addNode(v3.NodeID[:])
	network.addNode(v4.NodeID[:])
	network.sync()

	// all validators are offline
	network.setOffline(v4.NodeID[:])
	network.setOffline(v3.NodeID[:])
	network.setOffline(v2.NodeID[:])

	// the future validator set is different than the current validator set ourNodeIsIn
	pChain.setValidatorSetAt(10, futureValidatorSet)
	pChain.advanceHeight(10)

	// the node joins, but because the pchain validator set is different than our epoch we will sync as a non-validator
	node := network.addNode(ourNodeMapping.NodeID[:])
	isValidator, epochsReplicated := node.role()
	require.False(t, epochsReplicated)
	require.False(t, isValidator, "a node whose indexed set is not the latest must replicate epochs first") // even though we are a validator

	// bring 2 node back online. The threshold for non-validators to complete epoch replication is 2 votes,
	// but to make a quorum is 3. This means the pchain transition will not occur, however the node should now sync as a validator
	network.setOnline(v2.NodeID[:])
	network.setOnline(v3.NodeID[:])
	node.sync()

	// The only way for the epoch transition to finish is if ourNode becomes a validator
	// and produces an approval & participates in the finalization.
	network.waitUntilSealingBlock(futureValidatorSet.Nodes())
	isValidator, _ = node.role()
	require.True(t, isValidator)
}

// requireValidatorAtEpoch waits until n runs a validator at epoch.
func requireValidatorAtEpoch(t *testing.T, n *node, epoch uint64) {
	require.Eventually(t, func() bool {
		n.inst.lock.Lock()
		defer n.inst.lock.Unlock()
		return n.inst.e != nil && n.inst.e.Epoch == epoch
	}, 10*time.Second, 10*time.Millisecond)
}

// sealingBlockResponse returns a replication response holding the finalized block at seq in storage.
func sealingBlockResponse(t *testing.T, storage *testStorage, seq uint64) *common.Message {
	block, finalization, err := storage.Retrieve(seq)
	require.NoError(t, err)
	require.NotNil(t, block.SealingBlockInfo())
	qr := verifiedQuorumRoundToQuorumRound(t, common.VerifiedQuorumRound{VerifiedBlock: block, Finalization: &finalization})
	return &common.Message{ReplicationResponse: &common.ReplicationResponse{Data: []common.QuorumRound{qr}}}
}

// TestValidatorCatchesUpToNextEpoch asserts an offline validator that missed a sealing block
// rejoins the new epoch. The epoch only rejoins the network when a messages for the new epoch
// are being sent.
func TestValidatorCatchesUpToNextEpoch(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)
	v4 := newNodeMapping(4)
	offline := newNodeMapping(5)

	pChain := newTestPChain(metadata.NodeBLSMappings{v1, v2, v3, v4, offline})
	network := newNetwork(t, pChain)
	network.addNode(v1.NodeID[:])
	network.addNode(v2.NodeID[:])
	network.addNode(v3.NodeID[:])
	network.addNode(v4.NodeID[:])
	node := network.addNode(offline.NodeID[:])
	network.sync()

	network.setOffline(offline.NodeID[:])

	// advance the p-chain to create a new epoch
	newValidatorSet := metadata.NodeBLSMappings{v1, v2, v3, offline}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)
	sealing := network.waitUntilSealingBlock(newValidatorSet.Nodes())

	network.setOnline(offline.NodeID[:])

	// The node is now-online, and it should receive a threshold of finalizations for the next epoch
	// when the other nodes accept the new block
	network.acceptNewBlock()
	requireValidatorAtEpoch(t, node, sealing.BlockHeader().Seq)
}

// TestValidatorCatchesUpFromSealingBlock asserts a validator whose last indexed block was a
// sealing block catches up to the next epoch when offline.
func TestValidatorCatchesUpFromSealingBlock(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)
	v4 := newNodeMapping(4)
	offline := newNodeMapping(5)

	pChain := newTestPChain(metadata.NodeBLSMappings{v1, v2, v3, v4, offline})
	network := newNetwork(t, pChain)
	network.addNode(v1.NodeID[:])
	network.addNode(v2.NodeID[:])
	network.addNode(v3.NodeID[:])
	network.addNode(v4.NodeID[:])
	node := network.addNode(offline.NodeID[:])
	network.sync()

	firstSet := metadata.NodeBLSMappings{v1, v2, v3, offline}
	pChain.setValidatorSetAt(10, firstSet)
	pChain.advanceHeight(10)
	firstSealing := network.waitUntilSealingBlock(firstSet.Nodes())

	// go offline with the sealing block as our last indexed block
	requireValidatorAtEpoch(t, node, firstSealing.BlockHeader().Seq)
	network.setOffline(offline.NodeID[:])
	require.Equal(t, firstSealing.BlockHeader().Seq, node.storage.NumBlocks()-1)

	secondSet := metadata.NodeBLSMappings{v1, v2, v3, v4, offline}
	pChain.setValidatorSetAt(20, secondSet)
	pChain.advanceHeight(20)
	secondSealing := network.waitUntilSealingBlock(secondSet.Nodes())

	network.setOnline(offline.NodeID[:])
	network.acceptNewBlock()
	requireValidatorAtEpoch(t, node, secondSealing.BlockHeader().Seq)
}

// TestValidatorCatchesUpFromIntermediateEpoch asserts an offline validator gets a threshold of sealing blocks
// for an intermediate epoch(one that is ahead of ours, but not the latest).
func TestValidatorCatchesUpFromIntermediateEpoch(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)
	v4 := newNodeMapping(4)
	offline := newNodeMapping(5)

	pChain := newTestPChain(metadata.NodeBLSMappings{v1, v2, v3, v4, offline})
	network := newNetwork(t, pChain)
	node1 := network.addNode(v1.NodeID[:])
	network.addNode(v2.NodeID[:])
	network.addNode(v3.NodeID[:])
	network.addNode(v4.NodeID[:])
	node := network.addNode(offline.NodeID[:])
	network.sync()

	network.setOffline(offline.NodeID[:])

	// add some intermediate blocks in the epoch
	network.acceptNewBlock()

	firstSet := metadata.NodeBLSMappings{v1, v2, v3, offline}
	pChain.setValidatorSetAt(10, firstSet)
	pChain.advanceHeight(10)
	firstSealing := network.waitUntilSealingBlock(firstSet.Nodes())

	// add some intermediate blocks in the epoch
	network.acceptNewBlock()

	secondSet := metadata.NodeBLSMappings{v1, v2, v3, v4, offline}
	pChain.setValidatorSetAt(20, secondSet)
	pChain.advanceHeight(20)
	secondSealing := network.waitUntilSealingBlock(secondSet.Nodes())

	// add some intermediate blocks in the epoch
	network.acceptNewBlock()

	// a threshold of validators report the first sealing block directly to the offline node
	for _, from := range []common.NodeID{v1.NodeID[:], v2.NodeID[:]} {
		require.NoError(t, node.inst.HandleMessage(sealingBlockResponse(t, node1.storage, firstSealing.BlockHeader().Seq), from))
	}

	// because the new epoch is at a height greater than what our offline node has indexed, it should
	// start as a non-validator
	require.Eventually(t, func() bool {
		isValidator, _ := node.role()
		return !isValidator
	}, 10*time.Second, 10*time.Millisecond)

	network.setOnline(offline.NodeID[:])
	network.acceptNewBlock()
	requireValidatorAtEpoch(t, node, secondSealing.BlockHeader().Seq)
}

// TestValidatorRemovedWhileOffline asserts an offline validator removed from the validator set
// keeps indexing as a non-validator once it learns of the sealing block.
func TestValidatorRemovedWhileOffline(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)
	v4 := newNodeMapping(4)
	offline := newNodeMapping(5)

	pChain := newTestPChain(metadata.NodeBLSMappings{v1, v2, v3, v4, offline})
	network := newNetwork(t, pChain)
	network.addNode(v1.NodeID[:])
	network.addNode(v2.NodeID[:])
	network.addNode(v3.NodeID[:])
	network.addNode(v4.NodeID[:])
	node := network.addNode(offline.NodeID[:])
	network.sync()

	network.setOffline(offline.NodeID[:])

	newValidatorSet := metadata.NodeBLSMappings{v1, v2, v3, v4}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)
	network.waitUntilSealingBlock(newValidatorSet.Nodes())

	network.setOnline(offline.NodeID[:])
	network.acceptNewBlock()
	isValidator, _ := node.role()
	require.False(t, isValidator)
}

// TestInstanceBlacklistConfigurationRollsOverAcrossEpochs builds a chain across epochs that alternate between two and three
// validators, whose configured max blacklisted rounds differ, and then inspects the chain to ensure that the max blacklisted
// orbits rolls over from one epoch to the next, and that each block moves it by at most a single orbit towards the
// configuration of the node that built it. Every epoch has at least two validators, so every block is verified by a
// validator other than the one that built it.
func TestInstanceBlacklistConfigurationRollsOverAcrossEpochs(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)

	// The first ever blacklist starts from GenesisMaxBlacklistedRounds (2310 orbits of two validators),
	// so the configurations are close to it, to let the chain reach them within a few blocks.
	// v3 is configured differently, and is a validator only in the second epoch.
	maxBlacklistedRoundsByNode := map[string]uint16{
		string(v1.NodeID[:]): common.GenesisMaxBlacklistedRounds - 8,  // 2306 orbits of two validators, 1537 orbits of three validators
		string(v2.NodeID[:]): common.GenesisMaxBlacklistedRounds - 8,  // 2306 orbits of two validators, 1537 orbits of three validators
		string(v3.NodeID[:]): common.GenesisMaxBlacklistedRounds + 60, // 1560 orbits of three validators
	}
	twoValidators := metadata.NodeBLSMappings{v1, v2}
	threeValidators := metadata.NodeBLSMappings{v1, v2, v3}

	pChain := newTestPChain(twoValidators)
	network := newNetwork(t, pChain)
	addNode := func(vdr metadata.NodeBLSMapping) *node {
		return network.addNodeWithConfig(vdr.NodeID[:], nodeConfig{params: paramsWithMaxBlacklistedRounds(maxBlacklistedRoundsByNode[string(vdr.NodeID[:])])})
	}

	// The first epoch is run by v1 and v2.
	node1 := addNode(v1)
	addNode(v2)
	network.sync()
	for range 4 {
		network.acceptNewBlock()
	}

	// v3 joins in the second epoch.
	addNode(v3).sync()
	pChain.setValidatorSetAt(10, threeValidators)
	pChain.advanceHeight(10)
	network.waitUntilSealingBlock(threeValidators.Nodes())
	for range 9 {
		network.acceptNewBlock()
	}

	// v3 leaves in the third epoch.
	pChain.setValidatorSetAt(20, twoValidators)
	pChain.advanceHeight(20)
	network.waitUntilSealingBlock(twoValidators.Nodes())
	for range 8 {
		network.acceptNewBlock()
	}

	// Inspect the chain: every block moves the max blacklisted orbits of its parent, possibly from the previous epoch,
	// by a single orbit towards the configuration of the node that built it. The first ever blacklist has v1's and v2's configuration.
	// When the parent is from an epoch with a different number of validators, its max blacklisted orbits is first rescaled
	// to the new number of validators.
	validators := map[uint16]common.NodeIDs{2: twoValidators.NodeIDs(), 3: threeValidators.NodeIDs()}
	orbits := uint16(common.GenesisMaxBlacklistedRounds / len(twoValidators))
	var epochSizes []uint16
	var prevBlacklist common.Blacklist
	for seq := uint64(1); seq < network.seq; seq++ {
		block := node1.storage.WaitForBlockCommit(seq)
		blacklist := block.Blacklist()
		if len(epochSizes) == 0 || epochSizes[len(epochSizes)-1] != blacklist.NodeCount() {
			if len(epochSizes) > 0 {
				fresh := prevBlacklist.Stateless(blacklist.NodeCount())
				orbits = fresh.MaxBlacklistedOrbits()
			}
			epochSizes = append(epochSizes, blacklist.NodeCount())
		}

		leader := simplex.LeaderForRound(validators[blacklist.NodeCount()], block.BlockHeader().Round)
		target := maxBlacklistedRoundsByNode[string(leader)] / blacklist.NodeCount()
		switch {
		case orbits < target:
			orbits++
		case orbits > target:
			orbits--
		}
		require.Equal(t, orbits, blacklist.MaxBlacklistedOrbits(), "block %d built by %s", seq, leader)
		prevBlacklist = blacklist
	}

	require.Equal(t, []uint16{2, 3, 2}, epochSizes)
	// The last epoch is run by v1 and v2, so the chain ends up with their configuration.
	require.Equal(t, maxBlacklistedRoundsByNode[string(v1.NodeID[:])]/uint16(len(twoValidators)), orbits)
}

// TestInstanceMaxBlacklistedRoundsRollingRestart restarts the validators one after another with a higher max blacklisted rounds,
// and then again with a lower one, while the chain keeps growing. It then inspects the chain to ensure that each block moves
// the max blacklisted orbits by a single orbit towards the configuration of the node that built it at the time,
// and that the chain ends up with the configuration all nodes were eventually restarted with.
func TestInstanceMaxBlacklistedRoundsRollingRestart(t *testing.T) {
	const (
		initialRounds   = common.GenesisMaxBlacklistedRounds      // 1155 orbits of 4 validators
		increasedRounds = common.GenesisMaxBlacklistedRounds + 12 // 1158 orbits of 4 validators
		decreasedRounds = common.GenesisMaxBlacklistedRounds - 8  // 1153 orbits of 4 validators
	)

	genesisSet := metadata.NodeBLSMappings{newNodeMapping(1), newNodeMapping(2), newNodeMapping(3), newNodeMapping(4)}
	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)

	// maxBlacklistedRoundsByNode holds the max blacklisted rounds each node is currently configured with.
	maxBlacklistedRoundsByNode := make(map[string]uint16)
	nodes := make([]*node, len(genesisSet))
	for i, vdr := range genesisSet {
		nodes[i] = network.addNodeWithConfig(vdr.NodeID[:], nodeConfig{params: paramsWithMaxBlacklistedRounds(initialRounds)})
		maxBlacklistedRoundsByNode[string(vdr.NodeID[:])] = initialRounds
	}
	network.sync()

	// targets holds the max blacklisted orbits each block's builder was configured with when it built the block.
	// The first simplex block is built while the nodes sync, with the initial configuration.
	nodeCount := uint16(len(genesisSet))
	targets := map[uint64]uint16{1: initialRounds / nodeCount}
	acceptNewBlocks := func(count int) {
		for range count {
			block, _ := network.acceptNewBlock()
			leader := simplex.LeaderForRound(genesisSet.NodeIDs(), block.BlockHeader().Round)
			targets[block.BlockHeader().Seq] = maxBlacklistedRoundsByNode[string(leader)] / nodeCount
		}
	}

	acceptNewBlocks(4)
	for _, maxBlacklistedRounds := range []uint16{increasedRounds, decreasedRounds} {
		for i := range nodes {
			nodes[i] = nodes[i].restartWithNodeConfig(nodeConfig{params: paramsWithMaxBlacklistedRounds(maxBlacklistedRounds)})
			maxBlacklistedRoundsByNode[string(nodes[i].id)] = maxBlacklistedRounds
			acceptNewBlocks(4)
		}
		acceptNewBlocks(8)
	}

	// Inspect the chain: every block moves the max blacklisted orbits of its parent by exactly a single orbit
	// towards its target, or keeps it if it is already there.
	// The first ever blacklist has no parent, it starts from GenesisMaxBlacklistedRounds.
	prevMaxOrbits := common.GenesisMaxBlacklistedRounds / nodeCount
	var highest uint16
	for seq := uint64(1); seq < network.seq; seq++ {
		block := nodes[0].storage.WaitForBlockCommit(seq)
		blacklist := block.Blacklist()
		actual := blacklist.MaxBlacklistedOrbits()

		switch target := targets[seq]; {
		case prevMaxOrbits < target:
			require.Equal(t, prevMaxOrbits+1, actual, "block %d", seq)
		case prevMaxOrbits > target:
			require.Equal(t, prevMaxOrbits-1, actual, "block %d", seq)
		default:
			require.Equal(t, prevMaxOrbits, actual, "block %d", seq)
		}
		prevMaxOrbits = actual
		highest = max(highest, actual)
	}

	// The chain reached the increased configuration, and ended up with the decreased one.
	require.Equal(t, increasedRounds/nodeCount, highest)
	require.Equal(t, decreasedRounds/nodeCount, prevMaxOrbits)
}

// TestInstanceMaxBlacklistedRoundsDifferAtGenesis runs a network whose validators are configured with different
// max blacklisted rounds from the very first block. The first blacklist must not depend on the configuration of its
// builder, otherwise validators configured differently would reject each other's blocks, and the network would never
// commit a block. It ensures that every block is committed by every validator.
func TestInstanceMaxBlacklistedRoundsDifferAtGenesis(t *testing.T) {
	const (
		lowRounds  = 8                                      // 2 orbits of 4 validators
		highRounds = common.GenesisMaxBlacklistedRounds + 8 // 1157 orbits of 4 validators
	)

	genesisSet := metadata.NodeBLSMappings{newNodeMapping(1), newNodeMapping(2), newNodeMapping(3), newNodeMapping(4)}
	configured := map[string]uint16{
		string(genesisSet[0].NodeID[:]): lowRounds,
		string(genesisSet[1].NodeID[:]): lowRounds,
		string(genesisSet[2].NodeID[:]): highRounds,
		string(genesisSet[3].NodeID[:]): highRounds,
	}

	pChain := newTestPChain(genesisSet)
	network := newNetwork(t, pChain)
	for _, vdr := range genesisSet {
		network.addNodeWithConfig(vdr.NodeID[:], nodeConfig{params: paramsWithMaxBlacklistedRounds(configured[string(vdr.NodeID[:])])})
	}
	network.sync()
	for range 8 {
		network.acceptNewBlock()
	}
}

// finalizedBlockResponse returns a replication response holding the finalized block at seq in storage.
func finalizedBlockResponse(t *testing.T, storage *testStorage, seq uint64) *common.Message {
	block, finalization, err := storage.Retrieve(seq)
	require.NoError(t, err)
	qr := verifiedQuorumRoundToQuorumRound(t, common.VerifiedQuorumRound{VerifiedBlock: block, Finalization: &finalization})
	return &common.Message{ReplicationResponse: &common.ReplicationResponse{Data: []common.QuorumRound{qr}}}
}

// TestNonValidatorSurvivesStaleTelockQuorumRound asserts a non-validator keeps following the chain after a peer
// replays a finalized Telock of the old epoch at seq k+2, where k is the sealing block, before the non-validator
// indexes k+1. Indexing k+1 drops the old epoch, and the stale Telock must not then halt the non-validator.
func TestNonValidatorSurvivesStaleTelockQuorumRound(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	target := newNodeMapping(42)
	attacker := newNodeMapping(66)

	pChain := newTestPChain(metadata.NodeBLSMappings{v1})
	network := newNetwork(t, pChain)
	validator := network.addNode(v1.NodeID[:]).sync()
	network.addNode(v2.NodeID[:]).sync()
	nonValidator := network.addNode(target.NodeID[:]).sync()

	newValidatorSet := metadata.NodeBLSMappings{v1, v2}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)
	sealing := network.waitUntilSealingBlock(newValidatorSet.Nodes())
	sealingHeader := sealing.BlockHeader()

	isValidator, epochsReplicated := nonValidator.role()
	require.True(t, epochsReplicated)
	require.False(t, isValidator)

	// Messages to the non-validator are delivered by hand from here on, so a halted non-validator
	// does not fail the network's message delivery.
	network.setOffline(target.NodeID[:])

	// Two Telocks were finalized at the end of the old epoch. The one at k+2 is replayed to the non-validator.
	// The test QC is not verified cryptographically, so the old epoch's validator signs the Telocks here.
	makeTelock := func(seq uint64, prev common.Digest) *ParsedBlock {
		return &ParsedBlock{StateMachineBlock: metadata.StateMachineBlock{
			Metadata: metadata.StateMachineMetadata{
				SimplexProtocolMetadata: common.ProtocolMetadata{
					Epoch: sealingHeader.Epoch,
					Round: sealingHeader.Round + (seq - sealingHeader.Seq),
					Seq:   seq,
					Prev:  prev,
				},
				SimplexEpochInfo: metadata.SimplexEpochInfo{SealingBlockSeq: sealingHeader.Seq},
			},
		}}
	}
	firstTelock := makeTelock(sealingHeader.Seq+1, sealingHeader.Digest)
	secondTelock := makeTelock(sealingHeader.Seq+2, firstTelock.BlockHeader().Digest)
	require.Equal(t, metadata.BlockTypeTelock, secondTelock.Type())

	toBeSigned := common.ToBeSignedFinalization{BlockHeader: secondTelock.BlockHeader()}
	sig, err := toBeSigned.Sign(&testCryptoOps{})
	require.NoError(t, err)
	telockFinalization := common.Finalization{
		Finalization: toBeSigned,
		QC:           testutil.TestQC{{Signer: v1.NodeID[:], Value: sig}},
	}
	replay := &common.Message{ReplicationResponse: &common.ReplicationResponse{
		Data: []common.QuorumRound{{Block: secondTelock, Finalization: &telockFinalization}},
	}}
	require.NoError(t, nonValidator.inst.HandleMessage(replay, attacker.NodeID[:]))

	// The new epoch finalizes k+1, and the non-validator indexes it, dropping the old epoch.
	network.acceptNewBlock()
	require.NoError(t, nonValidator.inst.HandleMessage(finalizedBlockResponse(t, validator.storage, sealingHeader.Seq+1), v1.NodeID[:]))
	nonValidator.storage.WaitForBlockCommit(sealingHeader.Seq + 1)

	// The new epoch finalizes k+2, and the non-validator must still index it.
	network.acceptNewBlock()
	require.NoError(t, nonValidator.inst.HandleMessage(finalizedBlockResponse(t, validator.storage, sealingHeader.Seq+2), v1.NodeID[:]))
	require.Eventually(t, func() bool {
		return nonValidator.storage.NumBlocks() > sealingHeader.Seq+2
	}, 10*time.Second, 10*time.Millisecond, "non-validator never indexed the new epoch's block at k+2")
}

// A non-validator receives a replayed Telock T2 at seq k+2, built on top of Telock T1 at seq k+1.
// The new epoch has already indexed a block that competes with T1, and as a result T2 cannot be executed, because it depends on T1,
// but also the block in the new epoch that competes with its index with T2 cannot be scheduled because the sequence
// has already been scheduled.
func TestNonValidatorStallsOnStaleTelockDependency(t *testing.T) {
	v1 := newNodeMapping(1)
	v2 := newNodeMapping(2)
	v3 := newNodeMapping(3)
	v4 := newNodeMapping(4)
	nonValidatorMapping := newNodeMapping(42)

	oldValidatorSet := metadata.NodeBLSMappings{v1, v2, v3, v4}
	pChain := newTestPChain(oldValidatorSet)
	network := newNetwork(t, pChain)
	node1 := network.addNode(v1.NodeID[:])
	node2 := network.addNode(v2.NodeID[:])
	node3 := network.addNode(v3.NodeID[:])
	network.addNode(v4.NodeID[:])
	network.sync()

	nonValidator := network.addNode(nonValidatorMapping.NodeID[:]).sync()

	// The non-validator is not part of the new epoch either, so it keeps running as a non-validator.
	newValidatorSet := metadata.NodeBLSMappings{v1, v2, v3}
	pChain.setValidatorSetAt(10, newValidatorSet)
	pChain.advanceHeight(10)
	sealing := network.waitUntilSealingBlock(newValidatorSet.Nodes())
	sealingSeq := sealing.BlockHeader().Seq
	oldEpoch := sealing.BlockHeader().Epoch
	nonValidator.storage.WaitForBlockCommit(sealingSeq)

	isValidator, epochsReplicated := nonValidator.role()
	require.True(t, epochsReplicated)
	require.False(t, isValidator)

	sealingBlock, _, err := node1.storage.GetBlock(sealingSeq)
	require.NoError(t, err)

	// telock() builds the Telock of the old epoch at seq, like the MSM does while the sealing block is not yet finalized.
	telock := func(seq, round uint64, prev common.Digest) *ParsedBlock {
		md := sealingBlock.Metadata.Clone()
		md.SimplexProtocolMetadata.Seq = seq
		md.SimplexProtocolMetadata.Round = round
		md.SimplexProtocolMetadata.Prev = prev
		md.SimplexEpochInfo = metadata.SimplexEpochInfo{
			PChainReferenceHeight:     sealingBlock.Metadata.SimplexEpochInfo.PChainReferenceHeight,
			NextPChainReferenceHeight: sealingBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight,
			SealingBlockSeq:           sealingSeq,
			PrevVMBlockSeq:            sealingSeq,
		}
		return &ParsedBlock{StateMachineBlock: metadata.StateMachineBlock{Metadata: md}}
	}

	sealingRound := sealing.BlockHeader().Round
	t1 := telock(sealingSeq+1, sealingRound+1, sealing.BlockHeader().Digest)
	t2 := telock(sealingSeq+2, sealingRound+2, t1.BlockHeader().Digest)

	// A quorum of the old epoch finalized T2.
	var qc testutil.TestQC
	for _, id := range oldValidatorSet.NodeIDs() {
		qc = append(qc, common.Signature{Signer: id})
	}
	t2Finalization := common.Finalization{
		Finalization: common.ToBeSignedFinalization{BlockHeader: t2.BlockHeader()},
		QC:           qc,
	}

	// The leader of T2's round replays T2's finalization and block.
	t2Leader := simplex.LeaderForRound(oldValidatorSet.NodeIDs(), t2.BlockHeader().Round)
	require.NoError(t, nonValidator.inst.HandleMessage(&common.Message{Finalization: &t2Finalization}, t2Leader))
	vote, err := testutil.NewTestVote(t2, t2Leader)
	require.NoError(t, err)
	require.NoError(t, nonValidator.inst.HandleMessage(&common.Message{
		BlockMessage: &common.BlockMessage{Vote: *vote, Block: t2},
	}, t2Leader))

	// The new epoch builds two blocks with sequences k+1 and k+2. Only wait for the validators to commit them,
	// so a stalled non-validator fails the assertion below instead of hanging the test.
	for range 2 {
		network.waitUntilValidatorsReady()
		network.pending.addPendingBlock()
		for _, validator := range []*node{node1, node2, node3} {
			validator.storage.WaitForBlockCommit(network.seq)
		}
		network.seq++
	}

	require.Eventually(t, func() bool {
		return nonValidator.storage.NumBlocks() > sealingSeq+2
	}, 10*time.Second, 10*time.Millisecond,
		"non-validator never indexed seq %d of epoch %d after a replay of the Telock of epoch %d at that seq",
		sealingSeq+2, sealingSeq, oldEpoch)
}
