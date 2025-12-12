// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/sync"
)

func Test_Firewood_Sync(t *testing.T) {
	for _, serverSize := range []int{0, 1, 1_000, 10_000, 100_000} {
		for _, clientSize := range []int{0, 1_000} {
			t.Run(fmt.Sprintf("numKeys=%d_clientKeys=%d", serverSize, clientSize), func(t *testing.T) {
				t.Parallel()
				testSync(t, 0, clientSize, serverSize)
			})
		}
	}
}

func testSync(t *testing.T, seed int64, clientKeys int, serverKeys int) {
	serverDB := &db{db: generateDB(t, serverKeys, seed)}
	clientDB := generateDB(t, clientKeys, seed+1) // guarantee different data

	ctx := t.Context()
	root, err := serverDB.GetMerkleRoot(ctx)
	require.NoError(t, err)

	syncer, err := NewSyncer(
		clientDB,
		Config{
			RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetRangeProofHandler(serverDB, rangeProofMarshaler{})),
			ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, sync.NewGetChangeProofHandler(serverDB, rangeProofMarshaler{}, changeProofMarshaler{})),
			SimultaneousWorkLimit: 5,
			Log:                   logging.NoLog{},
			TargetRoot:            root,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	require.NotNil(t, syncer)

	require.NoError(t, syncer.Start(ctx))
	err = syncer.Wait(ctx)
	if errors.Is(err, sync.ErrFinishedWithUnexpectedRoot) {
		t.Log("syncer reported root mismatch; logging diff between DBs")
		logDiff(t, serverDB.db, clientDB)
	}
	require.NoError(t, err)
}

// generateDB creates a new Firewood database with up to [numKeys] random key/value pairs.
// The database will be automatically closed when the test ends.
// Note that each key/value pair may not be unique, so the resulting database may have fewer than [numKeys] entries.
func generateDB(t *testing.T, numKeys int, seed int64) *ffi.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "firewood.db")

	db, err := ffi.New(path, ffi.DefaultConfig())
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second) // allow some time for garbage collection
		defer cancel()
		require.NoError(t, db.Close(ctx))
	})

	if numKeys == 0 {
		return db
	}

	var (
		r         = rand.New(rand.NewSource(seed)) // #nosec G404
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

	_, err = db.Update(keys, vals)
	require.NoError(t, err)

	return db
}

// logDiff logs the differences between two Firewood databases.
// Useful when debugging state mismatches after sync.
func logDiff(t *testing.T, db1, db2 *ffi.Database) {
	t.Helper()

	rev1, err := db1.LatestRevision()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rev1.Drop())
	}()
	rev2, err := db2.LatestRevision()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rev2.Drop())
	}()

	if rev1.Root() == rev2.Root() {
		t.Log("both DBs have the same root:", rev1.Root())
		return
	}

	iter1, err := rev1.Iter(nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, iter1.Drop())
	}()
	iter2, err := rev2.Iter(nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, iter2.Drop())
	}()

	var (
		next1        = iter1.Next()
		next2        = iter2.Next()
		count1       int
		count2       int
		cmp          int
		prevKey      []byte
		missingCount int
	)
	for next1 && next2 {
		key1 := iter1.Key()
		key2 := iter2.Key()
		prevCmp := cmp
		cmp = bytes.Compare(key1, key2)
		// Log any missing keys before this key
		if cmp != prevCmp && missingCount > 0 {
			missingDB := "DB1"
			if prevCmp == -1 {
				missingDB = "DB2"
			}
			t.Logf("%d key(s) missing from %s in [%x, %x)", missingCount, missingDB, prevKey, key1)
			missingCount = 0
		}
		switch cmp {
		case 0:
			val1 := iter1.Value()
			val2 := iter2.Value()
			if !bytes.Equal(val1, val2) {
				t.Logf("key %x has different values:\n DB1: %x\n DB2: %x", key1, val1, val2)
			}

			next1 = iter1.Next()
			next2 = iter2.Next()
			count1++
			count2++

		case -1:
			next1 = iter1.Next()
			count1++
			if missingCount == 0 {
				prevKey = key1
			}
			missingCount++
		case 1:
			next2 = iter2.Next()
			count2++
			if missingCount == 0 {
				prevKey = key2
			}
			missingCount++
		}

		require.NoError(t, iter1.Err(), "iter1 error")
		require.NoError(t, iter2.Err(), "iter2 error")
	}
	if missingCount > 0 {
		t.Logf("%d final key(s) mismatched, starting at %x", missingCount, prevKey)
	}

	missingCount = 0
	for next1 {
		if missingCount == 0 {
			prevKey = iter1.Key()
		}
		next1 = iter1.Next()
		count1++
		missingCount++
		require.NoError(t, iter1.Err(), "iter1 error")
	}
	if missingCount > 0 {
		t.Logf("%d keys missing from DB2 starting with %x", missingCount, prevKey)
	}

	missingCount = 0
	for next2 {
		if missingCount == 0 {
			prevKey = iter2.Key()
		}
		next2 = iter2.Next()
		count2++
		missingCount++
		require.NoError(t, iter2.Err(), "iter2 error")
	}
	if missingCount > 0 {
		t.Logf("%d keys missing from DB1 starting with %x", missingCount, prevKey)
	}

	t.Logf("DB1 had %d keys, DB2 had %d keys", count1, count2)
}
