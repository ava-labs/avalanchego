// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nonvalidator

import (
	"context"
	"fmt"
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

var testNodes = common.Nodes{
	{Id: common.NodeID{1}, Weight: 1},
	{Id: common.NodeID{2}, Weight: 1},
	{Id: common.NodeID{3}, Weight: 1},
	{Id: common.NodeID{4}, Weight: 1},
}

var genesis = testutil.NewTestBlock(common.ProtocolMetadata{
	Seq:   0,
	Round: 0,
	Epoch: 0,
}, common.Blacklist{})

type messageInfo struct {
	msg  *common.Message
	from common.NodeID
	to   common.NodeID
}

// testChain is a helper that book-keeps the current chain-tip, alongside any
// blocks and finalizations indexed on the chain. It allows easy creation of block and sealing blocks.
// It also manages validator sets, and the SignatureAggregatorCreator required by non-validators to verify finalizations.
type testChain struct {
	*testutil.InMemStorage
	t *testing.T

	// seq, epoch, prev, sealingBlockHash defines the current tip of testChain.
	// the next block uses seq + 1, epoch, prevDigest = digest, etc...
	seq              uint64
	epoch            uint64
	digest           common.Digest
	sealingBlockHash common.Digest

	validatorSets map[uint64]common.Nodes
}

func (c *testChain) String() string {
	return fmt.Sprintf("TestChain: Current Epoch: %d, Current Seq: %d", c.epoch, c.seq)
}

// newSeededChain returns a testChain whose storage is indexed through seq=lastSeq:
//
//	seq 0           — genesis (epoch 0)
//	seq 1           — sealing block opening epoch 1 (validatorSet = testNodes)
//	seq 2..lastSeq  — epoch 1 blocks
func newSeededChain(t *testing.T, nodes common.Nodes, lastSeq uint64) *testChain {
	require.GreaterOrEqual(t, lastSeq, uint64(1), "lastSeq must be >= 1 (0 is genesis, 1 is the first epoch's sealing block)")

	tc := &testChain{
		InMemStorage:  testutil.NewInMemStorage(),
		t:             t,
		digest:        genesis.Digest,
		validatorSets: make(map[uint64]common.Nodes),
		seq:           0, // set for clarity
		epoch:         0,
	}
	require.NoError(t, tc.Index(context.Background(), genesis, common.Finalization{}))
	tc.validatorSets[0] = nodes

	// firstSimplex comes after genesis
	sealingBlock := tc.appendFirstSimplexAfterGenesis(nodes)
	finalization := tc.newFinalization(sealingBlock)
	require.NoError(t, tc.Index(context.Background(), sealingBlock, finalization))

	for tc.seq < lastSeq {
		b := tc.appendBlock()
		finalization := tc.newFinalization(b)

		require.NoError(t, tc.Index(context.Background(), b, finalization))
	}

	return tc
}

// newSnowToSimplexChain returns a testChain whose storage is indexed through seq=lastSnowSeq:
func newSnowToSimplexChain(t *testing.T, lastSnowSeq uint64) *testChain {
	require.GreaterOrEqual(t, lastSnowSeq, uint64(1), "genesis must be indexed")

	tc := &testChain{
		InMemStorage:  testutil.NewInMemStorage(),
		t:             t,
		digest:        genesis.Digest,
		validatorSets: make(map[uint64]common.Nodes),
		seq:           0,
		epoch:         0,
	}

	// genesis
	require.NoError(t, tc.Index(context.Background(), genesis, common.Finalization{}))

	for tc.seq < lastSnowSeq {
		b := tc.appendBlock()
		require.NoError(t, tc.Index(context.Background(), b, common.Finalization{}))
	}

	require.Equal(t, tc.seq, lastSnowSeq)
	return tc
}

// appendBlock advances the chain by one non-sealing block in the current
// epoch. The block is constructed but NOT indexed.
func (tc *testChain) appendBlock() *testutil.TestBlock {
	tc.seq++
	block := newBlock(tc.seq, tc.epoch, tc.digest)
	tc.digest = block.Digest
	return block
}

func (tc *testChain) newFinalization(b common.VerifiedBlock) common.Finalization {
	nodes, ok := tc.validatorSets[b.BlockHeader().Epoch]
	require.True(tc.t, ok, "Validator set expected to have before creating a new finalization", b.BlockHeader().Epoch, b.BlockHeader().Seq)
	sigAgg := tc.signatureAggregatorCreator(nodes)
	finalization, _ := testutil.NewFinalizationRecord(tc.t, sigAgg, b, nodes.NodeIDs())

	return finalization
}

// appendSealing advances the chain by one sealing block announcing nextEpoch
// and validatorSet. The sealing block's metadata.Epoch is the current epoch;
// subsequent appendBlock calls live in nextEpoch. Not indexed.
func (tc *testChain) appendSealing(validatorSet common.Nodes) *sealingTestBlock {
	tc.seq++

	block := newSealingTestBlock(tc.seq, tc.epoch, tc.digest, &common.SealingBlockInfo{
		ValidatorSet:         validatorSet,
		PrevSealingBlockHash: tc.sealingBlockHash,
	})
	tc.digest = block.Digest
	tc.epoch = tc.seq
	tc.validatorSets[tc.epoch] = validatorSet
	tc.sealingBlockHash = block.Digest
	return block
}

// the first simplex block must return sealing block information
func (tc *testChain) appendFirstSimplexAfterGenesis(validatorSet common.Nodes) *sealingTestBlock {
	lastBlock, _, err := tc.Retrieve(tc.seq)
	tc.seq++
	firstEverEpoch := tc.seq
	require.NoError(tc.t, err)

	block := newSealingTestBlock(tc.seq, firstEverEpoch, tc.digest, &common.SealingBlockInfo{
		ValidatorSet:         validatorSet,
		PrevSealingBlockHash: lastBlock.BlockHeader().Digest,
	})
	tc.digest = block.Digest
	tc.epoch = firstEverEpoch
	tc.sealingBlockHash = block.Digest
	tc.validatorSets[firstEverEpoch] = validatorSet
	return block
}

func (tc *testChain) signatureAggregatorCreator(nodes []common.Node) common.SignatureAggregator {
	return countSignatureAggregatorCreator(nodes)
}

// countSignatureAggregatorCreator creates a SignatureAggregator that counts the signers belonging to nodes,
// and requires a quorum of them for IsQuorum, and F+1 of them to exceed the Byzantine threshold.
func countSignatureAggregatorCreator(nodes []common.Node) common.SignatureAggregator {
	// countMembers counts the signers that belong to nodes.
	countMembers := func(signatures []common.NodeID) int {
		count := 0
		nodeSet := make(map[string]struct{})
		for _, node := range nodes {
			nodeSet[node.Id.String()] = struct{}{}
		}

		for _, sig := range signatures {
			if _, ok := nodeSet[sig.String()]; ok {
				count++
			}
		}

		return count
	}
	return &testutil.TestSignatureAggregator{
		IsQuorumFunc: func(signatures []common.NodeID) bool {
			return countMembers(signatures) >= common.Quorum(len(nodes))
		},
		AboveByzantineThresholdFunc: func(signatures []common.NodeID) bool {
			return countMembers(signatures) >= common.F(len(nodes))+1
		},
		N: len(nodes),
	}
}

// weightedSignatureAggregatorCreator creates a SignatureAggregator whose quorum is more than two thirds of the stake,
// and whose Byzantine threshold is exceeded by more than a third of the stake.
func weightedSignatureAggregatorCreator(nodes []common.Node) common.SignatureAggregator {
	weights := make(map[string]uint64, len(nodes))
	var totalWeight uint64
	for _, node := range nodes {
		weights[string(node.Id)] = node.Weight
		totalWeight += node.Weight
	}
	signersWeight := func(signers []common.NodeID) uint64 {
		var weight uint64
		for _, signer := range signers {
			weight += weights[string(signer)]
		}
		return weight
	}
	return &testutil.TestSignatureAggregator{
		N: len(nodes),
		IsQuorumFunc: func(signers []common.NodeID) bool {
			return signersWeight(signers)*3 > totalWeight*2
		},
		AboveByzantineThresholdFunc: func(signers []common.NodeID) bool {
			return signersWeight(signers)*3 > totalWeight
		},
	}
}

// indexEpochs indexes sealing blocks at epochs, and normal blocks in between
func (tc *testChain) indexEpochs(epochs ...uint64) {
	// ensure that the new epoch we are adding is not already indexed
	require.Greater(tc.t, epochs[0], tc.seq)

	for _, epoch := range epochs {
		for tc.seq < epoch-1 {
			b := tc.appendBlock()
			finalization := tc.newFinalization(b)
			require.NoError(tc.t, tc.Index(context.Background(), b, finalization))
		}
		validatorSet, ok := tc.validatorSets[tc.epoch]
		require.True(tc.t, ok)

		newNodes := append(validatorSet, common.Node{
			Id:     common.NodeID{byte(epoch)},
			Weight: 1,
		})
		sealing := tc.appendSealing(newNodes)
		finalization := tc.newFinalization(sealing)
		require.NoError(tc.t, tc.Index(context.Background(), sealing, finalization))
	}
}

func (tc *testChain) nodes() common.Nodes {
	latestValidatorSet, ok := tc.validatorSets[tc.epoch]
	require.True(tc.t, ok)

	return latestValidatorSet
}
