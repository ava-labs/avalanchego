// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"crypto/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats/statstest"
	"github.com/ava-labs/avalanchego/ids"

	ethparams "github.com/ava-labs/libevm/params"
)

func TestCodeRequestHandler(t *testing.T) {
	database := memorydb.New()

	codeBytes := []byte("some code goes here")
	codeHash := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(database, codeHash, codeBytes)

	maxSizeCodeBytes := make([]byte, ethparams.MaxCodeSize)
	n, err := rand.Read(maxSizeCodeBytes)
	require.NoError(t, err)
	require.Equal(t, ethparams.MaxCodeSize, n)
	maxSizeCodeHash := crypto.Keccak256Hash(maxSizeCodeBytes)
	rawdb.WriteCode(database, maxSizeCodeHash, maxSizeCodeBytes)

	testHandlerStats := &statstest.TestHandlerStats{}
	codeRequestHandler := NewCodeRequestHandler(database, message.Codec, testHandlerStats)

	tests := map[string]struct {
		setup       func() (request message.CodeRequest, expectedCodeResponse [][]byte)
		verifyStats func(t *testing.T)
	}{
		"normal": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{codeHash},
				}, [][]byte{codeBytes}
			},
			verifyStats: func(t *testing.T) {
				require.Equal(t, uint32(1), testHandlerStats.CodeRequestCount)
				require.Equal(t, uint32(len(codeBytes)), testHandlerStats.CodeBytesReturnedSum)
			},
		},
		"duplicate hashes": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{codeHash, codeHash},
				}, nil
			},
			verifyStats: func(t *testing.T) {
				require.Equal(t, uint32(1), testHandlerStats.DuplicateHashesRequested)
			},
		},
		"too many hashes": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{{1}, {2}, {3}, {4}, {5}, {6}},
				}, nil
			},
			verifyStats: func(t *testing.T) {
				require.Equal(t, uint32(1), testHandlerStats.TooManyHashesRequested)
			},
		},
		"max size code handled": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{maxSizeCodeHash},
				}, [][]byte{maxSizeCodeBytes}
			},
			verifyStats: func(t *testing.T) {
				require.Equal(t, uint32(1), testHandlerStats.CodeRequestCount)
				require.Equal(t, uint32(ethparams.MaxCodeSize), testHandlerStats.CodeBytesReturnedSum)
			},
		},
	}

	for name, test := range tests {
		// Reset stats before each test
		testHandlerStats.Reset()

		t.Run(name, func(t *testing.T) {
			request, expectedResponse := test.setup()
			responseBytes, err := codeRequestHandler.OnCodeRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
			require.NoError(t, err)

			// If the expected response is empty, require that the handler returns an empty response and return early.
			if len(expectedResponse) == 0 {
				require.Empty(t, responseBytes, "expected response to be empty")
				return
			}
			var response message.CodeResponse
			_, err = message.Codec.Unmarshal(responseBytes, &response)
			require.NoError(t, err)
			require.Len(t, response.Data, len(expectedResponse))
			for i, code := range expectedResponse {
				require.Equal(t, code, response.Data[i], "code bytes mismatch at index %d", i)
			}
			test.verifyStats(t)
		})
	}
}
