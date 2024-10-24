// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
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

func TestSubnetOnlyValidator_constantsAreUnmodified(t *testing.T) {
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
		require.True(t, sov.constantsAreUnmodified(v))
	})
	t.Run("everything is different", func(t *testing.T) {
		v := randomizeSOV(randomSOV())
		require.True(t, sov.constantsAreUnmodified(v))
	})
	t.Run("different subnetID", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.SubnetID = ids.GenerateTestID()
		require.False(t, sov.constantsAreUnmodified(v))
	})
	t.Run("different nodeID", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.NodeID = ids.GenerateTestNodeID()
		require.False(t, sov.constantsAreUnmodified(v))
	})
	t.Run("different publicKey", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.PublicKey = utils.RandomBytes(bls.PublicKeyLen)
		require.False(t, sov.constantsAreUnmodified(v))
	})
	t.Run("different remainingBalanceOwner", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.RemainingBalanceOwner = utils.RandomBytes(32)
		require.False(t, sov.constantsAreUnmodified(v))
	})
	t.Run("different deactivationOwner", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.DeactivationOwner = utils.RandomBytes(32)
		require.False(t, sov.constantsAreUnmodified(v))
	})
	t.Run("different startTime", func(t *testing.T) {
		v := randomizeSOV(sov)
		v.StartTime = rand.Uint64() // #nosec G404
		require.False(t, sov.constantsAreUnmodified(v))
	})
}

func TestSubnetOnlyValidator_DatabaseHelpers(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(err)
	pk := bls.PublicFromSecretKey(sk)
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	var remainingBalanceOwner fx.Owner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.GenerateTestShortID(),
		},
	}
	remainingBalanceOwnerBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &remainingBalanceOwner)
	require.NoError(err)

	var deactivationOwner fx.Owner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.GenerateTestShortID(),
		},
	}
	deactivationOwnerBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &deactivationOwner)
	require.NoError(err)

	vdr := SubnetOnlyValidator{
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

	// Validator hasn't been put on disk yet
	gotVdr, err := getSubnetOnlyValidator(db, vdr.ValidationID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Zero(gotVdr)

	// Place the validator on disk
	require.NoError(putSubnetOnlyValidator(db, vdr))

	// Verify that the validator can be fetched from disk
	gotVdr, err = getSubnetOnlyValidator(db, vdr.ValidationID)
	require.NoError(err)
	require.Equal(vdr, gotVdr)

	// Remove the validator from disk
	require.NoError(deleteSubnetOnlyValidator(db, vdr.ValidationID))

	// Verify that the validator has been removed from disk
	gotVdr, err = getSubnetOnlyValidator(db, vdr.ValidationID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Zero(gotVdr)
}
