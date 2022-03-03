// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

// TestMarshalCodeRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalCodeRequest(t *testing.T) {
	codeRequest := CodeRequest{
		Hash: common.BytesToHash([]byte("some code pls")),
	}

	base64CodeRequest := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAc29tZSBjb2RlIHBscw=="

	codec, err := BuildCodec()
	assert.NoError(t, err)

	codeRequestBytes, err := codec.Marshal(Version, codeRequest)
	assert.NoError(t, err)
	assert.Equal(t, base64CodeRequest, base64.StdEncoding.EncodeToString(codeRequestBytes))

	var c CodeRequest
	_, err = codec.Unmarshal(codeRequestBytes, &c)
	assert.NoError(t, err)
	assert.Equal(t, codeRequest.Hash, c.Hash)
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
		Data: codeData,
	}

	base64CodeResponse := "AAAAAAAyUv38ByGCZU8WP18PmmIdcpVmx00QA3xNe7sEB9HixkmBhVrYaB0NhtHpHgAWeTnLZpQ="

	codec, err := BuildCodec()
	assert.NoError(t, err)

	codeResponseBytes, err := codec.Marshal(Version, codeResponse)
	assert.NoError(t, err)
	assert.Equal(t, base64CodeResponse, base64.StdEncoding.EncodeToString(codeResponseBytes))

	var c CodeResponse
	_, err = codec.Unmarshal(codeResponseBytes, &c)
	assert.NoError(t, err)
	assert.Equal(t, codeResponse.Data, c.Data)
}
