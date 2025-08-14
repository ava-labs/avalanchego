// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	ctx := context.Background()
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
			deserializedBlock, err := deserializer.DeserializeBlock(ctx, tt.blockBytes)
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

	genesis := newTestBlock(t, newBlockConfig{})
	b := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})
	b.metadata.Prev[0]++ // Invalid prev digest

	_, err := b.Verify(ctx)
	require.ErrorIs(t, err, errDigestNotFound)
}

// TestVerifyTwice tests that a block the same vmBlock will only
// have its Verify method called once, even if Verify is called multiple times.
func TestVerifyTwice(t *testing.T) {
	ctx := context.Background()

	genesis := newTestBlock(t, newBlockConfig{})
	b := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})

	// Verify the block for the first time
	_, err := b.Verify(ctx)
	require.NoError(t, err)

	// Attempt to verify the block again
	b.vmBlock.(*wrappedBlock).VerifyV = errors.New("should not be called again")
	_, err = b.Verify(ctx)
	require.NoError(t, err)
}

// TestVerifyGenesis tests that a block with a sequence number of 0 cannot be verified.
func TestVerifyGenesis(t *testing.T) {
	ctx := context.Background()

	genesis := newTestBlock(t, newBlockConfig{})
	_, err := genesis.Verify(ctx)
	require.ErrorIs(t, err, errGenesisVerification)
}

func TestVerify(t *testing.T) {
	ctx := context.Background()

	genesis := newTestBlock(t, newBlockConfig{})
	b := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})

	verifiedBlock, err := b.Verify(ctx)
	require.NoError(t, err)

	// Ensure the verified block matches the original block
	vBlockBytes, err := verifiedBlock.Bytes()
	require.NoError(t, err)

	blockBytes, err := b.Bytes()
	require.NoError(t, err)
	require.Equal(t, blockBytes, vBlockBytes, "block bytes should match after verification")
}

// TestVerifyParentAccepted tests that a block, whose parent has been verified
// and indexed, can also be verified and indexed successfully.
func TestVerifyParentAccepted(t *testing.T) {
	ctx := context.Background()

	genesis := newTestBlock(t, newBlockConfig{})
	seq1Block := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})
	seq2Block := newTestBlock(t, newBlockConfig{
		prev: seq1Block,
	})

	_, err := seq1Block.Verify(ctx)
	require.NoError(t, err)
	require.NoError(t, seq1Block.blockTracker.indexBlock(ctx, seq1Block.digest))
	require.Equal(t, snowtest.Accepted, seq1Block.vmBlock.(*wrappedBlock).Decidable.Status)

	// Verify the second block with the first block as its parent
	_, err = seq2Block.Verify(ctx)
	require.NoError(t, err)
	require.NoError(t, seq2Block.blockTracker.indexBlock(ctx, seq2Block.digest))
	require.Equal(t, snowtest.Accepted, seq2Block.vmBlock.(*wrappedBlock).Decidable.Status)

	// ensure tracker cleans up the block
	require.NotContains(t, genesis.blockTracker.simplexDigestsToBlock, seq1Block.digest)
}

func TestVerifyBlockRejectsSiblings(t *testing.T) {
	ctx := context.Background()

	genesis := newTestBlock(t, newBlockConfig{})
	// genesisChild0 and genesisChild1 are siblings, both children of genesis.
	// This can happen if we verify a block for round 1, but the network
	// notarizes the dummy block. Then we will verify a sibling block for round
	// 2 and must reject the round 1 block.
	genesisChild0 := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})
	genesisChild1 := newTestBlock(t, newBlockConfig{
		prev:  genesis,
		round: genesisChild0.metadata.Round + 1,
	})

	// Verify the second block with the first block as its parent
	_, err := genesisChild0.Verify(ctx)
	require.NoError(t, err)
	_, err = genesisChild1.Verify(ctx)
	require.NoError(t, err)

	// When the we index the second block, the first block should be rejected
	require.NoError(t, genesis.blockTracker.indexBlock(ctx, genesisChild1.digest))
	require.Equal(t, snowtest.Rejected, genesisChild0.vmBlock.(*wrappedBlock).Decidable.Status)
	require.Equal(t, snowtest.Accepted, genesisChild1.vmBlock.(*wrappedBlock).Decidable.Status)

	_, exists := genesis.blockTracker.getBlockByDigest(genesis.digest)
	require.False(t, exists)
}

func TestVerifyInnerBlockBreaksHashChain(t *testing.T) {
	ctx := context.Background()

	genesis := newTestBlock(t, newBlockConfig{})
	b := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})

	// This block does not extend the genesis, however it has a valid previous
	// digest.
	b.vmBlock.(*wrappedBlock).ParentV[0]++

	_, err := b.Verify(ctx)
	require.ErrorIs(t, err, errMismatchedPrevDigest)
}

func TestIndexBlockDigestNotFound(t *testing.T) {
	ctx := context.Background()

	genesis := newTestBlock(t, newBlockConfig{})

	unknownDigest := ids.GenerateTestID()
	err := genesis.blockTracker.indexBlock(ctx, simplex.Digest(unknownDigest))
	require.ErrorIs(t, err, errDigestNotFound)
}
