// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"math/rand"
	"slices"
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
)

var _ p2p.Handler = (*waitingHandler)(nil)

func Test_Creation(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(db)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(db)),
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)
}

func Test_Completion(t *testing.T) {
	require := require.New(t)

	emptyDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	emptyRoot, err := emptyDB.GetMerkleRoot(context.Background())
	require.NoError(err)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(emptyDB)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(emptyDB)),
		TargetRoot:            emptyRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)

	require.NoError(syncer.Start(context.Background()))
	require.NoError(syncer.Wait(context.Background()))

	syncer.workLock.Lock()
	require.Zero(syncer.unprocessedWork.Len())
	require.Equal(1, syncer.processedWork.Len())
	syncer.workLock.Unlock()
}

func Test_Midpoint(t *testing.T) {
	require := require.New(t)

	mid := midPoint(maybe.Some([]byte{1, 255}), maybe.Some([]byte{2, 1}))
	require.Equal(maybe.Some([]byte{2, 0}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Some([]byte{255, 255, 0}))
	require.Equal(maybe.Some([]byte{127, 255, 128}), mid)

	mid = midPoint(maybe.Some([]byte{255, 255, 255}), maybe.Some([]byte{255, 255}))
	require.Equal(maybe.Some([]byte{255, 255, 127, 128}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Some([]byte{255}))
	require.Equal(maybe.Some([]byte{127, 127}), mid)

	mid = midPoint(maybe.Some([]byte{1, 255}), maybe.Some([]byte{255, 1}))
	require.Equal(maybe.Some([]byte{128, 128}), mid)

	mid = midPoint(maybe.Some([]byte{140, 255}), maybe.Some([]byte{141, 0}))
	require.Equal(maybe.Some([]byte{140, 255, 127}), mid)

	mid = midPoint(maybe.Some([]byte{126, 255}), maybe.Some([]byte{127}))
	require.Equal(maybe.Some([]byte{126, 255, 127}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{127}), mid)

	low := midPoint(maybe.Nothing[[]byte](), mid)
	require.Equal(maybe.Some([]byte{63, 127}), low)

	high := midPoint(mid, maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{191}), high)

	mid = midPoint(maybe.Some([]byte{255, 255}), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{255, 255, 127, 127}), mid)

	mid = midPoint(maybe.Some([]byte{255}), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{255, 127, 127}), mid)

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

		mid = midPoint(maybe.Some(start), maybe.Some(end))
		require.Equal(-1, bytes.Compare(start, mid.Value()))
		require.Equal(-1, bytes.Compare(mid.Value(), end))
	}
}

func Test_Sync_FindNextKey_InSync(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 1000)
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
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(dbToSync)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(dbToSync)),
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)

	proofParser, ok := syncer.proofParser.(*proofParser)
	require.True(ok)

	require.NoError(syncer.Start(context.Background()))
	require.NoError(syncer.Wait(context.Background()))

	proof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)

	// the two dbs should be in sync, so next key should be nil
	lastKey := proof.KeyChanges[len(proof.KeyChanges)-1].Key
	nextKey, err := findNextKey(context.Background(), db, lastKey, maybe.Nothing[[]byte](), proof.EndProof, proofParser.tokenSize)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	// add an extra value to sync db past the last key returned
	newKey := midPoint(maybe.Some(lastKey), maybe.Nothing[[]byte]())
	newKeyVal := newKey.Value()
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

	nextKey, err = findNextKey(context.Background(), db, lastKey, maybe.Some(endPointBeforeNewKey), proof.EndProof, proofParser.tokenSize)
	require.NoError(err)

	// next key would be after the end of the range, so it returns Nothing instead
	require.True(nextKey.IsNothing())
}

func Test_Sync_FindNextKey_Deleted(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x10}, []byte{1}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{2}))

	syncRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(db)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(db)),
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	proofParser, ok := syncer.proofParser.(*proofParser)
	require.True(ok)

	// 0x12 was "deleted" and there should be no extra node in the proof since there was nothing with a common prefix
	noExtraNodeProof, err := db.GetProof(context.Background(), []byte{0x12})
	require.NoError(err)

	// 0x11 was "deleted" and 0x11.0x11 should be in the exclusion proof
	extraNodeProof, err := db.GetProof(context.Background(), []byte{0x11})
	require.NoError(err)

	// there is now another value in the range that needs to be sync'ed
	require.NoError(db.Put([]byte{0x13}, []byte{3}))

	nextKey, err := findNextKey(context.Background(), db, []byte{0x12}, maybe.Some([]byte{0x20}), noExtraNodeProof.Path, proofParser.tokenSize)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x13}), nextKey)

	nextKey, err = findNextKey(context.Background(), db, []byte{0x11}, maybe.Some([]byte{0x20}), extraNodeProof.Path, proofParser.tokenSize)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x13}), nextKey)
}

func Test_Sync_FindNextKey_BranchInLocal(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{2}))

	targetRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	proof, err := db.GetProof(context.Background(), []byte{0x11, 0x11})
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(db)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(db)),
		TargetRoot:            targetRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	proofParser, ok := syncer.proofParser.(*proofParser)
	require.True(ok)
	require.NoError(db.Put([]byte{0x11, 0x15}, []byte{4}))

	nextKey, err := findNextKey(context.Background(), db, []byte{0x11, 0x11}, maybe.Some([]byte{0x20}), proof.Path, proofParser.tokenSize)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x11, 0x15}), nextKey)
}

func Test_Sync_FindNextKey_BranchInReceived(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x12}, []byte{2}))
	require.NoError(db.Put([]byte{0x12, 0xA0}, []byte{4}))

	targetRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	proof, err := db.GetProof(context.Background(), []byte{0x12})
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(db)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(db)),
		TargetRoot:            targetRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)

	proofParser, ok := syncer.proofParser.(*proofParser)
	require.True(ok)

	require.NoError(db.Delete([]byte{0x12, 0xA0}))

	nextKey, err := findNextKey(context.Background(), db, []byte{0x12}, maybe.Some([]byte{0x20}), proof.Path, proofParser.tokenSize)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x12, 0xA0}), nextKey)
}

func Test_Sync_FindNextKey_ExtraValues(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 1000)
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
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(dbToSync)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(dbToSync)),
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)
	proofParser, ok := syncer.proofParser.(*proofParser)
	require.True(ok)

	require.NoError(syncer.Start(context.Background()))
	require.NoError(syncer.Wait(context.Background()))

	proof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)

	// add an extra value to local db
	lastKey := proof.KeyChanges[len(proof.KeyChanges)-1].Key
	midpoint := midPoint(maybe.Some(lastKey), maybe.Nothing[[]byte]())
	midPointVal := midpoint.Value()

	require.NoError(db.Put(midPointVal, []byte{1}))

	// next key at prefix of newly added point
	nextKey, err := findNextKey(context.Background(), db, lastKey, maybe.Nothing[[]byte](), proof.EndProof, proofParser.tokenSize)
	require.NoError(err)
	require.True(nextKey.HasValue())

	require.True(isPrefix(midPointVal, nextKey.Value()))

	require.NoError(db.Delete(midPointVal))

	require.NoError(dbToSync.Put(midPointVal, []byte{1}))

	proof, err = dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Some(lastKey), 500)
	require.NoError(err)

	// next key at prefix of newly added point
	nextKey, err = findNextKey(context.Background(), db, lastKey, maybe.Nothing[[]byte](), proof.EndProof, proofParser.tokenSize)
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

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(db)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(db)),
		TargetRoot:            ids.Empty,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)
	proofParser, ok := syncer.proofParser.(*proofParser)
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

		nextKey, err := findNextKey(
			context.Background(),
			db,
			lastReceivedKey,
			rangeEnd,
			nil, /* endProof */
			proofParser.tokenSize,
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
	dbToSync, err := generateTrie(t, r, 500)
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
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(dbToSync)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(dbToSync)),
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)
	proofParser, ok := syncer.proofParser.(*proofParser)
	require.True(ok)

	require.NoError(syncer.Start(context.Background()))
	require.NoError(syncer.Wait(context.Background()))

	proof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 100)
	require.NoError(err)
	lastKey := proof.KeyChanges[len(proof.KeyChanges)-1].Key

	// local db has a different child than remote db
	lastKey = append(lastKey, 16)
	require.NoError(db.Put(lastKey, []byte{1}))

	require.NoError(dbToSync.Put(lastKey, []byte{2}))

	proof, err = dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Some(proof.KeyChanges[len(proof.KeyChanges)-1].Key), 100)
	require.NoError(err)

	nextKey, err := findNextKey(context.Background(), db, proof.KeyChanges[len(proof.KeyChanges)-1].Key, maybe.Nothing[[]byte](), proof.EndProof, proofParser.tokenSize)
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
	remoteDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	config := newDefaultDBConfig()
	localDB, err := merkledb.New(
		context.Background(),
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
		require.NoError(localDB.CommitRangeProof(
			context.Background(),
			startKey,
			endKey,
			remoteProof,
		))

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
		ctx := context.Background()
		syncer, err := NewManager(ManagerConfig{
			DB:                    localDB,
			RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(remoteDB)),
			ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(remoteDB)),
			TargetRoot:            ids.GenerateTestID(),
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
			BranchFactor:          merkledb.BranchFactor16,
		}, prometheus.NewRegistry())
		require.NoError(err)
		require.NotNil(syncer)
		proofParser, ok := syncer.proofParser.(*proofParser)
		require.True(ok)

		gotFirstDiff, err := findNextKey(
			context.Background(),
			localDB,
			lastReceivedKey,
			endKey,
			remoteProof.EndProof,
			proofParser.tokenSize,
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
					Handler: NewGetRangeProofHandler(db),
					c:       &counter{m: 2},
				})
			},
		},
		{
			name: "change proof bad response - too many keys in response",
			changeProofClient: func(db merkledb.MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					response.KeyChanges = append(response.KeyChanges, make([]merkledb.KeyChange, defaultRequestKeyLimit)...)
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
					Handler: NewGetChangeProofHandler(db),
					c:       &counter{m: 2},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := context.Background()
			dbToSync, err := generateTrie(t, r, 3*maxKeyValuesLimit)
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

			rangeProofHandler := NewGetRangeProofHandler(dbToSync)
			rangeProofClient = p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, rangeProofHandler)
			if tt.rangeProofClient != nil {
				rangeProofClient = tt.rangeProofClient(dbToSync)
			}

			changeProofHandler := NewGetChangeProofHandler(dbToSync)
			changeProofClient = p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, changeProofHandler)
			if tt.changeProofClient != nil {
				changeProofClient = tt.changeProofClient(dbToSync)
			}

			syncer, err := NewManager(ManagerConfig{
				DB:                    db,
				RangeProofClient:      rangeProofClient,
				ChangeProofClient:     changeProofClient,
				TargetRoot:            syncRoot,
				SimultaneousWorkLimit: 5,
				Log:                   logging.NoLog{},
				BranchFactor:          merkledb.BranchFactor16,
			}, prometheus.NewRegistry())

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
	dbToSync, err := generateTrie(t, r, 3*maxKeyValuesLimit)
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
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(dbToSync)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(dbToSync)),
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)
	require.NoError(syncer.Start(context.Background()))

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

	newSyncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(dbToSync)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(dbToSync)),
		TargetRoot:            syncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
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

	dbToSync, err := generateTrie(t, r, 3*maxKeyValuesLimit)
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

	// Only let one response go through until we update the root.
	updatedRootChan := make(chan struct{}, 1)
	updatedRootChan <- struct{}{}

	ctx := context.Background()
	rangeProofClient := p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, &waitingHandler{
		handler:         NewGetRangeProofHandler(dbToSync),
		updatedRootChan: updatedRootChan,
	})

	changeProofClient := p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, &waitingHandler{
		handler:         NewGetChangeProofHandler(dbToSync),
		updatedRootChan: updatedRootChan,
	})

	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      rangeProofClient,
		ChangeProofClient:     changeProofClient,
		TargetRoot:            firstSyncRoot,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)

	require.NoError(syncer.Start(context.Background()))

	// Wait until we've processed some work
	// before updating the sync target.
	require.Eventually(
		func() bool {
			syncer.workLock.Lock()
			defer syncer.workLock.Unlock()

			return syncer.processedWork.Len() > 0
		},
		5*time.Second,
		10*time.Millisecond,
	)
	require.NoError(syncer.UpdateSyncTarget(secondSyncRoot))
	close(updatedRootChan)

	require.NoError(syncer.Wait(context.Background()))
	require.NoError(syncer.Error())

	newRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(secondSyncRoot, newRoot)
}

func Test_Sync_UpdateSyncTarget(t *testing.T) {
	require := require.New(t)

	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	ctx := context.Background()
	m, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(db)),
		ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(db)),
		TargetRoot:            ids.Empty,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)

	// Populate [m.processWork] to ensure that UpdateSyncTarget
	// moves the work to [m.unprocessedWork].
	item := &workItem{
		start:       maybe.Some([]byte{1}),
		end:         maybe.Some([]byte{2}),
		localRootID: ids.GenerateTestID(),
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
