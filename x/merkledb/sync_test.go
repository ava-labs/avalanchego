// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/merkle/sync"
	"github.com/ava-labs/avalanchego/database/merkle/sync/synctest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var (
	rangeProofMarshaler  = RangeProofMarshaler{}
	changeProofMarshaler = ChangeProofMarshaler{}
)

func Test_Creation(t *testing.T) {
	require := require.New(t)

	db, err := New(
		t.Context(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx := t.Context()
	syncer, err := sync.NewSyncer(
		db,
		ids.Empty,
		sync.Config{},
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetRangeProofHandler(db, rangeProofMarshaler)),
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetChangeProofHandler(db, rangeProofMarshaler, changeProofMarshaler)),
		rangeProofMarshaler,
		changeProofMarshaler,
	)
	require.NoError(err)
	require.NotNil(syncer)
	require.NoError(syncer.Sync(t.Context()))
}

// Tests that we are able to sync to the correct root while the server is
// updating
func Test_Sync_Result_Correct_Root(t *testing.T) {
	t.Skip("FLAKY: panic: test timed out after 2m0s")

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now))

	tests := []struct {
		name              string
		db                MerkleDB
		rangeProofClient  func(db MerkleDB) *p2p.Client
		changeProofClient func(db MerkleDB) *p2p.Client
	}{
		{
			name: "range proof bad response - too many leaves in response",
			rangeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *RangeProof) {
					response.KeyChanges = append(response.KeyChanges, KeyChange{})
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - removed first key in response",
			rangeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *RangeProof) {
					response.KeyChanges = response.KeyChanges[min(1, len(response.KeyChanges)):]
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - removed first key in response and replaced proof",
			rangeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *RangeProof) {
					response.KeyChanges = response.KeyChanges[min(1, len(response.KeyChanges)):]
					response.KeyChanges = []KeyChange{
						{
							Key:   []byte("foo"),
							Value: maybe.Some([]byte("bar")),
						},
					}
					response.StartProof = []ProofNode{
						{
							Key: Key{},
						},
					}
					response.EndProof = []ProofNode{
						{
							Key: Key{},
						},
					}
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - removed key from middle of response",
			rangeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *RangeProof) {
					i := rand.Intn(max(1, len(response.KeyChanges)-1))
					_ = slices.Delete(response.KeyChanges, i, min(len(response.KeyChanges), i+1))
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - start and end proof nodes removed",
			rangeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *RangeProof) {
					response.StartProof = nil
					response.EndProof = nil
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - end proof removed",
			rangeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *RangeProof) {
					response.EndProof = nil
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof bad response - empty proof",
			rangeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyRangeProofHandler(t, db, func(response *RangeProof) {
					response.StartProof = nil
					response.EndProof = nil
					response.KeyChanges = nil
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "range proof server flake",
			rangeProofClient: func(db MerkleDB) *p2p.Client {
				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, &flakyHandler{
					Handler: sync.NewGetRangeProofHandler(db, rangeProofMarshaler),
					c:       &counter{m: 2},
				})
			},
		},
		{
			name: "change proof bad response - too many keys in response",
			changeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *ChangeProof) {
					response.KeyChanges = append(response.KeyChanges, make([]KeyChange, sync.DefaultRequestKeyLimit)...)
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "change proof bad response - removed first key in response",
			changeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *ChangeProof) {
					response.KeyChanges = response.KeyChanges[min(1, len(response.KeyChanges)):]
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "change proof bad response - removed key from middle of response",
			changeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *ChangeProof) {
					i := rand.Intn(max(1, len(response.KeyChanges)-1))
					_ = slices.Delete(response.KeyChanges, i, min(len(response.KeyChanges), i+1))
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "change proof bad response - all proof keys removed from response",
			changeProofClient: func(db MerkleDB) *p2p.Client {
				handler := newFlakyChangeProofHandler(t, db, func(response *ChangeProof) {
					response.StartProof = nil
					response.EndProof = nil
				})

				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, handler)
			},
		},
		{
			name: "change proof flaky server",
			changeProofClient: func(db MerkleDB) *p2p.Client {
				return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, &flakyHandler{
					Handler: sync.NewGetChangeProofHandler(db, rangeProofMarshaler, changeProofMarshaler),
					c:       &counter{m: 2},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := t.Context()
			dbToSync, err := generateTrie(t, r, 3*sync.MaxKeyValuesLimit)
			require.NoError(err)

			syncRoot, err := dbToSync.GetMerkleRoot(ctx)
			require.NoError(err)

			db, err := New(
				ctx,
				memdb.New(),
				newDefaultDBConfig(),
			)
			require.NoError(err)

			var (
				rangeProofClient  *p2p.Client
				changeProofClient *p2p.Client
			)

			rangeProofHandler := sync.NewGetRangeProofHandler(dbToSync, rangeProofMarshaler)
			rangeProofClient = p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, rangeProofHandler)
			if tt.rangeProofClient != nil {
				rangeProofClient = tt.rangeProofClient(dbToSync)
			}

			changeProofHandler := sync.NewGetChangeProofHandler(dbToSync, rangeProofMarshaler, changeProofMarshaler)
			changeProofClient = p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, changeProofHandler)
			if tt.changeProofClient != nil {
				changeProofClient = tt.changeProofClient(dbToSync)
			}

			syncer, err := sync.NewSyncer(
				db,
				syncRoot,
				sync.Config{},
				rangeProofClient,
				changeProofClient,
				rangeProofMarshaler,
				changeProofMarshaler,
			)

			require.NoError(err)
			require.NotNil(syncer)

			// Start syncing from the server
			var eg errgroup.Group
			eg.Go(func() error {
				return syncer.Sync(ctx)
			})

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
			require.NoErrorf(eg.Wait(), "%T.Sync()", syncer)

			// We should have the same resulting root as the server
			wantRoot, err := dbToSync.GetMerkleRoot(ctx)
			require.NoError(err)

			gotRoot, err := db.GetMerkleRoot(ctx)
			require.NoError(err)
			require.Equal(wantRoot, gotRoot)
		})
	}
}

func Test_Sync_Result_Correct_Root_With_Sync_Restart(t *testing.T) {
	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now))
	dbToSync, err := generateTrie(t, r, 3*sync.MaxKeyValuesLimit)
	require.NoError(err)
	syncRoot, err := dbToSync.GetMerkleRoot(t.Context())
	require.NoError(err)

	db, err := New(
		t.Context(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	syncer, err := sync.NewSyncer(
		db,
		syncRoot,
		sync.Config{},
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetRangeProofHandler(dbToSync, rangeProofMarshaler)),
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetChangeProofHandler(dbToSync, rangeProofMarshaler, changeProofMarshaler)),
		rangeProofMarshaler,
		changeProofMarshaler,
	)
	require.NoError(err)
	require.NotNil(syncer)

	// Start syncing from the server, will be cancelled by the range proof handler
	var eg errgroup.Group
	eg.Go(func() error {
		return syncer.Sync(ctx)
	})

	// Wait until we've processed some work before closing
	require.Eventually(func() bool {
		return db.NewIterator().Next()
	}, 5*time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIsf(eg.Wait(), context.Canceled, "%T.Sync()", syncer)

	ctx = t.Context()
	newSyncer, err := sync.NewSyncer(
		db,
		syncRoot,
		sync.Config{},
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetRangeProofHandler(dbToSync, rangeProofMarshaler)),
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetChangeProofHandler(dbToSync, rangeProofMarshaler, changeProofMarshaler)),
		rangeProofMarshaler,
		changeProofMarshaler,
	)
	require.NoError(err)
	require.NotNil(newSyncer)

	require.NoError(newSyncer.Sync(ctx))

	newRoot, err := db.GetMerkleRoot(ctx)
	require.NoError(err)
	require.Equal(syncRoot, newRoot)
}

func Test_Sync_Result_Correct_Root_Update_Root_During(t *testing.T) {
	t.Skip("FLAKY")

	require := require.New(t)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now))

	dbToSync, err := generateTrie(t, r, 3*sync.MaxKeyValuesLimit)
	require.NoError(err)

	firstSyncRoot, err := dbToSync.GetMerkleRoot(t.Context())
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

	secondSyncRoot, err := dbToSync.GetMerkleRoot(t.Context())
	require.NoError(err)

	db, err := New(
		t.Context(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	var syncer *sync.Syncer[*RangeProof, *ChangeProof]
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)

	// Allow 1 request to go through before blocking
	actionHandler := synctest.NewCounterHandler(sync.NewGetRangeProofHandler(dbToSync, rangeProofMarshaler), func() {
		err := syncer.UpdateSyncTarget(secondSyncRoot)
		if err != nil {
			cancel(err)
		}
	}, 1)

	syncer, err = sync.NewSyncer(
		db,
		firstSyncRoot,
		sync.Config{},
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, actionHandler),
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetChangeProofHandler(dbToSync, rangeProofMarshaler, changeProofMarshaler)),
		rangeProofMarshaler,
		changeProofMarshaler,
	)
	require.NoError(err)
	require.NotNil(syncer)

	require.NoError(syncer.Sync(ctx))

	newRoot, err := db.GetMerkleRoot(ctx)
	require.NoError(err)
	require.Equal(secondSyncRoot, newRoot)
}

func Test_Sync_UpdateSyncTarget(t *testing.T) {
	require := require.New(t)
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now))

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

	db, err := New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	var syncer *sync.Syncer[*RangeProof, *ChangeProof]
	actionHandler := synctest.NewCounterHandler(sync.NewGetRangeProofHandler(dbToSync, rangeProofMarshaler), func() {
		err := syncer.UpdateSyncTarget(root1)
		if err != nil {
			cancel(err)
		}
	}, 0)
	syncer, err = sync.NewSyncer(
		db,
		root1,
		sync.Config{},
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, actionHandler),
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetChangeProofHandler(dbToSync, rangeProofMarshaler, changeProofMarshaler)),
		rangeProofMarshaler,
		changeProofMarshaler,
	)
	require.NoError(err)

	require.NoError(syncer.Sync(ctx))
}

func generateTrie(t *testing.T, r *rand.Rand, count int) (MerkleDB, error) {
	return generateTrieWithMinKeyLen(t, r, count, 0)
}

func generateTrieWithMinKeyLen(t *testing.T, r *rand.Rand, count int, minKeyLen int) (MerkleDB, error) {
	require := require.New(t)

	db, err := New(
		t.Context(),
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
