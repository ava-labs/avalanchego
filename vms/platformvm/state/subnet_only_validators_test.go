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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestSubnetOnlyValidatorDatabaseHelpers(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	vdr := &SubnetOnlyValidator{
		ValidationID: ids.GenerateTestID(),
		SubnetID:     ids.GenerateTestID(),
		NodeID:       ids.GenerateTestNodeID(),
		MinNonce:     rand.Uint64(), // #nosec G404
		Weight:       rand.Uint64(), // #nosec G404
		Balance:      rand.Uint64(), // #nosec G404
		PublicKey:    bls.PublicKeyToUncompressedBytes(bls.PublicFromSecretKey(sk)),
		EndTime:      rand.Uint64(), // #nosec G404
	}

	require.NoError(putSubnetOnlyValidator(db, vdr))
	gotVdr, err := getSubnetOnlyValidator(db, vdr.ValidationID)
	require.NoError(err)
	require.Equal(vdr, gotVdr)

	require.NoError(deleteSubnetOnlyValidator(db, vdr.ValidationID))
	gotVdr, err = getSubnetOnlyValidator(db, vdr.ValidationID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(gotVdr)
}

func TestBaseSubnetOnlyValidators_SingleWrite(t *testing.T) {
	tests := []struct {
		name                              string
		setup                             func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator)
		expectedInValidatorDB             bool
		expectedValidatorWeightDiffsDB    func(vdrNodeID ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff
		expectedValidatorPublicKeyDiffsDB func(vdrNodeID ids.NodeID, vdrPublicKeyBytes []byte) map[ids.NodeID][]byte
		expectedValidatorsManager         func(vdr *SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput
	}{
		{
			name: "Add",
			setup: func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator) {
				require.NoError(b.AddSubnetOnlyValidator(vdr))
			},
			expectedInValidatorDB: true,
			expectedValidatorWeightDiffsDB: func(ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff {
				return map[ids.NodeID]*ValidatorWeightDiff{}
			},
			expectedValidatorPublicKeyDiffsDB: func(vdrNodeID ids.NodeID, vdrPublicKeyBytes []byte) map[ids.NodeID][]byte {
				return map[ids.NodeID][]byte{
					vdrNodeID: vdrPublicKeyBytes,
				}
			},
			expectedValidatorsManager: func(vdr *SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput {
				return map[ids.NodeID]*validators.GetValidatorOutput{
					vdr.NodeID: {
						NodeID:    vdr.NodeID,
						PublicKey: bls.PublicKeyFromValidUncompressedBytes(vdr.PublicKey),
						Weight:    vdr.Weight,
					},
				}
			},
		},
		{
			name: "Add + IncreaseBalance",
			setup: func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator) {
				toAdd := uint64(10)
				balance := vdr.Balance
				require.NoError(b.AddSubnetOnlyValidator(vdr))
				require.NoError(b.IncreaseSubnetOnlyValidatorBalance(vdr.ValidationID, toAdd))
				require.Equal(toAdd+balance, vdr.Balance)
			},
			expectedInValidatorDB: true,
			expectedValidatorWeightDiffsDB: func(ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff {
				return map[ids.NodeID]*ValidatorWeightDiff{}
			},
			expectedValidatorPublicKeyDiffsDB: func(vdrNodeID ids.NodeID, vdrPublicKeyBytes []byte) map[ids.NodeID][]byte {
				return map[ids.NodeID][]byte{
					vdrNodeID: vdrPublicKeyBytes,
				}
			},
			expectedValidatorsManager: func(vdr *SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput {
				return map[ids.NodeID]*validators.GetValidatorOutput{
					vdr.NodeID: {
						NodeID:    vdr.NodeID,
						PublicKey: bls.PublicKeyFromValidUncompressedBytes(vdr.PublicKey),
						Weight:    vdr.Weight,
					},
				}
			},
		},
		{
			name: "Add + Disable",
			setup: func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator) {
				require.NoError(b.AddSubnetOnlyValidator(vdr))

				refund, err := b.DisableSubnetOnlyValidator(vdr.ValidationID)
				require.NoError(err)
				require.Equal(vdr.Balance, refund)
			},
			expectedInValidatorDB: true,
			expectedValidatorWeightDiffsDB: func(ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff {
				return map[ids.NodeID]*ValidatorWeightDiff{
					ids.EmptyNodeID: {
						Decrease: false,
						Amount:   100,
					},
				}
			},
			expectedValidatorPublicKeyDiffsDB: func(ids.NodeID, []byte) map[ids.NodeID][]byte {
				return map[ids.NodeID][]byte{}
			},
			expectedValidatorsManager: func(*SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput {
				return map[ids.NodeID]*validators.GetValidatorOutput{
					ids.EmptyNodeID: {
						NodeID:    ids.EmptyNodeID,
						PublicKey: nil,
						Weight:    100,
					},
				}
			},
		},
		{
			name: "Add + Set(Weight=0)",
			setup: func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator) {
				require.NoError(b.AddSubnetOnlyValidator(vdr))

				refund, err := b.SetSubnetOnlyValidatorWeight(vdr.ValidationID, 0, 0)
				require.NoError(err)
				require.Equal(refund, vdr.Balance)
			},
			expectedInValidatorDB: false,
			expectedValidatorWeightDiffsDB: func(ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff {
				return map[ids.NodeID]*ValidatorWeightDiff{}
			},
			expectedValidatorPublicKeyDiffsDB: func(ids.NodeID, []byte) map[ids.NodeID][]byte {
				return map[ids.NodeID][]byte{}
			},
			expectedValidatorsManager: func(*SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput {
				return map[ids.NodeID]*validators.GetValidatorOutput{}
			},
		},
		{
			name: "Add + Disable + Set(Weight=0)",
			setup: func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator) {
				require.NoError(b.AddSubnetOnlyValidator(vdr))

				refund, err := b.DisableSubnetOnlyValidator(vdr.ValidationID)
				require.NoError(err)
				require.Equal(refund, vdr.Balance)

				refund, err = b.SetSubnetOnlyValidatorWeight(vdr.ValidationID, 0, 0)
				require.NoError(err)
				require.Zero(refund)
			},
			expectedInValidatorDB: false,
			expectedValidatorWeightDiffsDB: func(ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff {
				return map[ids.NodeID]*ValidatorWeightDiff{}
			},
			expectedValidatorPublicKeyDiffsDB: func(ids.NodeID, []byte) map[ids.NodeID][]byte {
				return map[ids.NodeID][]byte{}
			},
			expectedValidatorsManager: func(*SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput {
				return map[ids.NodeID]*validators.GetValidatorOutput{}
			},
		},
		{
			name: "Add + Set + IncreaseBalance + IneffectiveSet + IncreaseBalance + Set(Weight=0)",
			setup: func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator) {
				balance := vdr.Balance

				require.NoError(b.AddSubnetOnlyValidator(vdr))

				refund, err := b.SetSubnetOnlyValidatorWeight(vdr.ValidationID, 10, 1)
				require.NoError(err)
				require.Zero(refund)

				toAdd := uint64(15)
				balance += toAdd
				require.NoError(b.IncreaseSubnetOnlyValidatorBalance(vdr.ValidationID, toAdd))

				refund, err = b.SetSubnetOnlyValidatorWeight(vdr.ValidationID, 10000, 0)
				require.NoError(err)
				require.Zero(refund)

				toAdd = uint64(35)
				balance += toAdd
				require.NoError(b.IncreaseSubnetOnlyValidatorBalance(vdr.ValidationID, toAdd))

				refund, err = b.SetSubnetOnlyValidatorWeight(vdr.ValidationID, 0, 2)
				require.NoError(err)
				require.Equal(balance, refund)
			},
			expectedInValidatorDB: false,
			expectedValidatorWeightDiffsDB: func(ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff {
				return map[ids.NodeID]*ValidatorWeightDiff{}
			},
			expectedValidatorPublicKeyDiffsDB: func(ids.NodeID, []byte) map[ids.NodeID][]byte {
				return map[ids.NodeID][]byte{}
			},
			expectedValidatorsManager: func(*SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput {
				return map[ids.NodeID]*validators.GetValidatorOutput{}
			},
		},
		{
			name: "Add + Disable + Set + IncreaseBalance",
			setup: func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator) {
				require.NoError(b.AddSubnetOnlyValidator(vdr))

				refund, err := b.DisableSubnetOnlyValidator(vdr.ValidationID)
				require.NoError(err)
				require.Equal(vdr.Balance, refund)

				refund, err = b.SetSubnetOnlyValidatorWeight(vdr.ValidationID, 10000, 0)
				require.NoError(err)
				require.Zero(refund)

				toAdd := uint64(35)
				require.NoError(b.IncreaseSubnetOnlyValidatorBalance(vdr.ValidationID, toAdd))

				gotVdr, added, err := b.GetSubnetOnlyValidator(vdr.ValidationID)
				require.NoError(err)
				require.True(added)
				require.Equal(toAdd+b.GetAccumulatedBalance(), gotVdr.Balance)
			},
			expectedInValidatorDB: true,
			expectedValidatorWeightDiffsDB: func(ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff {
				return map[ids.NodeID]*ValidatorWeightDiff{}
			},
			expectedValidatorPublicKeyDiffsDB: func(vdrNodeID ids.NodeID, vdrPublicKeyBytes []byte) map[ids.NodeID][]byte {
				return map[ids.NodeID][]byte{
					vdrNodeID: vdrPublicKeyBytes,
				}
			},
			expectedValidatorsManager: func(vdr *SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput {
				return map[ids.NodeID]*validators.GetValidatorOutput{
					vdr.NodeID: {
						NodeID:    vdr.NodeID,
						PublicKey: bls.PublicKeyFromValidUncompressedBytes(vdr.PublicKey),
						Weight:    10000,
					},
				}
			},
		},
		{
			name: "Add + Disable + Add + Disable",
			setup: func(require *require.Assertions, b *baseSubnetOnlyValidators, vdr *SubnetOnlyValidator) {
				vdr2 := &SubnetOnlyValidator{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     vdr.SubnetID,
					NodeID:       ids.GenerateTestNodeID(),
					MinNonce:     0,
					Weight:       10,
					Balance:      10,
					PublicKey:    []byte{},
					EndTime:      0,
				}

				require.NoError(b.AddSubnetOnlyValidator(vdr2))

				refund, err := b.DisableSubnetOnlyValidator(vdr2.ValidationID)
				require.NoError(err)
				require.Equal(vdr2.Balance, refund)

				require.NoError(b.AddSubnetOnlyValidator(vdr))

				refund, err = b.DisableSubnetOnlyValidator(vdr.ValidationID)
				require.NoError(err)
				require.Equal(vdr.Balance, refund)
			},
			expectedInValidatorDB: true,
			expectedValidatorWeightDiffsDB: func(ids.NodeID) map[ids.NodeID]*ValidatorWeightDiff {
				return map[ids.NodeID]*ValidatorWeightDiff{
					ids.EmptyNodeID: {
						Decrease: false,
						Amount:   110,
					},
				}
			},
			expectedValidatorPublicKeyDiffsDB: func(ids.NodeID, []byte) map[ids.NodeID][]byte {
				return map[ids.NodeID][]byte{}
			},
			expectedValidatorsManager: func(*SubnetOnlyValidator) map[ids.NodeID]*validators.GetValidatorOutput {
				return map[ids.NodeID]*validators.GetValidatorOutput{
					ids.EmptyNodeID: {
						NodeID:    ids.EmptyNodeID,
						PublicKey: nil,
						Weight:    110,
					},
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			sk, err := bls.NewSecretKey()
			require.NoError(err)

			var (
				weight                    uint64 = 100
				nonce                     uint64 = 0
				balance                   uint64 = 10
				validatorDB                      = memdb.New()
				validatorWeightDiffsDB           = memdb.New()
				validatorPublicKeyDiffsDB        = memdb.New()
				validatorsManager                = validators.NewManager()
				pk                               = bls.PublicFromSecretKey(sk)
				height                    uint64 = 1
				b                                = newBaseSubnetOnlyValidators(validatorDB)
			)

			vdr := &SubnetOnlyValidator{
				ValidationID: ids.GenerateTestID(),
				SubnetID:     ids.GenerateTestID(),
				NodeID:       ids.GenerateTestNodeID(),
				MinNonce:     nonce,
				Weight:       weight,
				Balance:      balance,
				PublicKey:    bls.PublicKeyToUncompressedBytes(pk),
				EndTime:      0,
			}

			test.setup(require, b, vdr)

			require.NoError(b.Write(height, validatorDB, validatorWeightDiffsDB, validatorPublicKeyDiffsDB, validatorsManager))

			gotVdr, err := getSubnetOnlyValidator(validatorDB, vdr.ValidationID)
			if test.expectedInValidatorDB {
				require.NoError(err)
				require.Equal(gotVdr, vdr)
			} else {
				require.ErrorIs(err, database.ErrNotFound)
			}

			iter := validatorWeightDiffsDB.NewIterator()
			defer iter.Release()
			gotValidatorWeightDiffDB := make(map[ids.NodeID]*ValidatorWeightDiff)
			for iter.Next() {
				key := iter.Key()
				gotSubnetID, gotHeight, gotNodeID, err := unmarshalDiffKey(key)
				require.NoError(err)
				require.Equal(height, gotHeight)
				require.Equal(vdr.SubnetID, gotSubnetID)

				val := iter.Value()
				weightDiff, err := unmarshalWeightDiff(val)
				require.NoError(err)

				gotValidatorWeightDiffDB[gotNodeID] = weightDiff
			}
			require.Equal(test.expectedValidatorWeightDiffsDB(vdr.NodeID), gotValidatorWeightDiffDB)

			iter = validatorPublicKeyDiffsDB.NewIterator()
			defer iter.Release()
			gotValidatorPublicKeyDiffsDB := make(map[ids.NodeID][]byte)
			for iter.Next() {
				key := iter.Key()
				gotSubnetID, gotHeight, gotNodeID, err := unmarshalDiffKey(key)
				require.NoError(err)
				require.Equal(height, gotHeight)
				require.Equal(vdr.SubnetID, gotSubnetID)

				gotValidatorPublicKeyDiffsDB[gotNodeID] = iter.Value()
			}
			require.Equal(test.expectedValidatorPublicKeyDiffsDB(vdr.NodeID, vdr.PublicKey), gotValidatorPublicKeyDiffsDB)

			vdrMap := validatorsManager.GetMap(vdr.SubnetID)
			require.Equal(test.expectedValidatorsManager(vdr), vdrMap)
		})
	}
}
