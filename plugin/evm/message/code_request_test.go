// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"
)

// TestMarshalCodeRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalCodeRequest(t *testing.T) {
	codeRequest := CodeRequest{
		Hashes: []common.Hash{common.BytesToHash([]byte("some code pls"))},
	}

	base64CodeRequest := "AAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAHNvbWUgY29kZSBwbHM="

	codeRequestBytes, err := Codec.Marshal(Version, codeRequest)
	assert.NoError(t, err)
	assert.Equal(t, base64CodeRequest, base64.StdEncoding.EncodeToString(codeRequestBytes))

	var c CodeRequest
	_, err = Codec.Unmarshal(codeRequestBytes, &c)
	assert.NoError(t, err)
	assert.Equal(t, codeRequest.Hashes, c.Hashes)
}

// TestMarshalCodeResponse asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalCodeResponse(t *testing.T) {
	// generate some random code data
	// set random seed for deterministic random
	rand.Seed(1)
	codeData := make([]byte, 50)
	_, err := rand.Read(codeData)
	assert.NoError(t, err)

	codeResponse := CodeResponse{
		Data: [][]byte{codeData},
	}

	base64CodeResponse := "AAAAAAABAAAAMlL9/AchgmVPFj9fD5piHXKVZsdNEAN8TXu7BAfR4sZJgYVa2GgdDYbR6R4AFnk5y2aU"

	codeResponseBytes, err := Codec.Marshal(Version, codeResponse)
	assert.NoError(t, err)
	assert.Equal(t, base64CodeResponse, base64.StdEncoding.EncodeToString(codeResponseBytes))

	var c CodeResponse
	_, err = Codec.Unmarshal(codeResponseBytes, &c)
	assert.NoError(t, err)
	assert.Equal(t, codeResponse.Data, c.Data)
}
