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

func TestSubnetOnlyValidator_validateConstants(t *testing.T) {
	sov := SubnetOnlyValidator{
		ValidationID:          ids.GenerateTestID(),
		SubnetID:              ids.GenerateTestID(),
		NodeID:                ids.GenerateTestNodeID(),
		PublicKey:             utils.RandomBytes(bls.PublicKeyLen),
		RemainingBalanceOwner: utils.RandomBytes(32),
		StartTime:             rand.Uint64(), // #nosec G404
	}

	tests := []struct {
		name     string
		v        SubnetOnlyValidator
		expected bool
	}{
		{
			name:     "equal",
			v:        sov,
			expected: true,
		},
		{
			name: "everything is different",
			v: SubnetOnlyValidator{
				ValidationID:          ids.GenerateTestID(),
				SubnetID:              ids.GenerateTestID(),
				NodeID:                ids.GenerateTestNodeID(),
				PublicKey:             utils.RandomBytes(bls.PublicKeyLen),
				RemainingBalanceOwner: utils.RandomBytes(32),
				StartTime:             rand.Uint64(), // #nosec G404
			},
			expected: true,
		},
		{
			name: "different subnetID",
			v: SubnetOnlyValidator{
				ValidationID:          sov.ValidationID,
				SubnetID:              ids.GenerateTestID(),
				NodeID:                sov.NodeID,
				PublicKey:             sov.PublicKey,
				RemainingBalanceOwner: sov.RemainingBalanceOwner,
				StartTime:             sov.StartTime,
			},
		},
		{
			name: "different nodeID",
			v: SubnetOnlyValidator{
				ValidationID:          sov.ValidationID,
				SubnetID:              sov.SubnetID,
				NodeID:                ids.GenerateTestNodeID(),
				PublicKey:             sov.PublicKey,
				RemainingBalanceOwner: sov.RemainingBalanceOwner,
				StartTime:             sov.StartTime,
			},
		},
		{
			name: "different publicKey",
			v: SubnetOnlyValidator{
				ValidationID:          sov.ValidationID,
				SubnetID:              sov.SubnetID,
				NodeID:                sov.NodeID,
				PublicKey:             utils.RandomBytes(bls.PublicKeyLen),
				RemainingBalanceOwner: sov.RemainingBalanceOwner,
				StartTime:             sov.StartTime,
			},
		},
		{
			name: "different remainingBalanceOwner",
			v: SubnetOnlyValidator{
				ValidationID:          sov.ValidationID,
				SubnetID:              sov.SubnetID,
				NodeID:                sov.NodeID,
				PublicKey:             sov.PublicKey,
				RemainingBalanceOwner: utils.RandomBytes(32),
				StartTime:             sov.StartTime,
			},
		},
		{
			name: "different startTime",
			v: SubnetOnlyValidator{
				ValidationID:          sov.ValidationID,
				SubnetID:              sov.SubnetID,
				NodeID:                sov.NodeID,
				PublicKey:             sov.PublicKey,
				RemainingBalanceOwner: sov.RemainingBalanceOwner,
				StartTime:             rand.Uint64(), // #nosec G404
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sov := sov
			v := test.v

			randomize := func(v *SubnetOnlyValidator) {
				v.Weight = rand.Uint64()            // #nosec G404
				v.MinNonce = rand.Uint64()          // #nosec G404
				v.EndAccumulatedFee = rand.Uint64() // #nosec G404
			}
			randomize(&sov)
			randomize(&v)

			require.Equal(t, test.expected, sov.validateConstants(v))
		})
	}
}

func TestSubnetOnlyValidator_DatabaseHelpers(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(err)
	pk := bls.PublicFromSecretKey(sk)
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	var owner fx.Owner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.GenerateTestShortID(),
		},
	}
	ownerBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &owner)
	require.NoError(err)

	vdr := SubnetOnlyValidator{
		ValidationID:          ids.GenerateTestID(),
		SubnetID:              ids.GenerateTestID(),
		NodeID:                ids.GenerateTestNodeID(),
		PublicKey:             pkBytes,
		RemainingBalanceOwner: ownerBytes,
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
