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

// TestMarshalCodeRequest requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalCodeRequest(t *testing.T) {
	codeRequest := message.CodeRequest{
		Hashes: []common.Hash{common.BytesToHash([]byte("some code pls"))},
	}

	base64CodeRequest := "AAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAHNvbWUgY29kZSBwbHM="

	utilstest.ForEachCodec(t, func(_ string, c codec.Manager) {
		codeRequestBytes, err := c.Marshal(message.Version, codeRequest)
		require.NoError(t, err)
		require.Equal(t, base64CodeRequest, base64.StdEncoding.EncodeToString(codeRequestBytes))

		var req message.CodeRequest
		_, err = c.Unmarshal(codeRequestBytes, &req)
		require.NoError(t, err)
		require.Equal(t, codeRequest.Hashes, req.Hashes)
	})
}

// TestMarshalCodeResponse requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalCodeResponse(t *testing.T) {
	// generate some random code data
	// set random seed for deterministic random
	rand := rand.New(rand.NewSource(1))

	codeData := make([]byte, 50)
	_, err := rand.Read(codeData)
	require.NoError(t, err)

	codeResponse := message.CodeResponse{
		Data: [][]byte{codeData},
	}

	base64CodeResponse := "AAAAAAABAAAAMlL9/AchgmVPFj9fD5piHXKVZsdNEAN8TXu7BAfR4sZJgYVa2GgdDYbR6R4AFnk5y2aU"
	utilstest.ForEachCodec(t, func(_ string, cdc codec.Manager) {
		codeResponseBytes, err := cdc.Marshal(message.Version, codeResponse)
		require.NoError(t, err)
		require.Equal(t, base64CodeResponse, base64.StdEncoding.EncodeToString(codeResponseBytes))

		var c message.CodeResponse
		_, err = cdc.Unmarshal(codeResponseBytes, &c)
		require.NoError(t, err)
		require.Equal(t, codeResponse.Data, c.Data)
	})
}
