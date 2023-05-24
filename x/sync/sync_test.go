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

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

var _ Client = &mockClient{}

func newNoopTracer() trace.Tracer {
	tracer, _ := trace.New(trace.Config{Enabled: false})
	return tracer
}

type mockClient struct {
	db *merkledb.Database
}

func (client *mockClient) GetChangeProof(ctx context.Context, request *ChangeProofRequest, _ *merkledb.Database) (*merkledb.ChangeProof, error) {
	return client.db.GetChangeProof(ctx, request.StartingRoot, request.EndingRoot, request.Start, request.End, int(request.Limit))
}

func (client *mockClient) GetRangeProof(ctx context.Context, request *RangeProofRequest) (*merkledb.RangeProof, error) {
	return client.db.GetRangeProofAtRoot(ctx, request.Root, request.Start, request.End, int(request.Limit))
}

func Test_Creation(t *testing.T) {
	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		merkledb.Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 0,
			NodeCacheSize: 1000,
		},
	)
	require.NoError(t, err)

	syncer, err := NewStateSyncManager(StateSyncConfig{
		SyncDB:                db,
		Client:                &mockClient{},
		TargetRoot:            ids.Empty,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
	})
	require.NotNil(t, syncer)
	require.NoError(t, err)
}

func Test_Completion(t *testing.T) {
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
		require.NoError(t, err)
		emptyRoot, err := emptyDB.GetMerkleRoot(context.Background())
		require.NoError(t, err)
		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(t, err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: emptyDB},
			TargetRoot:            emptyRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(t, err)
		require.NotNil(t, syncer)
		err = syncer.StartSyncing(context.Background())
		require.NoError(t, err)
		err = syncer.Wait(context.Background())
		require.NoError(t, err)
		syncer.workLock.Lock()
		require.Equal(t, 0, syncer.unprocessedWork.Len())
		require.Equal(t, 1, syncer.processedWork.Len())
		syncer.workLock.Unlock()
	}
}

func Test_Midpoint(t *testing.T) {
	mid := midPoint([]byte{1, 255}, []byte{2, 1})
	require.Equal(t, []byte{2, 0}, mid)

	mid = midPoint(nil, []byte{255, 255, 0})
	require.Equal(t, []byte{127, 255, 128}, mid)

	mid = midPoint([]byte{255, 255, 255}, []byte{255, 255})
	require.Equal(t, []byte{255, 255, 127, 128}, mid)

	mid = midPoint(nil, []byte{255})
	require.Equal(t, []byte{127, 127}, mid)

	mid = midPoint([]byte{1, 255}, []byte{255, 1})
	require.Equal(t, []byte{128, 128}, mid)

	mid = midPoint([]byte{140, 255}, []byte{141, 0})
	require.Equal(t, []byte{140, 255, 127}, mid)

	mid = midPoint([]byte{126, 255}, []byte{127})
	require.Equal(t, []byte{126, 255, 127}, mid)

	mid = midPoint(nil, nil)
	require.Equal(t, []byte{127}, mid)

	low := midPoint(nil, mid)
	require.Equal(t, []byte{63, 127}, low)

	high := midPoint(mid, nil)
	require.Equal(t, []byte{191}, high)

	mid = midPoint([]byte{255, 255}, nil)
	require.Equal(t, []byte{255, 255, 127, 127}, mid)

	mid = midPoint([]byte{255}, nil)
	require.Equal(t, []byte{255, 127, 127}, mid)

	for i := 0; i < 5000; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404

		start := make([]byte, r.Intn(99)+1)
		_, err := r.Read(start)
		require.NoError(t, err)

		end := make([]byte, r.Intn(99)+1)
		_, err = r.Read(end)
		require.NoError(t, err)

		for bytes.Equal(start, end) {
			_, err = r.Read(end)
			require.NoError(t, err)
		}

		if bytes.Compare(start, end) == 1 {
			start, end = end, start
		}

		mid = midPoint(start, end)
		require.Equal(t, -1, bytes.Compare(start, mid))
		require.Equal(t, -1, bytes.Compare(mid, end))
	}
}

func Test_Sync_FindNextKey_InSync(t *testing.T) {
	for i := 0; i < 3; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 1000)
		require.NoError(t, err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(t, err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(t, err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(t, err)
		require.NotNil(t, syncer)

		err = syncer.StartSyncing(context.Background())
		require.NoError(t, err)
		err = syncer.Wait(context.Background())
		require.NoError(t, err)

		proof, err := dbToSync.GetRangeProof(context.Background(), nil, nil, 500)
		require.NoError(t, err)

		// the two dbs should be in sync, so next key should be nil
		lastKey := proof.KeyValues[len(proof.KeyValues)-1].Key
		nextKey, err := syncer.findNextKey(context.Background(), lastKey, nil, proof.EndProof)
		require.NoError(t, err)
		require.Nil(t, nextKey)

		// add an extra value to sync db past the last key returned
		newKey := midPoint(lastKey, nil)
		err = db.Put(newKey, []byte{1})
		require.NoError(t, err)

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
		require.NoError(t, err)

		// next key would be after the end of the range, so it returns nil instead
		require.Nil(t, nextKey)
	}
}

func Test_Sync_FindNextKey_ExtraValues(t *testing.T) {
	for i := 0; i < 10; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 1000)
		require.NoError(t, err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(t, err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(t, err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(t, err)
		require.NotNil(t, syncer)

		err = syncer.StartSyncing(context.Background())
		require.NoError(t, err)
		err = syncer.Wait(context.Background())
		require.NoError(t, err)

		proof, err := dbToSync.GetRangeProof(context.Background(), nil, nil, 500)
		require.NoError(t, err)

		// add an extra value to local db
		lastKey := proof.KeyValues[len(proof.KeyValues)-1].Key
		midpoint := midPoint(lastKey, nil)

		err = db.Put(midpoint, []byte{1})
		require.NoError(t, err)

		// next key at prefix of newly added point
		nextKey, err := syncer.findNextKey(context.Background(), lastKey, nil, proof.EndProof)
		require.NoError(t, err)
		require.NotNil(t, nextKey)

		require.True(t, isPrefix(midpoint, nextKey))

		err = db.Delete(midpoint)
		require.NoError(t, err)

		err = dbToSync.Put(midpoint, []byte{1})
		require.NoError(t, err)

		proof, err = dbToSync.GetRangeProof(context.Background(), nil, lastKey, 500)
		require.NoError(t, err)

		// next key at prefix of newly added point
		nextKey, err = syncer.findNextKey(context.Background(), lastKey, nil, proof.EndProof)
		require.NoError(t, err)
		require.NotNil(t, nextKey)

		// deal with odd length key
		require.True(t, isPrefix(midpoint, nextKey))
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
	for i := 0; i < 10; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 500)
		require.NoError(t, err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(t, err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(t, err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(t, err)
		require.NotNil(t, syncer)
		err = syncer.StartSyncing(context.Background())
		require.NoError(t, err)
		err = syncer.Wait(context.Background())
		require.NoError(t, err)

		proof, err := dbToSync.GetRangeProof(context.Background(), nil, nil, 100)
		require.NoError(t, err)
		lastKey := proof.KeyValues[len(proof.KeyValues)-1].Key

		// local db has a different child than remote db
		lastKey = append(lastKey, 16)
		err = db.Put(lastKey, []byte{1})
		require.NoError(t, err)

		err = dbToSync.Put(lastKey, []byte{2})
		require.NoError(t, err)

		proof, err = dbToSync.GetRangeProof(context.Background(), nil, proof.KeyValues[len(proof.KeyValues)-1].Key, 100)
		require.NoError(t, err)

		nextKey, err := syncer.findNextKey(context.Background(), proof.KeyValues[len(proof.KeyValues)-1].Key, nil, proof.EndProof)
		require.NoError(t, err)
		require.Equal(t, nextKey, lastKey)
	}
}

func Test_Sync_Result_Correct_Root(t *testing.T) {
	for i := 0; i < 3; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 5000)
		require.NoError(t, err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(t, err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(t, err)
		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(t, err)
		require.NotNil(t, syncer)
		err = syncer.StartSyncing(context.Background())
		require.NoError(t, err)

		err = syncer.Wait(context.Background())
		require.NoError(t, err)
		require.NoError(t, syncer.Error())

		// new db has fully sync'ed and should be at the same root as the original db
		newRoot, err := db.GetMerkleRoot(context.Background())
		require.NoError(t, err)
		require.Equal(t, syncRoot, newRoot)

		// make sure they stay in sync
		for x := 0; x < 50; x++ {
			addkey := make([]byte, r.Intn(50))
			_, err = r.Read(addkey)
			require.NoError(t, err)
			val := make([]byte, r.Intn(50))
			_, err = r.Read(val)
			require.NoError(t, err)

			err = db.Put(addkey, val)
			require.NoError(t, err)

			err = dbToSync.Put(addkey, val)
			require.NoError(t, err)

			addNilkey := make([]byte, r.Intn(50))
			_, err = r.Read(addNilkey)
			require.NoError(t, err)
			err = db.Put(addNilkey, nil)
			require.NoError(t, err)

			err = dbToSync.Put(addNilkey, nil)
			require.NoError(t, err)

			deleteKeyStart := make([]byte, r.Intn(50))
			_, err = r.Read(deleteKeyStart)
			require.NoError(t, err)

			it := dbToSync.NewIteratorWithStart(deleteKeyStart)
			if it.Next() {
				err = dbToSync.Delete(it.Key())
				require.NoError(t, err)
				err = db.Delete(it.Key())
				require.NoError(t, err)
			}
			require.NoError(t, it.Error())
			it.Release()

			syncRoot, err = dbToSync.GetMerkleRoot(context.Background())
			require.NoError(t, err)

			newRoot, err = db.GetMerkleRoot(context.Background())
			require.NoError(t, err)
			require.Equal(t, syncRoot, newRoot)
		}
	}
}

func Test_Sync_Result_Correct_Root_With_Sync_Restart(t *testing.T) {
	for i := 0; i < 5; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		dbToSync, err := generateTrie(t, r, 5000)
		require.NoError(t, err)
		syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
		require.NoError(t, err)

		db, err := merkledb.New(
			context.Background(),
			memdb.New(),
			merkledb.Config{
				Tracer:        newNoopTracer(),
				HistoryLength: 0,
				NodeCacheSize: 1000,
			},
		)
		require.NoError(t, err)

		syncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(t, err)
		require.NotNil(t, syncer)
		err = syncer.StartSyncing(context.Background())
		require.NoError(t, err)

		time.Sleep(15 * time.Millisecond)
		syncer.Close()

		newSyncer, err := NewStateSyncManager(StateSyncConfig{
			SyncDB:                db,
			Client:                &mockClient{db: dbToSync},
			TargetRoot:            syncRoot,
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
		})
		require.NoError(t, err)
		require.NotNil(t, newSyncer)
		err = newSyncer.StartSyncing(context.Background())
		require.NoError(t, err)
		require.NoError(t, newSyncer.Error())
		err = newSyncer.Wait(context.Background())
		require.NoError(t, err)
		newRoot, err := db.GetMerkleRoot(context.Background())
		require.NoError(t, err)
		require.Equal(t, syncRoot, newRoot)
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
		func(ctx context.Context, request *RangeProofRequest) (*merkledb.RangeProof, error) {
			return nil, errInvalidRangeProof
		},
	).AnyTimes()
	client.EXPECT().GetChangeProof(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request *ChangeProofRequest, _ *merkledb.Database) (*merkledb.ChangeProof, error) {
			return dbToSync.GetChangeProof(ctx, request.StartingRoot, request.EndingRoot, request.Start, request.End, int(request.Limit))
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
	require.NotNil(t, syncer)

	err = syncer.StartSyncing(context.Background())
	require.NoError(err)

	err = syncer.Wait(context.Background())
	require.ErrorIs(err, errInvalidRangeProof)
}

func Test_Sync_Result_Correct_Root_Update_Root_During(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for i := 0; i < 5; i++ {
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
			func(ctx context.Context, request *RangeProofRequest) (*merkledb.RangeProof, error) {
				<-updatedRootChan
				return dbToSync.GetRangeProofAtRoot(ctx, request.Root, request.Start, request.End, int(request.Limit))
			},
		).AnyTimes()
		client.EXPECT().GetChangeProof(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, request *ChangeProofRequest, _ *merkledb.Database) (*merkledb.ChangeProof, error) {
				<-updatedRootChan
				return dbToSync.GetChangeProof(ctx, request.StartingRoot, request.EndingRoot, request.Start, request.End, int(request.Limit))
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
		require.NotNil(t, syncer)
		for x := 0; x < 50; x++ {
			key := make([]byte, r.Intn(50))
			_, err = r.Read(key)
			require.NoError(err)

			val := make([]byte, r.Intn(50))
			_, err = r.Read(val)
			require.NoError(err)

			err = dbToSync.Put(key, val)
			require.NoError(err)

			deleteKeyStart := make([]byte, r.Intn(50))
			_, err = r.Read(deleteKeyStart)
			require.NoError(err)

			it := dbToSync.NewIteratorWithStart(deleteKeyStart)
			if it.Next() {
				err = dbToSync.Delete(it.Key())
				require.NoError(err)
			}
			require.NoError(it.Error())
			it.Release()
		}

		syncRoot, err = dbToSync.GetMerkleRoot(context.Background())
		require.NoError(err)

		err = syncer.StartSyncing(context.Background())
		require.NoError(err)

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
		err = syncer.UpdateSyncTarget(syncRoot)
		require.NoError(err)
		close(updatedRootChan)

		err = syncer.Wait(context.Background())
		require.NoError(err)
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
	err = m.UpdateSyncTarget(newSyncRoot)
	require.NoError(err)
	<-gotSignalChan

	require.Equal(newSyncRoot, m.config.TargetRoot)
	require.Equal(0, m.processedWork.Len())
	require.Equal(1, m.unprocessedWork.Len())
}

func generateTrie(t *testing.T, r *rand.Rand, count int) (*merkledb.Database, error) {
	db, _, err := generateTrieWithMinKeyLen(t, r, count, 0)
	return db, err
}

func generateTrieWithMinKeyLen(t *testing.T, r *rand.Rand, count int, minKeyLen int) (*merkledb.Database, [][]byte, error) {
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
			require.NoError(t, err)
			return key
		}

		// new key
		key := make([]byte, r.Intn(50)+minKeyLen)
		_, err = r.Read(key)
		require.NoError(t, err)
		return key
	}

	for i := 0; i < count; {
		value := make([]byte, r.Intn(51))
		if len(value) == 0 {
			value = nil
		} else {
			_, err = r.Read(value)
			require.NoError(t, err)
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
