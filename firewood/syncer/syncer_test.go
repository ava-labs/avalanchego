// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"errors"
	"math/rand"
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/x/sync"
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
			testSync(t, 0, tt.clientSize, tt.serverSize)
		})
	}
}

func testSync(t *testing.T, seed int64, clientKeys int, serverKeys int) {
	ctx := t.Context()

	serverDB := generateDB(t, serverKeys, seed)
	clientDB := generateDB(t, clientKeys, seed+1) // guarantee different data
	defer func() {
		require.NoError(t, serverDB.Close(ctx))
		require.NoError(t, clientDB.Close(ctx))
	}()

	root, err := serverDB.Root()
	require.NoError(t, err)

	syncer, err := New(
		Config{},
		clientDB,
		ids.ID(root),
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

// generateDB creates a new Firewood database with up to [numKeys] random key/value pairs.
// The function returned closes the database, waiting on the provided context, if there were no test failures.
// Note that each key/value pair may not be unique, so the resulting database may have fewer than [numKeys] entries.
func generateDB(t *testing.T, numKeys int, seed int64) *ffi.Database {
	t.Helper()
	db, err := ffi.New(t.TempDir(), ffi.EthereumNodeHashing)
	require.NoError(t, err)
	require.NotNil(t, db)

	if numKeys == 0 {
		return db
	}

	var (
		r         = rand.New(rand.NewSource(seed)) // #nosec G404
		ops       = make([]ffi.BatchOp, numKeys)
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

		ops = append(ops, ffi.Put(key, val))
	}

	_, err = db.Update(ops)
	require.NoError(t, err)

	return db
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
