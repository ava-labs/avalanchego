// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

func TestL1Validator_Compare(t *testing.T) {
	tests := []struct {
		name     string
		v        L1Validator
		o        L1Validator
		expected int
	}{
		{
			name: "v.EndAccumulatedFee < o.EndAccumulatedFee",
			v: L1Validator{
				ValidationID:      ids.GenerateTestID(),
				EndAccumulatedFee: 1,
			},
			o: L1Validator{
				ValidationID:      ids.GenerateTestID(),
				EndAccumulatedFee: 2,
			},
			expected: -1,
		},
		{
			name: "v.EndAccumulatedFee = o.EndAccumulatedFee, v.ValidationID < o.ValidationID",
			v: L1Validator{
				ValidationID:      ids.ID{0},
				EndAccumulatedFee: 1,
			},
			o: L1Validator{
				ValidationID:      ids.ID{1},
				EndAccumulatedFee: 1,
			},
			expected: -1,
		},
		{
			name: "v.EndAccumulatedFee = o.EndAccumulatedFee, v.ValidationID = o.ValidationID",
			v: L1Validator{
				ValidationID:      ids.ID{0},
				EndAccumulatedFee: 1,
			},
			o: L1Validator{
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

func TestL1Validator_immutableFieldsAreUnmodified(t *testing.T) {
	var (
		randomizeL1Validator = func(l1Validator L1Validator) L1Validator {
			// Randomize unrelated fields
			l1Validator.Weight = rand.Uint64()
			l1Validator.MinNonce = rand.Uint64()
			l1Validator.EndAccumulatedFee = rand.Uint64()
			return l1Validator
		}
		l1Validator = newL1Validator()
	)

	t.Run("equal", func(t *testing.T) {
		v := randomizeL1Validator(l1Validator)
		require.True(t, l1Validator.immutableFieldsAreUnmodified(v))
	})
	t.Run("everything is different", func(t *testing.T) {
		v := randomizeL1Validator(newL1Validator())
		require.True(t, l1Validator.immutableFieldsAreUnmodified(v))
	})
	t.Run("different subnetID", func(t *testing.T) {
		v := randomizeL1Validator(l1Validator)
		v.SubnetID = ids.GenerateTestID()
		require.False(t, l1Validator.immutableFieldsAreUnmodified(v))
	})
	t.Run("different nodeID", func(t *testing.T) {
		v := randomizeL1Validator(l1Validator)
		v.NodeID = ids.GenerateTestNodeID()
		require.False(t, l1Validator.immutableFieldsAreUnmodified(v))
	})
	t.Run("different publicKey", func(t *testing.T) {
		v := randomizeL1Validator(l1Validator)
		v.PublicKey = utils.RandomBytes(bls.PublicKeyLen)
		require.False(t, l1Validator.immutableFieldsAreUnmodified(v))
	})
	t.Run("different remainingBalanceOwner", func(t *testing.T) {
		v := randomizeL1Validator(l1Validator)
		v.RemainingBalanceOwner = utils.RandomBytes(32)
		require.False(t, l1Validator.immutableFieldsAreUnmodified(v))
	})
	t.Run("different deactivationOwner", func(t *testing.T) {
		v := randomizeL1Validator(l1Validator)
		v.DeactivationOwner = utils.RandomBytes(32)
		require.False(t, l1Validator.immutableFieldsAreUnmodified(v))
	})
	t.Run("different startTime", func(t *testing.T) {
		v := randomizeL1Validator(l1Validator)
		v.StartTime = rand.Uint64()
		require.False(t, l1Validator.immutableFieldsAreUnmodified(v))
	})
}

func TestGetL1Validator(t *testing.T) {
	var (
		l1Validator             = newL1Validator()
		dbWithL1Validator       = memdb.New()
		dbWithoutL1Validator    = memdb.New()
		cacheWithL1Validator    = lru.NewCache[ids.ID, maybe.Maybe[L1Validator]](10)
		cacheWithoutL1Validator = lru.NewCache[ids.ID, maybe.Maybe[L1Validator]](10)
	)

	require.NoError(t, putL1Validator(dbWithL1Validator, cacheWithL1Validator, l1Validator))
	require.NoError(t, deleteL1Validator(dbWithoutL1Validator, cacheWithoutL1Validator, l1Validator.ValidationID))

	tests := []struct {
		name                string
		cache               cache.Cacher[ids.ID, maybe.Maybe[L1Validator]]
		db                  database.KeyValueReader
		expectedL1Validator L1Validator
		expectedErr         error
		expectedEntry       maybe.Maybe[L1Validator]
	}{
		{
			name:                "cached with validator",
			cache:               cacheWithL1Validator,
			db:                  dbWithoutL1Validator,
			expectedL1Validator: l1Validator,
			expectedEntry:       maybe.Some(l1Validator),
		},
		{
			name:                "from disk with validator",
			cache:               lru.NewCache[ids.ID, maybe.Maybe[L1Validator]](10),
			db:                  dbWithL1Validator,
			expectedL1Validator: l1Validator,
			expectedEntry:       maybe.Some(l1Validator),
		},
		{
			name:        "cached without validator",
			cache:       cacheWithoutL1Validator,
			db:          dbWithL1Validator,
			expectedErr: database.ErrNotFound,
		},
		{
			name:        "from disk without validator",
			cache:       lru.NewCache[ids.ID, maybe.Maybe[L1Validator]](10),
			db:          dbWithoutL1Validator,
			expectedErr: database.ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			gotL1Validator, err := getL1Validator(test.cache, test.db, l1Validator.ValidationID)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedL1Validator, gotL1Validator)

			cachedL1Validator, ok := test.cache.Get(l1Validator.ValidationID)
			require.True(ok)
			require.Equal(test.expectedEntry, cachedL1Validator)
		})
	}
}

func TestPutL1Validator(t *testing.T) {
	var (
		require     = require.New(t)
		l1Validator = newL1Validator()
		db          = memdb.New()
		cache       = lru.NewCache[ids.ID, maybe.Maybe[L1Validator]](10)
	)
	expectedL1ValidatorBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, l1Validator)
	require.NoError(err)

	require.NoError(putL1Validator(db, cache, l1Validator))

	l1ValidatorBytes, err := db.Get(l1Validator.ValidationID[:])
	require.NoError(err)
	require.Equal(expectedL1ValidatorBytes, l1ValidatorBytes)

	l1ValidatorFromCache, ok := cache.Get(l1Validator.ValidationID)
	require.True(ok)
	require.Equal(maybe.Some(l1Validator), l1ValidatorFromCache)
}

func TestDeleteL1Validator(t *testing.T) {
	var (
		require      = require.New(t)
		validationID = ids.GenerateTestID()
		db           = memdb.New()
		cache        = lru.NewCache[ids.ID, maybe.Maybe[L1Validator]](10)
	)
	require.NoError(db.Put(validationID[:], nil))

	require.NoError(deleteL1Validator(db, cache, validationID))

	hasL1Validator, err := db.Has(validationID[:])
	require.NoError(err)
	require.False(hasL1Validator)

	l1ValidatorFromCache, ok := cache.Get(validationID)
	require.True(ok)
	require.Equal(maybe.Nothing[L1Validator](), l1ValidatorFromCache)
}

func newL1Validator() L1Validator {
	return L1Validator{
		ValidationID:          ids.GenerateTestID(),
		SubnetID:              ids.GenerateTestID(),
		NodeID:                ids.GenerateTestNodeID(),
		PublicKey:             utils.RandomBytes(bls.PublicKeyLen),
		RemainingBalanceOwner: utils.RandomBytes(32),
		DeactivationOwner:     utils.RandomBytes(32),
		StartTime:             rand.Uint64(),
		Weight:                rand.Uint64(),
		MinNonce:              rand.Uint64(),
		EndAccumulatedFee:     rand.Uint64(),
	}
}
