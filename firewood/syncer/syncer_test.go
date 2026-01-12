// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/x/sync"
	"github.com/ava-labs/avalanchego/x/sync/synctest"
)

func Test_Firewood_Sync(t *testing.T) {
	tests := []struct {
		name       string
		clientSize int
		serverSize int
	}{
		{
			name:       "both empty",
			clientSize: 0,
			serverSize: 0,
		},
		{
			name:       "one request from empty",
			clientSize: 0,
			serverSize: 1000,
		},
		{
			name:       "server empty",
			clientSize: 1000,
			serverSize: 0,
		},
		{
			name:       "one request replace all",
			clientSize: 1000,
			serverSize: 1000,
		},
		{
			name:       "10,000 keys from empty",
			clientSize: 0,
			serverSize: 10_000,
		},
		{
			name:       "100,000 keys from empty",
			clientSize: 0,
			serverSize: 100_000,
		},
		{
			name:       "10,000 keys replace all",
			clientSize: 10_000,
			serverSize: 10_000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSync(t, tt.clientSize, tt.serverSize)
		})
	}
}

func testSync(t *testing.T, clientKeys int, serverKeys int) {
	ctx := t.Context()
	r := rand.New(rand.NewSource(1))

	serverDB, root := generateDB(t, r, serverKeys)
	clientDB, _ := generateDB(t, r, clientKeys)
	defer func() {
		require.NoError(t, serverDB.Close(ctx))
		require.NoError(t, clientDB.Close(ctx))
	}()

	syncer, err := New(
		Config{},
		clientDB,
		root,
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetRangeProofHandler(serverDB)),
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(serverDB)),
	)
	require.NoError(t, err)
	require.NotNil(t, syncer)

	require.NoError(t, syncer.Start(ctx))
	err = syncer.Wait(ctx)
	if errors.Is(err, sync.ErrFinishedWithUnexpectedRoot) {
		t.Log("syncer reported root mismatch; logging diff between DBs")
		logDiff(t, serverDB, clientDB)
	}
	require.NoError(t, err)
}

func Test_Firewood_Sync_WithUpdate(t *testing.T) {
	tests := []struct {
		name                    string
		clientSize              int
		serverSize              int
		numRequestsBeforeUpdate int
	}{
		{
			name:                    "finish with stale root",
			clientSize:              0,
			serverSize:              1000, // one request
			numRequestsBeforeUpdate: 0,
		},
		{
			name:                    "replace all with stale root",
			clientSize:              1000,
			serverSize:              1000, // one request
			numRequestsBeforeUpdate: 0,
		},
		{
			name:                    "partial sync then update",
			clientSize:              0,
			serverSize:              10_000, // multiple requests
			numRequestsBeforeUpdate: 1,
		},
		{
			name:                    "partial sync with replace then update",
			clientSize:              5_000,
			serverSize:              10_000, // multiple requests
			numRequestsBeforeUpdate: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSyncWithUpdate(t, tt.clientSize, tt.serverSize, tt.numRequestsBeforeUpdate)
		})
	}
}

func testSyncWithUpdate(t *testing.T, clientKeys int, serverKeys int, numRequestsBeforeUpdate int) {
	r := rand.New(rand.NewSource(1))

	serverDB, root := generateDB(t, r, serverKeys)
	clientDB, _ := generateDB(t, r, clientKeys)
	defer func() {
		require.NoError(t, serverDB.Close(t.Context()))
		require.NoError(t, clientDB.Close(t.Context()))
	}()
	newRoot := fillDB(t, r, serverDB, serverKeys)

	intercept := &p2p.TestHandler{}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	syncer, err := New(
		Config{},
		clientDB,
		root,
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, intercept),
		p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, NewGetChangeProofHandler(serverDB)),
	)
	require.NoError(t, err)
	require.NotNil(t, syncer)

	synctest.AddFuncOnIntercept(intercept, NewGetRangeProofHandler(serverDB), func() {
		// Called in separate goroutine, allow graceful cancellation
		if !assert.NoError(t, syncer.UpdateSyncTarget(newRoot)) {
			cancel()
		}
	}, numRequestsBeforeUpdate)

	require.NoError(t, syncer.Start(ctx))

	err = syncer.Wait(ctx)
	finalRoot, rootErr := clientDB.Root()
	require.NoError(t, rootErr)
	if errors.Is(err, sync.ErrFinishedWithUnexpectedRoot) {
		t.Log("syncer reported root mismatch; logging diff between DBs")
		logDiff(t, serverDB, clientDB)
	}
	require.NoError(t, err)
	require.Equal(t, newRoot, ids.ID(finalRoot))
}

// generateDB creates a new Firewood database with up to [numKeys] random key/value pairs.
// The database should be closed by the caller.
// Note that each key/value pair may not be unique, so the resulting database may have fewer than [numKeys] entries.
// Returns the database and its resulting root.
func generateDB(t *testing.T, r *rand.Rand, numKeys int) (*ffi.Database, ids.ID) {
	t.Helper()
	db, err := ffi.New(t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, db)

	root := fillDB(t, r, db, numKeys)
	return db, root
}

// fillDB adds up to [numKeys] random key/value pairs to [db].
// Returns the resulting root of the database.
// Note that each key/value pair may not be unique, so the resulting database may have fewer than [numKeys] entries.
func fillDB(t *testing.T, r *rand.Rand, db *ffi.Database, numKeys int) ids.ID {
	if numKeys == 0 {
		root, err := db.Root()
		require.NoError(t, err)
		return ids.ID(root)
	}

	var (
		keys      = make([][]byte, numKeys)
		vals      = make([][]byte, numKeys)
		minLength = 1
		maxLength = 64
	)
	for range numKeys {
		// Random length between minLength and maxLength inclusive
		keyLen := r.Intn(maxLength-minLength+1) + minLength
		valLen := r.Intn(maxLength-minLength+1) + minLength

		key := make([]byte, keyLen)
		val := make([]byte, valLen)

		_, err := r.Read(key)
		require.NoError(t, err, "read never errors")
		_, err = r.Read(val)
		require.NoError(t, err, "read never errors")

		keys = append(keys, key)
		vals = append(vals, val)
	}

	root, err := db.Update(keys, vals)
	require.NoError(t, err)

	return ids.ID(root)
}

// logDiff logs the differences between two Firewood databases.
// Useful when debugging state mismatches after sync.
func logDiff(t *testing.T, wantDB, gotDB *ffi.Database) {
	t.Helper()

	wantRev, err := wantDB.LatestRevision()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, wantRev.Drop())
	}()
	gotRev, err := gotDB.LatestRevision()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, gotRev.Drop())
	}()

	if wantRev.Root() == gotRev.Root() {
		t.Log("both DBs have the same root:", wantRev.Root())
		return
	}

	wantIter, err := wantRev.Iter(nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, wantIter.Drop())
	}()
	gotIter, err := gotRev.Iter(nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, gotIter.Drop())
	}()

	var (
		wantNext     = wantIter.Next()
		gotNext      = gotIter.Next()
		wantCount    int
		gotCount     int
		cmp          int
		prevKey      []byte
		missingCount int
	)
	for wantNext && gotNext {
		wantKey := wantIter.Key()
		gotKey := gotIter.Key()
		prevCmp := cmp
		cmp = bytes.Compare(wantKey, gotKey)
		// Log any missing keys before this key
		if cmp != prevCmp && missingCount > 0 {
			missingDB := "want db"
			if prevCmp == -1 {
				missingDB = "got db"
			}
			t.Logf("%d key(s) missing from %s in [%x, %x)", missingCount, missingDB, prevKey, wantKey)
			missingCount = 0
		}
		switch cmp {
		case 0:
			wantVal := wantIter.Value()
			gotVal := gotIter.Value()
			if !bytes.Equal(wantVal, gotVal) {
				t.Logf("key %x has different values:\n want db: %x\n got db: %x", wantKey, wantVal, gotVal)
			}

			wantNext = wantIter.Next()
			gotNext = gotIter.Next()
			wantCount++
			gotCount++

		case -1:
			wantNext = wantIter.Next()
			wantCount++
			if missingCount == 0 {
				prevKey = wantKey
			}
			missingCount++
		case 1:
			gotNext = gotIter.Next()
			gotCount++
			if missingCount == 0 {
				prevKey = gotKey
			}
			missingCount++
		}
	}
	if missingCount > 0 {
		t.Logf("%d final key(s) mismatched, starting at %x", missingCount, prevKey)
	}

	missingCount = 0
	for wantNext {
		if missingCount == 0 {
			prevKey = wantIter.Key()
		}
		wantNext = wantIter.Next()
		wantCount++
		missingCount++
	}
	if missingCount > 0 {
		t.Logf("%d keys missing from got db starting with %x", missingCount, prevKey)
	}

	missingCount = 0
	for gotNext {
		if missingCount == 0 {
			prevKey = gotIter.Key()
		}
		gotNext = gotIter.Next()
		gotCount++
		missingCount++
	}
	if missingCount > 0 {
		t.Logf("%d keys missing from want db starting with %x", missingCount, prevKey)
	}

	t.Logf("want db had %d keys, got db had %d keys", wantCount, gotCount)
	assert.NoError(t, wantIter.Err())
	assert.NoError(t, gotIter.Err())
}
