// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
)

func TestLeafsRequestHandler_OnLeafsRequest(t *testing.T) {
	codec, err := message.BuildCodec()
	if err != nil {
		t.Fatal("unexpected error building codec", err)
	}

	stats := stats.NewNoopHandlerStats()
	memdb := memorydb.New()
	trieDB := trie.NewDatabase(memdb)
	tr, err := trie.New(common.Hash{}, trieDB)
	assert.NoError(t, err)

	// set random seed to ensure consistent data every time
	rand.Seed(1)

	// randomly generate some leaf data
	for i := 0; i < 10_000; i++ {
		data := make([]byte, rand.Intn(128)+128)
		if _, err = rand.Read(data); err != nil {
			t.Fatal("error reading random bytes", err)
		}

		key := crypto.Keccak256Hash(data)
		assert.NoError(t, tr.TryUpdate(key[:], data))
	}

	// commit the trie
	root, _, err := tr.Commit(nil)
	if err != nil {
		t.Fatal("could not commit trie", err)
	}

	// commit trie DB
	if err = trieDB.Commit(root, false, nil); err != nil {
		t.Fatal("error committing trieDB", err)
	}

	leafsHandler := NewLeafsRequestHandler(trieDB, stats, codec)

	tests := map[string]struct {
		prepareTestFn    func() (context.Context, message.LeafsRequest)
		assertResponseFn func(*testing.T, message.LeafsRequest, []byte, error)
	}{
		"zero limit dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     root,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    0,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.Nil(t, response)
				assert.Nil(t, err)
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
			},
		},
		"cancelled context dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     root,
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
		"max leaves overridden": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     root,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit * 10,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), maxLeavesLimit)
				assert.EqualValues(t, len(leafsResponse.Vals), maxLeavesLimit)
			},
		},
		"full range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     root,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), maxLeavesLimit)
				assert.EqualValues(t, len(leafsResponse.Vals), maxLeavesLimit)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(root, request.Start, leafsResponse.Keys[len(leafsResponse.Keys)-1], leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.True(t, more)
			},
		},
		"partial mid range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     root,
					Start:    bytes.Repeat([]byte{0x11}, common.HashLength),
					End:      bytes.Repeat([]byte{0x12}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), 39)
				assert.EqualValues(t, len(leafsResponse.Vals), 39)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(root, request.Start, request.End, leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.True(t, more)
			},
		},
		"partial end range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return context.Background(), message.LeafsRequest{
					Root:     root,
					Start:    bytes.Repeat([]byte{0xf0}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			assertResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				assert.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = codec.Unmarshal(response, &leafsResponse)
				assert.NoError(t, err)
				assert.EqualValues(t, len(leafsResponse.Keys), 603)
				assert.EqualValues(t, len(leafsResponse.Vals), 603)

				proofDB := memorydb.New()
				defer proofDB.Close()
				for i, proofKey := range leafsResponse.ProofKeys {
					if err = proofDB.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
						t.Fatal(err)
					}
				}

				more, err := trie.VerifyRangeProof(root, request.Start, request.End, leafsResponse.Keys, leafsResponse.Vals, proofDB)
				assert.NoError(t, err)
				assert.False(t, more)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, request := test.prepareTestFn()
			response, err := leafsHandler.OnLeafsRequest(ctx, ids.GenerateTestShortID(), 1, request)
			test.assertResponseFn(t, request, response, err)
		})
	}
}
