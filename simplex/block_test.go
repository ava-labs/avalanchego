// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
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
)

func TestBlockSerialization(t *testing.T) {
	unexpectedBlockBytes := errors.New("unexpected block bytes")

	testVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
		},
	}

	testBlock := snowmantest.Block{
		HeightV: 1,
		BytesV:  []byte("test block"),
	}

	b := &Block{
		vmBlock: &testBlock,
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
		expected      []byte
		parseFunc     func(context.Context, []byte) (snowman.Block, error)
		expectedError error
		blockBytes    []byte
	}{
		{
			name:       "block serialization",
			blockBytes: blockBytes,
			expected:   blockBytes,
			parseFunc: func(_ context.Context, b []byte) (snowman.Block, error) {
				require.Equal(t, b, testBlock.BytesV, "block bytes should match")
				return &testBlock, nil
			},
			expectedError: nil,
		},
		{
			name:          "block deserialization error",
			blockBytes:    blockBytes,
			expected:      nil,
			expectedError: unexpectedBlockBytes,
			parseFunc: func(_ context.Context, _ []byte) (snowman.Block, error) {
				return nil, unexpectedBlockBytes
			},
		},
		{
			name:          "corrupted block data",
			blockBytes:    []byte("corrupted data"),
			expected:      nil,
			expectedError: canoto.ErrInvalidWireType,
			parseFunc: func(_ context.Context, _ []byte) (snowman.Block, error) {
				return nil, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testVM.ParseBlockF = tt.parseFunc
			deserializer := &blockDeserializer{
				parser: testVM,
			}

			// Deserialize the block
			deserializedBlock, err := deserializer.DeserializeBlock(tt.blockBytes)
			require.ErrorIs(t, err, tt.expectedError)

			if tt.expectedError == nil {
				require.Equal(t, deserializedBlock.BlockHeader().ProtocolMetadata, b.BlockHeader().ProtocolMetadata)
			}
		})
	}
}

// TestBlockVerify tests a block cannot be verified more than once.
func TestBlockVerify(t *testing.T) {
	testBlock := snowmantest.Block{
		HeightV: 1,
		BytesV:  []byte("test block"),
	}

	b := &Block{
		vmBlock: &testBlock,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   1,
			Seq:     1,
			Prev:    [32]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
	}

	block, err := b.Verify(context.Background())
	require.NoError(t, err, "block should be verified successfully")

	// The block we get should be serializable into a block
	serializedBlock, err := block.Bytes()
	require.NoError(t, err, "block should be serializable")

	// Deserialize the block
	deserializer := &blockDeserializer{
		parser: &blocktest.VM{
			VM: enginetest.VM{
				T: t,
			},
			ParseBlockF: func(_ context.Context, b []byte) (snowman.Block, error) {
				require.Equal(t, b, testBlock.BytesV, "block bytes should match")
				return &testBlock, nil
			},
		},
	}
	deserializedBlock, err := deserializer.DeserializeBlock(serializedBlock)
	require.NoError(t, err, "block should be deserialized successfully")
	require.Equal(t, b.BlockHeader().ProtocolMetadata, deserializedBlock.BlockHeader().ProtocolMetadata)

	// Ensure no double verification
	_, err = b.Verify(context.Background())
	require.ErrorIs(t, err, errBlockAlreadyVerified)
}
