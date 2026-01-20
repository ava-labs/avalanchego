// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message_test

import (
	"encoding/base64"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
)

// TestMarshalBlockRequest requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalBlockRequest(t *testing.T) {
	blockRequest := message.BlockRequest{
		Hash:    common.BytesToHash([]byte("some hash is here yo")),
		Height:  1337,
		Parents: 64,
	}

	base64BlockRequest := "AAAAAAAAAAAAAAAAAABzb21lIGhhc2ggaXMgaGVyZSB5bwAAAAAAAAU5AEA="
	utilstest.ForEachCodec(t, func(_ string, c codec.Manager) {
		blockRequestBytes, err := c.Marshal(message.Version, blockRequest)
		require.NoError(t, err)
		require.Equal(t, base64BlockRequest, base64.StdEncoding.EncodeToString(blockRequestBytes))

		var b message.BlockRequest
		_, err = c.Unmarshal(blockRequestBytes, &b)
		require.NoError(t, err)
		require.Equal(t, blockRequest.Hash, b.Hash)
		require.Equal(t, blockRequest.Height, b.Height)
		require.Equal(t, blockRequest.Parents, b.Parents)
	})
}

// TestMarshalBlockResponse requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalBlockResponse(t *testing.T) {
	// create some random bytes
	// set seed to ensure deterministic random behaviour
	rand := rand.New(rand.NewSource(1))
	blocksBytes := make([][]byte, 32)
	for i := range blocksBytes {
		blocksBytes[i] = make([]byte, rand.Intn(32)+32) // min 32 length, max 64
		_, err := rand.Read(blocksBytes[i])
		require.NoError(t, err)
	}
	blockResponse := message.BlockResponse{
		Blocks: blocksBytes,
	}
	base64BlockResponse := "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"
	utilstest.ForEachCodec(t, func(_ string, c codec.Manager) {
		blockResponseBytes, err := c.Marshal(message.Version, blockResponse)
		require.NoError(t, err)
		require.Equal(t, base64BlockResponse, base64.StdEncoding.EncodeToString(blockResponseBytes))

		var b message.BlockResponse
		_, err = c.Unmarshal(blockResponseBytes, &b)
		require.NoError(t, err)
		require.Equal(t, blockResponse.Blocks, b.Blocks)
	})
}
