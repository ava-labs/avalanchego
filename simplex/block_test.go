// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"
)

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
	blockBytes, err := vb.Bytes()
	require.NoError(t, err)

	// Deserialize the block
	vb1, err := verifiedBlockFromBytes(blockBytes)
	require.NoError(t, err)

	// Check that the inner block data is preserved
	require.Equal(t, vb.BlockHeader(), vb1.BlockHeader())
}
