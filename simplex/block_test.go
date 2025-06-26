// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

type testChainVM struct {
	*blocktest.VM
}

// TestVerifiedBlock tests that a block can be serialized and deserialized correctly,
// and that the digest is computed correctly after deserialization.
func TestVerifiedBlockSerialization(t *testing.T) {
	vb := &VerifiedBlock{
		innerBlock: []byte("test block data"),
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
			Prev:    [32]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
	}

	// Serialize the block
	blockBytes := vb.Bytes()

	// Deserialize the block
	vb1, err := verifiedBlockFromBytes(blockBytes)
	require.NoError(t, err)

	// Check that the inner block data is preserved
	require.Equal(t, vb.BlockHeader(), vb1.BlockHeader())
}

func TestBlockDeserializer(t *testing.T) {
	unexpectedBlockBytes := errors.New("unexpected block bytes")
	testVM := &testChainVM{
		VM: &blocktest.VM{
			VM: enginetest.VM{
				T: t,
			},
		},
	}

	testVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		default:
			return nil, unexpectedBlockBytes
		}
	}

	deserializer := &blockDeserializer{
		parser: testVM,
	}

	vb := &VerifiedBlock{
		innerBlock: snowmantest.GenesisBytes,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
			Prev:    [32]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
	}

	// Serialize & Deserialize the block
	blockBytes := vb.Bytes()
	vb1, err := deserializer.DeserializeBlock(blockBytes)
	require.NoError(t, err)
	require.Equal(t, vb.BlockHeader(), vb1.BlockHeader())

	vbWrongBytes := &VerifiedBlock{
		innerBlock: []byte("wrong block data"),
		metadata:   simplex.ProtocolMetadata{},
	}

	wrongBytes := vbWrongBytes.Bytes()
	_, err = deserializer.DeserializeBlock(wrongBytes)
	require.ErrorIs(t, err, unexpectedBlockBytes)
}

// TestBlockVerify tests when a block is verified, it is added to the block tracker and
// the verify method is called on the block.
func TestBlockVerify(t *testing.T) {
	parsedBlock := &snowmantest.Block{}
	vm := &blocktest.VM{
		ParseBlockF: func(_ context.Context, _ []byte) (snowman.Block, error) {
			return parsedBlock, nil
		},
	}

	verifiedBlock := &VerifiedBlock{
		innerBlock: []byte("test block data"),
		metadata: simplex.ProtocolMetadata{
			Epoch: 1,
			Round: 1,
		},
	}

	tracker := &blockTracker{
		vm:             vm,
		acceptedBlocks: make(map[uint64][]blockData),
	}

	block := &Block{
		block:   verifiedBlock,
		parser:  vm,
		tracker: tracker,
	}

	vb, err := block.Verify(context.Background())
	require.NoError(t, err)
	require.Equal(t, verifiedBlock, vb)
	// Check we haven't accepted or rejected the block yet
	require.Equal(t, snowtest.Undecided, parsedBlock.Decidable.Status)
}

// TestRejectStaleBlocks tests that the block tracker correctly rejects stale blocks
// when a new block is accepted. It should call reject on all blocks that are
// older than the accepted block's round, and also reject blocks in the same round
// that are not the accepted block.
func TestRejectAllStaleBlocks(t *testing.T) {
	// older blocks should be rejected
	round0Blocks := []blockData{
		{
			block:  &snowmantest.Block{},
			digest: simplex.Digest{0x00, 0x01},
		},
	}

	// blocks in the same round should be rejected except the one with digest 0x02
	round1Blocks := []blockData{
		{
			block:  &snowmantest.Block{},
			digest: simplex.Digest{0x01},
		},
		{
			block:  &snowmantest.Block{},
			digest: simplex.Digest{0x02},
		},
		{
			block:  &snowmantest.Block{},
			digest: simplex.Digest{0x03},
		},
		{
			block:  &snowmantest.Block{},
			digest: simplex.Digest{0x04},
		},
	}

	// newer blocks, this block should not be rejected
	round2Blocks := []blockData{
		{
			block:  &snowmantest.Block{},
			digest: simplex.Digest{0x11},
		},
	}

	acceptedBlocks := make(map[uint64][]blockData)
	acceptedBlocks[0] = round0Blocks
	acceptedBlocks[1] = round1Blocks
	acceptedBlocks[2] = round2Blocks

	tracker := &blockTracker{
		acceptedBlocks: acceptedBlocks,
	}

	require.NoError(t, tracker.rejectAllStaleBlocks(1, simplex.Digest{0x02}))
	require.Empty(t, tracker.acceptedBlocks[1])
	require.Empty(t, tracker.acceptedBlocks[0])
	require.Len(t, tracker.acceptedBlocks[2], 1)

	for _, block := range round0Blocks {
		require.Equal(t, snowtest.Rejected, block.block.(*snowmantest.Block).Status)
	}

	for _, block := range round1Blocks {
		// require all except the one with digest 0x02 to be rejected
		if block.digest == round1Blocks[1].digest {
			require.Equal(t, snowtest.Undecided, block.block.(*snowmantest.Block).Status)
		} else {
			require.Equal(t, snowtest.Rejected, block.block.(*snowmantest.Block).Status)
		}
	}
}

func TestIndexBlock(t *testing.T) {
	acceptedBlockDigest := simplex.Digest{0x11}
	acceptBlockID := ids.GenerateTestID()
	round1Blocks := []blockData{
		{
			block: &snowmantest.Block{
				Decidable: snowtest.Decidable{
					IDV: acceptBlockID,
				},
			},
			digest: acceptedBlockDigest, // we will
		},
	}

	acceptedBlocks := make(map[uint64][]blockData)
	acceptedBlocks[1] = round1Blocks

	setPref := func(_ context.Context, id ids.ID) error {
		require.Equal(t, acceptBlockID, id)
		return nil
	}

	tracker := &blockTracker{
		acceptedBlocks: acceptedBlocks,
		vm: &blocktest.VM{
			SetPreferenceF: setPref,
		},
	}

	require.NoError(t, tracker.indexBlock(context.TODO(), 1, acceptedBlockDigest))
}

func TestIndexBlockNotVerified(t *testing.T) {
	acceptedBlockDigest := simplex.Digest{0x11}
	tracker := newBlockTracker()
	err := tracker.indexBlock(context.TODO(), 1, acceptedBlockDigest)
	require.ErrorIs(t, err, errBlockNotVerified)
}
