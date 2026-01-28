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
)

const (
	corethLeafsRequestB64    = "AAAAAAAAAAAAAAAAAAAAAABpbSBST09UaW5nIGZvciB5YQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIFL9/AchgmVPFj9fD5piHXKVZsdNEAN8TXu7BAfR4sZJAAAAIIGFWthoHQ2G0ekeABZ5OctmlNLEIqzSCKAHKTlIf2mZBAAB"
	subnetEVMLeafsRequestB64 = "AAAAAAAAAAAAAAAAAAAAAABpbSBST09UaW5nIGZvciB5YQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIFL9/AchgmVPFj9fD5piHXKVZsdNEAN8TXu7BAfR4sZJAAAAIIGFWthoHQ2G0ekeABZ5OctmlNLEIqzSCKAHKTlIf2mZBAA="
	leafsResponseFixtureB64  = "AAAAAAAQAAAAIE8WP18PmmIdcpVmx00QA3xNe7sEB9HixkmBhVrYaB0NAAAAIGagByk5SH9pmeudGKRHhARdh/PGfPInRumVr1olNnlRAAAAIK2zfFghtmgLTnyLdjobHUnUlVyEhiFjJSU/7HON16niAAAAIIYVu9oIMfUFmHWSHmaKW98sf8SERZLSVyvNBmjS1sUvAAAAIHHb2Wiw9xcu2FeUuzWLDDtSXaF4b5//CUJ52xlE69ehAAAAIPhMiSs77qX090OR9EXRWv1ClAQDdPaSS5jL+HE/jZYtAAAAIMr8yuOmvI+effHZKTM/+ZOTO+pvWzr23gN0NmxHGeQ6AAAAIBZZpE856x5YScYHfbtXIvVxeiiaJm+XZHmBmY6+qJwLAAAAIHOq53hmZ/fpNs1PJKv334ZrqlYDg2etYUXeHuj0qLCZAAAAIHiN5WOvpGfUnexqQOmh0AfwM8KCMGG90Oqln45NpkMBAAAAIKAQ13yW6oCnpmX2BvamO389/SVnwYl55NYPJmhtm/L7AAAAIAfuKbpk+Eq0PKDG5rkcH9O+iZBDQXnTr0SRo2kBLbktAAAAILsXyQKL6ZFOt2ScbJNHgAl50YMDVvKlTD3qsqS0R11jAAAAIOqxOTXzHYRIRRfpJK73iuFRwAdVklg2twdYhWUMMOwpAAAAIHnqPf5BNqv3UrO4Jx0D6USzyds2a3UEX479adIq5UEZAAAAIDLWEMqsbjP+qjJjo5lDcCS6nJsUZ4onTwGpEK4pX277AAAAEAAAAAmG0ekeABZ5OcsAAAAMuqL/bNRxxIPxX7kLAAAACov5IRGcFg8HAkQAAAAIUFTi0INr+EwAAAAOnQ97usvgJVqlt9RL7EAAAAAJfI0BkZLCQiTiAAAACxsGfYm8fwHx9XOYAAAADUs3OXARXoLtb0ElyPoAAAAKPr34iDoK2L6cOQAAAAoFIg0LKWiLc0uOAAAACCbJAf81TN4WAAAADBhPw50XNP9XFkKJUwAAAAuvvo+1aYfHf1gYUgAAAAqjcDk0v1CijaECAAAADkfLVT12lCZ670686kBrAAAADf5fWr9EzN4mO1YGYz4AAAAEAAAADlcyXwVWMEo+Pq4Uwo0MAAAADeo50qHks46vP0TGxu8AAAAOg2Ly9WQIVMFd/KyqiiwAAAAL7M5aOpS00zilFD4="
)

type messageFormat struct {
	leafReqType message.LeafsRequestType
	codec       codec.Manager
}

// TestMarshalLeafsRequest requires that the leafs request wire formats haven't changed.
func TestMarshalLeafsRequest(t *testing.T) {
	// generate some random code data
	// set random seed for deterministic random
	r := newTestRand()
	startBytes := randomBytes(t, r, common.HashLength)
	endBytes := randomBytes(t, r, common.HashLength)

	// Test coreth format - marshal concrete struct directly to verify wire format
	t.Run("coreth", func(t *testing.T) {
		request := message.CorethLeafsRequest{
			Root:     common.BytesToHash([]byte("im ROOTing for ya")),
			Account:  common.Hash{},
			Start:    startBytes,
			End:      endBytes,
			Limit:    1024,
			NodeType: message.StateTrieNode,
		}

		leafsRequestBytes, err := message.CorethCodec.Marshal(message.Version, request)
		require.NoError(t, err)
		require.Equal(t, corethLeafsRequestB64, base64.StdEncoding.EncodeToString(leafsRequestBytes))

		var decoded message.CorethLeafsRequest
		_, err = message.CorethCodec.Unmarshal(leafsRequestBytes, &decoded)
		require.NoError(t, err)
		require.Equal(t, request.Root, decoded.Root)
		require.Equal(t, request.Start, decoded.Start)
		require.Equal(t, request.End, decoded.End)
		require.Equal(t, request.Limit, decoded.Limit)
		require.Equal(t, request.NodeType, decoded.NodeType)
	})

	// Test subnet-evm format - marshal concrete struct directly to verify wire format
	t.Run("subnet-evm", func(t *testing.T) {
		request := message.SubnetEVMLeafsRequest{
			Root:     common.BytesToHash([]byte("im ROOTing for ya")),
			Account:  common.Hash{},
			Start:    startBytes,
			End:      endBytes,
			Limit:    1024,
			NodeType: message.StateTrieNode,
		}

		leafsRequestBytes, err := message.SubnetEVMCodec.Marshal(message.Version, request)
		require.NoError(t, err)
		require.Equal(t, subnetEVMLeafsRequestB64, base64.StdEncoding.EncodeToString(leafsRequestBytes))

		var decoded message.SubnetEVMLeafsRequest
		_, err = message.SubnetEVMCodec.Unmarshal(leafsRequestBytes, &decoded)
		require.NoError(t, err)
		require.Equal(t, request.Root, decoded.Root)
		require.Equal(t, request.Start, decoded.Start)
		require.Equal(t, request.End, decoded.End)
		require.Equal(t, request.Limit, decoded.Limit)
		// NodeType should not be serialized for subnet-evm
		require.Equal(t, message.NodeType(0), decoded.NodeType)
	})
}

// TestMarshalLeafsResponse requires that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalLeafsResponse(t *testing.T) {
	// generate some random code data
	// set random seed for deterministic random
	r := newTestRand()

	leafsResponse := newLeafsResponseFixture(t, r)

	forEachMessageFormat(t, func(t *testing.T, _ string, format messageFormat) {
		leafsResponseBytes, err := format.codec.Marshal(message.Version, leafsResponse)
		require.NoError(t, err)
		require.Equal(t, leafsResponseFixtureB64, base64.StdEncoding.EncodeToString(leafsResponseBytes))

		var l message.LeafsResponse
		_, err = format.codec.Unmarshal(leafsResponseBytes, &l)
		require.NoError(t, err)
		require.Equal(t, leafsResponse.Keys, l.Keys)
		require.Equal(t, leafsResponse.Vals, l.Vals)
		require.False(t, l.More) // make sure it is not serialized
		require.Equal(t, leafsResponse.ProofVals, l.ProofVals)
	})
}

// TestSubnetEVMLeafsRequestNodeTypeNotSerialized verifies that NodeType is not serialized
// and does not affect the encoded output. This ensures backward compatibility.
func TestSubnetEVMLeafsRequestNodeTypeNotSerialized(t *testing.T) {
	// set random seed for deterministic random
	r := newTestRand()
	startBytes := randomBytes(t, r, common.HashLength)
	endBytes := randomBytes(t, r, common.HashLength)

	// Create request without explicit NodeType (defaults to 0)
	leafsRequestDefault := message.SubnetEVMLeafsRequest{
		Root:     common.BytesToHash([]byte("test root")),
		Account:  common.Hash{},
		Start:    startBytes,
		End:      endBytes,
		Limit:    512,
		NodeType: message.NodeType(0),
	}

	// Create request with explicit NodeType
	leafsRequestWithNodeType := message.SubnetEVMLeafsRequest{
		Root:     common.BytesToHash([]byte("test root")),
		Account:  common.Hash{},
		Start:    startBytes,
		End:      endBytes,
		Limit:    512,
		NodeType: message.StateTrieNode,
	}

	bytesDefault, err := message.SubnetEVMCodec.Marshal(message.Version, leafsRequestDefault)
	require.NoError(t, err)

	bytesWithNodeType, err := message.SubnetEVMCodec.Marshal(message.Version, leafsRequestWithNodeType)
	require.NoError(t, err)

	require.Equal(t, bytesDefault, bytesWithNodeType, "NodeType should not affect serialization")

	var unmarshaled message.SubnetEVMLeafsRequest
	_, err = message.SubnetEVMCodec.Unmarshal(bytesWithNodeType, &unmarshaled)
	require.NoError(t, err)

	require.Equal(t, message.NodeType(0), unmarshaled.NodeType, "NodeType should not be serialized")
	require.Equal(t, leafsRequestDefault.RootHash(), unmarshaled.RootHash())
	require.Equal(t, leafsRequestDefault.StartKey(), unmarshaled.StartKey())
	require.Equal(t, leafsRequestDefault.EndKey(), unmarshaled.EndKey())
	require.Equal(t, leafsRequestDefault.LimitValue(), unmarshaled.LimitValue())
}

func forEachMessageFormat(t *testing.T, fn func(t *testing.T, name string, format messageFormat)) {
	t.Helper()
	formats := map[string]messageFormat{
		"coreth":     {leafReqType: message.CorethLeafsRequestType, codec: message.CorethCodec},
		"subnet-evm": {leafReqType: message.SubnetEVMLeafsRequestType, codec: message.SubnetEVMCodec},
	}
	for name, format := range formats {
		t.Run(name, func(t *testing.T) {
			fn(t, name, format)
		})
	}
}

func newTestRand() *rand.Rand {
	return rand.New(rand.NewSource(1))
}

func newLeafsResponseFixture(t *testing.T, r *rand.Rand) message.LeafsResponse {
	t.Helper()
	keysBytes := make([][]byte, 16)
	valsBytes := make([][]byte, 16)
	for i := range keysBytes {
		valSize := r.Intn(8) + 8 // min 8 bytes, max 16 bytes
		keysBytes[i] = randomBytes(t, r, common.HashLength)
		valsBytes[i] = randomBytes(t, r, valSize)
	}

	_ = randomBytes(t, r, common.HashLength) // keep deterministic stream aligned with legacy fixtures
	proofVals := make([][]byte, 4)
	for i := range proofVals {
		proofSize := r.Intn(8) + 8 // min 8 bytes, max 16 bytes
		proofVals[i] = randomBytes(t, r, proofSize)
	}

	return message.LeafsResponse{
		Keys:      keysBytes,
		Vals:      valsBytes,
		More:      true,
		ProofVals: proofVals,
	}
}

func randomBytes(t *testing.T, r *rand.Rand, size int) []byte {
	t.Helper()
	bytes := make([]byte, size)
	_, err := r.Read(bytes)
	require.NoError(t, err)
	return bytes
}
