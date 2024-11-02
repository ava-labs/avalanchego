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
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
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
		randomSOV = func() SubnetOnlyValidator {
			return SubnetOnlyValidator{
				ValidationID:          ids.GenerateTestID(),
				SubnetID:              ids.GenerateTestID(),
				NodeID:                ids.GenerateTestNodeID(),
				PublicKey:             utils.RandomBytes(bls.PublicKeyLen),
				RemainingBalanceOwner: utils.RandomBytes(32),
				DeactivationOwner:     utils.RandomBytes(32),
				StartTime:             rand.Uint64(), // #nosec G404
			}
		}
		randomizeSOV = func(sov SubnetOnlyValidator) SubnetOnlyValidator {
			// Randomize unrelated fields
			sov.Weight = rand.Uint64()            // #nosec G404
			sov.MinNonce = rand.Uint64()          // #nosec G404
			sov.EndAccumulatedFee = rand.Uint64() // #nosec G404
			return sov
		}
		sov = randomSOV()
	)

	t.Run("equal", func(t *testing.T) {
		v := randomizeSOV(sov)
		require.True(t, sov.immutableFieldsAreUnmodified(v))
	})
	t.Run("everything is different", func(t *testing.T) {
		v := randomizeSOV(randomSOV())
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

func TestSubnetOnlyValidator_DatabaseHelpers(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	var remainingBalanceOwner fx.Owner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.GenerateTestShortID(),
		},
	}
	remainingBalanceOwnerBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &remainingBalanceOwner)
	require.NoError(t, err)

	var deactivationOwner fx.Owner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.GenerateTestShortID(),
		},
	}
	deactivationOwnerBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &deactivationOwner)
	require.NoError(t, err)

	sov := SubnetOnlyValidator{
		ValidationID:          ids.GenerateTestID(),
		SubnetID:              ids.GenerateTestID(),
		NodeID:                ids.GenerateTestNodeID(),
		PublicKey:             pkBytes,
		RemainingBalanceOwner: remainingBalanceOwnerBytes,
		DeactivationOwner:     deactivationOwnerBytes,
		StartTime:             rand.Uint64(), // #nosec G404
		Weight:                rand.Uint64(), // #nosec G404
		MinNonce:              rand.Uint64(), // #nosec G404
		EndAccumulatedFee:     rand.Uint64(), // #nosec G404
	}

	var (
		addedDB                   = memdb.New()
		removedDB                 = memdb.New()
		addedAndRemovedDB         = memdb.New()
		addedAndRemovedAndAddedDB = memdb.New()

		addedCache                   = &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10}
		removedCache                 = &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10}
		addedAndRemovedCache         = &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10}
		addedAndRemovedAndAddedCache = &cache.LRU[ids.ID, maybe.Maybe[SubnetOnlyValidator]]{Size: 10}
	)

	// Place the validator on disk
	require.NoError(t, putSubnetOnlyValidator(addedDB, addedCache, sov))
	require.NoError(t, putSubnetOnlyValidator(addedAndRemovedDB, addedAndRemovedCache, sov))
	require.NoError(t, putSubnetOnlyValidator(addedAndRemovedAndAddedDB, addedAndRemovedAndAddedCache, sov))

	// Remove the validator on disk
	require.NoError(t, deleteSubnetOnlyValidator(removedDB, removedCache, sov.ValidationID))
	require.NoError(t, deleteSubnetOnlyValidator(addedAndRemovedDB, addedAndRemovedCache, sov.ValidationID))
	require.NoError(t, deleteSubnetOnlyValidator(addedAndRemovedAndAddedDB, addedAndRemovedAndAddedCache, sov.ValidationID))

	// Reintroduce the validator to disk
	require.NoError(t, putSubnetOnlyValidator(addedAndRemovedAndAddedDB, addedAndRemovedAndAddedCache, sov))

	addedTests := []struct {
		name  string
		db    database.Database
		cache cache.Cacher[ids.ID, maybe.Maybe[SubnetOnlyValidator]]
	}{
		{
			name:  "added in cache",
			db:    memdb.New(),
			cache: addedCache,
		},
		{
			name:  "added on disk",
			db:    addedDB,
			cache: emptySoVCache,
		},
		{
			name:  "added and removed and added in cache",
			db:    memdb.New(),
			cache: addedAndRemovedAndAddedCache,
		},
		{
			name:  "added and removed and added on disk",
			db:    addedAndRemovedAndAddedDB,
			cache: emptySoVCache,
		},
	}
	for _, test := range addedTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			gotSOV, err := getSubnetOnlyValidator(test.cache, test.db, sov.ValidationID)
			require.NoError(err)
			require.Equal(sov, gotSOV)
		})
	}

	removedTests := []struct {
		name  string
		db    database.Database
		cache cache.Cacher[ids.ID, maybe.Maybe[SubnetOnlyValidator]]
	}{
		{
			name:  "empty",
			db:    memdb.New(),
			cache: emptySoVCache,
		},
		{
			name:  "removed from cache",
			db:    addedDB,
			cache: removedCache,
		},
		{
			name:  "removed from disk",
			db:    removedDB,
			cache: emptySoVCache,
		},
	}
	for _, test := range removedTests {
		t.Run(test.name, func(t *testing.T) {
			_, err := getSubnetOnlyValidator(test.cache, test.db, sov.ValidationID)
			require.ErrorIs(t, err, database.ErrNotFound)
		})
	}
}
