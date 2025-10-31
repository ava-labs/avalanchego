// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

// TestMarshalLeafsRequest requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsRequest(t *testing.T) {
	t.Parallel()

	startBytes := make([]byte, common.HashLength)
	endBytes := make([]byte, common.HashLength)

	r := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic bytes for golden assertion
	_, err := r.Read(startBytes)
	require.NoError(t, err)
	_, err = r.Read(endBytes)
	require.NoError(t, err)
	require.NotEmpty(t, startBytes)
	require.NotEmpty(t, endBytes)

	leafsRequest := LeafsRequest{
		Root:     common.BytesToHash([]byte("im ROOTing for ya")),
		Account:  common.Hash{}, // Account defaults to zero value
		Start:    startBytes,
		End:      endBytes,
		Limit:    1024,
		NodeType: StateTrieNode,
	}

	base64LeafsRequest := "AAAAAAAAAAAAAAAAAAAAAABpbSBST09UaW5nIGZvciB5YQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIFL9/AchgmVPFj9fD5piHXKVZsdNEAN8TXu7BAfR4sZJAAAAIIGFWthoHQ2G0ekeABZ5OctmlNLEIqzSCKAHKTlIf2mZBAAB"

	leafsRequestBytes, err := Codec().Marshal(Version, leafsRequest)
	require.NoError(t, err)
	require.Equal(t, base64LeafsRequest, base64.StdEncoding.EncodeToString(leafsRequestBytes))

	var l LeafsRequest
	_, err = Codec().Unmarshal(leafsRequestBytes, &l)
	require.NoError(t, err)
	require.Equal(t, leafsRequest.Root, l.Root)
	require.Equal(t, leafsRequest.Start, l.Start)
	require.Equal(t, leafsRequest.End, l.End)
	require.Equal(t, leafsRequest.Limit, l.Limit)
	require.Equal(t, leafsRequest.NodeType, l.NodeType)
}

// TestMarshalLeafsResponse requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsResponse(t *testing.T) {
	t.Parallel()

	keysBytes := make([][]byte, 16)
	valsBytes := make([][]byte, 16)
	r := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic bytes for golden assertion
	for i := range keysBytes {
		keysBytes[i] = make([]byte, common.HashLength)
		n := r.Intn(8)
		valsBytes[i] = make([]byte, n+8)
		_, err := r.Read(keysBytes[i])
		require.NoError(t, err)
		_, err = r.Read(valsBytes[i])
		require.NoError(t, err)
	}

	nextKey := make([]byte, common.HashLength)
	_, err := r.Read(nextKey)
	require.NoError(t, err)
	require.NotEmpty(t, nextKey)

	proofVals := make([][]byte, 4)
	r2 := rand.New(rand.NewSource(2)) //nolint:gosec // deterministic bytes for golden assertion
	for i := range proofVals {
		n := r2.Intn(8)
		proofVals[i] = make([]byte, n+8)
		_, err := r2.Read(proofVals[i])
		require.NoError(t, err)
	}

	leafsResponse := LeafsResponse{
		Keys:      keysBytes,
		Vals:      valsBytes,
		More:      true,
		ProofVals: proofVals,
	}

	base64LeafsResponse := "AAAAAAAQAAAAIE8WP18PmmIdcpVmx00QA3xNe7sEB9HixkmBhVrYaB0NAAAAIGagByk5SH9pmeudGKRHhARdh/PGfPInRumVr1olNnlRAAAAIK2zfFghtmgLTnyLdjobHUnUlVyEhiFjJSU/7HON16niAAAAIIYVu9oIMfUFmHWSHmaKW98sf8SERZLSVyvNBmjS1sUvAAAAIHHb2Wiw9xcu2FeUuzWLDDtSXaF4b5//CUJ52xlE69ehAAAAIPhMiSs77qX090OR9EXRWv1ClAQDdPaSS5jL+HE/jZYtAAAAIMr8yuOmvI+effHZKTM/+ZOTO+pvWzr23gN0NmxHGeQ6AAAAIBZZpE856x5YScYHfbtXIvVxeiiaJm+XZHmBmY6+qJwLAAAAIHOq53hmZ/fpNs1PJKv334ZrqlYDg2etYUXeHuj0qLCZAAAAIHiN5WOvpGfUnexqQOmh0AfwM8KCMGG90Oqln45NpkMBAAAAIKAQ13yW6oCnpmX2BvamO389/SVnwYl55NYPJmhtm/L7AAAAIAfuKbpk+Eq0PKDG5rkcH9O+iZBDQXnTr0SRo2kBLbktAAAAILsXyQKL6ZFOt2ScbJNHgAl50YMDVvKlTD3qsqS0R11jAAAAIOqxOTXzHYRIRRfpJK73iuFRwAdVklg2twdYhWUMMOwpAAAAIHnqPf5BNqv3UrO4Jx0D6USzyds2a3UEX479adIq5UEZAAAAIDLWEMqsbjP+qjJjo5lDcCS6nJsUZ4onTwGpEK4pX277AAAAEAAAAAmG0ekeABZ5OcsAAAAMuqL/bNRxxIPxX7kLAAAACov5IRGcFg8HAkQAAAAIUFTi0INr+EwAAAAOnQ97usvgJVqlt9RL7EAAAAAJfI0BkZLCQiTiAAAACxsGfYm8fwHx9XOYAAAADUs3OXARXoLtb0ElyPoAAAAKPr34iDoK2L6cOQAAAAoFIg0LKWiLc0uOAAAACCbJAf81TN4WAAAADBhPw50XNP9XFkKJUwAAAAuvvo+1aYfHf1gYUgAAAAqjcDk0v1CijaECAAAADkfLVT12lCZ670686kBrAAAADf5fWr9EzN4mO1YGYz4AAAAEAAAACm8xRMCqTO1W29kAAAAIZ9wol8oW4YsAAAAOaGugcKI9oAJrZhCPutAAAAAPhENjuCNqN/goPvsnNn9u"

	leafsResponseBytes, err := Codec().Marshal(Version, leafsResponse)
	require.NoError(t, err)
	require.Equal(t, base64LeafsResponse, base64.StdEncoding.EncodeToString(leafsResponseBytes))

	var l LeafsResponse
	_, err = Codec().Unmarshal(leafsResponseBytes, &l)
	require.NoError(t, err)
	require.Equal(t, leafsResponse.Keys, l.Keys)
	require.Equal(t, leafsResponse.Vals, l.Vals)
	require.False(t, l.More) // make sure it is not serialized
	require.Equal(t, leafsResponse.ProofVals, l.ProofVals)
}
