// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats/statstest"
	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/ids"
)

func TestLeafsRequestHandler_OnLeafsRequest(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	testHandlerStats := &statstest.TestHandlerStats{}
	memdb := rawdb.NewMemoryDatabase()
	db := state.NewDatabase(memdb)
	trieDB := db.TrieDB()

	corruptedTrieRoot, _, _ := synctest.GenerateIndependentTrie(t, r, trieDB, 100, common.HashLength)
	tr, err := trie.New(trie.TrieID(corruptedTrieRoot), trieDB)
	require.NoError(t, err)
	// Corrupt [corruptedTrieRoot]
	synctest.CorruptTrie(t, memdb, tr, 5)

	largeTrieRoot, largeTrieKeys, _ := synctest.GenerateIndependentTrie(t, r, trieDB, 10_000, common.HashLength)
	smallTrieRoot, _, _ := synctest.GenerateIndependentTrie(t, r, trieDB, 500, common.HashLength)
	accountTrieRoot, accounts := synctest.FillAccounts(
		t,
		r,
		db,
		common.Hash{},
		10_000,
		func(_ *testing.T, i int, _ common.Address, acc types.StateAccount, _ state.Trie) types.StateAccount {
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
	snapConfig := snapshot.Config{
		CacheSize:  64,
		AsyncBuild: false,
		NoBuild:    false,
		SkipVerify: true,
	}

	leafsHandler := NewLeafsRequestHandler(trieDB, message.StateTrieKeyLength, snapshotProvider, message.CorethCodec, testHandlerStats)
	tests := map[string]struct {
		prepareTestFn     func() (context.Context, message.CorethLeafsRequest)
		requireResponseFn func(*testing.T, message.CorethLeafsRequest, []byte, error)
	}{
		"zero limit dropped": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					0,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"empty root dropped": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					common.Hash{},
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"bad start len dropped": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					common.Hash{},
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength+2),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"bad end len dropped": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					common.Hash{},
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength-1),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"missing root dropped": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					common.BytesToHash([]byte("something is missing here...")),
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.MissingRootCount)
			},
		},
		"corrupted trie drops request": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					corruptedTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.TrieErrorCount)
			},
		},
		"cancelled context dropped": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				return newLeafsRequest(t,
					ctx,
					largeTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
			},
		},
		"nil start and end range returns entire trie": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					smallTrieRoot,
					common.Hash{},
					nil,
					nil,
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 500)
				require.Len(t, leafsResponse.Vals, 500)
				require.Empty(t, leafsResponse.ProofVals)
			},
		},
		"nil end range treated like greatest possible value": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					smallTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					nil,
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 500)
				require.Len(t, leafsResponse.Vals, 500)
			},
		},
		"end greater than start dropped": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				return newLeafsRequest(t,
					ctx,
					largeTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0xbb}, common.HashLength),
					bytes.Repeat([]byte{0xaa}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
				require.Equal(t, uint32(1), testHandlerStats.InvalidLeafsRequestCount)
			},
		},
		"invalid node type dropped": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				return newLeafsRequest(t,
					ctx,
					largeTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0xbb}, common.HashLength),
					bytes.Repeat([]byte{0xaa}, common.HashLength),
					maxLeavesLimit,
					message.NodeType(11),
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.Nil(t, response)
				require.NoError(t, err)
			},
		},
		"max leaves overridden": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit*10,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, _ message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
			},
		},
		"full range with nil start": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					common.Hash{},
					nil,
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)
			},
		},
		"full range with 0x00 start": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0x00}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, int(maxLeavesLimit))
				require.Len(t, leafsResponse.Vals, int(maxLeavesLimit))
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)
			},
		},
		"partial mid range": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				startKey := largeTrieKeys[1_000]
				startKey[31]++                 // exclude start key from response
				endKey := largeTrieKeys[1_040] // include end key in response

				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					common.Hash{},
					startKey,
					endKey,
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 40)
				require.Len(t, leafsResponse.Vals, 40)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, true)
			},
		},
		"partial end range": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					common.Hash{},
					largeTrieKeys[9_400],
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Len(t, leafsResponse.Keys, 600)
				require.Len(t, leafsResponse.Vals, 600)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, false)
			},
		},
		"final end range": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					common.Hash{},
					bytes.Repeat([]byte{0xff}, common.HashLength),
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
				require.NoError(t, err)
				require.Empty(t, leafsResponse.Keys)
				require.Empty(t, leafsResponse.Vals)
				require.Equal(t, uint32(1), testHandlerStats.LeafsRequestCount)
				require.Equal(t, uint32(len(leafsResponse.Keys)), testHandlerStats.LeafsReturnedSum)
				requireRangeProofIsValid(t, &request, &leafsResponse, false)
			},
		},
		"small trie root": {
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				return newLeafsRequest(t,
					t.Context(),
					smallTrieRoot,
					common.Hash{},
					nil,
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NotEmpty(t, response)
				require.NoError(t, err)

				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
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
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				snap, err := snapshot.New(snapConfig, memdb, trieDB, common.Hash{}, accountTrieRoot)
				require.NoError(t, err)
				snapshotProvider.Snapshot = snap

				return newLeafsRequest(t,
					t.Context(),
					accountTrieRoot,
					common.Hash{},
					nil,
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
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
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
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

				return newLeafsRequest(t,
					t.Context(),
					accountTrieRoot,
					common.Hash{},
					nil,
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
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
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
				snap, err := snapshot.New(snapConfig, memdb, trieDB, common.Hash{}, accountTrieRoot)
				require.NoError(t, err)
				snapshotProvider.Snapshot = snap

				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					largeStorageAccount,
					nil,
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
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
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
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

				return newLeafsRequest(t,
					t.Context(),
					largeTrieRoot,
					largeStorageAccount,
					nil,
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
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
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
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

				return newLeafsRequest(t,
					t.Context(),
					smallTrieRoot,
					smallStorageAccount,
					nil,
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
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
			prepareTestFn: func() (context.Context, message.CorethLeafsRequest) {
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

				return newLeafsRequest(t,
					t.Context(),
					smallTrieRoot,
					smallStorageAccount,
					lastKey[:],
					bytes.Repeat([]byte{0xff}, common.HashLength),
					maxLeavesLimit,
					message.StateTrieNode,
				)
			},
			requireResponseFn: func(t *testing.T, request message.CorethLeafsRequest, response []byte, err error) {
				require.NoError(t, err)
				var leafsResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafsResponse)
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

func requireRangeProofIsValid(t *testing.T, request *message.CorethLeafsRequest, response *message.LeafsResponse, expectMore bool) {
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

// newLeafsRequest creates a new CorethLeafsRequest for testing.
// When account is common.Hash{} (empty), it creates an account trie request.
// When account is set to a specific account hash, it creates a storage trie request for that account.
func newLeafsRequest(
	t *testing.T,
	ctx context.Context,
	root common.Hash,
	account common.Hash,
	start, end []byte,
	limit uint16,
	nodeType message.NodeType,
) (context.Context, message.CorethLeafsRequest) {
	request, err := message.NewLeafsRequest(message.CorethLeafsRequestType, root, account, start, end, limit, nodeType)
	require.NoError(t, err)
	return ctx, request.(message.CorethLeafsRequest)
}
