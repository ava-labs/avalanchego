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

// TestMarshalLeafsRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsRequest(t *testing.T) {
	// generate some random code data
	// set random seed for deterministic random
	rand.Seed(1)

	startBytes := make([]byte, common.HashLength)
	endBytes := make([]byte, common.HashLength)

	_, err := rand.Read(startBytes)
	assert.NoError(t, err)

	_, err = rand.Read(endBytes)
	assert.NoError(t, err)

	leafsRequest := LeafsRequest{
		Root:     common.BytesToHash([]byte("im ROOTing for ya")),
		Start:    startBytes,
		End:      endBytes,
		Limit:    1024,
		NodeType: StateTrieNode,
	}

	base64LeafsRequest := "AAAAAAAAAAAAAAAAAAAAAABpbSBST09UaW5nIGZvciB5YQAAACBS/fwHIYJlTxY/Xw+aYh1ylWbHTRADfE17uwQH0eLGSQAAACCBhVrYaB0NhtHpHgAWeTnLZpTSxCKs0gigByk5SH9pmQQAAQ=="

	codec, err := BuildCodec()
	assert.NoError(t, err)

	leafsRequestBytes, err := codec.Marshal(Version, leafsRequest)
	assert.NoError(t, err)
	assert.Equal(t, base64LeafsRequest, base64.StdEncoding.EncodeToString(leafsRequestBytes))

	var l LeafsRequest
	_, err = codec.Unmarshal(leafsRequestBytes, &l)
	assert.NoError(t, err)
	assert.Equal(t, leafsRequest.Root, l.Root)
	assert.Equal(t, leafsRequest.Start, l.Start)
	assert.Equal(t, leafsRequest.End, l.End)
	assert.Equal(t, leafsRequest.Limit, l.Limit)
	assert.Equal(t, leafsRequest.NodeType, l.NodeType)
}

// TestMarshalLeafsResponse asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsResponse(t *testing.T) {
	// generate some random code data
	// set random seed for deterministic random
	rand.Seed(1)

	keysBytes := make([][]byte, 16)
	valsBytes := make([][]byte, 16)
	for i := range keysBytes {
		keysBytes[i] = make([]byte, common.HashLength)
		valsBytes[i] = make([]byte, rand.Intn(8)+8) // min 8 bytes, max 16 bytes

		_, err := rand.Read(keysBytes[i])
		assert.NoError(t, err)
		_, err = rand.Read(valsBytes[i])
		assert.NoError(t, err)
	}

	nextKey := make([]byte, common.HashLength)
	_, err := rand.Read(nextKey)
	assert.NoError(t, err)

	proofKeys := make([][]byte, 4)
	proofVals := make([][]byte, 4)
	for i := range proofKeys {
		proofKeys[i] = make([]byte, common.HashLength)
		proofVals[i] = make([]byte, rand.Intn(8)+8) // min 8 bytes, max 16 bytes

		_, err = rand.Read(proofKeys[i])
		assert.NoError(t, err)
		_, err = rand.Read(proofVals[i])
		assert.NoError(t, err)
	}

	leafsResponse := LeafsResponse{
		Keys:      keysBytes,
		Vals:      valsBytes,
		More:      true,
		ProofKeys: proofKeys,
		ProofVals: proofVals,
	}

	base64LeafsResponse := "AAAAAAAQAAAAIE8WP18PmmIdcpVmx00QA3xNe7sEB9HixkmBhVrYaB0NAAAAIGagByk5SH9pmeudGKRHhARdh/PGfPInRumVr1olNnlRAAAAIK2zfFghtmgLTnyLdjobHUnUlVyEhiFjJSU/7HON16niAAAAIIYVu9oIMfUFmHWSHmaKW98sf8SERZLSVyvNBmjS1sUvAAAAIHHb2Wiw9xcu2FeUuzWLDDtSXaF4b5//CUJ52xlE69ehAAAAIPhMiSs77qX090OR9EXRWv1ClAQDdPaSS5jL+HE/jZYtAAAAIMr8yuOmvI+effHZKTM/+ZOTO+pvWzr23gN0NmxHGeQ6AAAAIBZZpE856x5YScYHfbtXIvVxeiiaJm+XZHmBmY6+qJwLAAAAIHOq53hmZ/fpNs1PJKv334ZrqlYDg2etYUXeHuj0qLCZAAAAIHiN5WOvpGfUnexqQOmh0AfwM8KCMGG90Oqln45NpkMBAAAAIKAQ13yW6oCnpmX2BvamO389/SVnwYl55NYPJmhtm/L7AAAAIAfuKbpk+Eq0PKDG5rkcH9O+iZBDQXnTr0SRo2kBLbktAAAAILsXyQKL6ZFOt2ScbJNHgAl50YMDVvKlTD3qsqS0R11jAAAAIOqxOTXzHYRIRRfpJK73iuFRwAdVklg2twdYhWUMMOwpAAAAIHnqPf5BNqv3UrO4Jx0D6USzyds2a3UEX479adIq5UEZAAAAIDLWEMqsbjP+qjJjo5lDcCS6nJsUZ4onTwGpEK4pX277AAAAEAAAAAmG0ekeABZ5OcsAAAAMuqL/bNRxxIPxX7kLAAAACov5IRGcFg8HAkQAAAAIUFTi0INr+EwAAAAOnQ97usvgJVqlt9RL7EAAAAAJfI0BkZLCQiTiAAAACxsGfYm8fwHx9XOYAAAADUs3OXARXoLtb0ElyPoAAAAKPr34iDoK2L6cOQAAAAoFIg0LKWiLc0uOAAAACCbJAf81TN4WAAAADBhPw50XNP9XFkKJUwAAAAuvvo+1aYfHf1gYUgAAAAqjcDk0v1CijaECAAAADkfLVT12lCZ670686kBrAAAADf5fWr9EzN4mO1YGYz4AAAAEAAAAIFcyXwVWMEo+Pq4Uwo0M6jnSkBpScg2oXKHks46vP0TGAAAAIAhUwV38rLpTq3BbGNuUtNM4pRQ+Y0CNhySwzz+uF6P3AAAAIOnitFTVIrX/oXYEGT+4lmcQp5YHMspSz1PD9SDIibebAAAAIKwndB0/bGLLuxXZr7y/f32kGrBAjjlpwuLNzyM0OL8XAAAABAAAAA7G74Ni8vVPwA4J1vwlZAAAAA6b4Qcvtjw11gQsQWDzjgAAAAv1BM+1fHYBIy1YmwAAAAt0rOdwmk8JHpqD/Q=="

	codec, err := BuildCodec()
	assert.NoError(t, err)

	leafsResponseBytes, err := codec.Marshal(Version, leafsResponse)
	assert.NoError(t, err)
	assert.Equal(t, base64LeafsResponse, base64.StdEncoding.EncodeToString(leafsResponseBytes))

	var l LeafsResponse
	_, err = codec.Unmarshal(leafsResponseBytes, &l)
	assert.NoError(t, err)
	assert.Equal(t, leafsResponse.Keys, l.Keys)
	assert.Equal(t, leafsResponse.Vals, l.Vals)
	assert.False(t, l.More) // make sure it is not serialized
	assert.Equal(t, leafsResponse.ProofKeys, l.ProofKeys)
	assert.Equal(t, leafsResponse.ProofVals, l.ProofVals)
}
