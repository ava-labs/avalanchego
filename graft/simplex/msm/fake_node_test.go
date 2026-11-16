// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// emptyAuxInfoDigest is the candidate digest approvals commit to when the auxiliary info
// history is empty: LastHistoryDigest returns the zero digest in that case.
var emptyAuxInfoDigest common.Digest

func TestFakeNodeEpochChangesDespiteEmptyMempool(t *testing.T) {
	validatorSetRetriever := validatorSetRetriever{
		resultMap: map[uint64]NodeBLSMappings{
			0:   {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 1, NodeID: [20]byte{2}}},
			100: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 1, NodeID: [20]byte{2}}},
			200: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 2, NodeID: [20]byte{2}},
				{BLSKey: []byte{3}, Weight: 1, NodeID: [20]byte{3}}},
			300: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 2, NodeID: [20]byte{2}},
				{BLSKey: []byte{3}, Weight: 3, NodeID: [20]byte{3}}, {BLSKey: []byte{4}, Weight: 1, NodeID: [20]byte{4}}},
		},
	}

	var pChainHeight atomic.Uint64
	pChainHeight.Store(100)
	node := newFakeNode(t)
	myID := [20]byte{1}
	node.sm.MyNodeID = myID[:]
	node.sm.AuxiliaryInfoApp = &noopTestAuxInfoApp{}
	node.sm.GetValidatorSet = validatorSetRetriever.getValidatorSet
	node.sm.GetPChainHeightForProposing = func() uint64 {
		return pChainHeight.Load()
	}
	node.sm.GetPChainHeightForVerifying = func() uint64 {
		return pChainHeight.Load()
	}
	node.mempoolEmpty = true
	node.sm.MaxBlockBuildingWaitTime = 100 * time.Millisecond

	// This will be the zero block
	node.buildAndNotarizeBlock()
	if node.canFinalize() {
		node.tryFinalizeNextBlock()
	}

	pChainHeight.Store(200)

	firstEpoch := node.Epoch()

	for node.Epoch() == firstEpoch {
		node.buildAndNotarizeBlock()
		if node.canFinalize() {
			node.tryFinalizeNextBlock()
		}
		if flipCoin() {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{1}, PChainHeight: 200, Signature: signApproval(200, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		} else {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{2}, PChainHeight: 200, Signature: signApproval(200, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		}

		if node.isLastBlockSealing() {
			pChainHeight.Store(300)
		}
	}
}

func TestFakeNode(t *testing.T) {
	validatorSetRetriever := validatorSetRetriever{
		resultMap: map[uint64]NodeBLSMappings{
			0:   {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 1, NodeID: [20]byte{2}}},
			100: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 1, NodeID: [20]byte{2}}},
			200: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 2, NodeID: [20]byte{2}},
				{BLSKey: []byte{3}, Weight: 1, NodeID: [20]byte{3}}},
			300: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 2, NodeID: [20]byte{2}},
				{BLSKey: []byte{3}, Weight: 3, NodeID: [20]byte{3}}, {BLSKey: []byte{4}, Weight: 1, NodeID: [20]byte{4}}},
		},
	}

	var pChainHeight atomic.Uint64
	pChainHeight.Store(100)
	node := newFakeNode(t)
	myID := [20]byte{1}
	node.sm.MyNodeID = myID[:]
	node.sm.AuxiliaryInfoApp = &noopTestAuxInfoApp{}
	node.sm.GetValidatorSet = validatorSetRetriever.getValidatorSet
	node.sm.GetPChainHeightForProposing = func() uint64 {
		return pChainHeight.Load()
	}
	node.sm.GetPChainHeightForVerifying = func() uint64 {
		return pChainHeight.Load()
	}

	// Create some blocks and finalize them, until we reach height 10
	for node.Height() < 10 {
		node.act()
	}

	// Next, we increase the P-Chain height, which should cause the node to update its validator set and move to the new epoch.
	pChainHeight.Store(200)

	epoch := node.Epoch()
	for node.Epoch() == epoch {
		node.act()
		if flipCoin() {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{1}, PChainHeight: 200, Signature: signApproval(200, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		} else {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{2}, PChainHeight: 200, Signature: signApproval(200, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		}
	}

	t.Log("Epoch:", node.Epoch())
	require.Greater(t, node.Epoch(), uint64(1))

	// Finally, we increase the P-Chain height again, which should cause the node to update its validator set and move to the new epoch.

	pChainHeight.Store(300)

	epoch = node.Epoch()
	for node.Epoch() == epoch {
		node.act()
		if flipCoin() {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{2}, PChainHeight: 300, Signature: signApproval(300, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		} else {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{3}, PChainHeight: 300, Signature: signApproval(300, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		}
	}

	t.Log("Epoch:", node.Epoch())
	require.Greater(t, node.Epoch(), epoch)
}

func TestFakeNodeEmptyMempool(t *testing.T) {
	validatorSetRetriever := validatorSetRetriever{
		resultMap: map[uint64]NodeBLSMappings{
			0:   {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 1, NodeID: [20]byte{2}}},
			100: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 1, NodeID: [20]byte{2}}},
			200: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 2, NodeID: [20]byte{2}},
				{BLSKey: []byte{3}, Weight: 1, NodeID: [20]byte{3}}},
			300: {{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 2, NodeID: [20]byte{2}},
				{BLSKey: []byte{3}, Weight: 3, NodeID: [20]byte{3}}, {BLSKey: []byte{4}, Weight: 1, NodeID: [20]byte{4}}},
		},
	}

	var pChainHeight uint64 = 100
	node := newFakeNode(t)
	myID := [20]byte{1}
	node.sm.MyNodeID = myID[:]
	node.sm.AuxiliaryInfoApp = &noopTestAuxInfoApp{}
	node.sm.MaxBlockBuildingWaitTime = 100 * time.Millisecond
	node.sm.GetValidatorSet = validatorSetRetriever.getValidatorSet
	node.sm.GetPChainHeightForProposing = func() uint64 {
		return pChainHeight
	}
	node.sm.GetPChainHeightForVerifying = func() uint64 {
		return pChainHeight
	}

	// Create some blocks and finalize them, until we reach height 10
	for node.Height() < 10 {
		node.act()
	}

	// Next, we increase the P-Chain height, which should cause the node to update its validator set and move to the new epoch.
	pChainHeight = 200

	// However, we mark the mempool as empty, which should cause the node to wait until it sees a change in the P-Chain height, rather than building blocks on top of the old epoch.
	node.mempoolEmpty = true

	// We build blocks until the sealing block is finalized.
	for node.lastFinalizedBlock().Metadata.SimplexEpochInfo.BlockValidationDescriptor == nil {
		node.act()
		if flipCoin() {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{1}, PChainHeight: 200, Signature: signApproval(200, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		} else {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{2}, PChainHeight: 200, Signature: signApproval(200, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		}
	}

	node.mempoolEmpty = false

	// Build a new block and check that the node has transitioned to the new epoch,
	// rather than building a block on top of the old epoch.
	height := node.Height()

	for node.Height() == height {
		node.act()
	}
	require.Greater(t, node.Epoch(), uint64(1))

	t.Log("Epoch:", node.Epoch())

	epoch := node.Epoch()
	require.Greater(t, epoch, uint64(1))

	// Finally, we increase the P-Chain height again, which should cause the node to update its validator set and move to the new epoch.

	pChainHeight = 300

	for node.Height() < 30 {
		node.act()
		if flipCoin() {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{2}, PChainHeight: 300, Signature: signApproval(300, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		} else {
			node.sm.HandleApproval(&common.ValidatorSetApproval{NodeID: [20]byte{3}, PChainHeight: 300, Signature: signApproval(300, emptyAuxInfoDigest), AuxInfoDigest: emptyAuxInfoDigest})
		}
	}

	t.Log("Epoch:", node.Epoch())
	require.Greater(t, node.Epoch(), epoch)
	require.Equal(t, node.Height(), uint64(30))
}

type innerBlock struct {
	testutil.InnerBlock
	Prev [32]byte
}

type blockState struct {
	block      StateMachineBlock
	finalized  bool
	innerBlock avalanchego.VMBlock
}

type fakeNode struct {
	t            *testing.T
	epoch        uint64
	sm           *StateMachine
	mempoolEmpty bool
	// blocks holds notarized blocks in order. Finalized blocks always form a
	// prefix: all finalized entries precede all non-finalized entries.
	blocks []blockState
}

func (fn *fakeNode) WaitForProgress(ctx context.Context, pChainHeight uint64) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			height := fn.sm.GetPChainHeightForProposing()
			if height != pChainHeight {
				return nil
			}
		}
	}
}

func (fn *fakeNode) WaitForPendingBlock(ctx context.Context) {
	if fn.mempoolEmpty {
		<-ctx.Done()
		return
	}
}

func newFakeNode(t *testing.T) *fakeNode {
	sm, _ := newStateMachine(t)

	fn := &fakeNode{
		t:     t,
		sm:    sm,
		epoch: 1,
	}

	fn.sm.BlockBuilder = fn
	fn.sm.PChainProgressListener = fn

	fn.sm.GetBlock = func(seq uint64, digest common.Digest) (StateMachineBlock, *common.Finalization, error) {
		if seq == 0 {
			return genesisBlock, nil, nil
		}
		for _, bs := range fn.blocks {
			match := bs.block.Digest() == digest
			if !match {
				md := bs.block.Metadata.SimplexProtocolMetadata
				match = md.Seq == seq
			}
			if match {
				var fin *common.Finalization
				if bs.finalized {
					fin = &common.Finalization{}
				}
				return bs.block, fin, nil
			}
		}

		require.Failf(t, "not found block", "height: %d", seq)
		return StateMachineBlock{}, nil, fmt.Errorf("block not found")
	}

	return fn
}

// lastFinalizedBlock returns the most recently finalized block.
// Panics if nothing has been finalized.
func (fn *fakeNode) lastFinalizedBlock() StateMachineBlock {
	for i := len(fn.blocks) - 1; i >= 0; i-- {
		if fn.blocks[i].finalized {
			return fn.blocks[i].block
		}
	}
	panic("no finalized block")
}

func (fn *fakeNode) Height() uint64 {
	var count uint64
	for _, bs := range fn.blocks {
		if bs.finalized {
			count++
		}
	}
	return count
}

func (fn *fakeNode) Epoch() uint64 {
	return fn.blocks[len(fn.blocks)-1].block.Metadata.SimplexProtocolMetadata.Epoch
}

func (fn *fakeNode) act() {
	if fn.canFinalize() && flipCoin() {
		fn.tryFinalizeNextBlock()
		return
	}

	if flipCoin() {
		return
	}

	fn.buildAndNotarizeBlock()
}

func (fn *fakeNode) canFinalize() bool {
	return fn.nextUnfinalizedIndex() < len(fn.blocks)
}

func (fn *fakeNode) nextUnfinalizedIndex() int {
	for i, bs := range fn.blocks {
		if !bs.finalized {
			return i
		}
	}
	return len(fn.blocks)
}

func (fn *fakeNode) tryFinalizeNextBlock() {
	nextIndex := fn.nextUnfinalizedIndex()

	if fn.isNextBlockTelock(nextIndex) {
		return
	}

	fn.blocks[nextIndex].finalized = true
	block := fn.blocks[nextIndex].block

	md := block.Metadata.SimplexProtocolMetadata

	fn.sm.LatestPersistedHeight = md.Seq
	fn.t.Logf("Finalized block at height %d with epoch %d", md.Seq, md.Epoch)

	require.NoError(fn.t, fn.sm.OnBlockIndex(block))

	// If we just finalized a sealing block, trim trailing Telock blocks.
	if block.Metadata.SimplexEpochInfo.BlockValidationDescriptor != nil {
		fn.blocks = fn.blocks[:nextIndex+1]
		fn.t.Logf("Trimmed notarized blocks, new length: %d", len(fn.blocks))
		prevEpoch := fn.epoch
		fn.epoch = md.Seq
		fn.t.Logf("Epoch change from %d to %d", prevEpoch, fn.epoch)

		// The instance starts every epoch with a fresh state machine.
		sm, err := NewStateMachine(fn.sm.Config)
		require.NoError(fn.t, err)
		fn.sm = sm
	}
}

func (fn *fakeNode) isNextBlockTelock(nextIndex int) bool {
	if nextIndex == 0 {
		return false
	}
	return fn.blocks[nextIndex].block.Metadata.SimplexEpochInfo.SealingBlockSeq > 0
}

func (fn *fakeNode) isLastBlockSealing() bool {
	if len(fn.blocks) == 0 {
		return false
	}
	last := fn.blocks[len(fn.blocks)-1]
	return last.block.Metadata.SimplexEpochInfo.BlockValidationDescriptor != nil
}

func (fn *fakeNode) buildAndNotarizeBlock() {
	vmBlock, block := fn.buildBlock()
	require.NoError(fn.t, fn.sm.VerifyBlock(context.Background(), block))

	fn.blocks = append(fn.blocks, blockState{block: *block, innerBlock: vmBlock})
}

func (fn *fakeNode) buildBlock() (avalanchego.VMBlock, *StateMachineBlock) {
	parentBlock := fn.getParentBlock()

	lastMD, prevBlockDigest := fn.prepareMetadataAndPrevBlockDigest()

	_, finalization, err := fn.sm.GetBlock(lastMD.Seq, prevBlockDigest)
	require.NoError(fn.t, err)

	finalizedString := "not finalized"
	if finalization != nil {
		finalizedString = "finalized"
	}

	fn.t.Logf("Building a block on top of %s parent with epoch %d", finalizedString, parentBlock.Metadata.SimplexProtocolMetadata.Epoch)

	block, err := fn.sm.BuildBlock(context.Background(), common.ProtocolMetadata{
		Seq:   lastMD.Seq + 1,
		Round: lastMD.Round + 1,
		Epoch: fn.epoch,
		Prev:  prevBlockDigest,
	}, common.Blacklist{})
	require.NoError(fn.t, err)

	return block.InnerBlock, block
}

func (fn *fakeNode) prepareMetadataAndPrevBlockDigest() (*common.ProtocolMetadata, common.Digest) {
	var lastMD *common.ProtocolMetadata
	lastBlockDigest := genesisBlock.Digest()
	if len(fn.blocks) > 0 {
		lastBlock := fn.blocks[len(fn.blocks)-1].block
		lastBlockDigest = lastBlock.Digest()
		lastMD = &lastBlock.Metadata.SimplexProtocolMetadata
	} else {
		lastMD = &common.ProtocolMetadata{
			Prev:  lastBlockDigest,
			Epoch: 1,
		}
	}
	return lastMD, lastBlockDigest
}

func (fn *fakeNode) BuildBlock(ctx context.Context, _ uint64) (avalanchego.VMBlock, error) {
	if fn.mempoolEmpty {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	// Count the number of inner blocks in the chain
	var count int
	for _, bs := range fn.blocks {
		if bs.block.InnerBlock != nil {
			count++
		}
	}

	vmBlock := &innerBlock{
		Prev: fn.getLastVMBlockDigest(),
		InnerBlock: testutil.InnerBlock{
			Content:     randomBuff(10),
			TS:          time.Now(),
			BlockHeight: uint64(count),
		},
	}
	return vmBlock, nil
}

func (fn *fakeNode) getParentBlock() StateMachineBlock {
	if len(fn.blocks) > 0 {
		return fn.blocks[len(fn.blocks)-1].block
	}
	gb := genesisBlock.InnerBlock.(*testutil.InnerBlock)
	return StateMachineBlock{
		InnerBlock: &innerBlock{
			InnerBlock: *gb,
		},
	}
}

func (fn *fakeNode) getLastVMBlockDigest() common.Digest {
	for i := len(fn.blocks) - 1; i >= 0; i-- {
		if fn.blocks[i].block.InnerBlock != nil {
			return fn.blocks[i].block.Digest()
		}
	}
	return genesisBlock.Digest()
}

func randomBuff(n int) []byte {
	buff := make([]byte, n)
	_, err := rand.Read(buff)
	if err != nil {
		panic(err)
	}
	return buff
}

func flipCoin() bool {
	buff := make([]byte, 1)
	_, err := rand.Read(buff)
	if err != nil {
		panic(err)
	}

	lsb := buff[0] & 1

	return lsb == 1
}
