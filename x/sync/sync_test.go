// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/ethsync"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

var _ p2p.Handler = (*waitingHandler)(nil)

func Test_Creation(t *testing.T) {
	require := require.New(t)

	db, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewClient(t, ctx, NewGetRangeProofHandler(logging.NoLog{}, db), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
		ChangeProofClient:     p2ptest.NewClient(t, ctx, NewGetChangeProofHandler(logging.NoLog{}, db), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)
}

func Test_Completion(t *testing.T) {
	require := require.New(t)

	emptyDB, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	emptyRoot, err := emptyDB.GetMerkleRoot(context.Background())
	require.NoError(err)

	db, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewClient(t, ctx, NewGetRangeProofHandler(logging.NoLog{}, emptyDB), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
		ChangeProofClient:     p2ptest.NewClient(t, ctx, NewGetChangeProofHandler(logging.NoLog{}, emptyDB), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
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

func TestFindNextKeyEmptyEndProof(t *testing.T) {
	require := require.New(t)
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	db, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewClient(t, ctx, NewGetRangeProofHandler(logging.NoLog{}, db), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
		ChangeProofClient:     p2ptest.NewClient(t, ctx, NewGetChangeProofHandler(logging.NoLog{}, db), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
		TargetRoot:            ids.Empty,
		SimultaneousWorkLimit: 5,
		Log:                   logging.NoLog{},
		BranchFactor:          merkledb.BranchFactor16,
	}, prometheus.NewRegistry())
	require.NoError(err)
	require.NotNil(syncer)

	for i := 0; i < 100; i++ {
		lastReceivedKeyLen := 32 // XXX: fix keylen
		lastReceivedKey := make([]byte, lastReceivedKeyLen)
		_, _ = r.Read(lastReceivedKey) // #nosec G404

		rangeEndLen := 32 // XXX: fix keylen
		rangeEndBytes := make([]byte, rangeEndLen)
		_, _ = r.Read(rangeEndBytes) // #nosec G404

		rangeEnd := maybe.Nothing[[]byte]()
		if rangeEndLen > 0 {
			rangeEnd = maybe.Some(rangeEndBytes)
		}

		nextKey, err := syncer.findNextKey(
			context.Background(),
			lastReceivedKey,
			rangeEnd,
			nil, /* endProof */
		)
		require.NoError(err)
		require.Less(bytes.Compare(lastReceivedKey, nextKey.Value()), 0)
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

// Tests that we are able to sync to the correct root while the server is
// updating
func Test_Sync_Result_Correct_Root(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	tests := []struct {
		name              string
		rangeProofClient  func(db DB) *p2p.Client
		changeProofClient func(db DB) *p2p.Client
	}{
		{
			name: "range proof bad response - too many leaves in response",
			rangeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.KeyValues = append(response.KeyValues, merkledb.KeyValue{})
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "range proof bad response - removed first key in response",
			rangeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.KeyValues = response.KeyValues[min(1, len(response.KeyValues)):]
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "range proof bad response - removed first key in response and replaced proof",
			rangeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.KeyValues = response.KeyValues[min(1, len(response.KeyValues)):]
					response.KeyValues = []merkledb.KeyValue{
						{
							Key:   []byte("01234567890123456789012345678901"),
							Value: []byte("bar"),
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

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "range proof bad response - removed key from middle of response",
			rangeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					i := rand.Intn(max(1, len(response.KeyValues)-1)) // #nosec G404
					_ = slices.Delete(response.KeyValues, i, min(len(response.KeyValues), i+1))
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "range proof bad response - start and end proof nodes removed",
			rangeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.StartProof = nil
					response.EndProof = nil
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "range proof bad response - end proof removed",
			rangeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.EndProof = nil
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "range proof bad response - empty proof",
			rangeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *merkledb.RangeProof) {
					response.StartProof = nil
					response.EndProof = nil
					response.KeyValues = nil
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "range proof server flake",
			rangeProofClient: func(db DB) *p2p.Client {
				return p2ptest.NewClient(t, context.Background(), &flakyHandler{
					Handler: NewGetRangeProofHandler(logging.NoLog{}, db),
					c:       &counter{m: 2},
				}, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "change proof bad response - too many keys in response",
			changeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					response.KeyChanges = append(response.KeyChanges, make([]merkledb.KeyChange, defaultRequestKeyLimit)...)
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "change proof bad response - removed first key in response",
			changeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					response.KeyChanges = response.KeyChanges[min(1, len(response.KeyChanges)):]
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "change proof bad response - removed key from middle of response",
			changeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					i := rand.Intn(max(1, len(response.KeyChanges)-1)) // #nosec G404
					_ = slices.Delete(response.KeyChanges, i, min(len(response.KeyChanges), i+1))
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "change proof bad response - all proof keys removed from response",
			changeProofClient: func(db DB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *merkledb.ChangeProof) {
					response.StartProof = nil
					response.EndProof = nil
				})

				return p2ptest.NewClient(t, context.Background(), handler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
		{
			name: "change proof flaky server",
			changeProofClient: func(db DB) *p2p.Client {
				return p2ptest.NewClient(t, context.Background(), &flakyHandler{
					Handler: NewGetChangeProofHandler(logging.NoLog{}, db),
					c:       &counter{m: 2},
				}, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := context.Background()
			dbToSync, err := ethsync.New(
				context.Background(),
				memdb.New(),
				newDefaultDBConfig(),
			)
			require.NoError(err)
			batch := dbToSync.NewBatch()
			err = generateWithKeyLenAndTrie(t, batch, r, 3*maxKeyValuesLimit, 32, 32)
			require.NoError(err)

			syncRoot, err := dbToSync.GetMerkleRoot(ctx)
			require.NoError(err)

			db, err := ethsync.New(
				ctx,
				memdb.New(),
				newDefaultDBConfig(),
			)
			require.NoError(err)

			var (
				rangeProofClient  *p2p.Client
				changeProofClient *p2p.Client
			)

			rangeProofHandler := NewGetRangeProofHandler(logging.NoLog{}, dbToSync)
			rangeProofClient = p2ptest.NewClient(t, ctx, rangeProofHandler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			if tt.rangeProofClient != nil {
				rangeProofClient = tt.rangeProofClient(dbToSync)
			}

			changeProofHandler := NewGetChangeProofHandler(logging.NoLog{}, dbToSync)
			changeProofClient = p2ptest.NewClient(t, ctx, changeProofHandler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
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
				addkey := make([]byte, 32) // XXX: fix keylen
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
	dbToSync, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	batch := dbToSync.NewBatch()
	err = generateWithKeyLenAndTrie(t, batch, r, 3*maxKeyValuesLimit, 32, 32)
	require.NoError(err)
	syncRoot, err := dbToSync.GetMerkleRoot(context.Background())
	require.NoError(err)

	db, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := context.Background()
	syncer, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewClient(t, ctx, NewGetRangeProofHandler(logging.NoLog{}, dbToSync), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
		ChangeProofClient:     p2ptest.NewClient(t, ctx, NewGetChangeProofHandler(logging.NoLog{}, dbToSync), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
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
		RangeProofClient:      p2ptest.NewClient(t, ctx, NewGetRangeProofHandler(logging.NoLog{}, dbToSync), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
		ChangeProofClient:     p2ptest.NewClient(t, ctx, NewGetChangeProofHandler(logging.NoLog{}, dbToSync), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
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
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	dbToSync, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	batch := dbToSync.NewBatch()
	err = generateWithKeyLenAndTrie(t, batch, r, 3*maxKeyValuesLimit, 32, 32)
	require.NoError(err)

	firstSyncRoot, err := dbToSync.GetMerkleRoot(context.Background())
	require.NoError(err)

	for x := 0; x < 100; x++ {
		key := make([]byte, 32) //  XXX: fix keylen
		_, err = r.Read(key)
		require.NoError(err)

		val := make([]byte, r.Intn(50))
		_, err = r.Read(val)
		require.NoError(err)

		require.NoError(dbToSync.Put(key, val))

		deleteKeyStart := make([]byte, 32) // XXX: fix keylen
		_, err = r.Read(deleteKeyStart)
		require.NoError(err)

		nextKey, ok := dbToSync.IterateOneKey(deleteKeyStart) // XXX
		if ok {
			require.NoError(dbToSync.Put(nextKey, nil))
		}
	}

	secondSyncRoot, err := dbToSync.GetMerkleRoot(context.Background())
	require.NoError(err)

	db, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	// Only let one response go through until we update the root.
	updatedRootChan := make(chan struct{}, 1)
	updatedRootChan <- struct{}{}

	ctx := context.Background()
	rangeProofClient := p2ptest.NewClient(t, ctx, &waitingHandler{
		handler:         NewGetRangeProofHandler(logging.NoLog{}, dbToSync),
		updatedRootChan: updatedRootChan,
	}, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())

	changeProofClient := p2ptest.NewClient(t, ctx, &waitingHandler{
		handler:         NewGetChangeProofHandler(logging.NoLog{}, dbToSync),
		updatedRootChan: updatedRootChan,
	}, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())

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

	db, err := ethsync.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	ctx := context.Background()
	m, err := NewManager(ManagerConfig{
		DB:                    db,
		RangeProofClient:      p2ptest.NewClient(t, ctx, NewGetRangeProofHandler(logging.NoLog{}, db), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
		ChangeProofClient:     p2ptest.NewClient(t, ctx, NewGetChangeProofHandler(logging.NoLog{}, db), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()),
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
	db, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	if err != nil {
		return nil, err
	}
	defaultMaxKeyLen := 50
	return db, generateWithKeyLenAndTrie(t, db.NewBatch(), r, count, minKeyLen, defaultMaxKeyLen)
}

type batch interface {
	Put(key, value []byte) error
	Write() error
}

func generateWithKeyLenAndTrie(t *testing.T, batch batch, r *rand.Rand, count, minKeyLen, maxKeyLen int) error {
	require := require.New(t)

	var (
		allKeys  [][]byte
		seenKeys = make(map[string]struct{})
	)
	genKey := func() []byte {
		// new prefixed key
		if len(allKeys) > 2 && r.Intn(25) < 10 && false { // XXX: Disabled for now
			prefix := allKeys[r.Intn(len(allKeys))]
			key := make([]byte, r.Intn(50)+len(prefix))
			copy(key, prefix)
			_, err := r.Read(key[len(prefix):])
			require.NoError(err)
			return key
		}

		// new key
		keyLenRange := maxKeyLen - minKeyLen
		key := make([]byte, r.Intn(keyLenRange+1)+minKeyLen)
		_, err := r.Read(key)
		require.NoError(err)
		return key
	}

	for i := 0; i < count; {
		value := make([]byte, r.Intn(51)+1)
		if len(value) == 0 {
			value = nil
		} else {
			_, err := r.Read(value)
			require.NoError(err)
		}
		key := genKey()
		if _, seen := seenKeys[string(key)]; seen {
			continue // avoid duplicate keys so we always get the count
		}
		allKeys = append(allKeys, key)
		seenKeys[string(key)] = struct{}{}
		if err := batch.Put(key, value); err != nil {
			return err
		}
		i++
	}
	return batch.Write()
}
