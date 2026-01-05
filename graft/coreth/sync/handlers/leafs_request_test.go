// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats/statstest"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/statesync/statesynctest"
	"github.com/ava-labs/avalanchego/ids"
)

func TestLeafsRequestHandler_OnLeafsRequest(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	testHandlerStats := &statstest.TestHandlerStats{}
	memdb := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(memdb, nil)

	corruptedTrieRoot, _, _ := statesynctest.GenerateTrie(t, r, trieDB, 100, common.HashLength)
	tr, err := trie.New(trie.TrieID(corruptedTrieRoot), trieDB)
	require.NoError(t, err)
	// Corrupt [corruptedTrieRoot]
	statesynctest.CorruptTrie(t, memdb, tr, 5)

	largeTrieRoot, largeTrieKeys, _ := statesynctest.GenerateTrie(t, r, trieDB, 10_000, common.HashLength)
	smallTrieRoot, _, _ := statesynctest.GenerateTrie(t, r, trieDB, 500, common.HashLength)
	accountTrieRoot, accounts := statesynctest.FillAccounts(
		t,
		r,
		trieDB,
		common.Hash{},
		10_000,
		func(_ *testing.T, i int, acc types.StateAccount) types.StateAccount {
			// set the storage trie root for two accounts
			switch i {
			case 0:
				acc.Root = largeTrieRoot
			case 1:
				acc.Root = smallTrieRoot
			}

			return acc
		})

	// find the hash of the account we set to have a storage
	var (
		largeStorageAccount common.Hash
		smallStorageAccount common.Hash
	)
	for key, account := range accounts {
		if account.Root == largeTrieRoot {
			largeStorageAccount = crypto.Keccak256Hash(key.Address[:])
		}
		if account.Root == smallTrieRoot {
			smallStorageAccount = crypto.Keccak256Hash(key.Address[:])
		}
		if (largeStorageAccount != common.Hash{}) && (smallStorageAccount != common.Hash{}) {
			// we can break if we found both accounts of interest to the test
			break
		}
	}
	snapshotProvider := &TestSnapshotProvider{}
	leafsHandler := NewLeafsRequestHandler(trieDB, message.StateTrieKeyLength, snapshotProvider, message.Codec, testHandlerStats)
	snapConfig := snapshot.Config{
		CacheSize:  64,
		AsyncBuild: false,
		NoBuild:    false,
		SkipVerify: true,
	}

	tests := map[string]struct {
		prepareTestFn     func() (context.Context, message.LeafsRequest)
		requireResponseFn func(*testing.T, message.LeafsRequest, []byte, error)
	}{
		"zero limit dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    0,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"empty root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     common.Hash{},
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"bad start len dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     common.Hash{},
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength+2),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"bad end len dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     common.Hash{},
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength-1),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"empty storage root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     types.EmptyRootHash,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"missing root dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     common.BytesToHash([]byte("something is missing here...")),
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.MissingRootCount)
			},
		},
		"corrupted trie drops request": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     corruptedTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.TrieErrorCount)
			},
		},
		"cancelled context dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
			},
		},
		"nil start and end range returns entire trie": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Start:    nil,
					End:      nil,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 500)
				require.Len(t, leafsResponse.Vals, 500)
				require.Empty(t, leafsResponse.ProofVals)
			},
		},
		"nil end range treated like greatest possible value": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      nil,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 500)
				require.Len(t, leafsResponse.Vals, 500)
			},
		},
		"end greater than start dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0xbb}, common.HashLength),
					End:      bytes.Repeat([]byte{0xaa}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"invalid node type dropped": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				return ctx, message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0xbb}, common.HashLength),
					End:      bytes.Repeat([]byte{0xaa}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.NodeType(11),
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
			},
		},
		"max leaves overridden": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit * 10,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, _ message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
			},
		},
		"full range with nil start": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    nil,
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)
			},
		},
		"full range with 0x00 start": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)
			},
		},
		"partial mid range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				startKey := largeTrieKeys[1_000]
				startKey[31]++                 // exclude start key from response
				endKey := largeTrieKeys[1_040] // include end key in response
				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    startKey,
					End:      endKey,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 40)
				require.Len(t, leafsResponse.Vals, 40)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)
			},
		},
		"partial end range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    largeTrieKeys[9_400],
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 600)
				require.Len(t, leafsResponse.Vals, 600)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, false)
			},
		},
		"final end range": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Start:    bytes.Repeat([]byte{0xff}, common.HashLength),
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Empty(t, leafsResponse.Keys)
				require.Empty(t, leafsResponse.Vals)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, false)
			},
		},
		"small trie root": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				return t.Context(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Start:    nil,
					End:      bytes.Repeat([]byte{0xff}, common.HashLength),
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NotEmpty(t, response)
				require.NoError(t, err)

				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)

				require.Len(t, leafsResponse.Keys, 500)
				require.Len(t, leafsResponse.Vals, 500)
				require.Empty(t, leafsResponse.ProofVals)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, false)
			},
		},
		"account data served from snapshot": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				snap, err := snapshot.New(snapConfig, memdb, trieDB, common.Hash{}, accountTrieRoot)
				require.NoError(t, err)
				snapshotProvider.Snapshot = snap
				return t.Context(), message.LeafsRequest{
					Root:     accountTrieRoot,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadAttemptCount)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadSuccessCount)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)
			},
		},
		"partial account data served from snapshot": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				snap, err := snapshot.New(snapConfig, memdb, trieDB, common.Hash{}, accountTrieRoot)
				require.NoError(t, err)
				snapshotProvider.Snapshot = snap
				it := snap.DiskAccountIterator(common.Hash{})
				defer it.Release()
				i := 0
				for it.Next() {
					if i > int(maxLeavesLimit) {
						// no need to modify beyond the request limit
						break
					}
					// modify one entry of 1 in 4 segments
					if i%(segmentLen*4) == 0 {
						acc, err := types.FullAccount(it.Account())
						require.NoError(t, err)
						acc.Nonce++
						bytes := types.SlimAccountRLP(*acc)
						rawdb.WriteAccountSnapshot(memdb, it.Hash(), bytes)
					}
					i++
				}

				return t.Context(), message.LeafsRequest{
					Root:     accountTrieRoot,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadAttemptCount)
				require.Equal(t, uint32(0), testHandlerStats.SnapshotReadSuccessCount)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)

				// expect 1/4th of segments to be invalid
				numSegments := maxLeavesLimit / segmentLen
				require.Equal(t, uint32(numSegments/4), testHandlerStats.SnapshotSegmentInvalidCount)
				require.Equal(t, uint32(3*numSegments/4), testHandlerStats.SnapshotSegmentValidCount)
			},
		},
		"storage data served from snapshot": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				snap, err := snapshot.New(snapConfig, memdb, trieDB, common.Hash{}, accountTrieRoot)
				require.NoError(t, err)
				snapshotProvider.Snapshot = snap
				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Account:  largeStorageAccount,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadAttemptCount)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadSuccessCount)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)
			},
		},
		"partial storage data served from snapshot": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				snap, err := snapshot.New(snapConfig, memdb, trieDB, common.Hash{}, accountTrieRoot)
				require.NoError(t, err)
				snapshotProvider.Snapshot = snap
				it := snap.DiskStorageIterator(largeStorageAccount, common.Hash{})
				defer it.Release()
				i := 0
				for it.Next() {
					if i > int(maxLeavesLimit) {
						// no need to modify beyond the request limit
						break
					}
					// modify one entry of 1 in 4 segments
					if i%(segmentLen*4) == 0 {
						randomBytes := make([]byte, 5)
						_, err := r.Read(randomBytes)
						require.NoError(t, err)
						rawdb.WriteStorageSnapshot(memdb, largeStorageAccount, it.Hash(), randomBytes)
					}
					i++
				}

				return t.Context(), message.LeafsRequest{
					Root:     largeTrieRoot,
					Account:  largeStorageAccount,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadAttemptCount)
				require.Equal(t, uint32(0), testHandlerStats.SnapshotReadSuccessCount)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)

				// expect 1/4th of segments to be invalid
				numSegments := maxLeavesLimit / segmentLen
				require.Equal(t, uint32(numSegments/4), testHandlerStats.SnapshotSegmentInvalidCount)
				require.Equal(t, uint32(3*numSegments/4), testHandlerStats.SnapshotSegmentValidCount)
			},
		},
		"last snapshot key removed": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				snap, err := snapshot.New(snapConfig, memdb, trieDB, common.Hash{}, accountTrieRoot)
				require.NoError(t, err)
				snapshotProvider.Snapshot = snap
				it := snap.DiskStorageIterator(smallStorageAccount, common.Hash{})
				defer it.Release()
				var lastKey common.Hash
				for it.Next() {
					lastKey = it.Hash()
				}
				rawdb.DeleteStorageSnapshot(memdb, smallStorageAccount, lastKey)

				return t.Context(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Account:  smallStorageAccount,
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 500)
				require.Len(t, leafsResponse.Vals, 500)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadAttemptCount)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadSuccessCount)
				requireRangeProofIsValid(t, &request, &leafsResponse, false)
			},
		},
		"request last key when removed from snapshot": {
			prepareTestFn: func() (context.Context, message.LeafsRequest) {
				snap, err := snapshot.New(snapConfig, memdb, trieDB, common.Hash{}, accountTrieRoot)
				require.NoError(t, err)
				snapshotProvider.Snapshot = snap
				it := snap.DiskStorageIterator(smallStorageAccount, common.Hash{})
				defer it.Release()
				var lastKey common.Hash
				for it.Next() {
					lastKey = it.Hash()
				}
				rawdb.DeleteStorageSnapshot(memdb, smallStorageAccount, lastKey)

				return t.Context(), message.LeafsRequest{
					Root:     smallTrieRoot,
					Account:  smallStorageAccount,
					Start:    lastKey[:],
					Limit:    maxLeavesLimit,
					NodeType: message.StateTrieNode,
				}
			},
			requireResponseFn: func(t *testing.T, request message.LeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.Codec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 1)
				require.Len(t, leafsResponse.Vals, 1)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				require.Equal(t, uint32(1), testHandlerStats.SnapshotReadAttemptCount)
				require.Equal(t, uint32(0), testHandlerStats.SnapshotReadSuccessCount)
				requireRangeProofIsValid(t, &request, &leafsResponse, false)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, request := test.prepareTestFn()
			t.Cleanup(func() {
				<-snapshot.WipeSnapshot(memdb, true)
				testHandlerStats.Reset()
				snapshotProvider.Snapshot = nil // reset the snapshot to nil
			})

			response, err := leafsHandler.OnLeafsRequest(ctx, ids.GenerateTestNodeID(), 1, request)
			test.requireResponseFn(t, request, response, err)
		})
	}
}

func requireRangeProofIsValid(t *testing.T, request *message.LeafsRequest, response *message.LeafsResponse, expectMore bool) {
	t.Helper()

	var start []byte
	if len(request.Start) == 0 {
		start = bytes.Repeat([]byte{0x00}, common.HashLength)
	} else {
		start = request.Start
	}

	var proof ethdb.Database
	if len(response.ProofVals) > 0 {
		proof = rawdb.NewMemoryDatabase()
		defer proof.Close()
		for _, proofVal := range response.ProofVals {
			proofKey := crypto.Keccak256(proofVal)
			require.NoError(t, proof.Put(proofKey, proofVal))
		}
	}

	more, err := trie.VerifyRangeProof(request.Root, start, response.Keys, response.Vals, proof)
	require.NoError(t, err)
	require.Equal(t, expectMore, more)
}
