// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb_test

import (
	"bytes"
	"context"
	"math/rand"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

func Test_Creation(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := xsync.NewManager(
		db,
		xsync.ManagerConfig{
			RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetRangeProofHandler(db)),
			ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetChangeProofHandler(db)),
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(syncer)
	require.NoError(syncer.Start(context.Background()))
	require.NoError(syncer.Wait(context.Background()))
}

func Test_Sync_FindNextKey_InSync(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	now := time.Now().UnixNano()

	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 1000)
	require.NoError(err)

	db, err := merkledb.New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	// sync db to the same state as dbToSync
	proof, err := dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	proof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)

	// the two dbs should be in sync, so next key should be nil
	lastKey := proof.KeyChanges[len(proof.KeyChanges)-1].Key
	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	// add an extra value to sync db past the last key returned
	newKeyVal := make([]byte, len(lastKey))
	copy(newKeyVal, lastKey)
	newKeyVal = append(newKeyVal, 16) // make sure new key is after last key
	require.NoError(db.Put(newKeyVal, []byte{1}))

	// create a range endpoint that is before the newly added key, but after the last key
	endPointBeforeNewKey := make([]byte, len(newKeyVal))
	copy(endPointBeforeNewKey, newKeyVal)
	endPointBeforeNewKey[len(endPointBeforeNewKey)-1] = 8

	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some(endPointBeforeNewKey), proof)
	require.NoError(err)

	// next key would be after the end of the range, so it returns Nothing instead
	require.True(nextKey.IsNothing())
}

func Test_Sync_FindNextKey_Deleted(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	dbToSync, err := merkledb.New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(dbToSync.Put([]byte{0x10}, []byte{1}))
	require.NoError(dbToSync.Put([]byte{0x11, 0x11}, []byte{2}))

	// Create empty DB to commit to one key
	db, err := merkledb.New(ctx, memdb.New(), newDefaultDBConfig())
	require.NoError(err)
	require.NoError(db.Put([]byte{0x13}, []byte{3}))

	// 0x12 was "deleted" and there should be no extra node in the proof since there was nothing with a common prefix
	rangeProof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Some([]byte{0x12}), 100)
	require.NoError(err)

	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), rangeProof)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x13}), nextKey)

	// 0x11 was "deleted" and 0x11.0x11 should be in the exclusion proof
	extraNodeProof, err := dbToSync.GetProof(context.Background(), []byte{0x11})
	require.NoError(err)
	rangeProof.EndProof = extraNodeProof.Path

	// Reset the db and commit new proof
	require.NoError(db.Clear())
	require.NoError(db.Put([]byte{0x13}, []byte{3}))
	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), rangeProof)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x13}), nextKey)
}

func Test_Sync_FindNextKey_BranchInLocal(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	db, err := merkledb.New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{2}))

	rangeProof, err := db.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), 100)
	require.NoError(err)

	require.NoError(db.Put([]byte{0x11, 0x15}, []byte{4}))

	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), rangeProof)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x11, 0x15}), nextKey)
}

func Test_Sync_FindNextKey_BranchInReceived(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	db, err := merkledb.New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x12}, []byte{2}))
	require.NoError(db.Put([]byte{0x12, 0xA0}, []byte{4}))

	rangeProof, err := db.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x12}), 100)
	require.NoError(err)

	require.NoError(db.Delete([]byte{0x12, 0xA0}))

	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), rangeProof)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x12, 0xA0}), nextKey)
}

func Test_Sync_FindNextKey_ExtraValues(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 1000)
	require.NoError(err)

	// Make a matching DB
	db, err := merkledb.New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	rangeProof, err := dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	// Get a new partial range proof
	rangeProof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)

	// add an extra value to local db
	lastKey := rangeProof.KeyChanges[len(rangeProof.KeyChanges)-1].Key
	afterKeyVal := make([]byte, len(lastKey))
	copy(afterKeyVal, lastKey)
	afterKeyVal = append(afterKeyVal, 16) // make sure new key is after last key

	require.NoError(db.Put(afterKeyVal, []byte{1}))

	// next key at prefix of newly added point
	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.HasValue())
	require.True(isPrefix(afterKeyVal, nextKey.Value()))

	require.NoError(db.Delete(afterKeyVal))

	require.NoError(dbToSync.Put(afterKeyVal, []byte{1}))

	rangeProof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some(lastKey), 500)
	require.NoError(err)

	// next key at prefix of newly added point
	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.HasValue())

	// deal with odd length key
	require.True(isPrefix(afterKeyVal, nextKey.Value()))
}

func isPrefix(data []byte, prefix []byte) bool {
	if prefix[len(prefix)-1]%16 == 0 {
		index := 0
		for ; index < len(prefix)-1; index++ {
			if data[index] != prefix[index] {
				return false
			}
		}
		return data[index]>>4 == prefix[index]>>4
	}
	return bytes.HasPrefix(data, prefix)
}

func Test_Sync_FindNextKey_DifferentChild(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 500)
	require.NoError(err)

	// Make a matching DB
	db, err := merkledb.New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	rangeProof, err := dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	rangeProof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 100)
	require.NoError(err)
	lastKey := rangeProof.KeyChanges[len(rangeProof.KeyChanges)-1].Key

	// local db has a different child than remote db
	lastKey = append(lastKey, 16)
	require.NoError(db.Put(lastKey, []byte{1}))

	require.NoError(dbToSync.Put(lastKey, []byte{2}))

	rangeProof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some(rangeProof.KeyChanges[len(rangeProof.KeyChanges)-1].Key), 100)
	require.NoError(err)

	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.HasValue())
	require.Equal(lastKey, nextKey.Value())
}

// Test findNextKey by computing the expected result in a naive, inefficient
// way and comparing it to the actual result
func TestFindNextKeyRandom(t *testing.T) {
	now := time.Now().UnixNano()
	ctx := context.Background()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404
	require := require.New(t)

	// Create a "remote" database and "local" database
	remoteDB, err := merkledb.New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	config := newDefaultDBConfig()
	localDB, err := merkledb.New(
		ctx,
		memdb.New(),
		config,
	)
	require.NoError(err)

	var (
		numProofsToTest  = 250
		numKeyValues     = 250
		maxKeyLen        = 256
		maxValLen        = 256
		maxRangeStartLen = 8
		maxRangeEndLen   = 8
		maxProofLen      = 128
	)

	// Put random keys into the databases
	for _, db := range []database.Database{remoteDB, localDB} {
		for i := 0; i < numKeyValues; i++ {
			key := make([]byte, rand.Intn(maxKeyLen))
			_, _ = rand.Read(key)
			val := make([]byte, rand.Intn(maxValLen))
			_, _ = rand.Read(val)
			require.NoError(db.Put(key, val))
		}
	}

	// Repeatedly generate end proofs from the remote database and compare
	// the result of findNextKey to the expected result.
	for proofIndex := 0; proofIndex < numProofsToTest; proofIndex++ {
		// Generate a proof for a random key
		var (
			rangeStart []byte
			rangeEnd   []byte
		)
		// Generate a valid range start and end
		for rangeStart == nil || bytes.Compare(rangeStart, rangeEnd) == 1 {
			rangeStart = make([]byte, rand.Intn(maxRangeStartLen)+1)
			_, _ = rand.Read(rangeStart)
			rangeEnd = make([]byte, rand.Intn(maxRangeEndLen)+1)
			_, _ = rand.Read(rangeEnd)
		}

		startKey := maybe.Nothing[[]byte]()
		if len(rangeStart) > 0 {
			startKey = maybe.Some(rangeStart)
		}
		endKey := maybe.Nothing[[]byte]()
		if len(rangeEnd) > 0 {
			endKey = maybe.Some(rangeEnd)
		}

		remoteProof, err := remoteDB.GetRangeProof(
			ctx,
			startKey,
			endKey,
			rand.Intn(maxProofLen)+1,
		)
		require.NoError(err)

		if len(remoteProof.KeyChanges) == 0 {
			continue
		}
		lastReceivedKey := remoteProof.KeyChanges[len(remoteProof.KeyChanges)-1].Key

		// Commit the proof to the local database as we do
		// in the actual syncer.
		_, err = localDB.CommitRangeProof(
			ctx,
			startKey,
			endKey,
			remoteProof,
		)
		require.NoError(err)

		localProof, err := localDB.GetProof(
			context.Background(),
			lastReceivedKey,
		)
		require.NoError(err)

		type keyAndID struct {
			key merkledb.Key
			id  ids.ID
		}

		// Set of key prefix/ID pairs proven by the remote database's end proof.
		remoteKeyIDs := []keyAndID{}
		for _, node := range remoteProof.EndProof {
			for childIdx, childID := range node.Children {
				remoteKeyIDs = append(remoteKeyIDs, keyAndID{
					key: node.Key.Extend(merkledb.ToToken(childIdx, merkledb.BranchFactorToTokenSize[config.BranchFactor])),
					id:  childID,
				})
			}
		}

		// Set of key prefix/ID pairs proven by the local database's proof.
		localKeyIDs := []keyAndID{}
		for _, node := range localProof.Path {
			for childIdx, childID := range node.Children {
				localKeyIDs = append(localKeyIDs, keyAndID{
					key: node.Key.Extend(merkledb.ToToken(childIdx, merkledb.BranchFactorToTokenSize[config.BranchFactor])),
					id:  childID,
				})
			}
		}

		// Sort in ascending order by key prefix.
		serializedPathCompare := func(i, j keyAndID) int {
			return i.key.Compare(j.key)
		}
		slices.SortFunc(remoteKeyIDs, serializedPathCompare)
		slices.SortFunc(localKeyIDs, serializedPathCompare)

		// Filter out keys that are before the last received key
		findBounds := func(keyIDs []keyAndID) (int, int) {
			var (
				firstIdxInRange      = len(keyIDs)
				firstIdxInRangeFound = false
				firstIdxOutOfRange   = len(keyIDs)
			)
			for i, keyID := range keyIDs {
				if !firstIdxInRangeFound && bytes.Compare(keyID.key.Bytes(), lastReceivedKey) > 0 {
					firstIdxInRange = i
					firstIdxInRangeFound = true
					continue
				}
				if bytes.Compare(keyID.key.Bytes(), rangeEnd) > 0 {
					firstIdxOutOfRange = i
					break
				}
			}
			return firstIdxInRange, firstIdxOutOfRange
		}

		remoteFirstIdxAfterLastReceived, remoteFirstIdxAfterEnd := findBounds(remoteKeyIDs)
		remoteKeyIDs = remoteKeyIDs[remoteFirstIdxAfterLastReceived:remoteFirstIdxAfterEnd]

		localFirstIdxAfterLastReceived, localFirstIdxAfterEnd := findBounds(localKeyIDs)
		localKeyIDs = localKeyIDs[localFirstIdxAfterLastReceived:localFirstIdxAfterEnd]

		// Find smallest difference between the set of key/ID pairs proven by
		// the remote/local proofs for key/ID pairs after the last received key.
		var (
			smallestDiffKey merkledb.Key
			foundDiff       bool
		)
		for i := 0; i < len(remoteKeyIDs) && i < len(localKeyIDs); i++ {
			// See if the keys are different.
			smaller, bigger := remoteKeyIDs[i], localKeyIDs[i]
			if serializedPathCompare(localKeyIDs[i], remoteKeyIDs[i]) == -1 {
				smaller, bigger = localKeyIDs[i], remoteKeyIDs[i]
			}

			if smaller.key != bigger.key || smaller.id != bigger.id {
				smallestDiffKey = smaller.key
				foundDiff = true
				break
			}
		}
		if !foundDiff {
			// All the keys were equal. The smallest diff is the next key
			// in the longer of the lists (if they're not same length.)
			if len(remoteKeyIDs) < len(localKeyIDs) {
				smallestDiffKey = localKeyIDs[len(remoteKeyIDs)].key
			} else if len(remoteKeyIDs) > len(localKeyIDs) {
				smallestDiffKey = remoteKeyIDs[len(localKeyIDs)].key
			}
		}

		// Get the actual value from the syncer
		gotFirstDiff, err := localDB.CommitRangeProof(ctx, maybe.Nothing[[]byte](), endKey, remoteProof)
		require.NoError(err)

		if bytes.Compare(smallestDiffKey.Bytes(), rangeEnd) >= 0 {
			// The smallest key which differs is after the range end so the
			// next key to get should be nil because we're done fetching the range.
			require.True(gotFirstDiff.IsNothing())
		} else {
			require.Equal(smallestDiffKey.Bytes(), gotFirstDiff.Value())
		}
	}
}

// Tests that we are able to sync to the correct root while the server is
// updating
func Test_Sync_Result_Correct_Root(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	tests := []struct {
		name              string
		db                merkledb.MerkleDB
		rangeProofClient  func(db merkledb.MerkleDB) *p2p.Client
		changeProofClient func(db merkledb.MerkleDB) *p2p.Client
	}{
		{
			name: "range proof bad response - too many leaves in response",
			rangeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.KeyChanges = append(response.KeyChanges, merkledb.KeyChange{})
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - removed first key in response",
			rangeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.KeyChanges = response.KeyChanges[min(1, len(response.KeyChanges)):]
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - removed first key in response and replaced proof",
			rangeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.KeyChanges = response.KeyChanges[min(1, len(response.KeyChanges)):]
					response.KeyChanges = []merkledb.KeyChange{
						{
							Key:   []byte("foo"),
							Value: maybe.Some([]byte("bar")),
						},
					}
					response.StartProof = []merkledb.ProofNode{
						{
							Key: merkledb.Key{},
						},
					}
					response.EndProof = []merkledb.ProofNode{
						{
							Key: merkledb.Key{},
						},
					}
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - removed key from middle of response",
			rangeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					i := rand.Intn(max(1, len(response.KeyChanges)-1)) // #nosec G404
					_ = slices.Delete(response.KeyChanges, i, min(len(response.KeyChanges), i+1))
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - start and end proof nodes removed",
			rangeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.StartProof = nil
					response.EndProof = nil
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - end proof removed",
			rangeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.EndProof = nil
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - empty proof",
			rangeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.StartProof = nil
					response.EndProof = nil
					response.KeyChanges = nil
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof server flake",
			rangeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, &flakyHandler{
					Handler: xsync.NewGetRangeProofHandler(db),
					c:       &counter{m: 2},
				})
			},
		},
		{
			name: "change proof bad response - too many keys in response",
			changeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					response.KeyChanges = append(response.KeyChanges, make([]merkledb.KeyChange, xsync.DefaultRequestKeyLimit)...)
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "change proof bad response - removed first key in response",
			changeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					response.KeyChanges = response.KeyChanges[min(1, len(response.KeyChanges)):]
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "change proof bad response - removed key from middle of response",
			changeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					i := rand.Intn(max(1, len(response.KeyChanges)-1)) // #nosec G404
					_ = slices.Delete(response.KeyChanges, i, min(len(response.KeyChanges), i+1))
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "change proof bad response - all proof keys removed from response",
			changeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					response.StartProof = nil
					response.EndProof = nil
				})

				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "change proof flaky server",
			changeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				return p2ptest.NewSelfClient(t, context.Background(), ids.EmptyNodeID, &flakyHandler{
					Handler: xsync.NewGetChangeProofHandler(db),
					c:       &counter{m: 2},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := context.Background()
			dbToSync, err := generateTrie(t, r, 3*xsync.MaxKeyValuesLimit)
			require.NoError(err)

			syncRoot, err := dbToSync.GetMerkleRoot(ctx)
			require.NoError(err)

			db, err := merkledb.New(
				ctx,
				memdb.New(),
				newDefaultDBConfig(),
			)
			require.NoError(err)

			var (
				rangeProofClient  *p2p.Client
				changeProofClient *p2p.Client
			)

			rangeProofHandler := xsync.NewGetRangeProofHandler(dbToSync)
			rangeProofClient = p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, rangeProofHandler)
			if tt.rangeProofClient != nil {
				rangeProofClient = tt.rangeProofClient(dbToSync)
			}

			changeProofHandler := xsync.NewGetChangeProofHandler(dbToSync)
			changeProofClient = p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, changeProofHandler)
			if tt.changeProofClient != nil {
				changeProofClient = tt.changeProofClient(dbToSync)
			}

			syncer, err := xsync.NewManager(
				db,
				xsync.ManagerConfig{
					RangeProofClient:      rangeProofClient,
					ChangeProofClient:     changeProofClient,
					TargetRoot:            syncRoot,
					SimultaneousWorkLimit: 5,
					Log:                   logging.NoLog{},
				},
				prometheus.NewRegistry(),
			)

			require.NoError(err)
			require.NotNil(syncer)

			// Start syncing from the server
			require.NoError(syncer.Start(ctx))

			// Simulate writes on the server
			//
			// TODO add more writes when api is not flaky. There is an inherent
			// race condition in between writes where UpdateSyncTarget might
			// error because it has already reached the sync target before it
			// is called.
			for i := 0; i < 50; i++ {
				addkey := make([]byte, r.Intn(50))
				_, err = r.Read(addkey)
				require.NoError(err)
				val := make([]byte, r.Intn(50))
				_, err = r.Read(val)
				require.NoError(err)

				// Update the server's root + our sync target
				require.NoError(dbToSync.Put(addkey, val))
				targetRoot, err := dbToSync.GetMerkleRoot(ctx)
				require.NoError(err)

				// Simulate client periodically recording root updates
				require.NoError(syncer.UpdateSyncTarget(targetRoot))
			}

			// Block until all syncing is done
			require.NoError(syncer.Wait(ctx))

			// We should have the same resulting root as the server
			wantRoot, err := dbToSync.GetMerkleRoot(context.Background())
			require.NoError(err)

			gotRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)
			require.Equal(wantRoot, gotRoot)
		})
	}
}

func Test_Sync_Result_Correct_Root_With_Sync_Restart(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 3*xsync.MaxKeyValuesLimit)
	require.NoError(err)
	syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
	require.NoError(err)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := xsync.NewManager(
		db,
		xsync.ManagerConfig{
			RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetRangeProofHandler(dbToSync)),
			ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetChangeProofHandler(dbToSync)),
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(syncer)

	// Start syncing from the server, will be cancelled by the range proof handler
	require.NoError(syncer.Start(ctx))

	// Wait until we've processed some work before closing
	require.Eventually(func() bool {
		return db.NewIterator().Next()
	}, 5*time.Second, 5*time.Millisecond)

	newSyncer, err := xsync.NewManager(
		db,
		xsync.ManagerConfig{
			RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetRangeProofHandler(dbToSync)),
			ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetChangeProofHandler(dbToSync)),
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(newSyncer)

	require.NoError(newSyncer.Start(context.Background()))
	require.NoError(newSyncer.Error())
	require.NoError(newSyncer.Wait(context.Background()))

	newRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(syncRoot, newRoot)
}

func Test_Sync_Result_Correct_Root_Update_Root_During(t *testing.T) {
	t.Skip("FLAKY")

	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	dbToSync, err := generateTrie(t, r, 3*xsync.MaxKeyValuesLimit)
	require.NoError(err)

	firstSyncRoot, err := dbToSync.GetMerkleRoot(context.Background())
	require.NoError(err)

	for x := 0; x < 100; x++ {
		key := make([]byte, r.Intn(50))
		_, err = r.Read(key)
		require.NoError(err)

		val := make([]byte, r.Intn(50))
		_, err = r.Read(val)
		require.NoError(err)

		require.NoError(dbToSync.Put(key, val))

		deleteKeyStart := make([]byte, r.Intn(50))
		_, err = r.Read(deleteKeyStart)
		require.NoError(err)

		it := dbToSync.NewIteratorWithStart(deleteKeyStart)
		if it.Next() {
			require.NoError(dbToSync.Delete(it.Key()))
		}
		require.NoError(it.Error())
		it.Release()
	}

	secondSyncRoot, err := dbToSync.GetMerkleRoot(context.Background())
	require.NoError(err)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	rangeProofHandler := &p2pHandlerAction{
		Handler: xsync.NewGetRangeProofHandler(dbToSync),
	}

	ctx := context.Background()
	syncer, err := xsync.NewManager(
		db,
		xsync.ManagerConfig{
			RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, rangeProofHandler),
			ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetChangeProofHandler(dbToSync)),
			TargetRoot:            firstSyncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(syncer)

	// Allow 1 request to go through before blocking
	updatedRootChan := make(chan struct{}, 1)
	updatedRootChan <- struct{}{}
	once := &sync.Once{}
	rangeProofHandler.action = func() {
		select {
		case <-updatedRootChan:
			// do nothing, allow 1 request to go through
		default:
			once.Do(func() {
				require.NoError(syncer.UpdateSyncTarget(secondSyncRoot))
			})
		}
	}

	require.NoError(syncer.Start(context.Background()))
	require.NoError(syncer.Wait(context.Background()))
	require.NoError(syncer.Error())

	newRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(secondSyncRoot, newRoot)
}

func Test_Sync_UpdateSyncTarget(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	// Generate a source DB with two roots
	dbToSync, err := generateTrie(t, r, 1000)
	require.NoError(err)
	root1, err := dbToSync.GetMerkleRoot(ctx)
	require.NoError(err)
	val, _ := dbToSync.Get([]byte{0}) // get any value if it exists
	require.NoError(dbToSync.Put([]byte{0}, append(val, 0)))
	root2, err := dbToSync.GetMerkleRoot(ctx)
	require.NoError(err)
	require.NotEqual(root1, root2)

	db, err := merkledb.New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	rangeProofHandler := &p2pHandlerAction{
		Handler: xsync.NewGetRangeProofHandler(dbToSync),
	}
	m, err := xsync.NewManager(
		db,
		xsync.ManagerConfig{
			RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, rangeProofHandler),
			ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetChangeProofHandler(dbToSync)),
			TargetRoot:            root1,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	// Update sync target on first request
	once := &sync.Once{}
	rangeProofHandler.action = func() {
		once.Do(func() {
			require.NoError(m.UpdateSyncTarget(root2))
		})
	}
	require.NoError(m.Start(ctx))
	require.NoError(m.Wait(ctx))
}

func generateTrie(t *testing.T, r *rand.Rand, count int) (merkledb.MerkleDB, error) {
	return generateTrieWithMinKeyLen(t, r, count, 0)
}

func generateTrieWithMinKeyLen(t *testing.T, r *rand.Rand, count int, minKeyLen int) (merkledb.MerkleDB, error) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	if err != nil {
		return nil, err
	}
	var (
		allKeys  [][]byte
		seenKeys = make(map[string]struct{})
		batch    = db.NewBatch()
	)
	genKey := func() []byte {
		// new prefixed key
		if len(allKeys) > 2 && r.Intn(25) < 10 {
			prefix := allKeys[r.Intn(len(allKeys))]
			key := make([]byte, r.Intn(50)+len(prefix))
			copy(key, prefix)
			_, err := r.Read(key[len(prefix):])
			require.NoError(err)
			return key
		}

		// new key
		key := make([]byte, r.Intn(50)+minKeyLen)
		_, err = r.Read(key)
		require.NoError(err)
		return key
	}

	for i := 0; i < count; {
		value := make([]byte, r.Intn(51))
		if len(value) == 0 {
			value = nil
		} else {
			_, err = r.Read(value)
			require.NoError(err)
		}
		key := genKey()
		if _, seen := seenKeys[string(key)]; seen {
			continue // avoid duplicate keys so we always get the count
		}
		allKeys = append(allKeys, key)
		seenKeys[string(key)] = struct{}{}
		if err = batch.Put(key, value); err != nil {
			return db, err
		}
		i++
	}
	return db, batch.Write()
}
