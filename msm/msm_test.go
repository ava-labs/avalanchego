// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

var (
	emptyBlacklist = common.Blacklist{}
)

func TestMSMBuildAndVerifyBlocksAfterGenesis(t *testing.T) {
	validMD := common.ProtocolMetadata{
		Round: 1,
		Seq:   1,
		Epoch: 1,
		Prev:  genesisBlock.Digest(),
	}

	for _, testCase := range []struct {
		name        string
		md          common.ProtocolMetadata
		err         error
		configure   func(*StateMachine, *testConfig)
		mutateBlock func(*StateMachineBlock)
	}{
		{
			name: "correct information",
			md:   validMD,
		},
		{
			name: "verifying a genesis block",
			md:   validMD,
			mutateBlock: func(block *StateMachineBlock) {
				md := block.Metadata.SimplexProtocolMetadata
				md.Seq = 0
				block.Metadata.SimplexProtocolMetadata = md
			},
			err: errBuiltGenesisInnerBlock,
		},
		{
			name: "previous block not found",
			md:   validMD,
			configure: func(_ *StateMachine, tc *testConfig) {
				delete(tc.blockStore, 0)
			},
			err: common.ErrBlockNotFound,
		},
		{
			name: "parent has no inner block",
			md:   validMD,
			configure: func(_ *StateMachine, tc *testConfig) {
				tc.blockStore[0] = &outerBlock{
					block: StateMachineBlock{},
				}
			},
			err: errZeroBlockParentNoInnerBlock,
		},
		{
			name: "wrong epoch number",
			md:   validMD,
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexProtocolMetadata.Epoch = 2
			},
			err: errInvalidProtocolMetadataEpoch,
		},
		{
			name: "P-chain height too big",
			md:   validMD,
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.PChainHeight = 110
			},
			err: errInvalidPChainHeight,
		},
		{
			name: "P-chain height smaller than parent",
			md:   validMD,
			configure: func(sm *StateMachine, tc *testConfig) {
				sm.LastNonSimplexBlockPChainHeight = 99
			},
			err: errInvalidPChainHeight,
		},
		{
			name: "nil BlockValidationDescriptor",
			md:   validMD,
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexEpochInfo.BlockValidationDescriptor = nil
			},
			err: errBlockDigestMismatch,
		},
		{
			name: "membership mismatch",
			md:   validMD,
			configure: func(sm *StateMachine, tc *testConfig) {
				sm.GenesisValidatorSet = NodeBLSMappings{
					{BLSKey: []byte{1}, Weight: 1},
				}
			},
			err: errBlockDigestMismatch,
		},
		{
			name: "SimplexEpochInfo mismatch",
			md:   validMD,
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexEpochInfo.PrevVMBlockSeq = 999
			},
			err: errBlockDigestMismatch,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sm1, _ := newStateMachine(t)
			sm2, testConfig2 := newStateMachine(t)

			if testCase.configure != nil {
				testCase.configure(sm2, testConfig2)
			}

			block, err := sm1.BuildBlock(context.Background(), testCase.md, emptyBlacklist)
			require.NoError(t, err)
			require.NotNil(t, block)

			if testCase.mutateBlock != nil {
				testCase.mutateBlock(block)
			}

			err = sm2.VerifyBlock(context.Background(), block)
			if testCase.err != nil {
				require.ErrorIs(t, err, testCase.err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestMSMFirstSimplexBlockAfterPreSimplexBlocks(t *testing.T) {
	preSimplexParent := StateMachineBlock{
		InnerBlock: &testutil.InnerBlock{
			TS:          time.Now(),
			BlockHeight: 42,
			Content:     []byte{4, 5, 6},
		},
		// Since the height is 42, this can't be a genesis block, so it must be a
		// pre-Simplex block. It already participates in an ICM epoch, which the zero
		// block built on top of it inherits.
		Metadata: StateMachineMetadata{
			ICMEpochInfo: ICMEpochInfo{
				PChainEpochHeight: 100,
				EpochNumber:       1,
			},
		},
	}

	md := common.ProtocolMetadata{
		Round: 0,
		Seq:   43,
		Epoch: 43,
		Prev:  preSimplexParent.InnerBlock.Digest(),
	}

	sm1, testConfig1 := newStateMachine(t)
	sm2, testConfig2 := newStateMachine(t)

	testConfig1.blockStore[0] = &outerBlock{
		block: preSimplexParent,
	}

	testConfig1.blockStore[42] = &outerBlock{block: preSimplexParent}
	testConfig2.blockStore[42] = &outerBlock{block: preSimplexParent}

	sm1.LastNonSimplexInnerBlock = testConfig1.blockStore[42].block.InnerBlock
	sm2.LastNonSimplexInnerBlock = testConfig1.blockStore[42].block.InnerBlock

	testConfig1.blockBuilder.Block = &testutil.InnerBlock{
		TS:          time.Now(),
		BlockHeight: 43,
		Content:     []byte{7, 8, 9},
	}

	block, err := sm1.BuildBlock(context.Background(), md, emptyBlacklist)
	require.NoError(t, err)
	require.NotNil(t, block)

	require.Equal(t, &StateMachineBlock{
		Metadata: StateMachineMetadata{
			Timestamp:               uint64(preSimplexParent.InnerBlock.Timestamp().UnixMilli()),
			PChainHeight:            100,
			SimplexProtocolMetadata: md,
			SimplexEpochInfo: SimplexEpochInfo{
				PChainReferenceHeight: 100,
				PrevVMBlockSeq:        42,
				BlockValidationDescriptor: &BlockValidationDescriptor{
					AggregatedMembership: AggregatedMembership{
						Members: testConfig1.validatorSetRetriever.result,
					},
				},
			},
			ICMEpochInfo: ICMEpochInfo{
				PChainEpochHeight: 100,
				EpochNumber:       1,
			},
		},
	}, block)

	require.NoError(t, sm2.VerifyBlock(context.Background(), block))
}

// TestMSMZeroBlockPrevIsLastNonSimplexInnerBlockDigest ensures the zero block anchors itself to the
// last non-Simplex block via that block's inner digest, and not via the digest consensus hands us
// in the protocol metadata.
func TestMSMZeroBlockPrevIsLastNonSimplexInnerBlockDigest(t *testing.T) {
	preSimplexParent := StateMachineBlock{
		InnerBlock: &testutil.InnerBlock{
			TS:          time.Now(),
			BlockHeight: 42,
			Content:     []byte{4, 5, 6},
		},
	}

	innerDigest := common.Digest(preSimplexParent.InnerBlock.Digest())
	outerDigest := common.Digest(preSimplexParent.Digest())
	require.NotEqual(t, innerDigest, outerDigest)

	newZeroBlockStateMachine := func(t *testing.T) *StateMachine {
		sm, tc := newStateMachine(t)
		tc.blockStore[42] = &outerBlock{block: preSimplexParent}
		sm.LastNonSimplexInnerBlock = preSimplexParent.InnerBlock
		return sm
	}

	t.Run("built zero block points to the inner digest", func(t *testing.T) {
		sm := newZeroBlockStateMachine(t)

		block, err := sm.BuildBlock(context.Background(), common.ProtocolMetadata{
			Round: 0,
			Seq:   43,
			Epoch: 43,
			Prev:  outerDigest,
		}, emptyBlacklist)
		require.NoError(t, err)

		require.Equal(t, innerDigest, block.Metadata.SimplexProtocolMetadata.Prev)
		require.Equal(t, preSimplexParent.InnerBlock.Height()+1, block.Metadata.SimplexProtocolMetadata.Seq)

		// A different node, which only knows the last non-Simplex block, accepts it.
		require.NoError(t, newZeroBlockStateMachine(t).VerifyBlock(context.Background(), block))
	})

	t.Run("zero block not pointing to the inner digest is rejected", func(t *testing.T) {
		sm := newZeroBlockStateMachine(t)

		block, err := sm.BuildBlock(context.Background(), common.ProtocolMetadata{
			Round: 0,
			Seq:   43,
			Epoch: 43,
			Prev:  innerDigest,
		}, emptyBlacklist)
		require.NoError(t, err)

		block.Metadata.SimplexProtocolMetadata.Prev = outerDigest

		err = newZeroBlockStateMachine(t).VerifyBlock(context.Background(), block)
		require.ErrorIs(t, err, errZeroBlockPrevDigestMismatch)
	})
}

func TestMSMBuildBlockRejectsZeroSeq(t *testing.T) {
	// Seq 0 is reserved for the genesis block, which should never be built.
	sm, _ := newStateMachine(t)

	block, err := sm.BuildBlock(context.Background(), common.ProtocolMetadata{Seq: 0}, emptyBlacklist)
	require.ErrorIs(t, err, errInvalidProtocolMetadataSeq)
	require.Nil(t, block)
}

func TestMSMNormalOp(t *testing.T) {
	// The chain's zero block sits at simplexStartHeight and opens the epoch every later block belongs to.
	const simplexStartHeight, chainEndHeight = 5, 10

	newPChainHeight := uint64(200)
	newValidatorSet := NodeBLSMappings{
		{BLSKey: []byte{5}, Weight: 1}, {BLSKey: []byte{6}, Weight: 1}, {BLSKey: []byte{7}, Weight: 1},
	}

	for _, testCase := range []struct {
		name                        string
		setup                       func(*StateMachine, *testConfig)
		mutateBlock                 func(*StateMachineBlock)
		err                         error
		expectedPChainHeight        uint64
		expectedNextPChainRefHeight uint64
		expectedICMEpochInfo        ICMEpochInfo
	}{
		{
			name:                 "correct information",
			expectedPChainHeight: 100,
			expectedICMEpochInfo: ICMEpochInfo{PChainEpochHeight: 100, EpochNumber: 1},
		},
		{
			name: "trying to build a genesis block",
			mutateBlock: func(block *StateMachineBlock) {
				md := block.Metadata.SimplexProtocolMetadata
				md.Seq = 0
				block.Metadata.SimplexProtocolMetadata = md
			},
			err: errBuiltGenesisInnerBlock,
		},
		{
			name: "previous block not found",
			mutateBlock: func(block *StateMachineBlock) {
				md := block.Metadata.SimplexProtocolMetadata
				md.Seq = 999
				block.Metadata.SimplexProtocolMetadata = md
			},
			err: common.ErrBlockNotFound,
		},
		{
			name: "P-chain height too big",
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.PChainHeight = 110
			},
			err: errPChainHeightTooBig,
		},
		{
			name: "P-chain height smaller than parent",
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.PChainHeight = 0
			},
			err: errPChainHeightSmallerThanParent,
		},
		{
			name: "wrong epoch number",
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexProtocolMetadata.Epoch = 2
			},
			err: errInvalidProtocolMetadataEpoch,
		},
		{
			name: "non-nil BlockValidationDescriptor",
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexEpochInfo.BlockValidationDescriptor = &BlockValidationDescriptor{}
			},
			err: errBlockDigestMismatch,
		},
		{
			name: "non-zero sealing block seq",
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexEpochInfo.SealingBlockSeq = 5
			},
			err: errBlockDigestMismatch,
		},
		{
			name: "wrong PChainReferenceHeight",
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexEpochInfo.PChainReferenceHeight = 50
			},
			err: errBlockDigestMismatch,
		},
		{
			name: "non-empty PrevSealingBlockHash",
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexEpochInfo.PrevSealingBlockHash = common.Digest{1, 2, 3}
			},
			err: errBlockDigestMismatch,
		},
		{
			name: "wrong PrevVMBlockSeq",
			mutateBlock: func(block *StateMachineBlock) {
				block.Metadata.SimplexEpochInfo.PrevVMBlockSeq = 999
			},
			err: errBlockDigestMismatch,
		},
		{
			name: "validator set change detected",
			setup: func(sm *StateMachine, tc *testConfig) {
				tc.validatorSetRetriever.resultMap = map[uint64]NodeBLSMappings{
					newPChainHeight: newValidatorSet,
				}
				sm.GetPChainHeightForProposing = func() uint64 { return newPChainHeight }
				sm.GetPChainHeightForVerifying = func() uint64 { return newPChainHeight }
			},
			expectedPChainHeight:        newPChainHeight,
			expectedNextPChainRefHeight: newPChainHeight,
			expectedICMEpochInfo:        ICMEpochInfo{PChainEpochHeight: 100, EpochNumber: 1},
		},
		{
			// The validator set changed, but the block that opened the epoch is not finalized yet,
			// so the builder must not start the transition: a verifier would reject a block that
			// does, since it requires that block to be finalized. The block is still built, at
			// the new P-chain height, and the transition waits for a later block.
			// This is needed when building blocks on top of the first ever simplex block.
			name: "validator set change not acted upon while the epoch's opening block is not finalized",
			setup: func(sm *StateMachine, tc *testConfig) {
				tc.validatorSetRetriever.resultMap = map[uint64]NodeBLSMappings{
					newPChainHeight: newValidatorSet,
				}
				sm.GetPChainHeightForProposing = func() uint64 { return newPChainHeight }
				sm.GetPChainHeightForVerifying = func() uint64 { return newPChainHeight }
				tc.blockStore[simplexStartHeight].finalization = nil
			},
			expectedPChainHeight:        newPChainHeight,
			expectedNextPChainRefHeight: 0,
			expectedICMEpochInfo:        ICMEpochInfo{PChainEpochHeight: 100, EpochNumber: 1},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sm1, testConfig1 := newStateMachine(t)
			sm2, testConfig2 := newStateMachine(t)

			chain := makeChain(t, simplexStartHeight, chainEndHeight, testConfig1.validatorSetRetriever.result)

			for i, block := range chain {
				testConfig1.blockStore[uint64(i)] = &outerBlock{block: block, finalization: &common.Finalization{}}
				testConfig2.blockStore[uint64(i)] = &outerBlock{block: block, finalization: &common.Finalization{}}
			}

			lastBlock := chain[len(chain)-1]
			md := lastBlock.Metadata.SimplexProtocolMetadata

			md.Seq++
			md.Round++
			md.Prev = lastBlock.Digest()

			blacklist := common.NewBlacklist(4, simplex.DefaultMaxBlacklistedRounds)

			blockTime := lastBlock.InnerBlock.Timestamp().Add(time.Second)

			fixedTime := func() time.Time { return blockTime }
			sm1.GetTime = fixedTime
			sm2.GetTime = fixedTime

			content := make([]byte, 10)
			_, err := rand.Read(content)
			require.NoError(t, err)

			testConfig1.blockBuilder.Block = &testutil.InnerBlock{
				TS:          blockTime,
				BlockHeight: lastBlock.InnerBlock.Height(),
				Content:     content,
			}

			if testCase.setup != nil {
				testCase.setup(sm1, testConfig1)
				testCase.setup(sm2, testConfig2)
			}

			block1, err := sm1.BuildBlock(context.Background(), md, blacklist)
			require.NoError(t, err)
			require.NotNil(t, block1)

			if testCase.mutateBlock != nil {
				testCase.mutateBlock(block1)
			}

			err = sm2.VerifyBlock(context.Background(), block1)
			if testCase.err != nil {
				require.ErrorIs(t, err, testCase.err)
				return
			}
			require.NoError(t, err)

			expected := &StateMachineBlock{
				InnerBlock: &testutil.InnerBlock{
					TS:          blockTime,
					BlockHeight: lastBlock.InnerBlock.Height(),
					Content:     content,
				},
				Metadata: StateMachineMetadata{
					SimplexBlacklist:        blacklist,
					Timestamp:               uint64(blockTime.UnixMilli()),
					PChainHeight:            testCase.expectedPChainHeight,
					SimplexProtocolMetadata: md,
					SimplexEpochInfo: SimplexEpochInfo{
						PChainReferenceHeight:     100,
						PrevVMBlockSeq:            lastBlock.InnerBlock.Height(),
						NextPChainReferenceHeight: testCase.expectedNextPChainRefHeight,
					},
					ICMEpochInfo: testCase.expectedICMEpochInfo,
				},
			}
			require.Equal(t, expected.Digest(), block1.Digest())
		})
	}
}

func TestByzantinePChainHeightStallsHonestBuilders(t *testing.T) {
	const simplexStartHeight, chainEndHeight = 5, 10
	const minimumPChainHeight, currentPChainHeight = 100, 150

	attacker, attackerConfig := newStateMachine(t)
	honestBuilder, honestBuilderConfig := newStateMachine(t)
	verifier, verifierConfig := newStateMachine(t)

	chain := makeChain(t, simplexStartHeight, chainEndHeight, attackerConfig.validatorSetRetriever.result)
	for i, block := range chain {
		for _, tc := range []*testConfig{attackerConfig, honestBuilderConfig, verifierConfig} {
			tc.blockStore[uint64(i)] = &outerBlock{block: block, finalization: &common.Finalization{}}
		}
	}

	// The attacker proposes at the current P-chain height, the highest height verifiers accept.
	attacker.GetPChainHeightForProposing = func() uint64 { return currentPChainHeight }
	// The honest builder proposes at the lagging minimum height.
	honestBuilder.GetPChainHeightForProposing = func() uint64 { return minimumPChainHeight }
	honestBuilder.GetPChainHeightForVerifying = func() uint64 { return currentPChainHeight }
	verifier.GetPChainHeightForVerifying = func() uint64 { return currentPChainHeight }

	lastBlock := chain[len(chain)-1]
	blockTime := lastBlock.InnerBlock.Timestamp().Add(time.Second)
	fixedTime := func() time.Time { return blockTime }
	attacker.GetTime = fixedTime
	honestBuilder.GetTime = fixedTime
	verifier.GetTime = fixedTime

	blacklist := common.NewBlacklist(4, simplex.DefaultMaxBlacklistedRounds)

	md := lastBlock.Metadata.SimplexProtocolMetadata
	md.Seq++
	md.Round++
	md.Prev = lastBlock.Digest()

	attackerConfig.blockBuilder.Block = &testutil.InnerBlock{
		TS:          blockTime,
		BlockHeight: lastBlock.InnerBlock.Height() + 1,
		Content:     []byte{1},
	}

	attackerBlock, err := attacker.BuildBlock(context.Background(), md, blacklist)
	require.NoError(t, err)
	require.Equal(t, uint64(currentPChainHeight), attackerBlock.Metadata.PChainHeight)

	// The attacker's block is valid, so honest nodes accept it.
	require.NoError(t, verifier.VerifyBlock(context.Background(), attackerBlock))
	require.NoError(t, honestBuilder.VerifyBlock(context.Background(), attackerBlock))

	for _, tc := range []*testConfig{attackerConfig, honestBuilderConfig, verifierConfig} {
		tc.blockStore[md.Seq] = &outerBlock{block: *attackerBlock}
	}

	md.Seq++
	md.Round++
	md.Prev = attackerBlock.Digest()

	honestBlockTime := blockTime.Add(time.Second)
	honestBuilder.GetTime = func() time.Time { return honestBlockTime }
	verifier.GetTime = func() time.Time { return honestBlockTime }

	honestBuilderConfig.blockBuilder.Block = &testutil.InnerBlock{
		TS:          honestBlockTime,
		BlockHeight: lastBlock.InnerBlock.Height() + 2,
		Content:     []byte{2},
	}

	honestBlock, err := honestBuilder.BuildBlock(context.Background(), md, blacklist)
	require.NoError(t, err)

	// An honest builder must never produce a block honest verifiers reject.
	require.NoError(t, verifier.VerifyBlock(context.Background(), honestBlock))
	require.GreaterOrEqual(t, honestBlock.Metadata.PChainHeight, attackerBlock.Metadata.PChainHeight)
}

func TestMSMFullEpochLifecycle(t *testing.T) {
	// Validator sets: epoch 1 uses validatorSet1, epoch 2 uses validatorSet2.
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

	// Align to a whole second: the ICM epoch boundary is second-granular (ComputeICMEpoch
	// truncates timestamps with .Unix() and uses a 1-second window), while the blocks below
	// are placed at sub-second offsets from startTime. If startTime had a sub-second component
	// close to 1s, the "+1s + few ms" offsets would spill into the next second and trigger an
	// extra ICM epoch transition, making the test flaky.
	startTime := time.Now().Truncate(time.Second)

	nextBlock := func(height uint64) *testutil.InnerBlock {
		return &testutil.InnerBlock{
			TS:          startTime.Add(time.Duration(height) * time.Millisecond),
			BlockHeight: height,
			Content:     []byte{byte(height)},
		}
	}

	// ----- Step 0: Building on top of genesis or upgrading to Simplex-----
	genesis := &testutil.InnerBlock{
		BlockHeight: 0, // Genesis block has height 0
		TS:          startTime,
		Content:     []byte{0},
	}

	notGenesis := &testutil.InnerBlock{
		BlockHeight: 42,
		TS:          startTime,
		Content:     []byte{0},
	}

	for _, testCase := range []struct {
		name                    string
		firstBlockBeforeSimplex *testutil.InnerBlock
		epochNum                uint64
		// firstBlockICMEpochInfo is the ICM epoch of the pre-Simplex parent, which the zero block
		// carries over. A genesis parent predates ICM, so its ICM epoch is empty and the first epoch
		// (icmEpoch1) begins on the block built on top of the zero block.
		firstBlockICMEpochInfo ICMEpochInfo
	}{
		{
			name:                    "building on top of genesis",
			firstBlockBeforeSimplex: genesis,
			epochNum:                1,
			firstBlockICMEpochInfo:  ICMEpochInfo{},
		},
		{
			name:                    "upgrading to Simplex from pre-Simplex blocks",
			firstBlockBeforeSimplex: notGenesis,
			epochNum:                notGenesis.Height() + 1,
			firstBlockICMEpochInfo: ICMEpochInfo{
				PChainEpochHeight: pChainHeight1,
				EpochNumber:       1,
				EpochStartTime:    uint64(startTime.Unix()),
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {

			currentPChainHeight := pChainHeight1

			getValidatorSet := func(height uint64) (NodeBLSMappings, error) {
				if height >= pChainHeight2 {
					return validatorSet2, nil
				}
				return validatorSet1, nil
			}
			// In production the two P-chain height functions differ: proposing uses a slightly
			// lagging "stable" height, while verifying uses the up-to-date real-time height.
			// getProposingPChainHeight is the lagging height the builder proposes at (it drives
			// the block contents, e.g. the validator-set-change observation at pChainHeight2).
			// getVerifyingPChainHeight stays ahead of it, modelling the more up-to-date height
			// the verifier checks against.
			const pChainHeightLag = uint64(50)
			getProposingPChainHeight := func() uint64 {
				return currentPChainHeight
			}
			getVerifyingPChainHeight := func() uint64 {
				return currentPChainHeight + pChainHeightLag
			}

			// Since we explicitly compare the built block with an expected value,
			// we need the timestamps to be deterministic. So instead of using time.Now(), we use a fixed
			// startTime and add offsets to it for each block.
			currentTime := startTime
			fixedTime := func() time.Time { return currentTime }

			// We exercise an ICM epoch transition by jumping block3's timestamp
			// past the 1-second ICM-epoch window.
			// ComputeICMEpoch transitions when the parent block's timestamp has
			// crossed the current ICM epoch's start + 1s, so block4 (and every
			// block after it) lands in ICM epoch 2.
			//
			// block3 is also the block where the validator set change is first
			// observed, so its Metadata.PChainHeight = pChainHeight2. Since the
			// transition takes input.ParentPChainHeight as the new epoch's
			// PChainEpochHeight, icmEpoch2.PChainEpochHeight = pChainHeight2.
			//   block2, block3: ICM epoch 1, started at startTime.
			//   block4 onward:  ICM epoch 2, started at block3's timestamp,
			//                   PChainEpochHeight = pChainHeight2.
			icmEpoch1 := ICMEpochInfo{
				PChainEpochHeight: pChainHeight1,
				EpochNumber:       1,
				EpochStartTime:    uint64(startTime.Unix()),
			}
			icmEpoch2 := ICMEpochInfo{
				PChainEpochHeight: pChainHeight2,
				EpochNumber:       2,
				EpochStartTime:    uint64(startTime.Unix()) + 1,
			}

			sm, tc := newStateMachine(t)
			sm.GetValidatorSet = getValidatorSet

			sm.GetTime = fixedTime

			// This test exercises the epoch/approval/seal lifecycle, not auxiliary info.
			// Uses an app (noopTestAuxInfoApp) whose history is always final so approvals are collected from the
			// first collecting round and no auxiliary info is generated. Auxiliary info
			// behavior is covered by TestVerifyCollectingApprovalsNotReady and
			// TestCollectAuxiliaryInfo.
			sm.AuxiliaryInfoApp = &noopTestAuxInfoApp{}
			tc.blockStore[0] = &outerBlock{block: StateMachineBlock{InnerBlock: genesis}}
			tc.blockStore[42] = &outerBlock{block: StateMachineBlock{InnerBlock: notGenesis}}

			sm.LastNonSimplexInnerBlock = testCase.firstBlockBeforeSimplex
			sm.GenesisValidatorSet = validatorSet1
			sm.LastNonSimplexBlockPChainHeight = pChainHeight1

			smVerify, tcVerify := newStateMachine(t)
			smVerify.AuxiliaryInfoApp = &noopTestAuxInfoApp{}
			smVerify.GetValidatorSet = getValidatorSet

			// sm only ever builds blocks and smVerify only ever verifies them.
			// Each one fails the test if it consults the P-chain height function it must not use,
			// proving that building reads GetPChainHeightForProposing and verifying reads GetPChainHeightForVerifying.
			sm.GetPChainHeightForProposing = getProposingPChainHeight
			sm.GetPChainHeightForVerifying = func() uint64 {
				require.FailNow(t, "builder must not use GetPChainHeightForVerifying when proposing")
				return 0
			}

			smVerify.GetPChainHeightForProposing = func() uint64 {
				require.FailNow(t, "verifier must not use GetPChainHeightForProposing when verifying")
				return 0
			}
			smVerify.GetPChainHeightForVerifying = getVerifyingPChainHeight

			smVerify.GetTime = fixedTime

			smVerify.LastNonSimplexInnerBlock = testCase.firstBlockBeforeSimplex
			smVerify.GenesisValidatorSet = validatorSet1
			smVerify.LastNonSimplexBlockPChainHeight = pChainHeight1

			// addBlock adds a block to both block stores so builder and verifier stay in sync.
			addBlock := func(seq uint64, block StateMachineBlock, fin *common.Finalization) {
				tc.blockStore[seq] = &outerBlock{block: block, finalization: fin}
				tcVerify.blockStore[seq] = &outerBlock{block: block, finalization: fin}
			}

			baseSeq := testCase.firstBlockBeforeSimplex.Height()
			addBlock(baseSeq, StateMachineBlock{InnerBlock: testCase.firstBlockBeforeSimplex, Metadata: StateMachineMetadata{
				// The zero block carries over the parent's ICM epoch.
				ICMEpochInfo: testCase.firstBlockICMEpochInfo,
			}}, nil)

			aggr := &signatureAggregator{}

			// ----- Step 1: Build zero epoch block (first simplex block) -----
			tc.blockBuilder.Block = nextBlock(1)
			md := common.ProtocolMetadata{
				Seq:   baseSeq + 1,
				Round: 0,
				Epoch: testCase.epochNum,
				Prev:  testCase.firstBlockBeforeSimplex.Digest(),
			}

			block1, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
			require.NoError(t, err)
			require.Equal(t, &StateMachineBlock{
				Metadata: StateMachineMetadata{
					Timestamp:               uint64(startTime.UnixMilli()),
					PChainHeight:            pChainHeight1,
					SimplexProtocolMetadata: md,
					SimplexEpochInfo: SimplexEpochInfo{
						PChainReferenceHeight: pChainHeight1,
						PrevVMBlockSeq:        baseSeq,
						BlockValidationDescriptor: &BlockValidationDescriptor{
							AggregatedMembership: AggregatedMembership{
								Members: validatorSet1,
							},
						},
					},
					// The zero block carries over the parent's ICM epoch (icmEpoch1 for a
					// pre-Simplex parent, empty for a genesis parent).
					ICMEpochInfo: testCase.firstBlockICMEpochInfo,
				},
			}, block1)
			addBlock(md.Seq, *block1, &common.Finalization{})

			require.NoError(t, smVerify.VerifyBlock(context.Background(), block1))

			// After we build the first block, the StateMachine should consider it as the latest persisted height.
			sm.LatestPersistedHeight = baseSeq + 1
			smVerify.LatestPersistedHeight = baseSeq + 1

			// ----- Step 2: Build a normal block (no validator set change) -----
			currentTime = startTime.Add(2 * time.Millisecond)
			tc.blockBuilder.Block = nextBlock(2)
			md = common.ProtocolMetadata{Seq: baseSeq + 2, Round: 1, Epoch: testCase.epochNum, Prev: block1.Digest()}
			block2, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
			require.NoError(t, err)
			require.Equal(t, &StateMachineBlock{
				InnerBlock: nextBlock(2),
				Metadata: StateMachineMetadata{
					Timestamp:               uint64(currentTime.UnixMilli()),
					PChainHeight:            pChainHeight1,
					SimplexProtocolMetadata: md,
					SimplexEpochInfo: SimplexEpochInfo{
						PChainReferenceHeight: pChainHeight1,
						PrevVMBlockSeq:        baseSeq,
					},
					ICMEpochInfo: icmEpoch1,
				},
			}, block2)
			addBlock(md.Seq, *block2, nil)

			require.NoError(t, smVerify.VerifyBlock(context.Background(), block2))

			// ----- Step 3: Build a normal block that detects a validator set change -----
			// Advance P-chain height so that GetValidatorSet returns a different set.
			currentPChainHeight = pChainHeight2

			// Jump block3's timestamp past the 1-second ICM-epoch window so
			// block4 (whose parent is block3) sees parentTimestamp >=
			// epochStart + 1s and transitions ICM to epoch 2.
			currentTime = startTime.Add(time.Second + 3*time.Millisecond)
			tc.blockBuilder.Block = nextBlock(3)
			md = common.ProtocolMetadata{Seq: baseSeq + 3, Round: 2, Epoch: testCase.epochNum, Prev: block2.Digest()}
			block3, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
			require.NoError(t, err)
			require.Equal(t, &StateMachineBlock{
				InnerBlock: nextBlock(3),
				Metadata: StateMachineMetadata{
					Timestamp:               uint64(currentTime.UnixMilli()),
					PChainHeight:            pChainHeight2,
					SimplexProtocolMetadata: md,
					SimplexEpochInfo: SimplexEpochInfo{
						PChainReferenceHeight:     pChainHeight1,
						PrevVMBlockSeq:            baseSeq + 2,
						NextPChainReferenceHeight: pChainHeight2,
					},
					ICMEpochInfo: icmEpoch1,
				},
			}, block3)
			addBlock(md.Seq, *block3, nil)

			require.NoError(t, smVerify.VerifyBlock(context.Background(), block3))

			require.NoError(t, sm.OnBlockIndex(*block3))

			// ----- Step 4: First collecting block (1/3 approvals, not enough to seal) -----

			sig1 := signApproval(pChainHeight2, emptyAuxInfoDigest)
			sm.HandleApproval(&common.ValidatorSetApproval{
				NodeID:        node1,
				PChainHeight:  pChainHeight2,
				AuxInfoDigest: emptyAuxInfoDigest,
				Signature:     sig1,
			})

			// node1 is at index 0 in validatorSet2 → bitmask bit 0 → {1}
			bitmask := []byte{1}
			sig, err := aggr.AppendSignatures(nil, sig1)
			require.NoError(t, err)

			currentTime = startTime.Add(time.Second + 4*time.Millisecond)
			tc.blockBuilder.Block = nextBlock(4)
			md = common.ProtocolMetadata{Seq: baseSeq + 4, Round: 3, Epoch: testCase.epochNum, Prev: block3.Digest()}
			block4, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
			require.NoError(t, err)
			require.Equal(t, &StateMachineBlock{
				InnerBlock: nextBlock(4),
				Metadata: StateMachineMetadata{
					Timestamp:               uint64(currentTime.UnixMilli()),
					PChainHeight:            pChainHeight2,
					SimplexProtocolMetadata: md,
					SimplexEpochInfo: SimplexEpochInfo{
						PChainReferenceHeight:     pChainHeight1,
						PrevVMBlockSeq:            baseSeq + 3,
						NextPChainReferenceHeight: pChainHeight2,
						NextEpochApprovals: &NextEpochApprovals{
							NodeIDs:   bitmask,
							Signature: sig,
						},
					},
					ICMEpochInfo: icmEpoch2,
				},
			}, block4)
			addBlock(md.Seq, *block4, nil)

			require.NoError(t, smVerify.VerifyBlock(context.Background(), block4))

			// ----- Step 5: Second collecting block (2/3 approvals, still not enough since threshold is strictly > 2/3) -----
			sig2 := signApproval(pChainHeight2, emptyAuxInfoDigest)
			sm.HandleApproval(&common.ValidatorSetApproval{
				NodeID:        node2,
				PChainHeight:  pChainHeight2,
				AuxInfoDigest: emptyAuxInfoDigest,
				Signature:     sig2,
			})

			// node2 is at index 1 → bitmask bits 0,1 → {3}
			sig, err = aggr.AppendSignatures(sig, sig2)
			require.NoError(t, err)
			bitmask = []byte{3}

			currentTime = startTime.Add(time.Second + 5*time.Millisecond)
			tc.blockBuilder.Block = nextBlock(5)
			md = common.ProtocolMetadata{Seq: baseSeq + 5, Round: 4, Epoch: testCase.epochNum, Prev: block4.Digest()}
			block5, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
			require.NoError(t, err)
			require.Equal(t, &StateMachineBlock{
				InnerBlock: nextBlock(5),
				Metadata: StateMachineMetadata{
					Timestamp:               uint64(currentTime.UnixMilli()),
					PChainHeight:            pChainHeight2,
					SimplexProtocolMetadata: md,
					SimplexEpochInfo: SimplexEpochInfo{
						PChainReferenceHeight:     pChainHeight1,
						PrevVMBlockSeq:            baseSeq + 4,
						NextPChainReferenceHeight: pChainHeight2,
						NextEpochApprovals: &NextEpochApprovals{
							NodeIDs:   bitmask,
							Signature: sig,
						},
					},
					ICMEpochInfo: icmEpoch2,
				},
			}, block5)
			addBlock(md.Seq, *block5, nil)

			require.NoError(t, smVerify.VerifyBlock(context.Background(), block5))

			// ----- Step 6: Sealing block (3/3 approvals, enough to seal) -----
			sig3 := signApproval(pChainHeight2, emptyAuxInfoDigest)
			sm.HandleApproval(&common.ValidatorSetApproval{
				NodeID:        node3,
				PChainHeight:  pChainHeight2,
				AuxInfoDigest: emptyAuxInfoDigest,
				Signature:     sig3,
			})

			// node3 is at index 2 → bitmask bits 0,1,2 → {7}
			sig6, err := aggr.AppendSignatures(sig, sig3)
			require.NoError(t, err)
			bitmask = []byte{7}

			currentTime = startTime.Add(time.Second + 6*time.Millisecond)
			tc.blockBuilder.Block = nextBlock(6)
			md = common.ProtocolMetadata{Seq: baseSeq + 6, Round: 5, Epoch: testCase.epochNum, Prev: block5.Digest()}
			block6, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
			require.NoError(t, err)
			require.Equal(t, &StateMachineBlock{
				InnerBlock: nextBlock(6),
				Metadata: StateMachineMetadata{
					Timestamp:               uint64(currentTime.UnixMilli()),
					PChainHeight:            pChainHeight2,
					SimplexProtocolMetadata: md,
					SimplexEpochInfo: SimplexEpochInfo{
						PChainReferenceHeight:     pChainHeight1,
						PrevVMBlockSeq:            baseSeq + 5,
						NextPChainReferenceHeight: pChainHeight2,
						SealingBlockSeq:           0,
						PrevSealingBlockHash:      block1.Digest(),
						BlockValidationDescriptor: &BlockValidationDescriptor{
							AggregatedMembership: AggregatedMembership{
								Members: validatorSet2,
							},
						},
						NextEpochApprovals: &NextEpochApprovals{
							NodeIDs:   bitmask,
							Signature: sig6,
						},
					},
					ICMEpochInfo: icmEpoch2,
				},
			}, block6)
			addBlock(md.Seq, *block6, nil)

			require.NoError(t, smVerify.VerifyBlock(context.Background(), block6))

			sealingSeq := baseSeq + 6 // The sealing block's sequence (md.Seq from step 6)

			backupStoreTC := tc.blockStore.clone()
			backupStoreTCVerify := tcVerify.blockStore.clone()

			for _, subTestCase := range []struct {
				name  string
				setup func()
			}{
				{
					name: "sealing block not finalized yet",
					setup: func() {
						addBlock(sealingSeq, tc.blockStore[sealingSeq].block, nil)
					},
				},
				{
					name: "sealing block immediately finalized",
					setup: func() {
						addBlock(sealingSeq, tc.blockStore[sealingSeq].block, &common.Finalization{})
					},
				},
			} {
				testName := fmt.Sprintf("%s-%s", testCase.name, subTestCase.name)
				t.Run(testName, func(t *testing.T) {
					tc.blockStore = backupStoreTC.clone()
					sm.GetBlock = tc.blockStore.getBlock
					tcVerify.blockStore = backupStoreTCVerify.clone()
					smVerify.GetBlock = tcVerify.blockStore.getBlock

					subTestCase.setup()

					tc.blockBuilder.Block = nextBlock(7)
					md = common.ProtocolMetadata{Seq: baseSeq + 7, Round: 6, Epoch: testCase.epochNum, Prev: block6.Digest()}

					// If the sealing block isn't finalized yet, we expect to build a Telock.
					// However, despite the fact that the block builder is willing to build a new block,
					// a Telock shouldn't contain an inner block.
					if tc.blockStore[sealingSeq].finalization == nil {
						// Telock shares the sealing block's timestamp slot.
						currentTime = startTime.Add(time.Second + 6*time.Millisecond)
						telock, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
						require.NoError(t, err)

						require.Equal(t, &StateMachineBlock{
							InnerBlock: nil,
							Metadata: StateMachineMetadata{
								Timestamp:               uint64(currentTime.UnixMilli()),
								PChainHeight:            pChainHeight2,
								SimplexProtocolMetadata: md,
								SimplexEpochInfo: SimplexEpochInfo{
									PChainReferenceHeight:     pChainHeight1,
									NextPChainReferenceHeight: pChainHeight2,
									PrevVMBlockSeq:            baseSeq + 6,
									SealingBlockSeq:           sealingSeq,
								},
								ICMEpochInfo: icmEpoch2,
							},
						}, telock)

						// Next, finalize the sealing block after we have built a Telock.
						addBlock(sealingSeq, tc.blockStore[sealingSeq].block, &common.Finalization{})
					}

					// ----- Step 7: Build a new epoch block (sealing block is finalized) -----

					// The first block of the new epoch is numbered after the sealing block's sequence.
					md.Epoch = sealingSeq

					currentTime = startTime.Add(time.Second + 7*time.Millisecond)
					block7, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
					require.NoError(t, err)
					require.Equal(t, &StateMachineBlock{
						InnerBlock: nextBlock(7),
						Metadata: StateMachineMetadata{
							Timestamp:               uint64(currentTime.UnixMilli()),
							PChainHeight:            pChainHeight2,
							SimplexProtocolMetadata: md,
							SimplexEpochInfo: SimplexEpochInfo{
								PChainReferenceHeight: pChainHeight2,
								PrevVMBlockSeq:        baseSeq + 6,
							},
							ICMEpochInfo: icmEpoch2,
						},
					}, block7)
					addBlock(md.Seq, *block7, nil)

					require.NoError(t, smVerify.VerifyBlock(context.Background(), block7))
				})
			}
		})
	}
}

func TestIdentifyCurrentState(t *testing.T) {
	bvd := &BlockValidationDescriptor{}
	for _, tc := range []struct {
		name     string
		epoch    uint64
		input    SimplexEpochInfo
		expected state
	}{
		{
			name:     "epoch 0 is first simplex block",
			epoch:    0,
			expected: stateFirstSimplexBlock,
		},
		{
			name:     "no next p-chain ref height means normal op",
			epoch:    1,
			input:    SimplexEpochInfo{NextPChainReferenceHeight: 0},
			expected: stateBuildBlockNormalOp,
		},
		{
			name:     "has sealing block seq means epoch sealed",
			epoch:    1,
			input:    SimplexEpochInfo{NextPChainReferenceHeight: 100, SealingBlockSeq: 5},
			expected: stateBuildBlockEpochSealed,
		},
		{
			name:     "has block validation descriptor means epoch sealed",
			epoch:    1,
			input:    SimplexEpochInfo{NextPChainReferenceHeight: 100, BlockValidationDescriptor: bvd},
			expected: stateBuildBlockEpochSealed,
		},
		{
			name:     "next p-chain ref height > 0 without sealing means collecting approvals",
			epoch:    1,
			input:    SimplexEpochInfo{NextPChainReferenceHeight: 100},
			expected: stateBuildCollectingApprovals,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			md := StateMachineMetadata{
				SimplexProtocolMetadata: common.ProtocolMetadata{Epoch: tc.epoch},
				SimplexEpochInfo:        tc.input,
			}
			require.Equal(t, tc.expected, md.NextState())
		})
	}
}

func TestAreNextEpochApprovalsSignersSupersetOfApprovalsOfPrevBlock(t *testing.T) {
	for _, tc := range []struct {
		name string
		prev SimplexEpochInfo
		next SimplexEpochInfo
		err  error
	}{
		{
			name: "prev has nil approvals",
			prev: SimplexEpochInfo{},
			next: SimplexEpochInfo{NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{3}}},
		},
		{
			name: "next is superset of prev",
			prev: SimplexEpochInfo{NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{1}}},
			next: SimplexEpochInfo{NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{3}}},
		},
		{
			name: "next equals prev",
			prev: SimplexEpochInfo{NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{3}}},
			next: SimplexEpochInfo{NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{3}}},
		},
		{
			name: "next is missing a signer from prev",
			prev: SimplexEpochInfo{NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{3}}},
			next: SimplexEpochInfo{NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{1}}},
			err:  errSignerSetShrunk,
		},
		{
			name: "prev has approvals but next has nil approvals",
			prev: SimplexEpochInfo{NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{1}}},
			next: SimplexEpochInfo{},
			err:  errNextEpochApprovalsShrunk,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := areNextEpochApprovalsSignersSupersetOfApprovalsOfPrevBlock(tc.prev, tc.next)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerifyNextPChainRefHeightNormal(t *testing.T) {
	const (
		prevPChainRefHeight = uint64(50)
		nextPChainRefHeight = uint64(80)
		sealingBlockSeq     = uint64(5)
	)

	setA := NodeBLSMappings{{BLSKey: []byte{1}, Weight: 1}}
	setB := NodeBLSMappings{{BLSKey: []byte{2}, Weight: 1}}

	prevMD := StateMachineMetadata{
		SimplexProtocolMetadata: common.ProtocolMetadata{Epoch: sealingBlockSeq},
		SimplexEpochInfo: SimplexEpochInfo{
			PChainReferenceHeight: prevPChainRefHeight,
		},
	}

	withChangedValidatorSet := func(tc *testConfig) {
		tc.validatorSetRetriever.resultMap = map[uint64]NodeBLSMappings{
			prevPChainRefHeight: setA,
			nextPChainRefHeight: setB,
		}
	}

	// The current validator set is now read from the parent epoch's sealing block, stored at
	// the parent's epoch. It must carry a validator descriptor and be finalized.
	currentEpochSealingBlock := func(finalized bool) *outerBlock {
		ob := &outerBlock{block: StateMachineBlock{Metadata: StateMachineMetadata{
			SimplexEpochInfo: SimplexEpochInfo{
				BlockValidationDescriptor: &BlockValidationDescriptor{
					AggregatedMembership: AggregatedMembership{Members: setA},
				},
				PrevSealingBlockHash: common.Digest{0xaa},
			},
		}}}
		if finalized {
			ob.finalization = &common.Finalization{}
		}
		return ob
	}

	tests := []struct {
		name  string
		next  SimplexEpochInfo
		setup func(tc *testConfig)
		err   error
	}{
		{
			name: "next height zero returns nil",
			next: SimplexEpochInfo{NextPChainReferenceHeight: 0},
			setup: func(tc *testConfig) {
				tc.blockStore[sealingBlockSeq] = currentEpochSealingBlock(true)
			},
		},
		{
			name: "next height set, sealing block finalized",
			next: SimplexEpochInfo{NextPChainReferenceHeight: nextPChainRefHeight},
			setup: func(tc *testConfig) {
				withChangedValidatorSet(tc)
				tc.blockStore[sealingBlockSeq] = currentEpochSealingBlock(true)
			},
		},
		{
			name: "next height set, sealing block not finalized",
			next: SimplexEpochInfo{NextPChainReferenceHeight: nextPChainRefHeight},
			setup: func(tc *testConfig) {
				withChangedValidatorSet(tc)
				tc.blockStore[sealingBlockSeq] = &outerBlock{finalization: nil}
			},
			err: errPrevSealingBlockNotFinalized,
		},
		{
			name: "next height set, sealing block missing",
			next: SimplexEpochInfo{NextPChainReferenceHeight: nextPChainRefHeight},
			setup: func(tc *testConfig) {
				withChangedValidatorSet(tc)
				delete(tc.blockStore, sealingBlockSeq)
			},
			err: common.ErrBlockNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, tc := newStateMachine(t)
			if tt.setup != nil {
				tt.setup(tc)
			}

			err := sm.verifyNextPChainRefHeightNormal(&StateMachineBlock{Metadata: prevMD}, tt.next)
			if tt.err == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.err)
		})
	}
}

// TestVerifyDoesNotInitializeApprovalStore asserts that verifying a block that starts an
// epoch transition does not create the approval store.
// Approvals received before initialization should be dropped,
// one received after should be included in the next block, and initializing
// the store with a different validator set is an error. Restarting on a transition
// block initializes the store.
func TestVerifyDoesNotInitializeApprovalStore(t *testing.T) {
	const simplexStartHeight, chainEndHeight = 5, 10
	newPChainHeight := uint64(200)

	currentSet := NodeBLSMappings{{BLSKey: []byte{1}, Weight: 1, NodeID: [20]byte{1}}, {BLSKey: []byte{2}, Weight: 1, NodeID: [20]byte{2}}}
	newSet := NodeBLSMappings{{BLSKey: []byte{2}, Weight: 1, NodeID: [20]byte{2}}, {BLSKey: []byte{3}, Weight: 1, NodeID: [20]byte{3}}}

	sm, tc := newStateMachine(t)
	sm.AuxiliaryInfoApp = &noopTestAuxInfoApp{}

	chain := makeChain(t, simplexStartHeight, chainEndHeight, currentSet)
	for i, block := range chain {
		tc.blockStore[uint64(i)] = &outerBlock{block: block, finalization: &common.Finalization{}}
	}
	tc.validatorSetRetriever.resultMap = map[uint64]NodeBLSMappings{newPChainHeight: newSet}

	lastBlock := chain[len(chain)-1]
	blockTime := lastBlock.InnerBlock.Timestamp().Add(time.Second)
	sm.GetTime = func() time.Time { return blockTime }
	sm.GetPChainHeightForProposing = func() uint64 { return newPChainHeight }
	sm.GetPChainHeightForVerifying = func() uint64 { return newPChainHeight }

	md := lastBlock.Metadata.SimplexProtocolMetadata
	md.Seq++
	md.Round++
	md.Prev = lastBlock.Digest()
	tc.blockBuilder.Block = &testutil.InnerBlock{TS: blockTime, BlockHeight: lastBlock.InnerBlock.Height()}

	// Build a transition block
	block, err := sm.BuildBlock(context.Background(), md, common.NewBlacklist(4, simplex.DefaultMaxBlacklistedRounds))
	require.NoError(t, err)
	require.Equal(t, newPChainHeight, block.Metadata.SimplexEpochInfo.NextPChainReferenceHeight)

	require.NoError(t, sm.VerifyBlock(context.Background(), block))
	tc.blockStore[md.Seq] = &outerBlock{block: *block}

	// nextBlockApprovers checks the approvals the state machine has processed by building the next block
	nextBlockApprovers := func() avalanchego.Bitmask {
		next := md
		next.Seq++
		next.Round++
		next.Prev = block.Digest()
		tc.blockBuilder.Block = &testutil.InnerBlock{TS: blockTime, BlockHeight: block.InnerBlock.Height() + 1}
		collecting, err := sm.BuildBlock(context.Background(), next, common.NewBlacklist(4, simplex.DefaultMaxBlacklistedRounds))
		require.NoError(t, err)
		return avalanchego.BitmaskFromBytes(collecting.Metadata.SimplexEpochInfo.NextEpochApprovals.NodeIDs)
	}

	var auxInfoDigest common.Digest
	approval := &common.ValidatorSetApproval{
		NodeID:        [20]byte{3},
		PChainHeight:  newPChainHeight,
		AuxInfoDigest: auxInfoDigest,
		Signature:     signApproval(newPChainHeight, auxInfoDigest),
	}
	const node3Index = 1

	// Send an approval for the block we verified above.
	// Because we have only verified but not indexed the transition block, we should not
	// process the approval
	sm.HandleApproval(approval)
	approvers := nextBlockApprovers()
	require.Zero(t, approvers.Len())

	// Indexing the block initialized the store, which should allow approvals to be processed
	require.NoError(t, sm.OnBlockIndex(*block))
	sm.HandleApproval(approval)
	approvers = nextBlockApprovers()
	require.True(t, approvers.Contains(node3Index))

	// Indexing another transition block should keep the store
	require.NoError(t, sm.OnBlockIndex(*block))
	approvers = nextBlockApprovers()
	require.True(t, approvers.Contains(node3Index))

	// We index a block where the pchain height has a different validator set.
	// This should cause an error since, a transition should always reference
	// the same validator set.
	otherPChainHeight := newPChainHeight + 1
	tc.validatorSetRetriever.resultMap[otherPChainHeight] = currentSet
	otherBlock := *block
	otherBlock.Metadata.SimplexEpochInfo.NextPChainReferenceHeight = otherPChainHeight
	require.ErrorIs(t, sm.OnBlockIndex(otherBlock), errApprovalStoreValidatorSetMismatch)

	// Restarting with the transition block as the latest persisted block should
	// initialize the store, so approvals are processed without indexing
	config := *sm.Config
	config.LatestPersistedHeight = md.Seq
	sm, err = NewStateMachine(&config)
	require.NoError(t, err)
	sm.HandleApproval(approval)
	approvers = nextBlockApprovers()
	require.True(t, approvers.Contains(node3Index))
}

func TestVerifyPChainHeight(t *testing.T) {
	tests := []struct {
		name     string
		proposed uint64
		current  uint64
		prev     uint64
		err      error
	}{
		{
			name:     "proposed equals current and parent",
			proposed: 10,
			current:  10,
			prev:     10,
		},
		{
			name:     "proposed equals current, above parent",
			proposed: 10,
			current:  10,
			prev:     5,
		},
		{
			name:     "proposed equals parent, below current",
			proposed: 5,
			current:  10,
			prev:     5,
		},
		{
			name:     "proposed strictly between parent and current",
			proposed: 7,
			current:  10,
			prev:     5,
		},
		{
			name:     "all zero",
			proposed: 0,
			current:  0,
			prev:     0,
		},
		{
			name:     "proposed greater than current",
			proposed: 11,
			current:  10,
			prev:     5,
			err:      errPChainHeightTooBig,
		},
		{
			name:     "proposed greater than current by one, current is zero",
			proposed: 1,
			current:  0,
			prev:     0,
			err:      errPChainHeightTooBig,
		},
		{
			name:     "parent greater than proposed",
			proposed: 5,
			current:  10,
			prev:     6,
			err:      errPChainHeightSmallerThanParent,
		},
		{
			name:     "proposed is zero, parent is non-zero",
			proposed: 0,
			current:  10,
			prev:     1,
			err:      errPChainHeightSmallerThanParent,
		},
		{
			// When both checks would trigger, "too big" takes precedence.
			name:     "both checks would fire, too-big wins",
			proposed: 20,
			current:  10,
			prev:     15,
			err:      errPChainHeightTooBig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyPChainHeight(tt.proposed, tt.current, tt.prev)
			if tt.err == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestVerifyTimestamp(t *testing.T) {
	now := time.Now()
	nowMilli := uint64(now.UnixMilli())
	skewMilli := uint64(maxSkew / time.Millisecond)

	tests := []struct {
		name     string
		proposed uint64
		prev     uint64
		err      error
	}{
		{
			name:     "proposed equals parent",
			proposed: nowMilli,
			prev:     nowMilli,
		},
		{
			name:     "proposed after parent, well within skew",
			proposed: nowMilli + 100,
			prev:     nowMilli - 100,
		},
		{
			name:     "proposed exactly at now + maxSkew",
			proposed: nowMilli + skewMilli,
			prev:     nowMilli,
		},
		{
			name:     "proposed below parent",
			proposed: nowMilli - 1,
			prev:     nowMilli,
			err:      errTimestampDecreasing,
		},
		{
			name:     "proposed one millisecond past now + maxSkew",
			proposed: nowMilli + skewMilli + 1,
			prev:     nowMilli,
			err:      errTimestampTooFarInFuture,
		},
		{
			name:     "proposed exceeds math.MaxInt64",
			proposed: uint64(math.MaxInt64) + 1,
			prev:     nowMilli,
			err:      errTimestampTooBig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := &StateMachineBlock{Metadata: StateMachineMetadata{Timestamp: tt.proposed}}
			prev := &StateMachineBlock{Metadata: StateMachineMetadata{Timestamp: tt.prev}}
			err := verifyTimestamp(block, prev, now, maxSkew)
			if tt.err == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestComputePrevVMBlockSeq(t *testing.T) {
	t.Run("parent has no inner block", func(t *testing.T) {
		parent := StateMachineBlock{
			InnerBlock: nil,
			Metadata:   StateMachineMetadata{SimplexEpochInfo: SimplexEpochInfo{PrevVMBlockSeq: 42}},
		}
		require.Equal(t, uint64(42), computePrevVMBlockSeq(&parent, 100))
	})

	t.Run("parent has inner block", func(t *testing.T) {
		parent := StateMachineBlock{
			InnerBlock: &fakeVMBlock{height: 10},
			Metadata:   StateMachineMetadata{SimplexEpochInfo: SimplexEpochInfo{PrevVMBlockSeq: 42}},
		}
		require.Equal(t, uint64(100), computePrevVMBlockSeq(&parent, 100))
	})
}

func TestSanitizeApprovals(t *testing.T) {
	node0 := avalanchego.NodeID{0}
	node1 := avalanchego.NodeID{1}
	node2 := avalanchego.NodeID{2}
	node3 := avalanchego.NodeID{3}

	nodeID2Index := map[avalanchego.NodeID]int{
		node0: 0,
		node1: 1,
		node2: 2,
	}

	logger := testutil.MakeLogger(t)

	t.Run("filters by p-chain height", func(t *testing.T) {
		approvals := ValidatorSetApprovals{
			{NodeID: node0, PChainHeight: 100},
			{NodeID: node1, PChainHeight: 200},
		}
		oldApproving := avalanchego.BitmaskFromBytes(nil)
		result := sanitizeApprovals(approvals, 100, common.Digest{}, nodeID2Index, oldApproving, logger)
		require.Len(t, result, 1)
		require.Equal(t, node0, result[0].NodeID)
	})

	t.Run("filters by aux info digest", func(t *testing.T) {
		approvals := ValidatorSetApprovals{
			{NodeID: node0, PChainHeight: 100, AuxInfoDigest: common.Digest{0xAA}},
			{NodeID: node1, PChainHeight: 100, AuxInfoDigest: common.Digest{0xBB}},
		}
		oldApproving := avalanchego.BitmaskFromBytes(nil)
		result := sanitizeApprovals(approvals, 100, common.Digest{0xAA}, nodeID2Index, oldApproving, logger)
		require.Len(t, result, 1)
		require.Equal(t, node0, result[0].NodeID)
	})

	t.Run("filters out already approved", func(t *testing.T) {
		approvals := ValidatorSetApprovals{
			{NodeID: node0, PChainHeight: 100},
			{NodeID: node1, PChainHeight: 100},
		}
		oldApproving := avalanchego.BitmaskFromBytes([]byte{1})
		result := sanitizeApprovals(approvals, 100, common.Digest{}, nodeID2Index, oldApproving, logger)
		require.Len(t, result, 1)
		require.Equal(t, node1, result[0].NodeID)
	})

	t.Run("filters out nodes not in validator set", func(t *testing.T) {
		approvals := ValidatorSetApprovals{
			{NodeID: node3, PChainHeight: 100},
			{NodeID: node2, PChainHeight: 100},
		}
		oldApproving := avalanchego.BitmaskFromBytes(nil)
		result := sanitizeApprovals(approvals, 100, common.Digest{}, nodeID2Index, oldApproving, logger)
		require.Len(t, result, 1)
		require.Equal(t, node2, result[0].NodeID)
	})

	t.Run("deduplicates by node ID", func(t *testing.T) {
		approvals := ValidatorSetApprovals{
			{NodeID: node0, PChainHeight: 100},
			{NodeID: node0, PChainHeight: 100},
		}
		oldApproving := avalanchego.BitmaskFromBytes(nil)
		result := sanitizeApprovals(approvals, 100, common.Digest{}, nodeID2Index, oldApproving, logger)
		require.Len(t, result, 1)
	})
}

func TestComputeNewApproverSignaturesAndSigners(t *testing.T) {
	node0 := avalanchego.NodeID{0}
	node1 := avalanchego.NodeID{1}
	node2 := avalanchego.NodeID{2}

	nodeID2Index := map[avalanchego.NodeID]int{
		node0: 0,
		node1: 1,
		node2: 2,
	}

	logger := testutil.MakeLogger(t)

	t.Run("duplicate peer with already-approved node does not double-aggregate", func(t *testing.T) {
		// node0 is already in the previous approvals (bit 0 set). A duplicate peer
		// entry for node0 must not append node0's signature to the new aggregate
		// (the prior aggregate already covers it via prevApprovals.Signature).
		prevApprovals := &NextEpochApprovals{
			NodeIDs:   []byte{1}, // bit 0
			Signature: []byte("existing"),
		}
		oldApproving := avalanchego.BitmaskFromBytes([]byte{1})

		peers := ValidatorSetApprovals{
			{NodeID: node0, Signature: []byte("sig0")},
			{NodeID: node0, Signature: []byte("sig0")},
		}

		aggSig, newApproving, err := computeNewApproverSignaturesAndSigners(prevApprovals, peers, oldApproving, nodeID2Index, concatAggregator{}, logger)
		require.NoError(t, err)
		require.True(t, newApproving.Contains(0))
		require.Equal(t, 1, newApproving.Len())
		// Only the existing aggregate should remain; node0's sig is already covered by it.
		require.Equal(t, []byte("existing"), aggSig)
	})

	t.Run("nil approvals", func(t *testing.T) {
		oldApproving := avalanchego.BitmaskFromBytes(nil)

		peers := ValidatorSetApprovals{
			{NodeID: node0, Signature: []byte("sig0")},
			{NodeID: node1, Signature: []byte("sig1")},
		}

		_, _, err := computeNewApproverSignaturesAndSigners(nil, peers, oldApproving, nodeID2Index, concatAggregator{}, logger)
		require.Error(t, err)
	})

	t.Run("new approvals with no previous", func(t *testing.T) {
		prevApprovals := &NextEpochApprovals{}
		oldApproving := avalanchego.BitmaskFromBytes(nil)

		peers := ValidatorSetApprovals{
			{NodeID: node0, Signature: []byte("sig0")},
			{NodeID: node1, Signature: []byte("sig1")},
		}

		aggSig, newApproving, err := computeNewApproverSignaturesAndSigners(prevApprovals, peers, oldApproving, nodeID2Index, concatAggregator{}, logger)
		require.NoError(t, err)
		require.True(t, newApproving.Contains(0))
		require.True(t, newApproving.Contains(1))
		require.False(t, newApproving.Contains(2))
		require.Equal(t, []byte("sig0sig1"), aggSig)
	})

	t.Run("new approvals added to existing", func(t *testing.T) {
		prevApprovals := &NextEpochApprovals{
			NodeIDs:   []byte{1}, // bit 0
			Signature: []byte("existing"),
		}
		oldApproving := avalanchego.BitmaskFromBytes([]byte{1}) // node0 already approved

		peers := ValidatorSetApprovals{
			{NodeID: node2, Signature: []byte("sig2")},
		}

		aggSig, newApproving, err := computeNewApproverSignaturesAndSigners(prevApprovals, peers, oldApproving, nodeID2Index, concatAggregator{}, logger)
		require.NoError(t, err)
		require.True(t, newApproving.Contains(0))  // preserved from old
		require.True(t, newApproving.Contains(2))  // newly added
		require.False(t, newApproving.Contains(1)) // not approved
		require.Equal(t, []byte("sig2existing"), aggSig)
	})

	t.Run("no new approvals with existing signature", func(t *testing.T) {
		prevApprovals := &NextEpochApprovals{
			NodeIDs:   []byte{1},
			Signature: []byte("existing"),
		}
		oldApproving := avalanchego.BitmaskFromBytes([]byte{1})

		aggSig, newApproving, err := computeNewApproverSignaturesAndSigners(prevApprovals, nil, oldApproving, nodeID2Index, concatAggregator{}, logger)
		require.NoError(t, err)
		require.True(t, newApproving.Contains(0))
		require.Equal(t, []byte("existing"), aggSig)
	})

	t.Run("peer not in validator set is skipped", func(t *testing.T) {
		prevApprovals := &NextEpochApprovals{}
		oldApproving := avalanchego.BitmaskFromBytes(nil)
		unknownNode := avalanchego.NodeID{99}

		peers := ValidatorSetApprovals{
			{NodeID: unknownNode, Signature: []byte("unknown")},
			{NodeID: node0, Signature: []byte("sig0")},
		}

		aggSig, newApproving, err := computeNewApproverSignaturesAndSigners(prevApprovals, peers, oldApproving, nodeID2Index, concatAggregator{}, logger)
		require.NoError(t, err)
		require.True(t, newApproving.Contains(0))
		require.Equal(t, 1, newApproving.Len())
		require.Equal(t, []byte("sig0"), aggSig)
	})

	t.Run("aggregation error propagated", func(t *testing.T) {
		prevApprovals := &NextEpochApprovals{}
		oldApproving := avalanchego.BitmaskFromBytes(nil)
		peers := ValidatorSetApprovals{
			{NodeID: node0, Signature: []byte("sig0")},
		}

		_, _, err := computeNewApproverSignaturesAndSigners(prevApprovals, peers, oldApproving, nodeID2Index, failingAggregator{}, logger)
		require.ErrorIs(t, err, errTestAggregationFailed)
	})
}

// TestBuildBlockCollectingApprovalsDedupsOwnApprovalAcrossRounds drives BuildBlock
// twice in the collecting-approvals state with no peer approvals at any point,
// and verifies that the optimistic self-sign added on each round is deduplicated:
// the approval bitmask carried in NextEpochApprovals does not grow between the
// first and second built block.
func TestBuildBlockCollectingApprovalsDedupsOwnApprovalAcrossRounds(t *testing.T) {
	sm, tc := newStateMachine(t)

	// This test is about approval dedup, not auxiliary info. Use an app whose history
	// is always final so approvals are collected from the first collecting round
	// (the builder only collects approvals once the aux info history is ready).
	sm.AuxiliaryInfoApp = &noopTestAuxInfoApp{}

	// Use concatAggregator so that AppendSignatures(existing) with zero new
	// signatures returns `existing` verbatim. This makes signature equality
	// a direct witness that no new signature was aggregated in.
	sm.SignatureAggregatorCreator = func(_ []common.Node) common.SignatureAggregator {
		return concatAggregator{}
	}

	// Place MyNodeID at index 0 of a 3-node validator set so quorum is not
	// reachable from a single approval (canSeal stays false on both rounds).
	var myID avalanchego.NodeID
	copy(myID[:], sm.MyNodeID)
	validators := NodeBLSMappings{
		{NodeID: avalanchego.NodeID(sm.MyNodeID), BLSKey: []byte{1}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xBB}, BLSKey: []byte{2}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xCC}, BLSKey: []byte{3}, Weight: 1},
	}
	tc.validatorSetRetriever.result = validators

	// Parent block: epoch transition has started (NextPChainReferenceHeight > 0)
	// but no approvals have been collected yet. NextState() returns
	// stateBuildCollectingApprovals.
	parentSeq := uint64(10)
	parent := StateMachineBlock{
		InnerBlock: &testutil.InnerBlock{TS: time.Now(), BlockHeight: 1, Content: []byte{0xAA}},
		Metadata: StateMachineMetadata{
			PChainHeight: 200,
			SimplexProtocolMetadata: common.ProtocolMetadata{
				Seq: parentSeq, Round: 5, Epoch: 1,
			},
			SimplexEpochInfo: SimplexEpochInfo{
				PChainReferenceHeight:     100,
				NextPChainReferenceHeight: 200,
				PrevVMBlockSeq:            parentSeq - 1,
			},
		},
	}
	tc.blockStore[parentSeq] = &outerBlock{block: parent}

	// ----- Round 1: first collecting-approvals block -----
	tc.blockBuilder.Block = &testutil.InnerBlock{TS: time.Now(), BlockHeight: 2, Content: []byte{0x01}}
	md1 := common.ProtocolMetadata{Seq: parentSeq + 1, Round: 6, Epoch: 1, Prev: parent.Digest()}
	block1, err := sm.BuildBlock(context.Background(), md1, emptyBlacklist)
	require.NoError(t, err)
	require.NotNil(t, block1.Metadata.SimplexEpochInfo.NextEpochApprovals,
		"first block in collecting-approvals state must carry NextEpochApprovals")

	firstNodeIDs := block1.Metadata.SimplexEpochInfo.NextEpochApprovals.NodeIDs
	firstSig := block1.Metadata.SimplexEpochInfo.NextEpochApprovals.Signature
	require.Equal(t, []byte{1}, firstNodeIDs, "only MyNodeID (bit 0) should be set after the first round")
	require.NotEmpty(t, firstSig)

	// Make block1 the parent of the next call.
	tc.blockStore[md1.Seq] = &outerBlock{block: *block1}

	// ----- Round 2: another collecting-approvals block, still no peer approvals -----
	tc.blockBuilder.Block = &testutil.InnerBlock{TS: time.Now(), BlockHeight: 3, Content: []byte{0x02}}
	md2 := common.ProtocolMetadata{Seq: md1.Seq + 1, Round: 7, Epoch: 1, Prev: block1.Digest()}
	block2, err := sm.BuildBlock(context.Background(), md2, emptyBlacklist)
	require.NoError(t, err)
	require.NotNil(t, block2.Metadata.SimplexEpochInfo.NextEpochApprovals)

	// The optimistic self-sign on round 2 must be deduplicated against the
	// prior NextEpochApprovals — the approver set must not have grown.
	require.Equal(t, firstNodeIDs, block2.Metadata.SimplexEpochInfo.NextEpochApprovals.NodeIDs,
		"approver bitmask must be unchanged after a second self-sign with no peer approvals")
	// And with concatAggregator, AppendSignatures(existing) returns existing
	// when no new signatures were aggregated, so the bytes must match exactly.
	require.Equal(t, firstSig, block2.Metadata.SimplexEpochInfo.NextEpochApprovals.Signature,
		"aggregated signature must be unchanged when no new approvals were aggregated")
}

func TestVerifyCollectingApprovalsNotReady(t *testing.T) {
	// Tests collecting-approvals state while the auxiliary info history is not yet final.
	// In that state the builder does not collect approvals,
	// so a block legitimately carries no NextEpochApprovals. The verifier must
	// (1) accept such a block, (2) not panic when NextEpochApprovals is nil, and
	// (3) reject a block that carries approvals before the aux info is ready.

	const (
		pChainRefHeight     = uint64(100)
		nextPChainRefHeight = uint64(200)
		parentSeq           = uint64(10)
	)

	newSM := func(t *testing.T) (*StateMachine, *testConfig, StateMachineBlock) {
		sm, tc := newStateMachine(t)
		// Default app (AuxiliaryInfoGenVerifier) for newStateMachine has threshold 2
		// so IsSufficient returns false: the aux info is not ready.
		sm.GetPChainHeightForProposing = func() uint64 { return nextPChainRefHeight }
		sm.GetPChainHeightForVerifying = func() uint64 { return nextPChainRefHeight }

		// Parent block: epoch transition in progress (NextPChainReferenceHeight > 0),
		// not yet sealed, so NextState() is stateBuildCollectingApprovals.
		parent := StateMachineBlock{
			InnerBlock: &testutil.InnerBlock{TS: time.Now(), BlockHeight: 1, Content: []byte{0xAA}},
			Metadata: StateMachineMetadata{
				PChainHeight: nextPChainRefHeight,
				SimplexProtocolMetadata: common.ProtocolMetadata{
					Seq: parentSeq, Round: 5, Epoch: 1,
				},
				SimplexEpochInfo: SimplexEpochInfo{
					PChainReferenceHeight:     pChainRefHeight,
					NextPChainReferenceHeight: nextPChainRefHeight,
					PrevVMBlockSeq:            parentSeq - 1,
				},
			},
		}
		tc.blockStore[parentSeq] = &outerBlock{block: parent}
		require.Equal(t, stateBuildCollectingApprovals, parent.Metadata.NextState())
		return sm, tc, parent
	}

	build := func(t *testing.T, sm *StateMachine, tc *testConfig, parent StateMachineBlock) *StateMachineBlock {
		tc.blockBuilder.Block = &testutil.InnerBlock{TS: time.Now(), BlockHeight: 2, Content: []byte{0x01}}
		md := common.ProtocolMetadata{Seq: parentSeq + 1, Round: 6, Epoch: 1, Prev: parent.Digest()}
		block, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
		require.NoError(t, err)
		return block
	}

	t.Run("built not-ready block verifies and carries no approvals", func(t *testing.T) {
		sm, tc, parent := newSM(t)
		block := build(t, sm, tc, parent)

		// No auxiliary info was received and the history isn't ready, so the builder collects
		// neither auxiliary info (nil batch) nor approvals.
		require.Nil(t, block.Metadata.AuxiliaryInfoBatch)
		require.Empty(t, block.Metadata.SimplexEpochInfo.NextEpochApprovals.NodeIDs)
		require.Empty(t, block.Metadata.SimplexEpochInfo.NextEpochApprovals.Signature)

		require.NoError(t, sm.VerifyBlock(context.Background(), block))
	})

	t.Run("nil NextEpochApprovals does not panic", func(t *testing.T) {
		sm, tc, parent := newSM(t)
		block := build(t, sm, tc, parent)
		block.Metadata.SimplexEpochInfo.NextEpochApprovals = nil

		// The regression: verifying a not-ready block with nil approvals must not panic.
		// (The digest no longer matches, so an error is expected — just not a panic.)
		require.NotPanics(t, func() {
			_ = sm.verifyCollectingApprovalsBlock(context.Background(), &parent, block, parentSeq)
		})
	})

	t.Run("approvals before aux info is ready are rejected", func(t *testing.T) {
		sm, tc, parent := newSM(t)
		block := build(t, sm, tc, parent)
		block.Metadata.SimplexEpochInfo.NextEpochApprovals = &NextEpochApprovals{
			NodeIDs:   []byte{1},
			Signature: []byte("sig"),
		}

		err := sm.verifyCollectingApprovalsBlock(context.Background(), &parent, block, parentSeq)
		require.ErrorContains(t, err, "expected no approvals")
	})
}

func TestCollectingApprovalsAuxInfoGating(t *testing.T) {
	// Walks a chain through the collecting-approvals state while the auxiliary info history fills up.
	// With a threshold of 2, the history only becomes final after two distinct votes,
	// so the first two collecting blocks carry auxiliary info but no approvals,
	// and approvals are collected only once the history is ready.
	// Each built block must verify.

	const (
		pChainRefHeight     = uint64(100)
		nextPChainRefHeight = uint64(200)
		parentSeq           = uint64(10)
	)

	sm, tc := newStateMachine(t)
	sm.GetPChainHeightForProposing = func() uint64 { return nextPChainRefHeight }
	sm.GetPChainHeightForVerifying = func() uint64 { return nextPChainRefHeight }

	// Deterministic votes so the built auxiliary info is predictable.
	vote1 := []byte("vote-1")
	vote2 := []byte("vote-2")
	votes := [][]byte{vote1, vote2}
	auxiliaryApp := &voteCountingAuxInfoApp{
		threshold: 2,
		randomTape: func() []byte {
			next := votes[0]
			votes = votes[1:]
			return next
		},
	}
	sm.AuxiliaryInfoApp = auxiliaryApp

	// A 3-node validator set including MyNodeID at index 0
	validators := NodeBLSMappings{
		{NodeID: avalanchego.NodeID(sm.MyNodeID), BLSKey: []byte{1}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xBB}, BLSKey: []byte{2}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xCC}, BLSKey: []byte{3}, Weight: 1},
	}
	tc.validatorSetRetriever.result = validators

	parent := StateMachineBlock{
		InnerBlock: &testutil.InnerBlock{TS: time.Now(), BlockHeight: 1, Content: []byte{0xAA}},
		Metadata: StateMachineMetadata{
			PChainHeight: nextPChainRefHeight,
			SimplexProtocolMetadata: common.ProtocolMetadata{
				Seq: parentSeq, Round: 5, Epoch: 1,
			},
			SimplexEpochInfo: SimplexEpochInfo{
				PChainReferenceHeight:     pChainRefHeight,
				NextPChainReferenceHeight: nextPChainRefHeight,
				PrevVMBlockSeq:            parentSeq - 1,
			},
		},
	}
	tc.blockStore[parentSeq] = &outerBlock{block: parent}

	// buildAndVerify constructs the next collecting block on top of prev, stores it so it can serve
	// as a parent (and as a back-pointer target for the aux info history), and verifies it.
	buildAndVerify := func(seq uint64, prev StateMachineBlock) *StateMachineBlock {
		tc.blockBuilder.Block = &testutil.InnerBlock{TS: time.Now(), BlockHeight: seq, Content: []byte{byte(seq)}}
		md := common.ProtocolMetadata{Seq: seq, Round: seq, Epoch: 1, Prev: prev.Digest()}
		block, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
		require.NoError(t, err)
		require.NoError(t, sm.VerifyBlock(context.Background(), block))
		tc.blockStore[seq] = &outerBlock{block: *block}
		return block
	}

	approvals := func(b *StateMachineBlock) *NextEpochApprovals {
		return b.Metadata.SimplexEpochInfo.NextEpochApprovals
	}
	// requireAuxInfo compares the meaningful fields, ignoring the cached canoto size.
	requireAuxInfo := func(want, got *AuxiliaryInfoBatch) {
		require.True(t, want.Equal(got), "expected aux info %+v, got %+v", want, got)
	}

	auxVersionId := auxiliaryApp.DefaultVersionID()
	firstAuxInfoBytes, err := auxiliaryApp.Generate(auxVersionId, validators, [][]byte{})
	require.NoError(t, err)
	firstAuxInfo := common.AuxiliaryInfo{
		Version: auxVersionId,
		Data:    firstAuxInfoBytes,
	}
	sm.HandleAuxiliaryInfo(firstAuxInfo, validators[0].NodeID)

	// block1: history empty, not final -> generates vote1, collects no approvals.
	block1 := buildAndVerify(parentSeq+1, parent)
	requireAuxInfo(&AuxiliaryInfoBatch{data: []common.AuxiliaryInfo{firstAuxInfo}}, block1.Metadata.AuxiliaryInfoBatch)
	require.Empty(t, approvals(block1).NodeIDs)

	// we get another auxiliary info sent
	auxInfoHistory, err := GetAuxiliaryHistory(block1, parentSeq+1, sm.GetBlock, auxVersionId)
	require.NoError(t, err)
	secondAuxInfoBytes, err := auxiliaryApp.Generate(auxVersionId, validators, auxInfoHistory.Data)
	require.NoError(t, err)
	secondAuxInfo := common.AuxiliaryInfo{
		Version: auxVersionId,
		Data:    secondAuxInfoBytes,
	}
	sm.HandleAuxiliaryInfo(secondAuxInfo, validators[1].NodeID)

	// block2: history [vote1], still not final -> generates vote2, collects no approvals.
	block2 := buildAndVerify(parentSeq+2, *block1)
	requireAuxInfo(&AuxiliaryInfoBatch{data: []common.AuxiliaryInfo{secondAuxInfo}, PrevAuxInfoSeq: parentSeq + 1}, block2.Metadata.AuxiliaryInfoBatch)
	require.Empty(t, approvals(block2).NodeIDs)

	block3 := buildAndVerify(parentSeq+3, *block2)
	requireAuxInfo(&AuxiliaryInfoBatch{PrevAuxInfoSeq: parentSeq + 2}, block3.Metadata.AuxiliaryInfoBatch)

	// The collected approval must be signed over the epoch-transition payload for the
	//next epoch's P-chain reference height (200) and the digest
	// of the final auxiliary info history, which is sha256 of the last vote (vote2).
	wantSigned, err := assembleApprovalToBeSigned(nextPChainRefHeight, sha256.Sum256(vote2))
	require.NoError(t, err)
	require.NoError(t, (&signatureVerifier{}).VerifySignature(wantSigned, approvals(block3).Signature, nil),
		"NextEpochApprovals signature must verify against P-chain height 200 and the digest of vote2")

	// block4: built on the empty-Info block3 while still collecting approvals (1/3 is below
	// quorum). Its PrevAuxInfoSeq must SKIP the empty block3 and point at block2 (parentSeq+2),
	// the most recent non-empty Info block -- not at its immediate parent block3 (parentSeq+3).
	// This is the case the rest of the chain never reaches and where "skip" differs from "successive".
	block4 := buildAndVerify(parentSeq+4, *block3)
	require.NotEqual(t, parentSeq+3, block4.Metadata.AuxiliaryInfoBatch.PrevAuxInfoSeq,
		"PrevAuxInfoSeq must not point at the empty-Info parent block3")
	requireAuxInfo(&AuxiliaryInfoBatch{PrevAuxInfoSeq: parentSeq + 2}, block4.Metadata.AuxiliaryInfoBatch)

	// block5: another empty-Info block on top of the empty block4. The back-pointer still skips
	// the whole empty run and points at block2, confirming the skip persists across consecutive
	// empty-Info blocks (collectAuxiliaryInfo finds the same most-recent non-empty block each time).
	block5 := buildAndVerify(parentSeq+5, *block4)
	requireAuxInfo(&AuxiliaryInfoBatch{PrevAuxInfoSeq: parentSeq + 2}, block5.Metadata.AuxiliaryInfoBatch)
}

// TestVerifyCollectingApprovalsRejectsIllegalAuxInfoBatch ensures the verifier must validate
// each proposed datum against the history including the datums before it in the batch.
// Each duplicate vote is a legal append on its own, but the second becomes illegal once the first is appended.
func TestVerifyCollectingApprovalsRejectsIllegalAuxInfoBatch(t *testing.T) {
	const (
		pChainRefHeight     = uint64(100)
		nextPChainRefHeight = uint64(200)
		parentSeq           = uint64(10)
	)

	sm, tc := newStateMachine(t)
	sm.GetPChainHeightForProposing = func() uint64 { return nextPChainRefHeight }
	sm.GetPChainHeightForVerifying = func() uint64 { return nextPChainRefHeight }

	validators := NodeBLSMappings{
		{NodeID: avalanchego.NodeID(sm.MyNodeID), BLSKey: []byte{1}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xBB}, BLSKey: []byte{2}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xCC}, BLSKey: []byte{3}, Weight: 1},
	}
	tc.validatorSetRetriever.result = validators

	parent := StateMachineBlock{
		InnerBlock: &testutil.InnerBlock{TS: time.Now(), BlockHeight: 1, Content: []byte{0xAA}},
		Metadata: StateMachineMetadata{
			PChainHeight: nextPChainRefHeight,
			SimplexProtocolMetadata: common.ProtocolMetadata{
				Seq: parentSeq, Round: 5, Epoch: 1,
			},
			SimplexEpochInfo: SimplexEpochInfo{
				PChainReferenceHeight:     pChainRefHeight,
				NextPChainReferenceHeight: nextPChainRefHeight,
				PrevVMBlockSeq:            parentSeq - 1,
			},
		},
	}
	tc.blockStore[parentSeq] = &outerBlock{block: parent}

	vote := common.AuxiliaryInfo{Version: 1, Data: []byte("vote-1")}
	sm.HandleAuxiliaryInfo(vote, validators[1].NodeID)

	tc.blockBuilder.Block = &testutil.InnerBlock{TS: time.Now(), BlockHeight: 2, Content: []byte{0x01}}
	md := common.ProtocolMetadata{Seq: parentSeq + 1, Round: 6, Epoch: 1, Prev: parent.Digest()}
	block, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
	require.NoError(t, err)
	wantBatch := &AuxiliaryInfoBatch{data: []common.AuxiliaryInfo{vote}}
	require.True(t, wantBatch.Equal(block.Metadata.AuxiliaryInfoBatch))

	// A malicious proposer includes the same vote twice.
	block.Metadata.AuxiliaryInfoBatch = &AuxiliaryInfoBatch{data: []common.AuxiliaryInfo{vote, vote}}

	err = sm.VerifyBlock(context.Background(), block)
	require.ErrorIs(t, err, errAuxInfoIllegalAppend)
}

func TestCollectingApprovalsAuxInfoVersionIDIsBackwardCompatible(t *testing.T) {
	// Backward compatibility: once an epoch has a VersionID set on its auxiliary info, that
	// VersionID must be reused for the rest of the epoch -- for both building AND verifying
	// subsequent blocks -- even if the application's DefaultVersionID() later changes.
	//
	// GetAuxiliaryHistory only consults DefaultVersionID() when the auxiliary info history is
	// empty; once a block carries a VersionID, every later build and verify reads that VersionID
	// back from the chain instead. Auxiliary info is no longer generated inside the block: it
	// arrives from peers via HandleAuxiliaryInfo and is collected into the block being built. So the
	// first collecting block establishes the epoch's VersionID (1) from the received vote while the
	// default is still 1, then we flip DefaultVersionID() to 2. Because the epoch already carries a
	// VersionID on-chain, every Generate()/IsLegalAppend()/IsSufficient() invocation -- on both the
	// build and verify paths -- must keep using VersionID 1, never 2. The app asserts that
	// internally: it requires the VersionID it receives to equal expectedVersionID, which we hold at
	// 1 throughout.

	const (
		pChainRefHeight     = uint64(100)
		nextPChainRefHeight = uint64(200)
		parentSeq           = uint64(10)
	)

	sm, tc := newStateMachine(t)
	sm.GetPChainHeightForVerifying = func() uint64 { return nextPChainRefHeight }
	sm.GetPChainHeightForProposing = func() uint64 { return nextPChainRefHeight }

	// threshold 4 so the history never becomes sufficient across the three collecting blocks we
	// build: every block collects a freshly received auxiliary vote (never approvals), giving one
	// "first" build under the original default and two "later" builds after the default changes.
	// defaultVersionID starts at 1 (the original default); expectedVersionID stays 1 for the whole
	// test -- the app asserts every invocation uses it.
	app := &versionRecordingAuxInfoApp{
		t:                 t,
		threshold:         4,
		votes:             [][]byte{[]byte("vote-1"), []byte("vote-2"), []byte("vote-3")},
		defaultVersionID:  1,
		expectedVersionID: 1,
	}
	sm.AuxiliaryInfoApp = app

	validators := NodeBLSMappings{
		{NodeID: avalanchego.NodeID(sm.MyNodeID), BLSKey: []byte{1}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xBB}, BLSKey: []byte{2}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xCC}, BLSKey: []byte{3}, Weight: 1},
	}
	tc.validatorSetRetriever.result = validators

	// A plain parent with no auxiliary info yet: the epoch's VersionID is established by the first
	// received auxiliary vote rather than pre-seeded into the block.
	parent := StateMachineBlock{
		InnerBlock: &testutil.InnerBlock{TS: time.Now(), BlockHeight: 1, Content: []byte{0xAA}},
		Metadata: StateMachineMetadata{
			PChainHeight: nextPChainRefHeight,
			SimplexProtocolMetadata: common.ProtocolMetadata{
				Seq: parentSeq, Round: 5, Epoch: 1,
			},
			SimplexEpochInfo: SimplexEpochInfo{
				PChainReferenceHeight:     pChainRefHeight,
				NextPChainReferenceHeight: nextPChainRefHeight,
				PrevVMBlockSeq:            parentSeq - 1,
			},
		},
	}
	tc.blockStore[parentSeq] = &outerBlock{block: parent}

	// buildAndVerify constructs the next collecting block on top of prev, verifies it, and stores it so it
	// can serve as the next parent (and as a back-pointer target for the aux info history).
	buildAndVerify := func(seq uint64, prev StateMachineBlock) *StateMachineBlock {
		tc.blockBuilder.Block = &testutil.InnerBlock{TS: time.Now(), BlockHeight: seq, Content: []byte{byte(seq)}}
		md := common.ProtocolMetadata{Seq: seq, Round: seq, Epoch: 1, Prev: prev.Digest()}
		block, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
		require.NoError(t, err)
		require.NoError(t, sm.VerifyBlock(context.Background(), block))
		tc.blockStore[seq] = &outerBlock{block: *block}
		return block
	}

	// receiveAuxVote generates the next vote under VersionID 1 and delivers it as if received from
	// the given validator, so the next built block collects it into its auxiliary info.
	receiveAuxVote := func(from avalanchego.NodeID) {
		data, err := app.Generate(app.expectedVersionID, validators, nil)
		require.NoError(t, err)
		sm.HandleAuxiliaryInfo(common.AuxiliaryInfo{Version: app.expectedVersionID, Data: data}, from)
	}

	// block1: default is still 1 and the history is empty, so the received vote (VersionID 1) sets
	// the epoch's VersionID. Building and verifying block1 both read 1 from the default. We then flip
	// the default to 2; every later build/verify must keep reading 1 back from the chain.
	receiveAuxVote(validators[0].NodeID)
	block1 := buildAndVerify(parentSeq+1, parent)
	require.Equal(t, common.VersionID(1), block1.Metadata.AuxiliaryInfoBatch.data[0].Version)
	app.defaultVersionID = 2

	// block2, block3: the default is now 2, but each block's build and verify still read VersionID 1
	// back from the chain and ignore the changed default.
	receiveAuxVote(validators[1].NodeID)
	block2 := buildAndVerify(parentSeq+2, *block1)
	require.Equal(t, common.VersionID(1), block2.Metadata.AuxiliaryInfoBatch.data[0].Version)

	receiveAuxVote(validators[2].NodeID)
	block3 := buildAndVerify(parentSeq+3, *block2)
	require.Equal(t, common.VersionID(1), block3.Metadata.AuxiliaryInfoBatch.data[0].Version)
}

func TestCollectingApprovalsIncludesMultipleAuxInfoMessages(t *testing.T) {
	// Multiple auxiliary info messages received from distinct validators are all collected into a
	// single built block's AuxiliaryInfoBatch.

	const (
		pChainRefHeight     = uint64(100)
		nextPChainRefHeight = uint64(200)
		parentSeq           = uint64(10)
	)

	sm, tc := newStateMachine(t)
	sm.GetPChainHeightForProposing = func() uint64 { return nextPChainRefHeight }
	sm.GetPChainHeightForVerifying = func() uint64 { return nextPChainRefHeight }

	// A high threshold keeps the history from ever becoming sufficient, so the block stays in the
	// collecting-approvals state and carries the received auxiliary info instead of sealing.
	sm.AuxiliaryInfoApp = &voteCountingAuxInfoApp{threshold: 10}

	validators := NodeBLSMappings{
		{NodeID: avalanchego.NodeID(sm.MyNodeID), BLSKey: []byte{1}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xBB}, BLSKey: []byte{2}, Weight: 1},
		{NodeID: avalanchego.NodeID{0xCC}, BLSKey: []byte{3}, Weight: 1},
	}
	tc.validatorSetRetriever.result = validators

	parent := StateMachineBlock{
		InnerBlock: &testutil.InnerBlock{TS: time.Now(), BlockHeight: 1, Content: []byte{0xAA}},
		Metadata: StateMachineMetadata{
			PChainHeight: nextPChainRefHeight,
			SimplexProtocolMetadata: common.ProtocolMetadata{
				Seq: parentSeq, Round: 5, Epoch: 1,
			},
			SimplexEpochInfo: SimplexEpochInfo{
				PChainReferenceHeight:     pChainRefHeight,
				NextPChainReferenceHeight: nextPChainRefHeight,
				PrevVMBlockSeq:            parentSeq - 1,
			},
		},
	}
	tc.blockStore[parentSeq] = &outerBlock{block: parent}

	version := sm.AuxiliaryInfoApp.DefaultVersionID()

	// buildWithAuxMessages delivers one distinct auxiliary message per validator, builds a block on
	// top of prev, verifies and stores it, and asserts the block collected exactly those messages.
	// collectAuxInfo orders entries by NodeID, so the payloads are compared as a set.
	buildWithAuxMessages := func(seq uint64, prev StateMachineBlock, payloads [][]byte) *StateMachineBlock {
		require.Len(t, payloads, len(validators))
		for i, payload := range payloads {
			sm.HandleAuxiliaryInfo(common.AuxiliaryInfo{Version: version, Data: payload}, validators[i].NodeID)
		}

		tc.blockBuilder.Block = &testutil.InnerBlock{TS: time.Now(), BlockHeight: seq, Content: []byte{byte(seq)}}
		md := common.ProtocolMetadata{Seq: seq, Round: seq, Epoch: 1, Prev: prev.Digest()}
		block, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
		require.NoError(t, err)
		require.NoError(t, sm.VerifyBlock(context.Background(), block))
		tc.blockStore[seq] = &outerBlock{block: *block}

		require.NotNil(t, block.Metadata.AuxiliaryInfoBatch)
		gotPayloads := make([][]byte, 0, len(payloads))
		for _, info := range block.Metadata.AuxiliaryInfoBatch.data {
			require.Equal(t, version, info.Version)
			gotPayloads = append(gotPayloads, info.Data)
		}
		require.ElementsMatch(t, payloads, gotPayloads)
		return block
	}

	// First batch of three messages lands in block1.
	block1 := buildWithAuxMessages(parentSeq+1, parent, [][]byte{[]byte("aux-a"), []byte("aux-b"), []byte("aux-c")})

	// A second batch of three messages lands in block2, built on top of block1.
	block2 := buildWithAuxMessages(parentSeq+2, *block1, [][]byte{[]byte("aux-d"), []byte("aux-e"), []byte("aux-f")})

	// block2 links back to block1, the most recent block carrying non-empty auxiliary info.
	require.Equal(t, parentSeq+1, block2.Metadata.AuxiliaryInfoBatch.PrevAuxInfoSeq)
}

// blockingBlockBuilder waits in WaitForPendingBlock until it is handed a pending block, the way a
// real VM waits on its mempool. The notification is consumed by a single waiter.
type blockingBlockBuilder struct {
	pending chan struct{}
}

func (b *blockingBlockBuilder) BuildBlock(context.Context, uint64) (avalanchego.VMBlock, error) {
	return nil, nil
}

func (b *blockingBlockBuilder) WaitForPendingBlock(ctx context.Context) {
	select {
	case <-b.pending:
	case <-ctx.Done():
	}
}

// TestMSMWaitForPendingBlock checks when WaitForPendingBlock stops waiting for the VM. It must
// return on its own if BuildBlock would have produced a block.
// In cases like the first Simplex block, an epoch transition in progress, or a Telock extending a
// sealed epoch, blocks are produced regardless of the VM.
// In other cases, the decision to produce a block is delegated to the VM.
func TestMSMWaitForPendingBlock(t *testing.T) {
	const waitTime = 500 * time.Millisecond

	// The current validator set is now read from a block's BlockValidationDescriptor rather than
	// from GetValidatorSet(refHeight), so every block whose set we consult must carry one.
	defaultSet := NodeBLSMappings{{BLSKey: []byte{1}, Weight: 1}, {BLSKey: []byte{2}, Weight: 1}}

	var (
		normal     = SimplexEpochInfo{PChainReferenceHeight: 100}
		collecting = SimplexEpochInfo{PChainReferenceHeight: 100, NextPChainReferenceHeight: 200}
		telock     = SimplexEpochInfo{PChainReferenceHeight: 100, NextPChainReferenceHeight: 200, SealingBlockSeq: 5}
		sealing    = SimplexEpochInfo{PChainReferenceHeight: 100, NextPChainReferenceHeight: 200,
			BlockValidationDescriptor: &BlockValidationDescriptor{AggregatedMembership: AggregatedMembership{Members: defaultSet}}, PrevSealingBlockHash: common.Digest{0xaa}}
	)

	// Every parent below belongs to epoch 1.
	block := func(sei SimplexEpochInfo, finalized bool) *outerBlock {
		ob := &outerBlock{block: StateMachineBlock{Metadata: StateMachineMetadata{
			SimplexProtocolMetadata: common.ProtocolMetadata{Epoch: 1},
			SimplexEpochInfo:        sei,
		}}}
		if finalized {
			ob.finalization = &common.Finalization{}
		}
		return ob
	}

	// zeroBlock is the finalized first simplex block that opens epoch 1 and records its validator set.
	zeroBlock := func(set NodeBLSMappings) *outerBlock {
		return &outerBlock{
			finalization: &common.Finalization{},
			block: StateMachineBlock{Metadata: StateMachineMetadata{
				SimplexProtocolMetadata: common.ProtocolMetadata{Epoch: 1},
				SimplexEpochInfo: SimplexEpochInfo{
					BlockValidationDescriptor: &BlockValidationDescriptor{AggregatedMembership: AggregatedMembership{Members: set}},
				},
			}},
		}
	}

	for _, tt := range []struct {
		name string
		// blocks seeds the block store with parent blocks, keyed by their sequence.
		blocks blockStore
		// seq is the sequence of the block we are asking to build (ProtocolMetadata.Seq).
		seq uint64
		// validatorSets seeds the validator set retriever, keyed by P-chain reference height.
		validatorSets map[uint64]NodeBLSMappings
		// pChainHeight, when non-zero, overrides the P-chain height used for proposing/verifying,
		// i.e. the height whose validator set is compared against the current epoch's set.
		pChainHeight uint64
		// vmHasBlock indicates the underlying VM has a pending block ready to build.
		vmHasBlock bool
		// returnsOnItsOwn is true when WaitForPendingBlock is expected to return without
		// needing external cancellation, i.e. the state machine itself has a block to emit.
		returnsOnItsOwn bool
	}{
		{
			// Never happens, we don't build the genesis block. Delegate to the VM as is.
			name: "genesis sequence",
		},
		{
			// The parent is the pre-Simplex genesis block, so the zero block is next.
			name:            "first simplex block",
			seq:             1,
			returnsOnItsOwn: true,
		},
		{
			// The state is unknown, so keep waiting rather than force a block.
			name: "parent block cannot be retrieved",
			seq:  2,
		},
		{
			name:            "collecting approvals for the next epoch",
			seq:             2,
			blocks:          blockStore{1: block(collecting, false)},
			returnsOnItsOwn: true,
		},
		{
			// A Telock has to be emitted to extend the epoch, and it never carries an inner block.
			name:            "sealing block parent, not finalized",
			seq:             2,
			blocks:          blockStore{1: block(sealing, false)},
			returnsOnItsOwn: true,
		},
		{
			// The new epoch is open, so this is an ordinary block again.
			name:   "sealing block parent, finalized",
			seq:    2,
			blocks: blockStore{1: block(sealing, true)},
		},
		{
			// We just moved to the new epoch, whose validator set is the one the sealing block
			// carries. If the set at the latest P-chain height differs from it, another transition
			// is due, so WaitForPendingBlock returns on its own.
			name:            "sealing block parent, finalized, validator set changed again",
			seq:             2,
			blocks:          blockStore{1: block(sealing, true)},
			pChainHeight:    200,
			validatorSets:   map[uint64]NodeBLSMappings{200: {{BLSKey: []byte{9}, Weight: 1}}},
			returnsOnItsOwn: true,
		},
		{
			name:            "Telock parent, sealing block not finalized",
			seq:             8,
			blocks:          blockStore{7: block(telock, false), 5: block(sealing, false)},
			returnsOnItsOwn: true,
		},
		{
			// The Telock we build on is not finalized, but the sealing block it points at is, so
			// the epoch is over. Reading the Telock's own finalization would conclude otherwise.
			name:   "Telock parent, sealing block finalized",
			seq:    8,
			blocks: blockStore{7: block(telock, false), 5: block(sealing, true)},
		},
		{
			// The sealing block cannot be read, so we cannot tell whether the epoch was sealed.
			// Rather than force a Telock on incomplete information, we defer to the VM and only
			// return once it has a block (or the round is cancelled).
			name:   "Telock parent, sealing block cannot be retrieved",
			seq:    8,
			blocks: blockStore{7: block(telock, false)},
		},
		{
			name:   "normal operation, VM has nothing to build",
			seq:    2,
			blocks: blockStore{1: block(normal, false)},
		},
		{
			name:            "normal operation, VM has a pending block",
			seq:             2,
			blocks:          blockStore{1: block(normal, false)},
			vmHasBlock:      true,
			returnsOnItsOwn: true,
		},
		{
			// The validator set changed, so a block must record the new P-chain reference height
			// even if the VM has nothing to put in it. The current set is read from the epoch's
			// zero block (seq 1); the set at the latest P-chain height differs from it.
			name:            "normal operation, validator set changed",
			seq:             3,
			blocks:          blockStore{1: zeroBlock(defaultSet), 2: block(SimplexEpochInfo{PChainReferenceHeight: 100}, false)},
			pChainHeight:    200,
			validatorSets:   map[uint64]NodeBLSMappings{200: {{BLSKey: []byte{9}, Weight: 1}}},
			returnsOnItsOwn: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bb := &blockingBlockBuilder{pending: make(chan struct{}, 1)}
			if tt.vmHasBlock {
				bb.pending <- struct{}{}
			}

			sm, cfg := newStateMachine(t)
			sm.BlockBuilder = bb
			sm.MaxBlockBuildingWaitTime = waitTime
			if tt.pChainHeight != 0 {
				sm.GetPChainHeightForProposing = func() uint64 { return tt.pChainHeight }
				sm.GetPChainHeightForVerifying = func() uint64 { return tt.pChainHeight }
			}
			cfg.validatorSetRetriever.resultMap = tt.validatorSets
			for seq, blk := range tt.blocks {
				cfg.blockStore[seq] = blk
			}

			md := common.ProtocolMetadata{Seq: tt.seq}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan struct{})
			go func() {
				defer close(done)
				sm.WaitForPendingBlock(ctx, md)
			}()

			if tt.returnsOnItsOwn {
				select {
				case <-done:
				case <-time.After(time.Minute):
					require.FailNow(t, "WaitForPendingBlock should have returned on its own")
				}
				return
			}

			select {
			case <-done:
				require.FailNow(t, "WaitForPendingBlock returned with nothing to build")
			case <-time.After(waitTime * 5):
			}

			cancel()
			select {
			case <-done:
			case <-time.After(time.Second * 5):
				require.FailNow(t, "WaitForPendingBlock ignored cancellation")
			}
		})
	}
}

// buildFirstSimplexBlock builds and stores the first simplex ("zero") block on top of
// genesis, so that a follow-up normal-op block can be built on it.
func buildFirstSimplexBlock(t *testing.T, sm *StateMachine, tc *testConfig) *StateMachineBlock {
	md := common.ProtocolMetadata{Round: 1, Seq: 1, Epoch: 1, Prev: genesisBlock.Digest()}
	block, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
	require.NoError(t, err)
	require.NotNil(t, block)
	tc.blockStore[1] = &outerBlock{block: *block}
	return block
}

// emptyBlockRequestingBuilder is a BlockBuilder that models the VM's inner block builder
// being cancelled: BuildBlock optionally cancels the build context with a given cause and
// then returns an error. WaitForPendingBlock is a no-op so the block-building decider
// proceeds to decide that an inner block should be built.
type emptyBlockRequestingBuilder struct {
	cancel      context.CancelCauseFunc
	cancelCause error // if non-nil, BuildBlock cancels the context with this cause before failing
	err         error // error returned by BuildBlock
}

func (b *emptyBlockRequestingBuilder) BuildBlock(context.Context, uint64) (avalanchego.VMBlock, error) {
	if b.cancelCause != nil {
		b.cancel(b.cancelCause)
	}
	return nil, b.err
}

func (b *emptyBlockRequestingBuilder) WaitForPendingBlock(context.Context) {}

// TestMSMBuildBlockBuildsEmptyBlockWhenBlockBuildingCancelled covers the branch in
// buildBlockOrTransitionEpoch where the block-building decider returns an error: when the
// context was cancelled with ErrShouldBuildEmptyBlock, an empty block is returned instead
// of propagating the error.
func TestMSMBuildBlockBuildsEmptyBlockWhenBlockBuildingCancelled(t *testing.T) {
	t.Run("empty-block cause yields an empty block", func(t *testing.T) {
		sm, tc := newStateMachine(t)
		block1 := buildFirstSimplexBlock(t, sm, tc)

		md := common.ProtocolMetadata{Round: 2, Seq: 2, Epoch: 1, Prev: block1.Digest()}
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(common.ErrShouldBuildEmptyBlock)

		block, err := sm.BuildBlock(ctx, md, emptyBlacklist)
		require.NoError(t, err)
		require.NotNil(t, block)
		require.Nil(t, block.InnerBlock)
		require.Nil(t, block.Metadata.AuxiliaryInfoBatch)
		require.Equal(t, md, block.Metadata.SimplexProtocolMetadata)
		// The empty block carries the current P-chain height for proposing (100 in this config).
		require.Equal(t, uint64(100), block.Metadata.PChainHeight)
	})

	t.Run("other cancellation propagates the error", func(t *testing.T) {
		sm, tc := newStateMachine(t)
		block1 := buildFirstSimplexBlock(t, sm, tc)

		md := common.ProtocolMetadata{Round: 2, Seq: 2, Epoch: 1, Prev: block1.Digest()}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		block, err := sm.BuildBlock(ctx, md, emptyBlacklist)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, block)
	})
}

// TestMSMBuildBlockBuildsEmptyBlockWhenInnerBlockBuildingCancelled covers the branch in
// buildBlockOrTransitionEpoch where the inner (VM) block builder fails: when the context
// was cancelled with ErrShouldBuildEmptyBlock, an empty block is returned instead of
// propagating the error.
func TestMSMBuildBlockBuildsEmptyBlockWhenInnerBlockBuildingCancelled(t *testing.T) {
	errVMBuildFailed := errors.New("vm failed to build inner block")

	t.Run("empty-block cause yields an empty block", func(t *testing.T) {
		sm, tc := newStateMachine(t)
		block1 := buildFirstSimplexBlock(t, sm, tc)

		// The decider runs first with a live context and decides to build an inner block;
		// the inner builder then cancels the context with the empty-block cause and fails.
		ctx, cancel := context.WithCancelCause(context.Background())
		sm.BlockBuilder = &emptyBlockRequestingBuilder{
			cancel:      cancel,
			cancelCause: common.ErrShouldBuildEmptyBlock,
			err:         errVMBuildFailed,
		}

		md := common.ProtocolMetadata{Round: 2, Seq: 2, Epoch: 1, Prev: block1.Digest()}
		block, err := sm.BuildBlock(ctx, md, emptyBlacklist)
		require.NoError(t, err)
		require.NotNil(t, block)
		require.Nil(t, block.InnerBlock)
		require.Equal(t, md, block.Metadata.SimplexProtocolMetadata)
		// The empty block carries the decided P-chain height (100 in this config).
		require.Equal(t, uint64(100), block.Metadata.PChainHeight)
	})

	t.Run("inner build failure without empty-block cause propagates the error", func(t *testing.T) {
		sm, tc := newStateMachine(t)
		block1 := buildFirstSimplexBlock(t, sm, tc)

		sm.BlockBuilder = &emptyBlockRequestingBuilder{err: errVMBuildFailed}

		md := common.ProtocolMetadata{Round: 2, Seq: 2, Epoch: 1, Prev: block1.Digest()}
		block, err := sm.BuildBlock(context.Background(), md, emptyBlacklist)
		require.ErrorIs(t, err, errVMBuildFailed)
		require.Nil(t, block)
	})
}

func TestGetCurrentValidatorSet(t *testing.T) {
	validators := NodeBLSMappings{{BLSKey: []byte{1}, Weight: 1}, {BLSKey: []byte{2}, Weight: 1}}
	finalized := &common.Finalization{}

	descriptor := func(members NodeBLSMappings) *BlockValidationDescriptor {
		return &BlockValidationDescriptor{AggregatedMembership: AggregatedMembership{Members: members}}
	}

	// zeroBlock opens the first epoch: it carries a descriptor and points to no previous sealing block.
	zeroBlock := func(bvd *BlockValidationDescriptor, finalization *common.Finalization) *outerBlock {
		return &outerBlock{
			finalization: finalization,
			block: StateMachineBlock{Metadata: StateMachineMetadata{
				SimplexEpochInfo: SimplexEpochInfo{BlockValidationDescriptor: bvd},
			}},
		}
	}

	// sealingBlock at seq opens the epoch numbered seq: it carries a descriptor and points to the previous sealing block.
	sealingBlock := func(seq uint64, bvd *BlockValidationDescriptor, finalization *common.Finalization) *outerBlock {
		return &outerBlock{
			finalization: finalization,
			block: StateMachineBlock{Metadata: StateMachineMetadata{
				SimplexProtocolMetadata: common.ProtocolMetadata{Seq: seq},
				SimplexEpochInfo:        SimplexEpochInfo{PrevSealingBlockHash: common.Digest{1}, BlockValidationDescriptor: bvd},
			}},
		}
	}

	// normalBlock is a block in the epoch that neither opens nor seals it.
	normalBlock := func(seq, epoch uint64) *outerBlock {
		return &outerBlock{
			finalization: finalized,
			block: StateMachineBlock{Metadata: StateMachineMetadata{
				SimplexProtocolMetadata: common.ProtocolMetadata{Seq: seq, Epoch: epoch},
			}},
		}
	}

	for _, testCase := range []struct {
		name string
		// epoch is the epoch of the block whose validator set is looked up.
		epoch uint64
		// parent is the epoch info of the block whose epoch's validator set is looked up.
		parent SimplexEpochInfo
		blocks blockStore

		expected NodeBLSMappings
		// expectedErr is a substring of the expected error message.
		expectedErr string
		// expectedErrIs is a sentinel the expected error must wrap.
		expectedErrIs error
	}{
		{
			name:     "normal block in the first epoch reads the zero block",
			epoch:    1,
			blocks:   blockStore{1: zeroBlock(descriptor(validators), finalized)},
			expected: validators,
		},
		{
			name:     "the zero block is read even before it is finalized",
			epoch:    1,
			blocks:   blockStore{1: zeroBlock(descriptor(validators), nil)},
			expected: validators,
		},
		{
			name:     "normal block in a later epoch reads the finalized sealing block",
			epoch:    10,
			blocks:   blockStore{10: sealingBlock(10, descriptor(validators), finalized)},
			expected: validators,
		},
		{
			name:     "transitioning block reads the sealing block of its epoch",
			epoch:    10,
			parent:   SimplexEpochInfo{NextPChainReferenceHeight: 300},
			blocks:   blockStore{10: sealingBlock(10, descriptor(validators), finalized)},
			expected: validators,
		},
		{
			name:        "sealing block that is not finalized is rejected",
			epoch:       10,
			blocks:      blockStore{10: sealingBlock(10, descriptor(validators), nil)},
			expectedErr: "sealing block 10 is not finalized",
		},
		{
			name:          "missing epoch block",
			epoch:         10,
			blocks:        blockStore{},
			expectedErrIs: common.ErrBlockNotFound,
		},
		{
			name:        "sealing block without validators",
			epoch:       10,
			blocks:      blockStore{10: sealingBlock(10, descriptor(nil), finalized)},
			expectedErr: "block 10 has no validators",
		},
		{
			name:        "epoch block that carries no descriptor",
			epoch:       10,
			blocks:      blockStore{10: normalBlock(10, 1)},
			expectedErr: "block 10 has no validators",
		},
		{
			name:        "sealing block as parent is rejected",
			epoch:       1,
			parent:      SimplexEpochInfo{PrevSealingBlockHash: common.Digest{1}, BlockValidationDescriptor: descriptor(validators)},
			blocks:      blockStore{1: zeroBlock(descriptor(validators), finalized)},
			expectedErr: "did not expect block 20 to be a sealing block or a Telock",
		},
		{
			name:        "Telock as parent is rejected",
			epoch:       1,
			parent:      SimplexEpochInfo{SealingBlockSeq: 15},
			blocks:      blockStore{1: zeroBlock(descriptor(validators), finalized)},
			expectedErr: "did not expect block 20 to be a sealing block or a Telock",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parent := &StateMachineBlock{Metadata: StateMachineMetadata{
				SimplexProtocolMetadata: common.ProtocolMetadata{Seq: 20, Epoch: testCase.epoch},
				SimplexEpochInfo:        testCase.parent,
			}}

			got, err := getCurrentValidatorSet(parent, testCase.blocks.getBlock)

			if testCase.expectedErrIs != nil {
				require.ErrorIs(t, err, testCase.expectedErrIs)
				require.Nil(t, got)
				return
			}
			if testCase.expectedErr != "" {
				require.ErrorContains(t, err, testCase.expectedErr)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.expected, got)
		})
	}
}
