// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestLeafsRequestHandler_OnLeafsRequest(t *testing.T) {
	rand.Seed(1)
	mockHandlerStats := &stats.MockHandlerStats{}
	memdb := memorydb.New()
	trieDB := trie.NewDatabase(memdb)

	corruptedTrieRoot, _, _ := trie.GenerateTrie(t, trieDB, 100, common.HashLength)
	largeTrieRoot, largeTrieKeys, _ := trie.GenerateTrie(t, trieDB, 10_000, common.HashLength)
	smallTrieRoot, _, _ := trie.GenerateTrie(t, trieDB, 500, common.HashLength)

	// Corrupt [corruptedTrieRoot]
	trie.CorruptTrie(t, trieDB, corruptedTrieRoot, 5)

	leafsHandler := NewLeafsRequestHandler(trieDB, message.Codec, mockHandlerStats)

	tests := map[string]struct {
		prepareTestFn    func() (context.Context, message.LeafsRequest)
		assertResponseFn func(*testing.T, message.LeafsRequest, []byte, error)
	}{
		"zero limit dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    0,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"empty root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     common.Hash{},
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"bad start len dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     common.Hash{},
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength+2),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"bad end len dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     common.Hash{},
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength-1),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"empty storage root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     types.EmptyRootHash,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"missing root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     common.BytesToHash([]byte("something is missing here...")),
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.MissingRootCount)
			},
		},
		"corrupted trie drops request": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     corruptedTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, uint32(1), mockHandlerStats.TrieErrorCount)
			},
		},
		"cancelled context dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
			},
		},
		"nil start and end range returns entire trie": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Start:    nil,
					End:      nil,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.Len(t, leafsResponse.Keys, 500)
				assert.Len(t, leafsResponse.Vals, 500)
				assert.Len(t, leafsResponse.ProofKeys, 0)
				assert.Len(t, leafsResponse.ProofVals, 0)
			},
		},
		"nil end range treated like greatest possible value": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      nil,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.Len(t, leafsResponse.Keys, 500)
				assert.Len(t, leafsResponse.Vals, 500)
			},
		},
		"end greater than start dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0xbb}, common.HashLength),
					End:      bytes.Repeat([]byte{0xaa}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
				assert.EqualValues(t, 1, mockHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"invalid node type dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0xbb}, common.HashLength),
					End:      bytes.Repeat([]byte{0xaa}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.NodeType(11),
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
			},
		},
		"max leaves overridden": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit * 10,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), maxLeavesLimit)
				assert.EqualValues(t, len(leafsResponse.Vals), maxLeavesLimit)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)
			},
		},
		"full range with nil start": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    nil,
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), maxLeavesLimit)
				assert.EqualValues(t, len(leafsResponse.Vals), maxLeavesLimit)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(largeTrieRoot, bytes.Repeat([]byte{0x00}, common.HashLength), leafsResponse.Keys[len(leafsResponse.Keys)-1], leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.True(t, more)
			},
		},
		"full range with 0x00 start": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), maxLeavesLimit)
				assert.EqualValues(t, len(leafsResponse.Vals), maxLeavesLimit)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(largeTrieRoot, bytes.Repeat([]byte{0x00}, common.HashLength), leafsResponse.Keys[len(leafsResponse.Keys)-1], leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.True(t, more)
			},
		},
		"partial mid range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				startKey := largeTrieKeys[1_000]
				startKey[31] = startKey[31] + 1 // exclude start key from response
				endKey := largeTrieKeys[1_040]  // include end key in response
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    startKey,
					End:      endKey,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, 40, len(leafsResponse.Keys))
				assert.EqualValues(t, 40, len(leafsResponse.Vals))
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(largeTrieRoot, request.Start, request.End, leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.True(t, more)
			},
		},
		"partial end range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    largeTrieKeys[9_400],
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, 600, len(leafsResponse.Keys))
				assert.EqualValues(t, 600, len(leafsResponse.Vals))
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(largeTrieRoot, request.Start, request.End, leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.False(t, more)
			},
		},
		"final end range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0xff}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), 0)
				assert.EqualValues(t, len(leafsResponse.Vals), 0)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(request.Root, request.Start, request.End, leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.False(t, more)
			},
		},
		"small trie root": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Start:    nil,
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NotEmpty(t, response)

				var leafsResponse message.LeafsResponse
				if _, err = message.Codec.Unmarshal(response, &leafsResponse); err != nil {
					t.Fatalf("unexpected error when unmarshalling LeafsResponse: %v", err)
				}

				assert.EqualValues(t, 500, len(leafsResponse.Keys))
				assert.EqualValues(t, 500, len(leafsResponse.Vals))
				assert.Empty(t, leafsResponse.ProofKeys)
				assert.Empty(t, leafsResponse.ProofVals)
				assert.EqualValues(t, 1, mockHandlerStats.LeafsRequestCount)
				assert.EqualValues(t, len(leafsResponse.Keys), mockHandlerStats.LeafsReturnedSum)

				firstKey := bytes.Repeat([]byte{0x00}, common.HashLength)
				lastKey := leafsResponse.Keys[len(leafsResponse.Keys)-1]

				more, err := trie.VerifyRangeProof(smallTrieRoot, firstKey, lastKey, leafsResponse.Keys, leafsResponse.Vals, nil)
				assert.NoError(t, err)
				assert.False(t, more)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, request := test.prepareTestFn()
			response, err := leafsHandler.OnLeafsRequest(ctx, ids.GenerateTestNodeID(), 1, request)
			test.assertResponseFn(t, request, response, err)
			mockHandlerStats.Reset()
		})
	}
}
