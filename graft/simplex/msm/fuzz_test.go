// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// noopLogger is a simplex.Logger that discards everything, so buildEpochChain can run
// without a *testing.T and the chain can be built once, outside the fuzz target.
type noopLogger struct{}

func (noopLogger) Fatal(string, ...zap.Field) {}
func (noopLogger) Error(string, ...zap.Field) {}
func (noopLogger) Warn(string, ...zap.Field)  {}
func (noopLogger) Info(string, ...zap.Field)  {}
func (noopLogger) Trace(string, ...zap.Field) {}
func (noopLogger) Debug(string, ...zap.Field) {}
func (noopLogger) Verbo(string, ...zap.Field) {}

// authoritativeField names a consensus field that the verifier reconstructs itself
// (from the parent block and the state machine), rather than copying it from the
// proposer. Tampering with any of these must be rejected, either by an explicit check
// or by the expected-vs-proposed block digest comparison.
type authoritativeField struct {
	name string
	// set overwrites the field with v.
	set func(m *StateMachineMetadata, v uint64)
}

var authoritativeFields = []authoritativeField{
	{"SimplexEpochInfo.PChainReferenceHeight", func(m *StateMachineMetadata, v uint64) {
		m.SimplexEpochInfo.PChainReferenceHeight = v
	}},
	{"SimplexProtocolMetadata.Epoch", func(m *StateMachineMetadata, v uint64) {
		m.SimplexProtocolMetadata.Epoch = v
	}},
	{"SimplexEpochInfo.PrevVMBlockSeq", func(m *StateMachineMetadata, v uint64) {
		m.SimplexEpochInfo.PrevVMBlockSeq = v
	}},
	{"SimplexEpochInfo.SealingBlockSeq", func(m *StateMachineMetadata, v uint64) {
		m.SimplexEpochInfo.SealingBlockSeq = v
	}},
	{"ICMEpochInfo.EpochNumber", func(m *StateMachineMetadata, v uint64) {
		m.ICMEpochInfo.EpochNumber = v
	}},
	{"ICMEpochInfo.EpochStartTime", func(m *StateMachineMetadata, v uint64) {
		m.ICMEpochInfo.EpochStartTime = v
	}},
	{"ICMEpochInfo.PChainEpochHeight", func(m *StateMachineMetadata, v uint64) {
		m.ICMEpochInfo.PChainEpochHeight = v
	}},
}

// numBuiltBlocks is the number of blocks buildEpochChain produces (used to seed the
// fuzzer with one entry per block); buildEpochChain asserts it stays in sync.
const numBuiltBlocks = 8

// FuzzVerifyBlock has one MSM build a full chain of blocks: the first Simplex block on
// top of genesis, blocks transitioning the epoch through every state (normal op,
// collecting approvals, sealing/epoch-sealed), the transition from the first epoch into
// a second epoch, and a block within that second epoch. Those blocks are the fuzzer's
// inputs (selected by index). For each input, a freshly instantiated verifier MSM first
// verifies the unfuzzed block (which must succeed), then verifies a copy whose
// consensus-authoritative metadata has been mutated (which must fail).

// The mutation is applied at the field level (rather than by flipping serialized bytes)
// so the fuzzed block is always well-formed: byte-level mutations of the Canoto encoding
// overwhelmingly corrupt the structure and merely exercise the decoder. Each fuzzed field
// is reconstructed by the verifier from the parent block (or checked against an exact
// value), so any real change is guaranteed to be rejected.
func FuzzVerifyBlock(f *testing.F) {
	for blockIdx := 0; blockIdx < numBuiltBlocks; blockIdx++ {
		for fieldIdx := range authoritativeFields {
			f.Add(blockIdx, fieldIdx, uint64(0))
			f.Add(blockIdx, fieldIdx, uint64(0xffffffffffffffff))
		}
	}

	// Build the chain (and its verifier MSM) once and reuse it across iterations: the
	// blocks are fixed inputs, so there's no need to rebuild them every time.
	blocks, sm := buildEpochChain(f, noopLogger{})

	f.Fuzz(func(t *testing.T, blockIdx, fieldIdx int, value uint64) {

		// Converting to uint wraps negative fuzz inputs into range without the overflow
		// edge case that int negation (e.g. -math.MinInt) would hit.
		bi := int(uint(blockIdx) % uint(len(blocks)))
		fi := int(uint(fieldIdx) % uint(len(authoritativeFields)))
		field := authoritativeFields[fi]

		// Clone the selected block so mutations can't leak into the shared chain that is
		// reused across iterations.
		block := cloneBlock(t, blocks[bi])

		// Model the verifier as knowing the P-chain exactly up to the height the block
		// references — the minimal knowledge required to verify it, mirroring production
		// where GetPChainHeightForVerifying returns the verifier's latest observed height.
		sm.GetPChainHeightForProposing = func() uint64 { return block.Metadata.PChainHeight }
		sm.GetPChainHeightForVerifying = func() uint64 { return block.Metadata.PChainHeight }

		// The unfuzzed block built by the chain MSM must verify.
		require.NoError(t, sm.VerifyBlock(context.Background(), block),
			"unfuzzed block at index %d must verify", bi)

		// Mutating an authoritative field must make the block fail verification.
		fuzzedMD := block.Metadata
		field.set(&fuzzedMD, value)

		if fieldIdx%2 == 1 && block.Metadata.AuxiliaryInfoBatch == nil {
			// value|1 forces a non-zero PrevAuxInfoSeq: collecting-approvals blocks reconstruct it
			// as 0 (parent has no aux info), so value 0 would match and slip through unrejected.
			fuzzedMD.AuxiliaryInfoBatch = &AuxiliaryInfoBatch{PrevAuxInfoSeq: value | 1}
		}

		if bytes.Equal(fuzzedMD.MarshalCanoto(), block.Metadata.MarshalCanoto()) {
			t.Skip() // no-op mutation
		}
		fuzzed := &StateMachineBlock{InnerBlock: block.InnerBlock, Metadata: fuzzedMD}
		require.Error(t, sm.VerifyBlock(context.Background(), fuzzed),
			"block at index %d with mutated %s must be rejected", bi, field.name)
	})
}

// cloneBlock returns a deep copy of b: the metadata is round-tripped through its Canoto
// encoding so the copy shares no pointers with b, and the inner block is reused as-is
// since verification only reads it.
func cloneBlock(t *testing.T, b *StateMachineBlock) *StateMachineBlock {
	var md StateMachineMetadata
	require.NoError(t, md.UnmarshalCanoto(b.Metadata.MarshalCanoto()))
	return &StateMachineBlock{InnerBlock: b.InnerBlock, Metadata: md}
}

// buildEpochChain drives a single MSM from genesis through a full epoch lifecycle and
// into a second epoch, and returns the blocks it built (seq 1..numBuiltBlocks) together
// with a separate verifier MSM preloaded with the whole chain.
//
// The blocks exercise every build/verify state:
//
//	block1: stateFirstSimplexBlock (zero block, epoch 1)
//	block2: stateBuildBlockNormalOp (epoch 1)
//	block3: stateBuildBlockNormalOp, observes a validator-set change (epoch 1)
//	block4: stateBuildCollectingApprovals (epoch 1)
//	block5: stateBuildCollectingApprovals (epoch 1)
//	block6: sealing block, built while collecting the quorum approval (epoch 1)
//	block7: stateBuildBlockEpochSealed -> first block of epoch 2
//	block8: stateBuildBlockNormalOp (epoch 2)
func buildEpochChain(tb testing.TB, logger common.Logger) ([]*StateMachineBlock, *StateMachine) {
	node1 := [20]byte{1}
	node2 := [20]byte{2}
	node3 := [20]byte{3}

	validatorSet1 := NodeBLSMappings{
		{NodeID: node1, BLSKey: []byte{1}, Weight: 1},
		{NodeID: node2, BLSKey: []byte{2}, Weight: 1},
		{NodeID: node3, BLSKey: []byte{3}, Weight: 1},
	}
	validatorSet2 := NodeBLSMappings{
		{NodeID: node1, BLSKey: []byte{1}, Weight: 1},
		{NodeID: node2, BLSKey: []byte{4}, Weight: 1},
		{NodeID: node3, BLSKey: []byte{5}, Weight: 1},
	}

	pChainHeight1 := uint64(100)
	pChainHeight2 := uint64(200)

	// Align to a whole second so the second-granular ICM epoch boundary is deterministic
	// (see the comment in TestMSMFullEpochLifecycle).
	startTime := time.Now().Truncate(time.Second)

	getValidatorSet := func(height uint64) (NodeBLSMappings, error) {
		if height >= pChainHeight2 {
			return validatorSet2, nil
		}
		return validatorSet1, nil
	}

	// Use a genesis block anchored at startTime: the zero block carries over the last
	// non-Simplex block's timestamp, so it must not be ahead of the blocks built after it.
	genesis := StateMachineBlock{InnerBlock: &InnerBlock{BlockHeight: 0, TS: startTime, bytes: []byte{0}}}

	currentPChainHeight := pChainHeight1
	currentTime := startTime

	sm, tc := newStateMachineWithLogger(tb, logger)
	// This chain exercises the epoch lifecycle, not auxiliary info. Use an app whose
	// history is always final so approvals are collected from the first collecting
	// round (the builder only collects approvals once the aux info history is ready).
	sm.AuxiliaryInfoApp = &noopTestAuxInfoApp{}
	sm.GetValidatorSet = getValidatorSet
	sm.GetPChainHeightForProposing = func() uint64 { return currentPChainHeight }
	sm.GetPChainHeightForVerifying = func() uint64 { return currentPChainHeight }
	sm.GetTime = func() time.Time { return currentTime }
	sm.GenesisValidatorSet = validatorSet1
	sm.LastNonSimplexBlockPChainHeight = pChainHeight1
	sm.LastNonSimplexInnerBlock = genesis.InnerBlock
	tc.blockStore[0] = &outerBlock{block: genesis}

	ctx := context.Background()

	addBlock := func(seq uint64, b *StateMachineBlock, fin *common.Finalization) {
		tc.blockStore[seq] = &outerBlock{block: *b, finalization: fin}
	}
	nextInner := func(h uint64) *InnerBlock {
		return &InnerBlock{
			TS:          startTime.Add(time.Duration(h) * time.Millisecond),
			BlockHeight: h,
			bytes:       []byte{byte(h)},
		}
	}
	build := func(seq, round, epoch uint64, prev *StateMachineBlock) *StateMachineBlock {
		var prevDigest common.Digest
		if prev != nil {
			prevDigest = prev.Digest()
		} else {
			prevDigest = genesis.Digest()
		}
		md := common.ProtocolMetadata{Seq: seq, Round: round, Epoch: epoch, Prev: prevDigest}
		block, err := sm.BuildBlock(ctx, md, emptyBlacklist)
		require.NoError(tb, err)
		return block
	}

	// ----- Epoch 1 -----

	// block1: the zero block, finalized so it can later serve as the epoch's reference for
	// the validator-set change and the sealing-block computation.
	tc.blockBuilder.Block = nextInner(1)
	block1 := build(1, 0, 1, nil)
	addBlock(1, block1, &common.Finalization{})
	sm.LatestPersistedHeight = 1

	// block2: a normal in-epoch block.
	currentTime = startTime.Add(2 * time.Millisecond)
	tc.blockBuilder.Block = nextInner(2)
	block2 := build(2, 1, 1, block1)
	addBlock(2, block2, nil)

	// block3: a normal block that observes a validator-set change, so its successor must
	// collect approvals for the next epoch.
	currentPChainHeight = pChainHeight2
	currentTime = startTime.Add(time.Second + 3*time.Millisecond)
	tc.blockBuilder.Block = nextInner(3)
	block3 := build(3, 2, 1, block2)
	addBlock(3, block3, nil)
	require.NoError(tb, sm.OnBlockIndex(*block3))

	// The noopTestAuxInfoApp is always "ready" with an empty aux info history, and
	// LastHistoryDigest returns the zero digest for an empty history. That zero value is the
	// candidate digest the builder signs over, so peer approvals must carry it to survive
	// sanitizeApprovals' digest filter.
	var auxInfoDigest common.Digest

	// block4 & block5: collecting-approvals blocks (1/3 then 2/3, not enough to seal).
	sm.HandleApproval(&common.ValidatorSetApproval{NodeID: node1, PChainHeight: pChainHeight2, AuxInfoDigest: auxInfoDigest, Signature: signApproval(pChainHeight2, auxInfoDigest)})
	currentTime = startTime.Add(time.Second + 4*time.Millisecond)
	tc.blockBuilder.Block = nextInner(4)
	block4 := build(4, 3, 1, block3)
	addBlock(4, block4, nil)

	sm.HandleApproval(&common.ValidatorSetApproval{NodeID: node2, PChainHeight: pChainHeight2, AuxInfoDigest: auxInfoDigest, Signature: signApproval(pChainHeight2, auxInfoDigest)})
	currentTime = startTime.Add(time.Second + 5*time.Millisecond)
	tc.blockBuilder.Block = nextInner(5)
	block5 := build(5, 4, 1, block4)
	addBlock(5, block5, nil)

	// block6: the sealing block (3/3 approvals). Its successor is in stateBuildBlockEpochSealed.
	sm.HandleApproval(&common.ValidatorSetApproval{NodeID: node3, PChainHeight: pChainHeight2, AuxInfoDigest: auxInfoDigest, Signature: signApproval(pChainHeight2, auxInfoDigest)})
	currentTime = startTime.Add(time.Second + 6*time.Millisecond)
	tc.blockBuilder.Block = nextInner(6)
	block6 := build(6, 5, 1, block5)
	require.Equal(tb, stateBuildBlockEpochSealed, block6.Metadata.NextState())
	// Finalize the sealing block so the epoch transition can proceed.
	addBlock(6, block6, &common.Finalization{})

	// ----- Epoch 2 (its epoch number is the sealing block's sequence, 6) -----

	// block7: the first block of the new epoch, built in stateBuildBlockEpochSealed.
	sealingSeq := uint64(6)
	currentTime = startTime.Add(time.Second + 7*time.Millisecond)
	tc.blockBuilder.Block = nextInner(7)
	block7 := build(7, 6, sealingSeq, block6)
	addBlock(7, block7, nil)

	// block8: a normal in-epoch block in the second epoch.
	currentTime = startTime.Add(time.Second + 8*time.Millisecond)
	tc.blockBuilder.Block = nextInner(8)
	block8 := build(8, 7, sealingSeq, block7)
	addBlock(8, block8, nil)

	blocks := []*StateMachineBlock{block1, block2, block3, block4, block5, block6, block7, block8}
	require.Len(tb, blocks, numBuiltBlocks)

	// Build a separate verifier MSM with its own copy of the fully populated store.
	verifier, vtc := newStateMachineWithLogger(tb, logger)
	verifier.AuxiliaryInfoApp = &noopTestAuxInfoApp{}
	vtc.blockStore = tc.blockStore.clone()
	verifier.GetBlock = vtc.blockStore.getBlock
	verifier.GetValidatorSet = getValidatorSet
	// GetPChainHeightForVerifying is set by the caller per verified block (see FuzzVerifyBlock).
	// A time safely after every block, so the not-too-far-in-future check always passes.
	verifyTime := startTime.Add(2 * time.Second)
	verifier.GetTime = func() time.Time { return verifyTime }
	verifier.GenesisValidatorSet = validatorSet1
	verifier.LastNonSimplexBlockPChainHeight = pChainHeight1
	verifier.LastNonSimplexInnerBlock = genesis.InnerBlock

	return blocks, verifier
}
