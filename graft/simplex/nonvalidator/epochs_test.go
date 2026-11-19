// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nonvalidator

import (
	"context"
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// sealingTestBlock wraps testutil.TestBlock so tests can control whether the
// block reports itself as a sealing block.
type sealingTestBlock struct {
	*testutil.TestBlock
	sealingInfo *common.SealingBlockInfo
}

func (b *sealingTestBlock) SealingBlockInfo() *common.SealingBlockInfo {
	return b.sealingInfo
}

func (b *sealingTestBlock) Verify(_ context.Context, _ ...common.VerifyOptions) (common.VerifiedBlock, error) {
	return b, nil
}

func newSealingTestBlock(seq, epoch uint64, prev common.Digest, sealingInfo *common.SealingBlockInfo) *sealingTestBlock {
	return &sealingTestBlock{
		TestBlock: testutil.NewTestBlock(common.ProtocolMetadata{
			Seq:   seq,
			Round: seq,
			Epoch: epoch,
			Prev:  prev,
		}, common.Blacklist{}),
		sealingInfo: sealingInfo,
	}
}

type indexedBlock struct {
	seq         uint64
	round       uint64
	epoch       uint64
	sealingInfo *common.SealingBlockInfo
}

func TestNewEpochs(t *testing.T) {
	// nonSimplexBlock represents a block from before the first ever Simplex block —
	// i.e., genesis(epoch 0) or the last block produced by the prior consensus (Snowman).
	// Its epoch is 0 and it has no sealing info.
	var nonSimplexBlock = indexedBlock{seq: 0, round: 0, epoch: 0, sealingInfo: nil}

	nodes := common.Nodes{
		{Id: common.NodeID{1}},
		{Id: common.NodeID{2}},
		{Id: common.NodeID{3}},
		{Id: common.NodeID{4}},
	}

	sigAggCreator := func(n []common.Node) common.SignatureAggregator {
		return &testutil.TestSignatureAggregator{N: len(n)}
	}

	tests := []struct {
		name          string
		blocks        []indexedBlock
		expectedErr   error
		expectedEpoch uint64
		expectEmpty   bool
		expectedLen   int
	}{
		{
			name:        "no last accepted",
			blocks:      nil,
			expectedErr: errNoGenesis,
		},
		{
			// genesis is the only block — pre-Simplex, epoch 0.
			name: "last accepted is genesis",
			blocks: []indexedBlock{
				nonSimplexBlock,
			},
			expectEmpty: true,
		},
		{
			// the latest block is itself a sealing block at seq 2.
			name: "last accepted is sealing",
			blocks: []indexedBlock{
				nonSimplexBlock,
				{seq: 1, round: 1, epoch: 1, sealingInfo: &common.SealingBlockInfo{ValidatorSet: nodes}},
				{seq: 2, round: 2, epoch: 2, sealingInfo: &common.SealingBlockInfo{ValidatorSet: nodes}},
			},
			expectedEpoch: 2,
			expectedLen:   1,
		},
		{
			// the latest block is not a sealing block; epoch field points back to
			// the sealing block at seq 1.
			name: "last accepted is not sealing",
			blocks: []indexedBlock{
				nonSimplexBlock,
				{seq: 1, round: 1, epoch: 1, sealingInfo: &common.SealingBlockInfo{ValidatorSet: nodes}},
				{seq: 2, round: 2, epoch: 1, sealingInfo: nil},
				{seq: 3, round: 3, epoch: 1, sealingInfo: nil},
			},
			expectedEpoch: 1,
			expectedLen:   1,
		},
		{
			// the latest block is not a sealing block; its epoch field points back to
			// the block at seq 1, but that block has no sealing info either.
			name: "referenced block is missing sealing info",
			blocks: []indexedBlock{
				nonSimplexBlock,
				{seq: 1, round: 1, epoch: 1, sealingInfo: nil},
				{seq: 2, round: 2, epoch: 1, sealingInfo: nil},
			},
			expectedErr: errMissingSealingInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := testutil.NewInMemStorage()
			for _, b := range tt.blocks {
				block := newSealingTestBlock(b.seq, b.epoch, common.Digest{}, b.sealingInfo)
				require.NoError(t, storage.Index(context.Background(), block, common.Finalization{}))
			}

			epochs, err := newEpochs(storage, sigAggCreator)
			require.ErrorIs(t, err, tt.expectedErr)
			if err != nil {
				return
			}

			if tt.expectEmpty {
				require.Empty(t, epochs)
				return
			}

			require.Len(t, epochs, tt.expectedLen)
			meta, ok := epochs[tt.expectedEpoch]
			require.True(t, ok)
			require.Equal(t, tt.expectedEpoch, meta.epoch)
			require.Equal(t, nodes, meta.nodes)
			require.Len(t, meta.eligibleSigners, len(nodes))
			for _, n := range nodes {
				_, ok := meta.eligibleSigners[string(n.Id)]
				require.True(t, ok)
			}
			require.NotNil(t, meta.signatureAggregator)
		})
	}
}

func TestRemoveOldEpochs(t *testing.T) {
	newEpochsMap := func() epochs {
		return epochs{
			1: &epochMetadata{epoch: 1},
			2: &epochMetadata{epoch: 2},
			3: &epochMetadata{epoch: 3},
			4: &epochMetadata{epoch: 4},
		}
	}

	tests := []struct {
		name           string
		startEpoch     uint64
		expectedEpochs []uint64
	}{
		{
			name:           "remove none",
			startEpoch:     0,
			expectedEpochs: []uint64{1, 2, 3, 4},
		},
		{
			name:           "remove some",
			startEpoch:     3,
			expectedEpochs: []uint64{3, 4},
		},
		{
			name:           "remove all",
			startEpoch:     5,
			expectedEpochs: []uint64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEpochsMap()
			e.removeOldEpochs(tt.startEpoch)

			require.Len(t, e, len(tt.expectedEpochs))
			for _, epoch := range tt.expectedEpochs {
				_, ok := e[epoch]
				require.True(t, ok, "expected epoch %d to remain", epoch)
			}
		})
	}
}

// TestCanValidate checks whether the epochs struct properly validates higher epochs
// by keeping track of received sealing blocks.
func TestCanValidate(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	// create our epochs
	e3, e4, e5 := tc.appendSealing(testNodes), tc.appendSealing(testNodes), tc.appendSealing(testNodes)
	b6 := tc.appendBlock()

	sigAggCreator := func(n []common.Node) common.SignatureAggregator {
		return &testutil.TestSignatureAggregator{N: len(n)}
	}

	epochs, err := newEpochs(tc, sigAggCreator)
	require.NoError(t, err)

	// should not be able to validate anything yet
	require.False(t, epochs.canValidate(b6))
	require.False(t, epochs.canValidate(e5))
	require.False(t, epochs.canValidate(e4))
	require.False(t, epochs.canValidate(e3))

	// say epoch 5 has been seen f + 1 times
	epochs[e5.Metadata.Seq] = newEpochMetadata(e5.Metadata.Seq, e5.SealingBlockInfo(), sigAggCreator)

	// we should be able to validate backwards now
	require.False(t, epochs.canValidate(b6))
	require.False(t, epochs.canValidate(e5))
	require.True(t, epochs.canValidate(e4))
	require.False(t, epochs.canValidate(e3))

	epochs[e4.Metadata.Seq] = newEpochMetadata(e4.Metadata.Seq, e4.SealingBlockInfo(), sigAggCreator)
	require.False(t, epochs.canValidate(b6))
	require.False(t, epochs.canValidate(e5))
	require.False(t, epochs.canValidate(e4)) // cannot validate twice
	require.True(t, epochs.canValidate(e3))
}

func newSealingQuorumRound(epoch uint64, numValidators int) *common.QuorumRound {
	validatorSet := make(common.Nodes, numValidators)
	for i := range validatorSet {
		validatorSet[i] = common.Node{Id: common.NodeID{byte(i + 1)}, Weight: 1}
	}

	return &common.QuorumRound{
		Block: newSealingTestBlock(1, epoch, common.Digest{}, &common.SealingBlockInfo{
			ValidatorSet: validatorSet,
		}),
	}
}

// TestCollectedQuorumRound feeds an epochReplicator a sealing-block quorum round
// for an unknown epoch and asserts collectedQuorumRound only confirms the epoch
// once a threshold of distinct validators have voted for the same digest.
func TestCollectedQuorumRound(t *testing.T) {
	tests := []struct {
		name string
		qr   *common.QuorumRound
	}{
		{
			name: "4 validator nodes",
			qr:   newSealingQuorumRound(1, 4),
		},
		{
			name: "16 validator nodes",
			qr:   newSealingQuorumRound(1, 16),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			voters := tt.qr.Block.SealingBlockInfo().ValidatorSet
			// votes required to confirm the epoch.
			threshold := common.F(len(voters)) + 1
			require.GreaterOrEqual(t, len(voters), threshold, "need at least threshold validators to vote with")
			e := newEpochReplicator(testutil.MakeLogger(t, 1), func() common.Nodes {
				return voters
			}, countSignatureAggregatorCreator)

			// Each distinct vote below the threshold leaves the epoch unconfirmed.
			for i := 0; i < threshold-1; i++ {
				require.False(t, e.maybeObserveThresholdResponses(tt.qr.Block.SealingBlockInfo(), tt.qr.Block.BlockHeader(), voters[i].Id))
			}

			// The threshold-th distinct vote for the same digest confirms it.
			require.True(t, e.maybeObserveThresholdResponses(tt.qr.Block.SealingBlockInfo(), tt.qr.Block.BlockHeader(), voters[threshold-1].Id))
		})
	}
}

// TestCollectedSealingBlockInfoOneResponsePerValidator asserts each validator counts
// toward one epoch, the highest it has sent, so a single validator can never reach
// the threshold and re-sending an older sealing block does not discard a newer one.
func TestCollectedSealingBlockInfoOneResponsePerValidator(t *testing.T) {
	qr := newSealingQuorumRound(1, 4)
	info := qr.Block.SealingBlockInfo()
	validators := info.ValidatorSet
	e := newEpochReplicator(testutil.MakeLogger(t, 1), func() common.Nodes {
		return validators
	}, countSignatureAggregatorCreator)

	seq5 := newSealingTestBlock(5, 1, common.Digest{}, info).BlockHeader()
	seq6 := newSealingTestBlock(6, 1, common.Digest{}, info).BlockHeader()

	// threshold is 2, one validator sending distinct sequences never reaches it
	require.False(t, e.maybeObserveThresholdResponses(info, seq6, validators[0].Id))
	require.False(t, e.maybeObserveThresholdResponses(info, seq5, validators[0].Id))

	// voters[0] still counts toward seq 6, not the older seq 5
	require.False(t, e.maybeObserveThresholdResponses(info, seq5, validators[1].Id))
	require.True(t, e.maybeObserveThresholdResponses(info, seq6, validators[1].Id))
}

// TestCollectedSealingBlockInfoIgnoresRemovedValidators asserts a stored response from a node
// that left the latest validator set no longer counts toward the threshold.
func TestCollectedSealingBlockInfoIgnoresRemovedValidators(t *testing.T) {
	qr := newSealingQuorumRound(1, 4)
	info := qr.Block.SealingBlockInfo()
	validators := info.ValidatorSet
	latest := validators
	e := newEpochReplicator(testutil.MakeLogger(t, 1), func() common.Nodes {
		return latest
	}, countSignatureAggregatorCreator)

	bh := qr.Block.BlockHeader()
	require.False(t, e.maybeObserveThresholdResponses(info, bh, validators[0].Id))

	// replace validators[0] so its old vote shouldn't make us reach a threshold of 2
	latest = append(common.Nodes{{Id: common.NodeID{5}, Weight: 1}}, validators[1:]...)
	require.False(t, e.maybeObserveThresholdResponses(info, bh, validators[1].Id))
}

// weightedNodes returns a validator set where the node with ID i+1 has weights[i].
func weightedNodes(weights ...uint64) common.Nodes {
	nodes := make(common.Nodes, len(weights))
	for i, weight := range weights {
		nodes[i] = common.Node{Id: common.NodeID{byte(i + 1)}, Weight: weight}
	}
	return nodes
}

func TestCollectedSealingBlockInfoWeightedByStake(t *testing.T) {
	info := &common.SealingBlockInfo{}
	sealingBlock := newSealingTestBlock(5, 1, common.Digest{1}, info).BlockHeader()
	otherSealingBlock := newSealingTestBlock(5, 1, common.Digest{2}, info).BlockHeader()
	require.NotEqual(t, sealingBlock.Digest, otherSealingBlock.Digest)

	type report struct {
		from     byte
		header   common.BlockHeader
		accepted bool
	}

	for _, tc := range []struct {
		name       string
		validators common.Nodes
		reports    []report
	}{
		{
			name:       "F+1 low-stake reporters are not enough",
			validators: weightedNodes(100, 100, 100, 100, 1, 1, 1),
			reports: []report{
				{from: 5, header: sealingBlock},
				{from: 6, header: sealingBlock},
				{from: 7, header: sealingBlock},
			},
		},
		{
			name: "a single reporter with more than a third of the stake is enough",
			// 200 of 350
			validators: weightedNodes(200, 50, 50, 50),
			reports: []report{
				{from: 1, header: sealingBlock, accepted: true},
			},
		},
		{
			name: "exactly a third of the stake is not enough",
			// 100 of 300, and then 101 of 300
			validators: weightedNodes(100, 100, 99, 1),
			reports: []report{
				{from: 1, header: sealingBlock},
				{from: 4, header: sealingBlock, accepted: true},
			},
		},
		{
			name: "stake reporting different sealing blocks is not pooled",
			// 110 of 400 for each sealing block, and then 200 of 400 for the first one
			validators: weightedNodes(110, 110, 90, 90),
			reports: []report{
				{from: 1, header: sealingBlock},
				{from: 2, header: otherSealingBlock},
				{from: 3, header: sealingBlock, accepted: true},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEpochReplicator(testutil.MakeLogger(t, 1), func() common.Nodes {
				return tc.validators
			}, weightedSignatureAggregatorCreator)

			for i, r := range tc.reports {
				accepted := e.maybeObserveThresholdResponses(info, r.header, common.NodeID{r.from})
				require.Equal(t, r.accepted, accepted, "report %d from node %d", i, r.from)
			}
		})
	}
}

// TestCollectedSealingBlockInfoUsesLatestStake asserts a stored response counts with the stake
// its sender has in the latest validator set, not the stake it had when the response arrived.
func TestCollectedSealingBlockInfoUsesLatestStake(t *testing.T) {
	info := &common.SealingBlockInfo{}
	bh := newSealingTestBlock(5, 1, common.Digest{}, info).BlockHeader()

	latest := weightedNodes(100, 100, 100, 100)
	e := newEpochReplicator(testutil.MakeLogger(t, 1), func() common.Nodes {
		return latest
	}, weightedSignatureAggregatorCreator)

	// 100 of 400
	require.False(t, e.maybeObserveThresholdResponses(info, bh, common.NodeID{1}))

	// Node 1 moves most of its stake to node 4. Under the previous stake, node 2 would bring
	// the reporters to 200 of 400, but under the latest stake they hold only 101 of 400.
	latest = weightedNodes(1, 100, 100, 199)
	require.False(t, e.maybeObserveThresholdResponses(info, bh, common.NodeID{2}))

	// 201 of 400
	require.True(t, e.maybeObserveThresholdResponses(info, bh, common.NodeID{3}))
}
