// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ Client = &mockClient{}

func newNoopTracer() trace.Tracer {
	tracer, _ := trace.New(trace.Config{Enabled: false})
	return tracer
}

type mockClient struct {
	db *merkledb.Database
}

func (client *mockClient) GetChangeProof(ctx context.Context, request *syncpb.ChangeProofRequest, _ *merkledb.Database) (*merkledb.ChangeProof, error) {
	startRoot, err := ids.ToID(request.StartRoot)
	if err != nil {
		return nil, err
	}
	endRoot, err := ids.ToID(request.EndRoot)
	if err != nil {
		return nil, err
	}
	return client.db.GetChangeProof(ctx, startRoot, endRoot, request.Start, request.End, int(request.KeyLimit))
}

func (client *mockClient) GetRangeProof(ctx context.Context, request *syncpb.RangeProofRequest) (*merkledb.RangeProof, error) {
	root, err := ids.ToID(request.Root)
	if err != nil {
		return nil, err
	}
	return client.db.GetRangeProofAtRoot(ctx, root, request.Start, request.End, int(request.KeyLimit))
}

func Test_Creation(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 0,
			NodeCacheSize: 1000,
		},
	)
	require.NoError(err)

	syncer, err := NewStateSyncManager(StateSyncConfig{
		SyncDB:                db,
		Client:                &mockClient{},
		TargetRoot:            ids.Empty,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
	})
	require.NotNil(syncer)
	require.NoError(err)
}

func Test_Completion(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 10; i++ {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		emptyDB, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(err)
		emptyRoot, err := emptyDB.GetMerkleRoot(context.Background())
		require.NoError(err)
		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: emptyDB},
			TargetRoot:            emptyRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)
		require.NotNil(syncer)
		require.NoError(syncer.StartSyncing(context.Background()))
		require.NoError(syncer.Wait(context.Background()))
		require.NoError(err)
		syncer.workLock.Lock()
		require.Zero(syncer.unprocessedWork.Len())
		require.Equal(1, syncer.processedWork.Len())
		syncer.workLock.Unlock()
	}
}

func Test_Midpoint(t *testing.T) {
	require := require.New(t)

	mid := midPoint([]byte{1, 255}, []byte{2, 1})
	require.Equal([]byte{2, 0}, mid)

	mid = midPoint(nil, []byte{255, 255, 0})
	require.Equal([]byte{127, 255, 128}, mid)

	mid = midPoint([]byte{255, 255, 255}, []byte{255, 255})
	require.Equal([]byte{255, 255, 127, 128}, mid)

	mid = midPoint(nil, []byte{255})
	require.Equal([]byte{127, 127}, mid)

	mid = midPoint([]byte{1, 255}, []byte{255, 1})
	require.Equal([]byte{128, 128}, mid)

	mid = midPoint([]byte{140, 255}, []byte{141, 0})
	require.Equal([]byte{140, 255, 127}, mid)

	mid = midPoint([]byte{126, 255}, []byte{127})
	require.Equal([]byte{126, 255, 127}, mid)

	mid = midPoint(nil, nil)
	require.Equal([]byte{127}, mid)

	low := midPoint(nil, mid)
	require.Equal([]byte{63, 127}, low)

	high := midPoint(mid, nil)
	require.Equal([]byte{191}, high)

	mid = midPoint([]byte{255, 255}, nil)
	require.Equal([]byte{255, 255, 127, 127}, mid)

	mid = midPoint([]byte{255}, nil)
	require.Equal([]byte{255, 127, 127}, mid)

	for i := 0; i < 5000; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404

		start := make([]byte, r.Intn(99)+1)
		_, err := r.Read(start)
		require.NoError(err)

		end := make([]byte, r.Intn(99)+1)
		_, err = r.Read(end)
		require.NoError(err)

		for bytes.Equal(start, end) {
			_, err = r.Read(end)
			require.NoError(err)
		}

		if bytes.Compare(start, end) == 1 {
			start, end = end, start
		}

		mid = midPoint(start, end)
		require.Equal(-1, bytes.Compare(start, mid))
		require.Equal(-1, bytes.Compare(mid, end))
	}
}

func Test_Sync_FindNextKey_InSync(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 3; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 1000)
		require.NoError(err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)
		require.NotNil(syncer)

		require.NoError(syncer.StartSyncing(context.Background()))
		require.NoError(syncer.Wait(context.Background()))

		proof, err := dbToSync.GetRangeProof(context.Background(), nil, nil, 500)
		require.NoError(err)

		// the two dbs should be in sync, so next key should be nil
		lastKey := proof.KeyValues[len(proof.KeyValues)-1].Key
		nextKey, err := syncer.findNextKey(context.Background(), lastKey, nil, proof.EndProof)
		require.NoError(err)
		require.Nil(nextKey)

		// add an extra value to sync db past the last key returned
		newKey := midPoint(lastKey, nil)
		require.NoError(db.Put(newKey, []byte{1}))

		// create a range endpoint that is before the newly added key, but after the last key
		endPointBeforeNewKey := make([]byte, 0, 2)
		for i := 0; i < len(newKey); i++ {
			endPointBeforeNewKey = append(endPointBeforeNewKey, newKey[i])

			// we need the new key to be after the last key
			// don't subtract anything from the current byte if newkey and lastkey are equal
			if lastKey[i] == newKey[i] {
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

		nextKey, err = syncer.findNextKey(context.Background(), lastKey, endPointBeforeNewKey, proof.EndProof)
		require.NoError(err)

		// next key would be after the end of the range, so it returns nil instead
		require.Nil(nextKey)
	}
}

func Test_Sync_FindNextKey_Deleted(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 0,
			NodeCacheSize: 1000,
		},
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x10}, []byte{1}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{2}))

	syncRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	syncer, err := NewStateSyncManager(StateSyncConfig{
		SyncDB:                db,
		Client:                &mockClient{db: nil},
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
	})
	require.NoError(err)

	// 0x12 was "deleted" and there should be no extra node in the proof since there was nothing with a common prefix
	noExtraNodeProof, err := db.GetProof(context.Background(), []byte{0x12})
	require.NoError(err)

	// 0x11 was "deleted" and 0x11.0x11 should be in the exclusion proof
	extraNodeProof, err := db.GetProof(context.Background(), []byte{0x11})
	require.NoError(err)

	// there is now another value in the range that needs to be sync'ed
	require.NoError(db.Put([]byte{0x13}, []byte{3}))

	nextKey, err := syncer.findNextKey(context.Background(), []byte{0x12}, []byte{0x20}, noExtraNodeProof.Path)
	require.NoError(err)
	require.Equal([]byte{0x13}, nextKey)

	nextKey, err = syncer.findNextKey(context.Background(), []byte{0x11}, []byte{0x20}, extraNodeProof.Path)
	require.NoError(err)
	require.Equal([]byte{0x13}, nextKey)
}

func Test_Sync_FindNextKey_BranchInLocal(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 0,
			NodeCacheSize: 1000,
		},
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{2}))

	syncRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	proof, err := db.GetProof(context.Background(), []byte{0x11, 0x11})
	require.NoError(err)

	syncer, err := NewStateSyncManager(StateSyncConfig{
		SyncDB:                db,
		Client:                &mockClient{db: nil},
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
	})
	require.NoError(err)
	require.NoError(db.Put([]byte{0x12}, []byte{4}))

	nextKey, err := syncer.findNextKey(context.Background(), []byte{0x11, 0x11}, []byte{0x20}, proof.Path)
	require.NoError(err)
	require.Equal([]byte{0x12}, nextKey)
}

func Test_Sync_FindNextKey_BranchInReceived(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 0,
			NodeCacheSize: 1000,
		},
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x12}, []byte{2}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{3}))

	syncRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	proof, err := db.GetProof(context.Background(), []byte{0x11, 0x11})
	require.NoError(err)

	syncer, err := NewStateSyncManager(StateSyncConfig{
		SyncDB:                db,
		Client:                &mockClient{db: nil},
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
	})
	require.NoError(err)
	require.NoError(db.Delete([]byte{0x12}))

	nextKey, err := syncer.findNextKey(context.Background(), []byte{0x11, 0x11}, []byte{0x20}, proof.Path)
	require.NoError(err)
	require.Equal([]byte{0x12}, nextKey)
}

func Test_Sync_FindNextKey_ExtraValues(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 10; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 1000)
		require.NoError(err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)
		require.NotNil(syncer)

		require.NoError(syncer.StartSyncing(context.Background()))
		require.NoError(syncer.Wait(context.Background()))

		proof, err := dbToSync.GetRangeProof(context.Background(), nil, nil, 500)
		require.NoError(err)

		// add an extra value to local db
		lastKey := proof.KeyValues[len(proof.KeyValues)-1].Key
		midpoint := midPoint(lastKey, nil)

		require.NoError(db.Put(midpoint, []byte{1}))

		// next key at prefix of newly added point
		nextKey, err := syncer.findNextKey(context.Background(), lastKey, nil, proof.EndProof)
		require.NoError(err)
		require.NotNil(nextKey)

		require.True(isPrefix(midpoint, nextKey))

		require.NoError(db.Delete(midpoint))

		require.NoError(dbToSync.Put(midpoint, []byte{1}))

		proof, err = dbToSync.GetRangeProof(context.Background(), nil, lastKey, 500)
		require.NoError(err)

		// next key at prefix of newly added point
		nextKey, err = syncer.findNextKey(context.Background(), lastKey, nil, proof.EndProof)
		require.NoError(err)
		require.NotNil(nextKey)

		// deal with odd length key
		require.True(isPrefix(midpoint, nextKey))
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

	for i := 0; i < 10; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 500)
		require.NoError(err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)
		require.NotNil(syncer)
		require.NoError(syncer.StartSyncing(context.Background()))
		require.NoError(syncer.Wait(context.Background()))

		proof, err := dbToSync.GetRangeProof(context.Background(), nil, nil, 100)
		require.NoError(err)
		lastKey := proof.KeyValues[len(proof.KeyValues)-1].Key

		// local db has a different child than remote db
		lastKey = append(lastKey, 16)
		require.NoError(db.Put(lastKey, []byte{1}))

		require.NoError(dbToSync.Put(lastKey, []byte{2}))

		proof, err = dbToSync.GetRangeProof(context.Background(), nil, proof.KeyValues[len(proof.KeyValues)-1].Key, 100)
		require.NoError(err)

		nextKey, err := syncer.findNextKey(context.Background(), proof.KeyValues[len(proof.KeyValues)-1].Key, nil, proof.EndProof)
		require.NoError(err)
		require.Equal(nextKey, lastKey)
	}
}

// Test findNextKey by computing the expected result in a naive, inefficient
// way and comparing it to the actual result
func TestFindNextKeyRandom(t *testing.T) {
	rand := rand.New(rand.NewSource(1337)) //nolint:gosec
	require := require.New(t)

	// Create a "remote" database and "local" database
	remoteDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: defaultRequestKeyLimit,
			NodeCacheSize: defaultRequestKeyLimit,
		},
	)
	require.NoError(err)

	localDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: defaultRequestKeyLimit,
			NodeCacheSize: defaultRequestKeyLimit,
		},
	)
	require.NoError(err)

	var (
		numProofsToTest  = 500
		numKeyValues     = 500
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
		for rangeStart == nil || bytes.Compare(rangeStart, rangeEnd) == 1 {
			rangeStart = make([]byte, rand.Intn(maxRangeStartLen)+1)
			_, _ = rand.Read(rangeStart)
			rangeEnd = make([]byte, rand.Intn(maxRangeEndLen)+1)
			_, _ = rand.Read(rangeEnd)
		}

		remoteProof, err := remoteDB.GetRangeProof(
			context.Background(),
			rangeStart,
			rangeEnd,
			rand.Intn(maxProofLen)+1,
		)
		require.NoError(err)

		if len(remoteProof.KeyValues) == 0 {
			continue
		}
		lastReceivedKey := remoteProof.KeyValues[len(remoteProof.KeyValues)-1].Key

		// Commit the proof to the local database as we do
		// in the actual syncer.
		require.NoError(localDB.CommitRangeProof(
			context.Background(),
			rangeStart,
			remoteProof,
		))

		localProof, err := localDB.GetProof(
			context.Background(),
			lastReceivedKey,
		)
		require.NoError(err)

		type keyAndID struct {
			key merkledb.SerializedPath
			id  ids.ID
		}

		// Set of key prefix/ID pairs proven by the remote database's proof.
		remoteKeyIDs := []keyAndID{}

		for _, node := range remoteProof.EndProof {
			for childIdx, childID := range node.Children {
				remoteKeyIDs = append(remoteKeyIDs, keyAndID{
					key: node.KeyPath.AppendNibble(childIdx),
					id:  childID,
				})
			}
		}

		// Set of key prefix/ID pairs proven by the local database's proof.
		localKeyIDs := []keyAndID{}
		for _, node := range localProof.Path {
			for childIdx, childID := range node.Children {
				localKeyIDs = append(localKeyIDs, keyAndID{
					key: node.KeyPath.AppendNibble(childIdx),
					id:  childID,
				})
			}
		}

		// Sort in ascending order by key prefix.
		serializedPathLess := func(i, j keyAndID) bool {
			return bytes.Compare(i.key.Value, j.key.Value) < 0 ||
				(bytes.Equal(i.key.Value, j.key.Value) &&
					i.key.NibbleLength < j.key.NibbleLength)
		}
		slices.SortFunc(remoteKeyIDs, serializedPathLess)
		slices.SortFunc(localKeyIDs, serializedPathLess)

		// Filter out keys that are before the last received key
		findBounds := func(keyIDs []keyAndID) (int, int) {
			var (
				firstIdxInRange      = len(keyIDs)
				firstIdxInRangeFound = false
				firstIdxOutOfRange   = len(keyIDs)
			)
			for i, keyID := range keyIDs {
				if !firstIdxInRangeFound && bytes.Compare(keyID.key.Value, lastReceivedKey) >= 0 {
					firstIdxInRange = i
					firstIdxInRangeFound = true
					continue
				}
				if bytes.Compare(keyID.key.Value, rangeEnd) > 0 {
					firstIdxOutOfRange = i
					break
				}
			}
			return firstIdxInRange, firstIdxOutOfRange
		}

		remoteFirstIdxInRange, remoteFirstIdxOutOfRange := findBounds(remoteKeyIDs)
		remoteKeyIDs = remoteKeyIDs[remoteFirstIdxInRange:remoteFirstIdxOutOfRange]

		localFirstIdxInRange, localFirstIdxOutOfRange := findBounds(localKeyIDs)
		localKeyIDs = localKeyIDs[localFirstIdxInRange:localFirstIdxOutOfRange]

		// Find smallest difference between the set of key/ID pairs proven by
		// the remote/local proofs.
		var (
			smallestDiffKey merkledb.SerializedPath
			foundDiff       bool
		)
		for i := 0; i < len(remoteKeyIDs) && i < len(localKeyIDs); i++ {
			// See if the keys are different.
			smaller, bigger := remoteKeyIDs[i], localKeyIDs[i]
			if serializedPathLess(localKeyIDs[i], remoteKeyIDs[i]) {
				smaller, bigger = localKeyIDs[i], remoteKeyIDs[i]
			}

			if !smaller.key.Equal(bigger.key) {
				smallestDiffKey = smaller.key
				foundDiff = true
				break
			}
			// The keys are the same. See if the IDs are different.
			if smaller.id != bigger.id {
				smallestDiffKey = smaller.key // Keys are same so either is fine
				foundDiff = true
				break
			}
		}
		if !foundDiff {
			if len(remoteKeyIDs) < len(localKeyIDs) {
				smallestDiffKey = localKeyIDs[len(remoteKeyIDs)].key
			} else if len(remoteKeyIDs) > len(localKeyIDs) {
				smallestDiffKey = remoteKeyIDs[len(localKeyIDs)].key
			}
		}

		// Get the actual value from the syncer
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                localDB,
			Client:                &mockClient{db: nil},
			TargetRoot:            ids.GenerateTestID(),
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)

		gotFirstDiff, err := syncer.findNextKey(
			context.Background(),
			lastReceivedKey,
			rangeEnd,
			remoteProof.EndProof,
		)
		require.NoError(err)

		if bytes.Compare(smallestDiffKey.Value, rangeEnd) >= 0 {
			require.Nil(gotFirstDiff)
		} else {
			require.Equal(smallestDiffKey.Value, gotFirstDiff)
		}
	}
}

func Test_Sync_Result_Correct_Root(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 3; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 5000)
		require.NoError(err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)
		require.NotNil(syncer)
		require.NoError(syncer.StartSyncing(context.Background()))

		require.NoError(syncer.Wait(context.Background()))
		require.NoError(syncer.Error())

		// new db has fully sync'ed and should be at the same root as the original db
		newRoot, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)
		require.Equal(syncRoot, newRoot)

		// make sure they stay in sync
		for x := 0; x < 50; x++ {
			addkey := make([]byte, r.Intn(50))
			_, err = r.Read(addkey)
			require.NoError(err)
			val := make([]byte, r.Intn(50))
			_, err = r.Read(val)
			require.NoError(err)

			require.NoError(db.Put(addkey, val))

			require.NoError(dbToSync.Put(addkey, val))

			addNilkey := make([]byte, r.Intn(50))
			_, err = r.Read(addNilkey)
			require.NoError(err)
			require.NoError(db.Put(addNilkey, nil))

			require.NoError(dbToSync.Put(addNilkey, nil))

			deleteKeyStart := make([]byte, r.Intn(50))
			_, err = r.Read(deleteKeyStart)
			require.NoError(err)

			it := dbToSync.NewIteratorWithStart(deleteKeyStart)
			if it.Next() {
				require.NoError(dbToSync.Delete(it.Key()))
				require.NoError(db.Delete(it.Key()))
			}
			require.NoError(it.Error())
			it.Release()

			syncRoot, err = dbToSync.GetMerkleRoot(context.Background())
			require.NoError(err)

			newRoot, err = db.GetMerkleRoot(context.Background())
			require.NoError(err)
			require.Equal(syncRoot, newRoot)
		}
	}
}

func Test_Sync_Result_Correct_Root_With_Sync_Restart(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 5; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 5000)
		require.NoError(err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(err)

		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)
		require.NotNil(syncer)
		require.NoError(syncer.StartSyncing(context.Background()))

		// Wait until we've processed some work
		// before updating the sync target.
		require.Eventually(
			func() bool {
				syncer.workLock.Lock()
				defer syncer.workLock.Unlock()

				return syncer.processedWork.Len() > 0
			},
			5*time.Second,
			5*time.Millisecond,
		)
		syncer.Close()

		newSyncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)
		require.NotNil(newSyncer)
		require.NoError(newSyncer.StartSyncing(context.Background()))
		require.NoError(newSyncer.Error())
		require.NoError(newSyncer.Wait(context.Background()))
		require.NoError(err)
		newRoot, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)
		require.Equal(syncRoot, newRoot)
	}
}

func Test_Sync_Error_During_Sync(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	r := rand.New(rand.NewSource(int64(0))) // #nosec G404

	dbToSync, err := generateTrie(t, r, 100)
	require.NoError(err)

	syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
	require.NoError(err)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 0,
			NodeCacheSize: 1000,
		},
	)
	require.NoError(err)

	client := NewMockClient(ctrl)
	client.EXPECT().GetRangeProof(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request *syncpb.RangeProofRequest) (*merkledb.RangeProof, error) {
			return nil, errInvalidRangeProof
		},
	).AnyTimes()
	client.EXPECT().GetChangeProof(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request *syncpb.ChangeProofRequest, _ *merkledb.Database) (*merkledb.ChangeProof, error) {
			startRoot, err := ids.ToID(request.StartRoot)
			require.NoError(err)
			endRoot, err := ids.ToID(request.EndRoot)
			require.NoError(err)
			return dbToSync.GetChangeProof(ctx, startRoot, endRoot, request.Start, request.End, int(request.KeyLimit))
		},
	).AnyTimes()

	syncer, err := NewStateSyncManager(StateSyncConfig{
		SyncDB:                db,
		Client:                client,
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
	})
	require.NoError(err)
	require.NotNil(syncer)

	require.NoError(syncer.StartSyncing(context.Background()))

	require.ErrorIs(syncer.Wait(context.Background()), errInvalidRangeProof)
}

func Test_Sync_Result_Correct_Root_Update_Root_During(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for i := 0; i < 4; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404

		dbToSync, err := generateTrie(t, r, 10000)
		require.NoError(err)

		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(err)

		// Only let one response go through until we update the root.
		updatedRootChan := make(chan struct{}, 1)
		updatedRootChan <- struct{}{}
		client := NewMockClient(ctrl)
		client.EXPECT().GetRangeProof(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, request *syncpb.RangeProofRequest) (*merkledb.RangeProof, error) {
				<-updatedRootChan
				root, err := ids.ToID(request.Root)
				require.NoError(err)
				return dbToSync.GetRangeProofAtRoot(ctx, root, request.Start, request.End, int(request.KeyLimit))
			},
		).AnyTimes()
		client.EXPECT().GetChangeProof(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, request *syncpb.ChangeProofRequest, _ *merkledb.Database) (*merkledb.ChangeProof, error) {
				<-updatedRootChan
				startRoot, err := ids.ToID(request.StartRoot)
				require.NoError(err)
				endRoot, err := ids.ToID(request.EndRoot)
				require.NoError(err)
				return dbToSync.GetChangeProof(ctx, startRoot, endRoot, request.Start, request.End, int(request.KeyLimit))
			},
		).AnyTimes()

		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                client,
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(err)
		require.NotNil(syncer)
		for x := 0; x < 50; x++ {
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

		syncRoot, err = dbToSync.GetMerkleRoot(context.Background())
		require.NoError(err)

		require.NoError(syncer.StartSyncing(context.Background()))

		// Wait until we've processed some work
		// before updating the sync target.
		require.Eventually(
			func() bool {
				syncer.workLock.Lock()
				defer syncer.workLock.Unlock()

				return syncer.processedWork.Len() > 0
			},
			3*time.Second,
			10*time.Millisecond,
		)
		require.NoError(syncer.UpdateSyncTarget(syncRoot))
		close(updatedRootChan)

		require.NoError(syncer.Wait(context.Background()))
		require.NoError(syncer.Error())

		newRoot, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)
		require.Equal(syncRoot, newRoot)
	}
}

func Test_Sync_UpdateSyncTarget(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m, err := NewStateSyncManager(StateSyncConfig{
		SyncDB:                &merkledb.Database{}, // Not used
		Client:                NewMockClient(ctrl),  // Not used
		TargetRoot:            ids.Empty,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
	})
	require.NoError(err)

	// Populate [m.processWork] to ensure that UpdateSyncTarget
	// moves the work to [m.unprocessedWork].
	item := &syncWorkItem{
		start:       []byte{1},
		end:         []byte{2},
		LocalRootID: ids.GenerateTestID(),
	}
	m.processedWork.Insert(item)

	// Make sure that [m.unprocessedWorkCond] is signaled.
	gotSignalChan := make(chan struct{})
	// Don't UpdateSyncTarget until we're waiting for the signal.
	startedWaiting := make(chan struct{})
	go func() {
		m.workLock.Lock()
		defer m.workLock.Unlock()

		close(startedWaiting)
		m.unprocessedWorkCond.Wait()
		close(gotSignalChan)
	}()

	<-startedWaiting
	newSyncRoot := ids.GenerateTestID()
	require.NoError(m.UpdateSyncTarget(newSyncRoot))
	<-gotSignalChan

	require.Equal(newSyncRoot, m.config.TargetRoot)
	require.Zero(m.processedWork.Len())
	require.Equal(1, m.unprocessedWork.Len())
}

func generateTrie(t *testing.T, r *rand.Rand, count int) (*merkledb.Database, error) {
	db, _, err := generateTrieWithMinKeyLen(t, r, count, 0)
	return db, err
}

func generateTrieWithMinKeyLen(t *testing.T, r *rand.Rand, count int, minKeyLen int) (*merkledb.Database, [][]byte, error) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 1000,
			NodeCacheSize: 1000,
		},
	)
	if err != nil {
		return nil, nil, err
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
			return db, nil, err
		}
		i++
	}
	slices.SortFunc(allKeys, func(a, b []byte) bool {
		return bytes.Compare(a, b) < 0
	})
	return db, allKeys, batch.Write()
}
