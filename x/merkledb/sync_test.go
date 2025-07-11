// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/stretchr/testify/require"
)

// The main goal of these tests is to ensure that merkleDB's `findNextKey` function works as expected.
// The tests are non-exhaustive of edge cases and are not deterministic, but identify minimum requirements
// for the returned next key.
//
// TODO: Add more tests to cover edge cases.

func TestFullRangeProof(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now))

	numKeys := 1000
	fullDB, err := GenerateTrie(t, r, numKeys)
	require.NoError(err)

	emptyDB, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	proof, err := fullDB.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), numKeys)
	require.NoError(err)

	nextKey, err := emptyDB.CommitRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	// Ensure the roots match
	fullRoot, err := fullDB.GetMerkleRoot(context.Background())
	require.NoError(err)
	emptyRoot, err := emptyDB.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(fullRoot, emptyRoot)
}

func TestIncompleteRangeProof(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now))
	numKeys := 1000
	fullDB, err := GenerateTrie(t, r, numKeys)
	require.NoError(err)

	emptyDB, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	// get a range proof for the first 500 keys
	proof, err := fullDB.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)
	lastKey := ToKey(proof.KeyChanges[len(proof.KeyChanges)-1].Key)

	// commit the proof to the empty db
	nextKeyBytes, err := emptyDB.CommitRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	nextKey := ToKey(nextKeyBytes.Value())

	fullProof, err := fullDB.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), numKeys)
	require.NoError(err)
	actualNextKey := ToKey(fullProof.KeyChanges[500].Key)

	// We expect lastKey <= nextKey <= actualNextKey
	require.LessOrEqual(lastKey.Compare(nextKey), 0)
	require.LessOrEqual(nextKey.Compare(actualNextKey), 0)

	// Apply the next 250 keys and make the same checks
	proof, err = fullDB.GetRangeProof(context.Background(), nextKeyBytes, maybe.Nothing[[]byte](), 250)
	require.NoError(err)
	lastKey = ToKey(proof.KeyChanges[len(proof.KeyChanges)-1].Key)
	nextKeyBytes, err = emptyDB.CommitRangeProof(context.Background(), nextKeyBytes, maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	nextKey = ToKey(nextKeyBytes.Value())
	actualNextKey = ToKey(fullProof.KeyChanges[750].Key)
	require.LessOrEqual(lastKey.Compare(nextKey), 0)
	require.LessOrEqual(nextKey.Compare(actualNextKey), 0)

	// Apply the last 250 keys and we expect to be done.
	proof, err = fullDB.GetRangeProof(context.Background(), nextKeyBytes, maybe.Nothing[[]byte](), 250)
	require.NoError(err)
	nextKeyBytes, err = emptyDB.CommitRangeProof(context.Background(), nextKeyBytes, maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	require.True(nextKeyBytes.IsNothing())

	// Ensure the roots match
	fullRoot, err := fullDB.GetMerkleRoot(context.Background())
	require.NoError(err)
	emptyRoot, err := emptyDB.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(fullRoot, emptyRoot)
}

func TestPartialRangeProof(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now))
	numKeys := 1000
	fullDB, err := GenerateTrie(t, r, numKeys)
	require.NoError(err)

	emptyDB, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	// Get the full range proof to index keys
	proof, err := fullDB.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), numKeys)
	require.NoError(err)
	keys := proof.KeyChanges

	// Get a request for a partial range of keys
	startKeyBytes := maybe.Nothing[[]byte]()
	endKeyBytes := maybe.Some(keys[499].Key)
	proof, err = fullDB.GetRangeProof(context.Background(), startKeyBytes, endKeyBytes, 1000)
	require.NoError(err)

	// Commit the partial range proof to the empty db
	nextKeyBytes, err := emptyDB.CommitRangeProof(context.Background(), startKeyBytes, endKeyBytes, proof)
	require.NoError(err)

	// We should have finished the entire range proof, so nextKey should be Nothing
	require.True(nextKeyBytes.IsNothing())

	// Now, let's put an upper limit on the key to retrieve, but the limit for the number of keys is not sufficient to fill the proof.
	startKeyBytes = nextKeyBytes
	endKeyBytes = maybe.Some(keys[749].Key)
	proof, err = fullDB.GetRangeProof(context.Background(), startKeyBytes, endKeyBytes, 100)
	require.NoError(err)
	nextKeyBytes, err = emptyDB.CommitRangeProof(context.Background(), startKeyBytes, endKeyBytes, proof)
	require.NoError(err)

	// We expect the next key provided is before the actual next key in the full proof.
	// However, the behavior relative to the last key in this proof is not guaranteed.
	nextKey := ToKey(nextKeyBytes.Value())
	actualNextKey := ToKey(keys[600].Key)
	require.LessOrEqual(nextKey.Compare(actualNextKey), 0)

	// Commit the rest of the keys
	proof, err = fullDB.GetRangeProof(context.Background(), nextKeyBytes, maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKeyBytes, err = emptyDB.CommitRangeProof(context.Background(), nextKeyBytes, maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	require.True(nextKeyBytes.IsNothing())

	// Ensure the roots match
	fullRoot, err := fullDB.GetMerkleRoot(context.Background())
	require.NoError(err)
	emptyRoot, err := emptyDB.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(fullRoot, emptyRoot)
}

// TODO: These tests expose internal functions
func Test_Sync_FindNextKey_InSync(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := GenerateTrie(t, r, 1000)
	require.NoError(err)
	require.NoError(err)

	db, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	dbStruct, ok := db.(*merkleDB)
	require.True(ok)

	// Apply all the keys from dbToSync to db
	fullProof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), fullProof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	// Check the roots match
	dbToSyncRoot, err := dbToSync.GetMerkleRoot(context.Background())
	require.NoError(err)
	dbRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(dbToSyncRoot, dbRoot)

	// Get a partial range proof
	proof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)

	// the two dbs should be in sync, so next key should be nil
	lastKey := proof.KeyChanges[len(proof.KeyChanges)-1].Key
	nextKey, err = dbStruct.findNextKey(lastKey, maybe.Nothing[[]byte](), proof.EndProof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	// add an extra value to sync db past the last key returned
	newKeyVal := midpointWithEnd(lastKey)
	require.NoError(db.Put(newKeyVal, []byte{1}))

	// create a range endpoint that is before the newly added key, but after the last key
	endPointBeforeNewKey := make([]byte, 0, 2)
	for i := 0; i < len(newKeyVal); i++ {
		endPointBeforeNewKey = append(endPointBeforeNewKey, newKeyVal[i])

		// we need the new key to be after the last key
		// don't subtract anything from the current byte if newkey and lastkey are equal
		if lastKey[i] == newKeyVal[i] {
			continue
		}

		// if the first nibble is > 0, subtract "1" from it
		if endPointBeforeNewKey[i] >= 16 {
			endPointBeforeNewKey[i] -= 16
			break
		}
		// if the second nibble > 0, subtract 1 from it
		if endPointBeforeNewKey[i] > 0 {
			endPointBeforeNewKey[i] -= 1
			break
		}
		// both nibbles were 0, so move onto the next byte
	}

	nextKey, err = dbStruct.findNextKey(lastKey, maybe.Some(endPointBeforeNewKey), proof.EndProof)
	require.NoError(err)

	// next key would be after the end of the range, so it returns Nothing instead
	require.True(nextKey.IsNothing())
}

func Test_Sync_FindNextKey_Deleted(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	// Expose the internal struct
	dbStruct, ok := db.(*merkleDB)
	require.True(ok)

	require.NoError(db.Put([]byte{0x10}, []byte{1}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{2}))

	// 0x12 was "deleted" and there should be no extra node in the proof since there was nothing with a common prefix
	noExtraNodeProof, err := db.GetProof(context.Background(), []byte{0x12})
	require.NoError(err)

	// 0x11 was "deleted" and 0x11.0x11 should be in the exclusion proof
	extraNodeProof, err := db.GetProof(context.Background(), []byte{0x11})
	require.NoError(err)

	// there is now another value in the range that needs to be sync'ed
	require.NoError(db.Put([]byte{0x13}, []byte{3}))

	nextKey, err := dbStruct.findNextKey([]byte{0x12}, maybe.Some([]byte{0x20}), noExtraNodeProof.Path)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x13}), nextKey)

	nextKey, err = dbStruct.findNextKey([]byte{0x11}, maybe.Some([]byte{0x20}), extraNodeProof.Path)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x13}), nextKey)
}

func Test_Sync_FindNextKey_BranchInLocal(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	// Expose the internal struct
	dbStruct, ok := db.(*merkleDB)
	require.True(ok)

	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{2}))

	proof, err := db.GetProof(context.Background(), []byte{0x11, 0x11})
	require.NoError(err)

	require.NoError(db.Put([]byte{0x11, 0x15}, []byte{4}))

	nextKey, err := dbStruct.findNextKey([]byte{0x11, 0x11}, maybe.Some([]byte{0x20}), proof.Path)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x11, 0x15}), nextKey)
}

func Test_Sync_FindNextKey_BranchInReceived(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	// Expose the internal struct
	dbStruct, ok := db.(*merkleDB)
	require.True(ok)

	// Add a key that has a branch in the proof
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x12}, []byte{2}))
	require.NoError(db.Put([]byte{0x12, 0xA0}, []byte{3}))
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x12}, []byte{2}))
	require.NoError(db.Put([]byte{0x12, 0xA0}, []byte{4}))

	proof, err := db.GetProof(context.Background(), []byte{0x12})
	require.NoError(err)
	require.NoError(db.Delete([]byte{0x12, 0xA0}))

	nextKey, err := dbStruct.findNextKey([]byte{0x12}, maybe.Some([]byte{0x20}), proof.Path)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x12, 0xA0}), nextKey)
}

func Test_Sync_FindNextKey_ExtraValues(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := GenerateTrie(t, r, 1000)
	require.NoError(err)

	db, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	// Expose the internal struct
	dbStruct, ok := db.(*merkleDB)
	require.True(ok)

	// Sync the DB
	fullProof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), fullProof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	proof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)

	// add an extra value to local db
	lastKey := proof.KeyChanges[len(proof.KeyChanges)-1].Key
	midPointVal := midpointWithEnd(lastKey)

	require.NoError(db.Put(midPointVal, []byte{1}))

	// next key at prefix of newly added point
	nextKey, err = dbStruct.findNextKey(lastKey, maybe.Nothing[[]byte](), proof.EndProof)
	require.NoError(err)
	require.True(nextKey.HasValue())

	require.True(isPrefix(midPointVal, nextKey.Value()))

	require.NoError(db.Delete(midPointVal))

	require.NoError(dbToSync.Put(midPointVal, []byte{1}))

	proof, err = dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Some(lastKey), 500)
	require.NoError(err)

	// next key at prefix of newly added point
	nextKey, err = dbStruct.findNextKey(lastKey, maybe.Nothing[[]byte](), proof.EndProof)
	require.NoError(err)
	require.True(nextKey.HasValue())

	// deal with odd length key
	require.True(isPrefix(midPointVal, nextKey.Value()))
}

func TestFindNextKeyEmptyEndProof(t *testing.T) {
	require := require.New(t)
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	db, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	// Expose the internal struct
	dbStruct, ok := db.(*merkleDB)
	require.True(ok)

	for i := 0; i < 100; i++ {
		lastReceivedKeyLen := r.Intn(16)
		lastReceivedKey := make([]byte, lastReceivedKeyLen)
		_, _ = r.Read(lastReceivedKey) // #nosec G404

		rangeEndLen := r.Intn(16)
		rangeEndBytes := make([]byte, rangeEndLen)
		_, _ = r.Read(rangeEndBytes) // #nosec G404

		rangeEnd := maybe.Nothing[[]byte]()
		if rangeEndLen > 0 {
			rangeEnd = maybe.Some(rangeEndBytes)
		}

		nextKey, err := dbStruct.findNextKey(
			lastReceivedKey,
			rangeEnd,
			nil, // endProof
		)
		require.NoError(err)
		require.Equal(maybe.Some(append(lastReceivedKey, 0)), nextKey)
	}
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

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := GenerateTrie(t, r, 500)
	require.NoError(err)

	db, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	// Expose the internal struct
	dbStruct, ok := db.(*merkleDB)
	require.True(ok)

	// Sync the DB
	fullProof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), fullProof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	proof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 100)
	require.NoError(err)
	lastKey := proof.KeyChanges[len(proof.KeyChanges)-1].Key

	// local db has a different child than remote db
	lastKey = append(lastKey, 16)
	require.NoError(db.Put(lastKey, []byte{1}))

	require.NoError(dbToSync.Put(lastKey, []byte{2}))

	proof, err = dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Some(proof.KeyChanges[len(proof.KeyChanges)-1].Key), 100)
	require.NoError(err)

	nextKey, err = dbStruct.findNextKey(proof.KeyChanges[len(proof.KeyChanges)-1].Key, maybe.Nothing[[]byte](), proof.EndProof)
	require.NoError(err)
	require.True(nextKey.HasValue())
	require.Equal(lastKey, nextKey.Value())
}

// Test findNextKey by computing the expected result in a naive, inefficient
// way and comparing it to the actual result
func TestFindNextKeyRandom(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404
	require := require.New(t)

	// Create a "remote" database and "local" database
	remoteDB, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	require.NoError(err)

	config := newTestConfig()
	localDB, err := New(
		context.Background(),
		memdb.New(),
		config,
	)
	require.NoError(err)

	// Expose the internal struct
	localDBStruct, ok := localDB.(*merkleDB)
	require.True(ok)

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
			context.Background(),
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
			context.Background(),
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
			key Key
			id  ids.ID
		}

		// Set of key prefix/ID pairs proven by the remote database's end proof.
		remoteKeyIDs := []keyAndID{}
		for _, node := range remoteProof.EndProof {
			for childIdx, childID := range node.Children {
				remoteKeyIDs = append(remoteKeyIDs, keyAndID{
					key: node.Key.Extend(ToToken(childIdx, BranchFactorToTokenSize[config.BranchFactor])),
					id:  childID,
				})
			}
		}

		// Set of key prefix/ID pairs proven by the local database's proof.
		localKeyIDs := []keyAndID{}
		for _, node := range localProof.Path {
			for childIdx, childID := range node.Children {
				localKeyIDs = append(localKeyIDs, keyAndID{
					key: node.Key.Extend(ToToken(childIdx, BranchFactorToTokenSize[config.BranchFactor])),
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
			smallestDiffKey Key
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

		gotFirstDiff, err := localDBStruct.findNextKey(
			lastReceivedKey,
			endKey,
			remoteProof.EndProof,
		)
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

// Find the point between the key given and a key of all 255's
func midpointWithEnd(start []byte) []byte {
	midpoint := make([]byte, len(start))

	for i := 0; i < len(start); i++ {
		intI := int(start[i])
		mid := (intI + 256) / 2
		midpoint[i] = byte(mid)
	}

	return midpoint
}
