// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

func TestSubnetOnlyValidator_Compare(t *testing.T) {
	tests := []struct {
		name     string
		v        SubnetOnlyValidator
		o        SubnetOnlyValidator
		expected int
	}{
		{
			name: "v.EndAccumulatedFee < o.EndAccumulatedFee",
			v: SubnetOnlyValidator{
				ValidationID:      ids.GenerateTestID(),
				EndAccumulatedFee: 1,
			},
			o: SubnetOnlyValidator{
				ValidationID:      ids.GenerateTestID(),
				EndAccumulatedFee: 2,
			},
			expected: -1,
		},
		{
			name: "v.EndAccumulatedFee = o.EndAccumulatedFee, v.ValidationID < o.ValidationID",
			v: SubnetOnlyValidator{
				ValidationID:      ids.ID{0},
				EndAccumulatedFee: 1,
			},
			o: SubnetOnlyValidator{
				ValidationID:      ids.ID{1},
				EndAccumulatedFee: 1,
			},
			expected: -1,
		},
		{
			name: "v.EndAccumulatedFee = o.EndAccumulatedFee, v.ValidationID = o.ValidationID",
			v: SubnetOnlyValidator{
				ValidationID:      ids.ID{0},
				EndAccumulatedFee: 1,
			},
			o: SubnetOnlyValidator{
				ValidationID:      ids.ID{0},
				EndAccumulatedFee: 1,
			},
			expected: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, test.v.Compare(test.o))
			require.Equal(-test.expected, test.o.Compare(test.v))
			require.Equal(test.expected == -1, test.v.Less(test.o))
			require.False(test.o.Less(test.v))
		})
	}
}

func TestSubnetOnlyValidator_immutableFieldsAreUnmodified(t *testing.T) {
	var (
		randomizeSOV = func(sov SubnetOnlyValidator) SubnetOnlyValidator {
			// Randomize unrelated fields
			sov.Weight = rand.Uint64()            // #nosec G404
			sov.MinNonce = rand.Uint64()          // #nosec G404
			sov.EndAccumulatedFee = rand.Uint64() // #nosec G404
			return sov
		}
		sov = newSoV()
	)

	t.Run("equal", func(t *testing.T) {
		v := randomizeSOV(sov)
		require.True(t, sov.immutableFieldsAreUnmodified(v))
	})
	t.Run("everything is different", func(t *testing.T) {
		v := randomizeSOV(newSoV())
		require.True(t, sov.immutableFieldsAreUnmodified(v))
	})
	t.Run("different subnetID", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.SubnetID = ids.GenerateTestID()
		require.False(t, sov.immutableFieldsAreUnmodified(v))
	})
	t.Run("different nodeID", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.NodeID = ids.GenerateTestNodeID()
		require.False(t, sov.immutableFieldsAreUnmodified(v))
	})
	t.Run("different publicKey", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.PublicKey = utils.RandomBytes(bls.PublicKeyLen)
		require.False(t, sov.immutableFieldsAreUnmodified(v))
	})
	t.Run("different remainingBalanceOwner", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.RemainingBalanceOwner = utils.RandomBytes(32)
		require.False(t, sov.immutableFieldsAreUnmodified(v))
	})
	t.Run("different deactivationOwner", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.DeactivationOwner = utils.RandomBytes(32)
		require.False(t, sov.immutableFieldsAreUnmodified(v))
	})
	t.Run("different startTime", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.StartTime = rand.Uint64() // #nosec G404
		require.False(t, sov.immutableFieldsAreUnmodified(v))
	})
}

func TestGetSubnetOnlyValidator(t *testing.T) {
	var (
		sov             = newSoV()
		dbWithSoV       = memdb.New()
		dbWithoutSoV    = memdb.New()
		cacheWithSoV    = &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10}
		cacheWithoutSoV = &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10}
	)

	require.NoError(t, putSubnetOnlyValidator(dbWithSoV, cacheWithSoV, sov))
	require.NoError(t, deleteSubnetOnlyValidator(dbWithoutSoV, cacheWithoutSoV, sov.ValidationID))

	tests := []struct {
		name          string
		cache         cache.Cacher[ids.ID, maybe.Maybe[SubnetOnlyValidator]]
		db            database.KeyValueReader
		expectedSoV   SubnetOnlyValidator
		expectedErr   error
		expectedEntry maybe.Maybe[SubnetOnlyValidator]
	}{
		{
			name:          "cached with validator",
			cache:         cacheWithSoV,
			db:            dbWithoutSoV,
			expectedSoV:   sov,
			expectedEntry: maybe.Some(sov),
		},
		{
			name:          "from disk with validator",
			cache:         &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10},
			db:            dbWithSoV,
			expectedSoV:   sov,
			expectedEntry: maybe.Some(sov),
		},
		{
			name:        "cached without validator",
			cache:       cacheWithoutSoV,
			db:          dbWithSoV,
			expectedErr: database.ErrNotFound,
		},
		{
			name:        "from disk without validator",
			cache:       &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10},
			db:          dbWithoutSoV,
			expectedErr: database.ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			gotSoV, err := getSubnetOnlyValidator(test.cache, test.db, sov.ValidationID)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedSoV, gotSoV)

			cachedSoV, ok := test.cache.Get(sov.ValidationID)
			require.True(ok)
			require.Equal(test.expectedEntry, cachedSoV)
		})
	}
}

func TestPutSubnetOnlyValidator(t *testing.T) {
	var (
		require = require.New(t)
		sov     = newSoV()
		db      = memdb.New()
		cache   = &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10}
	)
	expectedSoVBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, sov)
	require.NoError(err)

	require.NoError(putSubnetOnlyValidator(db, cache, sov))

	sovBytes, err := db.Get(sov.ValidationID[:])
	require.NoError(err)
	require.Equal(expectedSoVBytes, sovBytes)

	sovFromCache, ok := cache.Get(sov.ValidationID)
	require.True(ok)
	require.Equal(maybe.Some(sov), sovFromCache)
}

func TestDeleteSubnetOnlyValidator(t *testing.T) {
	var (
		require      = require.New(t)
		validationID = ids.GenerateTestID()
		db           = memdb.New()
		cache        = &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10}
	)
	require.NoError(db.Put(validationID[:], nil))

	require.NoError(deleteSubnetOnlyValidator(db, cache, validationID))

	hasSoV, err := db.Has(validationID[:])
	require.NoError(err)
	require.False(hasSoV)

	sovFromCache, ok := cache.Get(validationID)
	require.True(ok)
	require.Equal(maybe.Nothing[SubnetOnlyValidator](), sovFromCache)
}

func newSoV() SubnetOnlyValidator {
	return SubnetOnlyValidator{
		ValidationID:          ids.GenerateTestID(),
		SubnetID:              ids.GenerateTestID(),
		NodeID:                ids.GenerateTestNodeID(),
		PublicKey:             utils.RandomBytes(bls.PublicKeyLen),
		RemainingBalanceOwner: utils.RandomBytes(32),
		DeactivationOwner:     utils.RandomBytes(32),
		StartTime:             rand.Uint64(), // #nosec G404
		Weight:                rand.Uint64(), // #nosec G404
		MinNonce:              rand.Uint64(), // #nosec G404
		EndAccumulatedFee:     rand.Uint64(), // #nosec G404
	}
}
