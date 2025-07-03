// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/StephenButtolph/canoto"
	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

func TestBlockSerialization(t *testing.T) {
	unexpectedBlockBytes := errors.New("unexpected block bytes")

	testBlock := snowmantest.BuildChild(snowmantest.Genesis)

	b := &Block{
		vmBlock: testBlock,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
			Prev:    [32]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
	}

	// Serialize the block
	blockBytes, err := b.Bytes()
	require.NoError(t, err)

	tests := []struct {
		name          string
		parseFunc     func(context.Context, []byte) (snowman.Block, error)
		expectedError error
		blockBytes    []byte
	}{
		{
			name:       "block serialization",
			blockBytes: blockBytes,
			parseFunc: func(_ context.Context, b []byte) (snowman.Block, error) {
				if !bytes.Equal(testBlock.BytesV, b) {
					return nil, unexpectedBlockBytes
				}
				return testBlock, nil
			},
			expectedError: nil,
		},
		{
			name:          "block deserialization error",
			blockBytes:    blockBytes,
			expectedError: unexpectedBlockBytes,
			parseFunc: func(_ context.Context, _ []byte) (snowman.Block, error) {
				return nil, unexpectedBlockBytes
			},
		},
		{
			name:          "corrupted block data",
			blockBytes:    []byte("corrupted data"),
			expectedError: canoto.ErrInvalidWireType,
			parseFunc: func(_ context.Context, _ []byte) (snowman.Block, error) {
				return nil, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testVM := &blocktest.VM{
				VM: enginetest.VM{
					T: t,
				},
			}

			testVM.ParseBlockF = tt.parseFunc
			deserializer := &blockDeserializer{
				parser: testVM,
			}

			// Deserialize the block
			deserializedBlock, err := deserializer.DeserializeBlock(tt.blockBytes)
			require.ErrorIs(t, err, tt.expectedError)

			if tt.expectedError == nil {
				require.Equal(t, b.BlockHeader().ProtocolMetadata, deserializedBlock.BlockHeader().ProtocolMetadata)
			}
		})
	}
}

// TestBlockVerify tests the verificatin of a block results in the same bytes as the original block.
func TestBlockVerify(t *testing.T) {
	testBlock := snowmantest.BuildChild(snowmantest.Genesis)
	testVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
		},
	}

	tracker := newBlockTracker(testVM)
	b := &Block{
		vmBlock: testBlock,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
			Prev:    [32]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
		blockTracker: tracker,
	}

	verifiedBlock, err := b.Verify(context.Background())
	require.NoError(t, err, "block should be verified successfully")

	vBlockBytes, err := verifiedBlock.Bytes()
	require.NoError(t, err)

	blockBytes, err := b.Bytes()
	require.NoError(t, err)

	require.Equal(t, blockBytes, vBlockBytes, "block bytes should match after verification")
}

func TestVerifyNotFound(t *testing.T) {
	testBlock := snowmantest.BuildChild(snowmantest.Genesis)
	testVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
		},
	}

	tracker := newBlockTracker(testVM)
	b := &Block{
		vmBlock: testBlock,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
			Prev:    [32]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
		blockTracker: tracker,
	}

	_, err := b.Verify(context.Background())
	require.ErrorIs(t, err, errDigestNotFound)
}

func TestVerifyParentNotAcceptedOrRejected(t *testing.T) {}

func TestVerifyParentAccepted(t *testing.T) {
	vmBlock0 := snowmantest.Genesis
	vmBlock1 := snowmantest.BuildChild(vmBlock0)
	testVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
		},
	}

	tracker := newBlockTracker(testVM)
	seq0 := &Block{
		vmBlock: vmBlock0,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   0,
			Seq:     0,
		},
		blockTracker: tracker,
	}

	seq0Bytes, err := seq0.Bytes()
	require.NoError(t, err)
	
	seq0Digest := computeDigest(seq0Bytes)
	seq0.digest = seq0Digest
	
	seq1 := &Block{
		vmBlock: vmBlock1,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
			Prev:    seq0Digest,
		},
		blockTracker: tracker,
	}
	seq1Bytes, err := seq1.Bytes()
	require.NoError(t, err)
	seq1Digest := computeDigest(seq1Bytes)
	seq1.digest = seq1Digest

	_, err = seq0.Verify(context.Background())
	require.NoError(t, err)

	err = tracker.indexBlock(context.Background(), seq0Digest)
	require.NoError(t, err)
	require.Equal(t, snowtest.Accepted, vmBlock0.Decidable.Status)

	// Verify the second block with the first block as its parent
	_, err = seq1.Verify(context.Background())
	require.NoError(t, err)
	err = tracker.indexBlock(context.Background(), seq1.digest)
	require.NoError(t, err)
	require.Equal(t, snowtest.Accepted, vmBlock1.Decidable.Status)

	// ensure tracker cleans up the block
	require.Nil(t, tracker.simplexDigestsToBlock[seq0Digest])
}

func TestVerifyBlockRejectsSiblings(t *testing.T) {
	vmBlock0 := snowmantest.Genesis
	block0Child0 := snowmantest.BuildChild(vmBlock0)
	block0Child1 := snowmantest.BuildChild(vmBlock0)
	testVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
		},
	}

	tracker := newBlockTracker(testVM)
	seq0 := &Block{
		vmBlock: vmBlock0,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   0,
			Seq:     0,
		},
		blockTracker: tracker,
	}

	seq0Bytes, err := seq0.Bytes()
	require.NoError(t, err)
	
	seq0Digest := computeDigest(seq0Bytes)
	seq0.digest = seq0Digest
	
	// round1Block and round2Block are siblings, both children of seq0
	// this can happen if we notarize a block for round 1, but the rest
	// of the network notarizes a dummy block for round 1. Then
	// we will verify a sibling block for round 2 and must reject the round 1 block.
	round1Block := &Block{
		vmBlock: block0Child0,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
			Prev:    seq0Digest,
		},
		blockTracker: tracker,
	}
	seq1Bytes, err := round1Block.Bytes()
	require.NoError(t, err)
	seq1Digest := computeDigest(seq1Bytes)
	round1Block.digest = seq1Digest

	round2Block := &Block{
		vmBlock: block0Child1,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   2,
			Seq:     1,
			Prev:    seq0Digest,
		},
		blockTracker: tracker,
	}
	seq2Bytes, err := round2Block.Bytes()
	require.NoError(t, err)
	seq2Digest := computeDigest(seq2Bytes)
	round2Block.digest = seq2Digest

	_, err = seq0.Verify(context.Background())
	require.NoError(t, err)

	err = tracker.indexBlock(context.Background(), seq0Digest)
	require.NoError(t, err)
	require.Equal(t, snowtest.Accepted, vmBlock0.Decidable.Status)

	// Verify the second block with the first block as its parent
	_, err = round1Block.Verify(context.Background())
	require.NoError(t, err)
	_, err = round2Block.Verify(context.Background())
	require.NoError(t, err)

	// index the second block, the first block should be rejected
	err = tracker.indexBlock(context.Background(), seq2Digest)
	require.NoError(t, err)
	require.Equal(t, snowtest.Accepted, block0Child1.Decidable.Status)
	require.Equal(t, snowtest.Rejected, block0Child0.Decidable.Status)

	require.Nil(t, tracker.getBlock(seq0Digest))
}

func TestVerifyInnerBlockBreaksHashChain(t *testing.T) {
	testVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
		},
	}

	tracker := newBlockTracker(testVM)
	ctx := context.Background()
	// we have a block whose metadata.prev does not point to their parent
	seq1 := snowmantest.BuildChild(snowmantest.Genesis)

	testVM.LastAcceptedF = func(_ context.Context) (ids.ID, error) {
		return snowmantest.Genesis.ID(), nil
	}

	seq1Block := &Block{
		vmBlock: seq1,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
		},
		blockTracker: tracker,
	}

	seq1Bytes, err := seq1Block.Bytes()
	require.NoError(t, err)
	seq1Digest := computeDigest(seq1Bytes)
	seq1Block.digest = seq1Digest
	
	_, err = seq1Block.Verify(ctx)
	require.NoError(t, err)

	// this block does not extend seq1
	seq2 := snowmantest.BuildChild(snowmantest.Genesis)
	seq2Block := &Block{
		vmBlock: seq2,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   2,
			Seq:     2,
			Prev:    seq1Digest, // a valid prev digest
		},
		blockTracker: tracker,
	}
	
	seq2Bytes, err := seq2Block.Bytes()
	require.NoError(t, err)
	seq2Digest := computeDigest(seq2Bytes)
	seq2Block.digest = seq2Digest

	_, err = seq2Block.Verify(ctx)
	require.ErrorIs(t, err, errMismatchedPrevDigest)
}




// Tests todo
/**
	// TestBlockPrevious 
	// TestBlockTracker removes indexed digests from map as well as rejected blocks
	// TestBlockTracker indexBlock rejects conflicting blocks(siblings)
	// 
*/



// // TestBlockVerify tests when a block is verified, it is added to the block tracker and
// // the verify method is called on the block.
// func TestBlockVerifyTracker(t *testing.T) {
// 	parsedBlock := &snowmantest.Block{}
// 	vm := &blocktest.VM{
// 		ParseBlockF: func(_ context.Context, _ []byte) (snowman.Block, error) {
// 			return parsedBlock, nil
// 		},
// 	}

// 	verifiedBlock := &VerifiedBlock{
// 		innerBlock: []byte("test block data"),
// 		metadata: simplex.ProtocolMetadata{
// 			Epoch: 1,
// 			Round: 1,
// 		},
// 	}

// 	tracker := &blockTracker{
// 		vm:             vm,
// 		acceptedBlocks: make(map[uint64][]blockData),
// 	}

// 	block := &Block{
// 		block:   verifiedBlock,
// 		parser:  vm,
// 		tracker: tracker,
// 	}

// 	vb, err := block.Verify(context.Background())
// 	require.NoError(t, err)
// 	require.Equal(t, verifiedBlock, vb)
// 	// Check we haven't accepted or rejected the block yet
// 	require.Equal(t, snowtest.Undecided, parsedBlock.Decidable.Status)
// }

// // TestRejectStaleBlocks tests that the block tracker correctly rejects stale blocks
// // when a new block is accepted. It should call reject on all blocks that are
// // older than the accepted block's round, and also reject blocks in the same round
// // that are not the accepted block.
// func TestRejectAllStaleBlocks(t *testing.T) {
// 	// older blocks should be rejected
// 	round0Blocks := []blockData{
// 		{
// 			block:  &snowmantest.Block{},
// 			digest: simplex.Digest{0x00, 0x01},
// 		},
// 	}

// 	// blocks in the same round should be rejected except the one with digest 0x02
// 	round1Blocks := []blockData{
// 		{
// 			block:  &snowmantest.Block{},
// 			digest: simplex.Digest{0x01},
// 		},
// 		{
// 			block:  &snowmantest.Block{},
// 			digest: simplex.Digest{0x02},
// 		},
// 		{
// 			block:  &snowmantest.Block{},
// 			digest: simplex.Digest{0x03},
// 		},
// 		{
// 			block:  &snowmantest.Block{},
// 			digest: simplex.Digest{0x04},
// 		},
// 	}

// 	// newer blocks, this block should not be rejected
// 	round2Blocks := []blockData{
// 		{
// 			block:  &snowmantest.Block{},
// 			digest: simplex.Digest{0x11},
// 		},
// 	}

// 	acceptedBlocks := make(map[uint64][]blockData)
// 	acceptedBlocks[0] = round0Blocks
// 	acceptedBlocks[1] = round1Blocks
// 	acceptedBlocks[2] = round2Blocks

// 	tracker := &blockTracker{
// 		acceptedBlocks: acceptedBlocks,
// 	}

// 	require.NoError(t, tracker.rejectAllStaleBlocks(1, simplex.Digest{0x02}))
// 	require.Empty(t, tracker.acceptedBlocks[1])
// 	require.Empty(t, tracker.acceptedBlocks[0])
// 	require.Len(t, tracker.acceptedBlocks[2], 1)

// 	for _, block := range round0Blocks {
// 		require.Equal(t, snowtest.Rejected, block.block.(*snowmantest.Block).Status)
// 	}

// 	for _, block := range round1Blocks {
// 		// require all except the one with digest 0x02 to be rejected
// 		if block.digest == round1Blocks[1].digest {
// 			require.Equal(t, snowtest.Undecided, block.block.(*snowmantest.Block).Status)
// 		} else {
// 			require.Equal(t, snowtest.Rejected, block.block.(*snowmantest.Block).Status)
// 		}
// 	}
// }

// func TestIndexBlock(t *testing.T) {
// 	acceptedBlockDigest := simplex.Digest{0x11}
// 	acceptBlockID := ids.GenerateTestID()
// 	round1Blocks := []blockData{
// 		{
// 			block: &snowmantest.Block{
// 				Decidable: snowtest.Decidable{
// 					IDV: acceptBlockID,
// 				},
// 			},
// 			digest: acceptedBlockDigest, // we will
// 		},
// 	}

// 	acceptedBlocks := make(map[uint64][]blockData)
// 	acceptedBlocks[1] = round1Blocks

// 	setPref := func(_ context.Context, id ids.ID) error {
// 		require.Equal(t, acceptBlockID, id)
// 		return nil
// 	}

// 	tracker := &blockTracker{
// 		acceptedBlocks: acceptedBlocks,
// 		vm: &blocktest.VM{
// 			SetPreferenceF: setPref,
// 		},
// 	}

// 	require.NoError(t, tracker.indexBlock(context.TODO(), 1, acceptedBlockDigest))
// }

// func TestIndexBlockNotVerified(t *testing.T) {
// 	acceptedBlockDigest := simplex.Digest{0x11}
// 	tracker := newBlockTracker()
// 	err := tracker.indexBlock(context.TODO(), 1, acceptedBlockDigest)
// 	require.ErrorIs(t, err, errBlockNotVerified)
// }