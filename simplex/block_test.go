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

// TestVerifyPrevNotFound attempts to verify a block with a prev digest that is not valid.
func TestVerifyPrevNotFound(t *testing.T) {
	ctx := context.Background()
	testBlock := snowmantest.BuildChild(snowmantest.Genesis)

	tracker := newBlockTracker(ids.GenerateTestID(), simplex.Digest{0x01})
	b := newBlockWithDigest(t, testBlock, tracker, 1, 1, [32]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07})

	_, err := b.Verify(ctx)
	require.ErrorIs(t, err, errDigestNotFound)
}

// TestVerifyPrevIsLatest tests that a block with a prev digest that is not found in the block tracker
// successfully verifies the block if it is the latest accepted block.
func TestVerifyPrevIsLatest(t *testing.T) {
	ctx := context.Background()
	latestAccepted := snowmantest.Genesis
	tracker := newBlockTracker(latestAccepted.ID(), simplex.Digest{})

	// Create latest accepted block, and its child
	latestBlock := newBlockWithDigest(t, latestAccepted, tracker, 0, 0, [32]byte{})
	tracker.lastAcceptedDigest = latestBlock.digest
	testBlock := snowmantest.BuildChild(latestAccepted)
	b := newBlockWithDigest(t, testBlock, tracker, 1, 1, latestBlock.digest)

	verifiedBlock, err := b.Verify(ctx)
	require.NoError(t, err)

	// Ensure the verified block matches the original block
	vBlockBytes, err := verifiedBlock.Bytes()
	require.NoError(t, err)

	blockBytes, err := b.Bytes()
	require.NoError(t, err)
	require.Equal(t, blockBytes, vBlockBytes, "block bytes should match after verification")

	require.NoError(t, tracker.indexBlock(ctx, b.digest))
	require.Equal(t, snowtest.Accepted, testBlock.Decidable.Status)

	// Ensure future blocks are verified on the new last accepted id and digest
	nextVMBlock := snowmantest.BuildChild(testBlock)
	nextBlock := newBlockWithDigest(t, nextVMBlock, tracker, 2, 2, b.digest)
	_, err = nextBlock.Verify(ctx)
	require.NoError(t, err)
	require.NoError(t, tracker.indexBlock(ctx, nextBlock.digest))
	require.Equal(t, snowtest.Accepted, nextVMBlock.Decidable.Status)
}

// TestVerifyLatestDigestMismatch tests verification fails if the latest accepted block's digest
// does not match the expected previous digest in the block metadata.
func TestVerifyLatestDigestMismatch(t *testing.T) {
	ctx := context.Background()
	latestAccepted := snowmantest.Genesis
	tracker := newBlockTracker(latestAccepted.ID(), simplex.Digest{0x01})

	// Create latest accepted block, and its child
	latestBlock := newBlockWithDigest(t, latestAccepted, tracker, 0, 0, [32]byte{})
	testBlock := snowmantest.BuildChild(latestAccepted)
	b := newBlockWithDigest(t, testBlock, tracker, 1, 1, latestBlock.digest)

	_, err := b.Verify(ctx)
	require.ErrorIs(t, err, errMismatchedPrevDigest)
}

// TestVerifyParentAccepted tests that a block, whose parent has been verified and indexed, can
// also be verified and indexed successfully.
func TestVerifyParentAccepted(t *testing.T) {
	ctx := context.Background()
	vmBlock0 := snowmantest.Genesis
	vmBlock1 := snowmantest.BuildChild(vmBlock0)

	tracker := newBlockTracker(ids.Empty, simplex.Digest{0x01})
	seq0Block := newBlockWithDigest(t, vmBlock0, tracker, 0, 0, [32]byte{})
	seq1Block := newBlockWithDigest(t, vmBlock1, tracker, 1, 1, seq0Block.digest)

	_, err := seq0Block.Verify(ctx)
	require.NoError(t, err)

	require.NoError(t, tracker.indexBlock(ctx, seq0Block.digest))
	require.Equal(t, snowtest.Accepted, vmBlock0.Decidable.Status)

	// Verify the second block with the first block as its parent
	_, err = seq1Block.Verify(ctx)
	require.NoError(t, err)
	require.NoError(t, tracker.indexBlock(ctx, seq1Block.digest))
	require.Equal(t, snowtest.Accepted, vmBlock1.Decidable.Status)

	// ensure tracker cleans up the block
	require.Nil(t, tracker.simplexDigestsToBlock[seq0Block.digest])
}

func TestVerifyBlockRejectsSiblings(t *testing.T) {
	ctx := context.Background()
	vmBlock0 := snowmantest.Genesis
	block0Child0 := snowmantest.BuildChild(vmBlock0)
	block0Child1 := snowmantest.BuildChild(vmBlock0)

	tracker := newBlockTracker(ids.Empty, simplex.Digest{0x01})
	seq0Block := newBlockWithDigest(t, vmBlock0, tracker, 0, 0, [32]byte{})

	// round1Block and round2Block are siblings, both children of seq0
	// this can happen if we notarize a block for round 1, but the rest
	// of the network notarizes a dummy block for round 1. Then
	// we will verify a sibling block for round 2 and must reject the round 1 block.
	round1Block := newBlockWithDigest(t, block0Child0, tracker, 1, 1, seq0Block.digest)
	round2Block := newBlockWithDigest(t, block0Child1, tracker, 2, 1, seq0Block.digest)

	_, err := seq0Block.Verify(ctx)
	require.NoError(t, err)

	require.NoError(t, tracker.indexBlock(ctx, seq0Block.digest))
	require.Equal(t, snowtest.Accepted, vmBlock0.Decidable.Status)

	// Verify the second block with the first block as its parent
	_, err = round1Block.Verify(ctx)
	require.NoError(t, err)
	_, err = round2Block.Verify(ctx)
	require.NoError(t, err)

	// When the we index the second block, the first block should be rejected
	require.NoError(t, tracker.indexBlock(ctx, round2Block.digest))
	require.Equal(t, snowtest.Accepted, block0Child1.Decidable.Status)
	require.Equal(t, snowtest.Rejected, block0Child0.Decidable.Status)

	require.Nil(t, tracker.getBlock(seq0Block.digest))
}

func TestVerifyInnerBlockBreaksHashChain(t *testing.T) {
	genesisDigest := simplex.Digest{0x00, 0x01}

	tracker := newBlockTracker(snowmantest.Genesis.ID(), genesisDigest)
	ctx := context.Background()

	// We verify this valid block
	seq1 := snowmantest.BuildChild(snowmantest.Genesis)
	seq1Block := newBlockWithDigest(t, seq1, tracker, 1, 1, genesisDigest)
	_, err := seq1Block.Verify(ctx)
	require.NoError(t, err)

	// This block does not extend seq1, however it has a valid previous digest(since seq1Block was verified)
	seq2 := snowmantest.BuildChild(snowmantest.Genesis)
	seq2Block := newBlockWithDigest(t, seq2, tracker, 2, 2, seq1Block.digest)
	_, err = seq2Block.Verify(ctx)
	require.ErrorIs(t, err, errMismatchedPrevDigest)
}

func TestIndexBlockDigestNotFound(t *testing.T) {
	tracker := newBlockTracker(ids.Empty, simplex.Digest{})
	ctx := context.Background()

	seq1 := snowmantest.BuildChild(snowmantest.Genesis)
	seq1Block := newBlockWithDigest(t, seq1, tracker, 1, 1, [32]byte{})
	err := tracker.indexBlock(ctx, seq1Block.digest)
	require.ErrorIs(t, err, errDigestNotFound)
}
