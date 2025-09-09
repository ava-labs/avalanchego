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

	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/sync/handlers/stats"

	ethparams "github.com/ava-labs/libevm/params"
)

func TestCodeRequestHandler(t *testing.T) {
	database := memorydb.New()

	codeBytes := []byte("some code goes here")
	codeHash := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(database, codeHash, codeBytes)

	maxSizeCodeBytes := make([]byte, ethparams.MaxCodeSize)
	n, err := rand.Read(maxSizeCodeBytes)
	assert.NoError(t, err)
	assert.Equal(t, ethparams.MaxCodeSize, n)
	maxSizeCodeHash := crypto.Keccak256Hash(maxSizeCodeBytes)
	rawdb.WriteCode(database, maxSizeCodeHash, maxSizeCodeBytes)

	mockHandlerStats := &stats.MockHandlerStats{}
	codeRequestHandler := NewCodeRequestHandler(database, message.Codec, mockHandlerStats)

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
				assert.EqualValues(t, 1, mockHandlerStats.CodeRequestCount)
				assert.EqualValues(t, len(codeBytes), mockHandlerStats.CodeBytesReturnedSum)
			},
		},
		"duplicate hashes": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{codeHash, codeHash},
				}, nil
			},
			verifyStats: func(t *testing.T) {
				assert.EqualValues(t, 1, mockHandlerStats.DuplicateHashesRequested)
			},
		},
		"too many hashes": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{{1}, {2}, {3}, {4}, {5}, {6}},
				}, nil
			},
			verifyStats: func(t *testing.T) {
				assert.EqualValues(t, 1, mockHandlerStats.TooManyHashesRequested)
			},
		},
		"max size code handled": {
			setup: func() (request message.CodeRequest, expectedCodeResponse [][]byte) {
				return message.CodeRequest{
					Hashes: []common.Hash{maxSizeCodeHash},
				}, [][]byte{maxSizeCodeBytes}
			},
			verifyStats: func(t *testing.T) {
				assert.EqualValues(t, 1, mockHandlerStats.CodeRequestCount)
				assert.EqualValues(t, ethparams.MaxCodeSize, mockHandlerStats.CodeBytesReturnedSum)
			},
		},
	}

	for name, test := range tests {
		// Reset stats before each test
		mockHandlerStats.Reset()

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
			test.verifyStats(t)
		})
	}
}
