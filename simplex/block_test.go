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

// TestVerifyPrevNotFound attempts to verify a block with a prev digest that is not valid.
func TestVerifyPrevNotFound(t *testing.T) {
	ctx := context.Background()

	tracker := newBlockTracker(newBlock(t, nil))
	b := newBlock(t, &testBlockConfig{
		blockTracker: tracker,
		round:        1,
		seq:          1,
		prev:         [32]byte{0x00, 0x012}, // Invalid prev digest
	})

	_, err := b.Verify(ctx)
	require.ErrorIs(t, err, errDigestNotFound)
}

// TestVerifyTwice tests that a block the same vmBlock will only
// have its Verify method called once, even if Verify is called multiple times.
func TestVerifyTwice(t *testing.T) {
	ctx := context.Background()
	b := newBlock(t, nil)

	// Verify the block for the first time
	_, err := b.Verify(ctx)
	require.NoError(t, err)

	// Attempt to verify the block again
	b.vmBlock.(*snowmantest.Block).VerifyV = errors.New("should not be called again")
	_, err = b.Verify(ctx)
	require.NoError(t, err)
}

// TestVerifyGenesis tests that a block with a sequence number of 0 cannot be verified.
func TestVerifyGenesis(t *testing.T) {
	ctx := context.Background()

	// Verify the block for the first time
	_, err := newGenesisBlock(t).Verify(ctx)
	require.ErrorIs(t, err, errGenesisVerification)
}

func TestVerify(t *testing.T) {
	ctx := context.Background()

	b := newBlock(t, nil)

	verifiedBlock, err := b.Verify(ctx)
	require.NoError(t, err)

	// Ensure the verified block matches the original block
	vBlockBytes, err := verifiedBlock.Bytes()
	require.NoError(t, err)

	blockBytes, err := b.Bytes()
	require.NoError(t, err)
	require.Equal(t, blockBytes, vBlockBytes, "block bytes should match after verification")
}

// TestVerifyPrevIsLatest tests that a block with a prev digest that is not found in the block tracker
// successfully verifies the block if it is the latest accepted block.
func TestVerifyPrevIsLatest(t *testing.T) {
	ctx := context.Background()

	b := newBlock(t, nil)
	tracker := b.blockTracker

	_, err := b.Verify(ctx)
	require.NoError(t, err)

	require.NoError(t, tracker.indexBlock(ctx, b.digest))
	require.Equal(t, snowtest.Accepted, b.vmBlock.(*snowmantest.Block).Decidable.Status)

	// Ensure future blocks are verified on the new last accepted id and digest
	nextVMBlock := snowmantest.BuildChild(b.vmBlock.(*snowmantest.Block))
	nextBlock := newBlock(t, &testBlockConfig{
		vmBlock:      nextVMBlock,
		blockTracker: tracker,
		round:        2,
		seq:          2,
		prev:         b.digest,
	})

	_, err = nextBlock.Verify(ctx)
	require.NoError(t, err)
	require.NoError(t, tracker.indexBlock(ctx, nextBlock.digest))
	require.Equal(t, snowtest.Accepted, nextVMBlock.Decidable.Status)
}

// TestVerifyParentAccepted tests that a block, whose parent has been verified and indexed, can
// also be verified and indexed successfully.
func TestVerifyParentAccepted(t *testing.T) {
	ctx := context.Background()

	seq1Block := newBlock(t, nil)
	tracker := seq1Block.blockTracker

	vmBlock2 := snowmantest.BuildChild(seq1Block.vmBlock.(*snowmantest.Block))
	seq2Block := newBlock(t, &testBlockConfig{
		vmBlock:      vmBlock2,
		blockTracker: tracker,
		round:        2,
		seq:          2,
		prev:         seq1Block.digest,
	})

	_, err := seq1Block.Verify(ctx)
	require.NoError(t, err)

	require.NoError(t, tracker.indexBlock(ctx, seq1Block.digest))
	require.Equal(t, snowtest.Accepted, seq1Block.vmBlock.(*snowmantest.Block).Decidable.Status)

	// Verify the second block with the first block as its parent
	_, err = seq2Block.Verify(ctx)
	require.NoError(t, err)
	require.NoError(t, tracker.indexBlock(ctx, seq2Block.digest))
	require.Equal(t, snowtest.Accepted, vmBlock2.Decidable.Status)

	// ensure tracker cleans up the block
	require.Nil(t, tracker.simplexDigestsToBlock[seq1Block.digest])
}

func TestVerifyBlockRejectsSiblings(t *testing.T) {
	ctx := context.Background()
	genesisBlock := newGenesisBlock(t)
	genesisChild0 := snowmantest.BuildChild(snowmantest.Genesis)
	genesisChild1 := snowmantest.BuildChild(snowmantest.Genesis)

	// round1Block and round2Block are siblings, both children of seq0
	// this can happen if we notarize a block for round 1, but the rest
	// of the network notarizes a dummy block for round 1. Then
	// we will verify a sibling block for round 2 and must reject the round 1 block.
	round1Block := newBlock(t, &testBlockConfig{
		vmBlock: genesisChild0,
		seq:     1,
		round:   1,
		prev:    genesisBlock.digest,
	})

	tracker := round1Block.blockTracker

	round2Block := newBlock(t, &testBlockConfig{
		vmBlock:      genesisChild1,
		blockTracker: tracker,
		seq:          1,
		round:        2,
		prev:         genesisBlock.digest,
	})

	// Verify the second block with the first block as its parent
	_, err := round1Block.Verify(ctx)
	require.NoError(t, err)
	_, err = round2Block.Verify(ctx)
	require.NoError(t, err)

	// When the we index the second block, the first block should be rejected
	require.NoError(t, tracker.indexBlock(ctx, round2Block.digest))
	require.Equal(t, snowtest.Rejected, round1Block.vmBlock.(*snowmantest.Block).Decidable.Status)
	require.Equal(t, snowtest.Accepted, round2Block.vmBlock.(*snowmantest.Block).Decidable.Status)

	_, exists := tracker.getBlockByDigest(genesisBlock.digest)
	require.False(t, exists)
}

func TestVerifyInnerBlockBreaksHashChain(t *testing.T) {
	ctx := context.Background()

	// We verify this valid block
	seq1Block := newBlock(t, nil)
	tracker := seq1Block.blockTracker

	_, err := seq1Block.Verify(ctx)
	require.NoError(t, err)

	// This block does not extend seq1, however it has a valid previous digest(since seq1Block was verified)
	seq2 := snowmantest.BuildChild(snowmantest.Genesis)
	seq2Block := newBlock(t, &testBlockConfig{
		vmBlock:      seq2,
		blockTracker: tracker,
		round:        2,
		seq:          2,
		prev:         seq1Block.digest,
	})
	_, err = seq2Block.Verify(ctx)
	require.ErrorIs(t, err, errMismatchedPrevDigest)
}

func TestIndexBlockDigestNotFound(t *testing.T) {
	tracker := newBlockTracker(newBlock(t, nil))
	ctx := context.Background()

	seq1Block := newBlock(t, nil)
	err := tracker.indexBlock(ctx, seq1Block.digest)
	require.ErrorIs(t, err, errDigestNotFound)
}
