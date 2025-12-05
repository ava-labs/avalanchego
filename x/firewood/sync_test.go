// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/logging"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	rangeProofMarshaler  = RangeProofMarshaler{}
	changeProofMarshaler = ChangeProofMarshaler{}
)

func Test_Firewood_Sync(t *testing.T) {
	tests := []int{0, 1, 1_000, 100_000}
	for _, numKeys := range tests {
		t.Run(fmt.Sprintf("numKeys=%d", numKeys), func(t *testing.T) {
			require := require.New(t)
			now := time.Now()
			fullDB := generateDB(t, numKeys, now.UnixNano())
			db := generateDB(t, 0, 0) // empty DB

			ctx := t.Context()
			root, err := fullDB.GetMerkleRoot(ctx)
			require.NoError(err)
			t.Log("generated full DB with root", root, "in", time.Since(now))

			syncer, err := xsync.NewManager(
				db,
				xsync.ManagerConfig[*RangeProof, *ChangeProof]{
					RangeProofMarshaler:   rangeProofMarshaler,
					ChangeProofMarshaler:  changeProofMarshaler,
					RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetRangeProofHandler(fullDB, rangeProofMarshaler)),
					ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetChangeProofHandler(fullDB, rangeProofMarshaler, changeProofMarshaler)),
					SimultaneousWorkLimit: 1,
					Log:                   logging.NoLog{},
					TargetRoot:            root,
					EmptyRoot:             ids.ID(types.EmptyRootHash),
				},
				prometheus.NewRegistry(),
			)
			require.NoError(err)
			require.NotNil(syncer)

			// Add logging for actual time syncing
			now = time.Now()
			require.NoError(syncer.Start(ctx))
			err = syncer.Wait(ctx)
			if errors.Is(err, xsync.ErrFinishedWithUnexpectedRoot) {
				t.Log("syncer reported root mismatch; logging diff between DBs")
				logDiff(t, fullDB, db)
			}
			require.NoError(err)
			t.Logf("synced %d keys in %s", numKeys, time.Since(now))
		})
	}
}

func generateDB(t *testing.T, numKeys int, seed int64) *syncDB {
	t.Helper()
	folder := t.TempDir()
	path := folder + "/firewood.db"

	fw, err := ffi.New(path, ffi.DefaultConfig())
	require.NoError(t, err)
	require.NotNil(t, fw)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		require.NoError(t, fw.Close(ctx))
	})

	db := New(fw)
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
	t.Logf("generating %d random keys/values with seed %d", numKeys, seed)
	for i := 0; i < numKeys; i++ {
		// Random length between minLength and maxLength inclusive
		keyLen := r.Intn(maxLength-minLength+1) + minLength
		valLen := r.Intn(maxLength-minLength+1) + minLength

		key := make([]byte, keyLen)
		val := make([]byte, valLen)

		// Fill with random bytes
		_, err := r.Read(key)
		require.NoError(t, err, "read never errors")
		_, err = r.Read(val)
		require.NoError(t, err, "read never errors")

		keys[i] = key
		vals[i] = val
	}

	_, err = fw.Update(keys, vals)
	require.NoError(t, err)

	return db
}

func logDiff(t *testing.T, db1, db2 *syncDB) {
	rev1, err := db1.fw.LatestRevision()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rev1.Drop())
	})
	rev2, err := db2.fw.LatestRevision()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rev2.Drop())
	})

	// check roots manually
	if rev1.Root() == rev2.Root() {
		t.Log("both DBs have the same root:", rev1.Root())
		return
	}

	iter1, err := rev1.Iter(nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, iter1.Drop())
	})
	iter2, err := rev2.Iter(nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, iter2.Drop())
	})

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
		if cmp != 0 && missingCount > 0 {
			missingDB := "DB1"
			if prevCmp == -1 {
				missingDB = "DB2"
			}
			t.Logf("%d keys missing from %s in [%x, %x)", missingCount, missingDB, prevKey, key1)
			missingCount = 0
		}
		switch {
		case cmp == 0:
			val1 := iter1.Value()
			val2 := iter2.Value()
			if !bytes.Equal(val1, val2) {
				t.Logf("key %x has different values:\n DB1: %x\n DB2: %x", key1, val1, val2)
			}

			next1 = iter1.Next()
			next2 = iter2.Next()
			count1++
			count2++

		case cmp == -1:
			next1 = iter1.Next()
			count1++
			if missingCount == 0 {
				prevKey = key1
			}
			missingCount++
		case cmp == 1:
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
