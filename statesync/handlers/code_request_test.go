// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"

	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers/stats"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
)

func TestCodeRequestHandler(t *testing.T) {
	codec, err := message.BuildCodec()
	if err != nil {
		t.Fatal("unexpected error when building codec", err)
	}

	database := memorydb.New()

	codeBytes := []byte("some code goes here")
	codeHash := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(database, codeHash, codeBytes)

	codeRequestHandler := NewCodeRequestHandler(database, stats.NewNoopHandlerStats(), codec)

	// query for known code entry
	responseBytes, err := codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestShortID(), 1, message.CodeRequest{Hash: codeHash})
	assert.NoError(t, err)

	var response message.CodeResponse
	if _, err = codec.Unmarshal(responseBytes, &response); err != nil {
		t.Fatal("error unmarshalling CodeResponse", err)
	}
	assert.True(t, bytes.Equal(codeBytes, response.Data))

	// query for missing code entry
	responseBytes, err = codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestShortID(), 2, message.CodeRequest{Hash: common.BytesToHash([]byte("some unknown hash"))})
	assert.NoError(t, err)
	assert.Nil(t, responseBytes)

	// assert max size code bytes are handled
	codeBytes = make([]byte, params.MaxCodeSize)
	n, err := rand.Read(codeBytes)
	assert.NoError(t, err)
	assert.Equal(t, params.MaxCodeSize, n)
	codeHash = crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(database, codeHash, codeBytes)

	responseBytes, err = codeRequestHandler.OnCodeRequest(context.Background(), ids.GenerateTestShortID(), 3, message.CodeRequest{Hash: codeHash})
	assert.NoError(t, err)
	assert.NotNil(t, responseBytes)

	response = message.CodeResponse{}
	if _, err = codec.Unmarshal(responseBytes, &response); err != nil {
		t.Fatal("error unmarshalling CodeResponse", err)
	}
	assert.True(t, bytes.Equal(codeBytes, response.Data))
}
