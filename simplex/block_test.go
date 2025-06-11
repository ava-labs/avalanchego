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

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
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
		vm: testVM,
	}

	// Create a verified block
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

	// Create a verified block
	vbWrongBytes := &VerifiedBlock{
		innerBlock: []byte("wrong block data"),
		metadata:   simplex.ProtocolMetadata{},
	}

	wrongBytes := vbWrongBytes.Bytes()
	_, err = deserializer.DeserializeBlock(wrongBytes)
	require.ErrorIs(t, err, unexpectedBlockBytes)
}
