// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats/statstest"
)

func TestCodeRequestHandler(t *testing.T) {
	database := memorydb.New()

	codeBytes := []byte("some code goes here")
	codeHash := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(database, codeHash, codeBytes)

	maxSizeCodeBytes := make([]byte, params.MaxCodeSize)
	n, err := rand.Read(maxSizeCodeBytes)
	assert.NoError(t, err)
	assert.Equal(t, params.MaxCodeSize, n)
	maxSizeCodeHash := crypto.Keccak256Hash(maxSizeCodeBytes)
	rawdb.WriteCode(database, maxSizeCodeHash, maxSizeCodeBytes)

	testHandlerStats := &statstest.TestHandlerStats{}
	codeRequestHandler := NewCodeRequestHandler(database, message.Codec, testHandlerStats)

	tests := map[string]struct {
		setup       func() (request message.CodeRequest, expectedCodeResponse [][]byte)
		verifyStats func(t *testing.T, stats *statstest.TestHandlerStats)
	}{
		"normal": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{codeHash},
				}, [][]byte{codeBytes}
			},
			verifyStats: func(t *testing.T, stats *statstest.TestHandlerStats) {
				assert.EqualValues(t, 1, testHandlerStats.CodeRequestCount)
				assert.EqualValues(t, len(codeBytes), testHandlerStats.CodeBytesReturnedSum)
			},
		},
		"duplicate hashes": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{codeHash, codeHash},
				}, nil
			},
			verifyStats: func(t *testing.T, stats *statstest.TestHandlerStats) {
				assert.EqualValues(t, 1, testHandlerStats.DuplicateHashesRequested)
			},
		},
		"too many hashes": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{{1}, {2}, {3}, {4}, {5}, {6}},
				}, nil
			},
			verifyStats: func(t *testing.T, stats *statstest.TestHandlerStats) {
				assert.EqualValues(t, 1, testHandlerStats.TooManyHashesRequested)
			},
		},
		"max size code handled": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{maxSizeCodeHash},
				}, [][]byte{maxSizeCodeBytes}
			},
			verifyStats: func(t *testing.T, stats *statstest.TestHandlerStats) {
				assert.EqualValues(t, 1, testHandlerStats.CodeRequestCount)
				assert.EqualValues(t, params.MaxCodeSize, testHandlerStats.CodeBytesReturnedSum)
			},
		},
	}

	for name, test := range tests {
		// Reset stats before each test
		testHandlerStats.Reset()

		t.Run(name, func(t *testing.T) {
			request, expectedResponse := test.setup()
			responseBytes, err := codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
			assert.NoError(t, err)

			// If the expected response is empty, assert that the handler returns an empty response and return early.
			if len(expectedResponse) == 0 {
				assert.Len(t, responseBytes, 0, "expected response to be empty")
				return
			}
			var response message.CodeResponse
			if _, err = message.Codec.Unmarshal(responseBytes, &response); err != nil {
				t.Fatal("error unmarshalling CodeResponse", err)
			}
			if len(expectedResponse) != len(response.Data) {
				t.Fatalf("Unexpected length of code data expected %d != %d", len(expectedResponse), len(response.Data))
			}
			for i, code := range expectedResponse {
				// assert.True(t, bytes.Equal(code, response.Data[i]), "code bytes mismatch at index %d", i)
				assert.Equal(t, code, response.Data[i], "code bytes mismatch at index %d", i)
			}
			test.verifyStats(t, testHandlerStats)
		})
	}
}
