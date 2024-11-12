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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestSubnetOnlyValidator_Less(t *testing.T) {
	tests := []struct {
		name  string
		v     *SubnetOnlyValidator
		o     *SubnetOnlyValidator
		equal bool
	}{
		{
			name: "v.EndAccumulatedFee < o.EndAccumulatedFee",
			v: &SubnetOnlyValidator{
				ValidationID:      ids.GenerateTestID(),
				EndAccumulatedFee: 1,
			},
			o: &SubnetOnlyValidator{
				ValidationID:      ids.GenerateTestID(),
				EndAccumulatedFee: 2,
			},
			equal: false,
		},
		{
			name: "v.EndAccumulatedFee = o.EndAccumulatedFee, v.ValidationID < o.ValidationID",
			v: &SubnetOnlyValidator{
				ValidationID:      ids.ID{0},
				EndAccumulatedFee: 1,
			},
			o: &SubnetOnlyValidator{
				ValidationID:      ids.ID{1},
				EndAccumulatedFee: 1,
			},
			equal: false,
		},
		{
			name: "v.EndAccumulatedFee = o.EndAccumulatedFee, v.ValidationID = o.ValidationID",
			v: &SubnetOnlyValidator{
				ValidationID:      ids.ID{0},
				EndAccumulatedFee: 1,
			},
			o: &SubnetOnlyValidator{
				ValidationID:      ids.ID{0},
				EndAccumulatedFee: 1,
			},
			equal: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			less := test.v.Less(test.o)
			require.Equal(!test.equal, less)

			greater := test.o.Less(test.v)
			require.False(greater)
		})
	}
}

func TestSubnetOnlyValidator_DatabaseHelpers(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	vdr := &SubnetOnlyValidator{
		ValidationID:      ids.GenerateTestID(),
		SubnetID:          ids.GenerateTestID(),
		NodeID:            ids.GenerateTestNodeID(),
		PublicKey:         bls.PublicKeyToUncompressedBytes(bls.PublicFromSecretKey(sk)),
		StartTime:         rand.Uint64(), // #nosec G404
		Weight:            rand.Uint64(), // #nosec G404
		MinNonce:          rand.Uint64(), // #nosec G404
		EndAccumulatedFee: rand.Uint64(), // #nosec G404
	}

	// Validator hasn't been put on disk yet
	gotVdr, err := getSubnetOnlyValidator(db, vdr.ValidationID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(gotVdr)

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
	require.Nil(gotVdr)
}
