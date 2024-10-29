// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx/fxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var defaultValidatorNodeID = ids.GenerateTestNodeID()

func newTestState(t testing.TB, db database.Database) *state {
	s, err := New(
		db,
		genesistest.NewBytes(t, genesistest.Config{
			NodeIDs: []ids.NodeID{defaultValidatorNodeID},
		}),
		prometheus.NewRegistry(),
		validators.NewManager(),
		upgradetest.GetConfig(upgradetest.Latest),
		&config.DefaultExecutionConfig,
		&snow.Context{
			NetworkID: constants.UnitTestID,
			NodeID:    ids.GenerateTestNodeID(),
			Log:       logging.NoLog{},
		},
		metrics.Noop,
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
	)
	require.NoError(t, err)
	require.IsType(t, (*state)(nil), s)
	return s.(*state)
}

func TestStateSyncGenesis(t *testing.T) {
	require := require.New(t)
	state := newTestState(t, memdb.New())

	staker, err := state.GetCurrentValidator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(err)
	require.NotNil(staker)
	require.Equal(defaultValidatorNodeID, staker.NodeID)

	delegatorIterator, err := state.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(err)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)

	stakerIterator, err := state.GetCurrentStakerIterator()
	require.NoError(err)
	require.Equal(
		[]*Staker{staker},
		iterator.ToSlice(stakerIterator),
	)

	_, err = state.GetPendingValidator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.ErrorIs(err, database.ErrNotFound)

	delegatorIterator, err = state.GetPendingDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(err)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)
}

// Whenever we add or remove a staker, a number of on-disk data structures
// should be updated.
//
// This test verifies that the on-disk data structures are updated as expected.
func TestState_writeStakers(t *testing.T) {
	const (
		primaryValidatorDuration = 28 * 24 * time.Hour
		primaryDelegatorDuration = 14 * 24 * time.Hour
		subnetValidatorDuration  = 21 * 24 * time.Hour

		primaryValidatorReward = iota
		primaryDelegatorReward
		subnetValidatorReward
	)
	var (
		primaryValidatorStartTime   = time.Now().Truncate(time.Second)
		primaryValidatorEndTime     = primaryValidatorStartTime.Add(primaryValidatorDuration)
		primaryValidatorEndTimeUnix = uint64(primaryValidatorEndTime.Unix())

		primaryDelegatorStartTime   = primaryValidatorStartTime
		primaryDelegatorEndTime     = primaryDelegatorStartTime.Add(primaryDelegatorDuration)
		primaryDelegatorEndTimeUnix = uint64(primaryDelegatorEndTime.Unix())

		subnetValidatorStartTime   = primaryValidatorStartTime
		subnetValidatorEndTime     = subnetValidatorStartTime.Add(subnetValidatorDuration)
		subnetValidatorEndTimeUnix = uint64(subnetValidatorEndTime.Unix())

		primaryValidatorData = txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			End:    primaryValidatorEndTimeUnix,
			Wght:   1234,
		}
		primaryDelegatorData = txs.Validator{
			NodeID: primaryValidatorData.NodeID,
			End:    primaryDelegatorEndTimeUnix,
			Wght:   6789,
		}
		subnetValidatorData = txs.Validator{
			NodeID: primaryValidatorData.NodeID,
			End:    subnetValidatorEndTimeUnix,
			Wght:   9876,
		}

		subnetID = ids.GenerateTestID()
	)

	unsignedAddPrimaryNetworkValidator := createPermissionlessValidatorTx(t, constants.PrimaryNetworkID, primaryValidatorData)
	addPrimaryNetworkValidator := &txs.Tx{Unsigned: unsignedAddPrimaryNetworkValidator}
	require.NoError(t, addPrimaryNetworkValidator.Initialize(txs.Codec))

	primaryNetworkPendingValidatorStaker, err := NewPendingStaker(
		addPrimaryNetworkValidator.ID(),
		unsignedAddPrimaryNetworkValidator,
	)
	require.NoError(t, err)

	primaryNetworkCurrentValidatorStaker, err := NewCurrentStaker(
		addPrimaryNetworkValidator.ID(),
		unsignedAddPrimaryNetworkValidator,
		primaryValidatorStartTime,
		primaryValidatorReward,
	)
	require.NoError(t, err)

	unsignedAddPrimaryNetworkDelegator := createPermissionlessDelegatorTx(constants.PrimaryNetworkID, primaryDelegatorData)
	addPrimaryNetworkDelegator := &txs.Tx{Unsigned: unsignedAddPrimaryNetworkDelegator}
	require.NoError(t, addPrimaryNetworkDelegator.Initialize(txs.Codec))

	primaryNetworkPendingDelegatorStaker, err := NewPendingStaker(
		addPrimaryNetworkDelegator.ID(),
		unsignedAddPrimaryNetworkDelegator,
	)
	require.NoError(t, err)

	primaryNetworkCurrentDelegatorStaker, err := NewCurrentStaker(
		addPrimaryNetworkDelegator.ID(),
		unsignedAddPrimaryNetworkDelegator,
		primaryDelegatorStartTime,
		primaryDelegatorReward,
	)
	require.NoError(t, err)

	unsignedAddSubnetValidator := createPermissionlessValidatorTx(t, subnetID, subnetValidatorData)
	addSubnetValidator := &txs.Tx{Unsigned: unsignedAddSubnetValidator}
	require.NoError(t, addSubnetValidator.Initialize(txs.Codec))

	subnetCurrentValidatorStaker, err := NewCurrentStaker(
		addSubnetValidator.ID(),
		unsignedAddSubnetValidator,
		subnetValidatorStartTime,
		subnetValidatorReward,
	)
	require.NoError(t, err)

	tests := map[string]struct {
		initialStakers []*Staker
		initialTxs     []*txs.Tx

		// Staker to insert or remove
		staker      *Staker
		addStakerTx *txs.Tx // If tx is nil, the staker is being removed

		// Check that the staker is duly stored/removed in P-chain state
		expectedCurrentValidator  *Staker
		expectedPendingValidator  *Staker
		expectedCurrentDelegators []*Staker
		expectedPendingDelegators []*Staker

		// Check that the validator entry has been set correctly in the
		// in-memory validator set.
		expectedValidatorSetOutput *validators.GetValidatorOutput

		// Check whether weight/bls keys diffs are duly stored
		expectedWeightDiff    *ValidatorWeightDiff
		expectedPublicKeyDiff maybe.Maybe[*bls.PublicKey]
	}{
		"add current primary network validator": {
			staker:                   primaryNetworkCurrentValidatorStaker,
			addStakerTx:              addPrimaryNetworkValidator,
			expectedCurrentValidator: primaryNetworkCurrentValidatorStaker,
			expectedValidatorSetOutput: &validators.GetValidatorOutput{
				NodeID:    primaryNetworkCurrentValidatorStaker.NodeID,
				PublicKey: primaryNetworkCurrentValidatorStaker.PublicKey,
				Weight:    primaryNetworkCurrentValidatorStaker.Weight,
			},
			expectedWeightDiff: &ValidatorWeightDiff{
				Decrease: false,
				Amount:   primaryNetworkCurrentValidatorStaker.Weight,
			},
			expectedPublicKeyDiff: maybe.Some[*bls.PublicKey](nil),
		},
		"add current primary network delegator": {
			initialStakers:            []*Staker{primaryNetworkCurrentValidatorStaker},
			initialTxs:                []*txs.Tx{addPrimaryNetworkValidator},
			staker:                    primaryNetworkCurrentDelegatorStaker,
			addStakerTx:               addPrimaryNetworkDelegator,
			expectedCurrentValidator:  primaryNetworkCurrentValidatorStaker,
			expectedCurrentDelegators: []*Staker{primaryNetworkCurrentDelegatorStaker},
			expectedValidatorSetOutput: &validators.GetValidatorOutput{
				NodeID:    primaryNetworkCurrentValidatorStaker.NodeID,
				PublicKey: primaryNetworkCurrentValidatorStaker.PublicKey,
				Weight:    primaryNetworkCurrentValidatorStaker.Weight + primaryNetworkCurrentDelegatorStaker.Weight,
			},
			expectedWeightDiff: &ValidatorWeightDiff{
				Decrease: false,
				Amount:   primaryNetworkCurrentDelegatorStaker.Weight,
			},
		},
		"add pending primary network validator": {
			staker:                   primaryNetworkPendingValidatorStaker,
			addStakerTx:              addPrimaryNetworkValidator,
			expectedPendingValidator: primaryNetworkPendingValidatorStaker,
		},
		"add pending primary network delegator": {
			initialStakers:            []*Staker{primaryNetworkPendingValidatorStaker},
			initialTxs:                []*txs.Tx{addPrimaryNetworkValidator},
			staker:                    primaryNetworkPendingDelegatorStaker,
			addStakerTx:               addPrimaryNetworkDelegator,
			expectedPendingValidator:  primaryNetworkPendingValidatorStaker,
			expectedPendingDelegators: []*Staker{primaryNetworkPendingDelegatorStaker},
		},
		"add current subnet validator": {
			initialStakers:           []*Staker{primaryNetworkCurrentValidatorStaker},
			initialTxs:               []*txs.Tx{addPrimaryNetworkValidator},
			staker:                   subnetCurrentValidatorStaker,
			addStakerTx:              addSubnetValidator,
			expectedCurrentValidator: subnetCurrentValidatorStaker,
			expectedValidatorSetOutput: &validators.GetValidatorOutput{
				NodeID:    subnetCurrentValidatorStaker.NodeID,
				PublicKey: primaryNetworkCurrentValidatorStaker.PublicKey,
				Weight:    subnetCurrentValidatorStaker.Weight,
			},
			expectedWeightDiff: &ValidatorWeightDiff{
				Decrease: false,
				Amount:   subnetCurrentValidatorStaker.Weight,
			},
			expectedPublicKeyDiff: maybe.Some[*bls.PublicKey](nil),
		},
		"delete current primary network validator": {
			initialStakers: []*Staker{primaryNetworkCurrentValidatorStaker},
			initialTxs:     []*txs.Tx{addPrimaryNetworkValidator},
			staker:         primaryNetworkCurrentValidatorStaker,
			expectedWeightDiff: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   primaryNetworkCurrentValidatorStaker.Weight,
			},
			expectedPublicKeyDiff: maybe.Some(primaryNetworkCurrentValidatorStaker.PublicKey),
		},
		"delete current primary network delegator": {
			initialStakers: []*Staker{
				primaryNetworkCurrentValidatorStaker,
				primaryNetworkCurrentDelegatorStaker,
			},
			initialTxs: []*txs.Tx{
				addPrimaryNetworkValidator,
				addPrimaryNetworkDelegator,
			},
			staker:                   primaryNetworkCurrentDelegatorStaker,
			expectedCurrentValidator: primaryNetworkCurrentValidatorStaker,
			expectedValidatorSetOutput: &validators.GetValidatorOutput{
				NodeID:    primaryNetworkCurrentValidatorStaker.NodeID,
				PublicKey: primaryNetworkCurrentValidatorStaker.PublicKey,
				Weight:    primaryNetworkCurrentValidatorStaker.Weight,
			},
			expectedWeightDiff: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   primaryNetworkCurrentDelegatorStaker.Weight,
			},
		},
		"delete pending primary network validator": {
			initialStakers: []*Staker{primaryNetworkPendingValidatorStaker},
			initialTxs:     []*txs.Tx{addPrimaryNetworkValidator},
			staker:         primaryNetworkPendingValidatorStaker,
		},
		"delete pending primary network delegator": {
			initialStakers: []*Staker{
				primaryNetworkPendingValidatorStaker,
				primaryNetworkPendingDelegatorStaker,
			},
			initialTxs: []*txs.Tx{
				addPrimaryNetworkValidator,
				addPrimaryNetworkDelegator,
			},
			staker:                   primaryNetworkPendingDelegatorStaker,
			expectedPendingValidator: primaryNetworkPendingValidatorStaker,
		},
		"delete current subnet validator": {
			initialStakers: []*Staker{primaryNetworkCurrentValidatorStaker, subnetCurrentValidatorStaker},
			initialTxs:     []*txs.Tx{addPrimaryNetworkValidator, addSubnetValidator},
			staker:         subnetCurrentValidatorStaker,
			expectedWeightDiff: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   subnetCurrentValidatorStaker.Weight,
			},
			expectedPublicKeyDiff: maybe.Some[*bls.PublicKey](primaryNetworkCurrentValidatorStaker.PublicKey),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			state := newTestState(t, db)

			addOrDeleteStaker := func(staker *Staker, add bool) {
				if add {
					switch {
					case staker.Priority.IsCurrentValidator():
						require.NoError(state.PutCurrentValidator(staker))
					case staker.Priority.IsPendingValidator():
						require.NoError(state.PutPendingValidator(staker))
					case staker.Priority.IsCurrentDelegator():
						state.PutCurrentDelegator(staker)
					case staker.Priority.IsPendingDelegator():
						state.PutPendingDelegator(staker)
					}
				} else {
					switch {
					case staker.Priority.IsCurrentValidator():
						state.DeleteCurrentValidator(staker)
					case staker.Priority.IsPendingValidator():
						state.DeletePendingValidator(staker)
					case staker.Priority.IsCurrentDelegator():
						state.DeleteCurrentDelegator(staker)
					case staker.Priority.IsPendingDelegator():
						state.DeletePendingDelegator(staker)
					}
				}
			}

			// create and store the initial stakers
			for _, staker := range test.initialStakers {
				addOrDeleteStaker(staker, true)
			}
			for _, tx := range test.initialTxs {
				state.AddTx(tx, status.Committed)
			}

			state.SetHeight(0)
			require.NoError(state.Commit())

			// create and store the staker under test
			addOrDeleteStaker(test.staker, test.addStakerTx != nil)
			if test.addStakerTx != nil {
				state.AddTx(test.addStakerTx, status.Committed)
			}

			state.SetHeight(1)
			require.NoError(state.Commit())

			// Perform the checks once immediately after committing to the
			// state, and once after re-loading the state from disk.
			for i := 0; i < 2; i++ {
				currentValidator, err := state.GetCurrentValidator(test.staker.SubnetID, test.staker.NodeID)
				if test.expectedCurrentValidator == nil {
					require.ErrorIs(err, database.ErrNotFound)

					if test.staker.SubnetID == constants.PrimaryNetworkID {
						// Uptimes are only considered for primary network validators
						_, _, err := state.GetUptime(test.staker.NodeID)
						require.ErrorIs(err, database.ErrNotFound)
					}
				} else {
					require.NoError(err)
					require.Equal(test.expectedCurrentValidator, currentValidator)

					if test.staker.SubnetID == constants.PrimaryNetworkID {
						// Uptimes are only considered for primary network validators
						upDuration, lastUpdated, err := state.GetUptime(currentValidator.NodeID)
						require.NoError(err)
						require.Zero(upDuration)
						require.Equal(currentValidator.StartTime, lastUpdated)
					}
				}

				pendingValidator, err := state.GetPendingValidator(test.staker.SubnetID, test.staker.NodeID)
				if test.expectedPendingValidator == nil {
					require.ErrorIs(err, database.ErrNotFound)
				} else {
					require.NoError(err)
					require.Equal(test.expectedPendingValidator, pendingValidator)
				}

				it, err := state.GetCurrentDelegatorIterator(test.staker.SubnetID, test.staker.NodeID)
				require.NoError(err)
				require.Equal(
					test.expectedCurrentDelegators,
					iterator.ToSlice(it),
				)

				it, err = state.GetPendingDelegatorIterator(test.staker.SubnetID, test.staker.NodeID)
				require.NoError(err)
				require.Equal(
					test.expectedPendingDelegators,
					iterator.ToSlice(it),
				)

				require.Equal(
					test.expectedValidatorSetOutput,
					state.validators.GetMap(test.staker.SubnetID)[test.staker.NodeID],
				)

				diffKey := marshalDiffKey(test.staker.SubnetID, 1, test.staker.NodeID)
				weightDiffBytes, err := state.validatorWeightDiffsDB.Get(diffKey)
				if test.expectedWeightDiff == nil {
					require.ErrorIs(err, database.ErrNotFound)
				} else {
					require.NoError(err)

					weightDiff, err := unmarshalWeightDiff(weightDiffBytes)
					require.NoError(err)
					require.Equal(test.expectedWeightDiff, weightDiff)
				}

				publicKeyDiffBytes, err := state.validatorPublicKeyDiffsDB.Get(diffKey)
				if test.expectedPublicKeyDiff.IsNothing() {
					require.ErrorIs(err, database.ErrNotFound)
				} else {
					require.NoError(err)

					expectedPublicKeyDiff := test.expectedPublicKeyDiff.Value()
					if expectedPublicKeyDiff != nil {
						require.Equal(expectedPublicKeyDiff, bls.PublicKeyFromValidUncompressedBytes(publicKeyDiffBytes))
					} else {
						require.Empty(publicKeyDiffBytes)
					}
				}

				// re-load the state from disk for the second iteration
				state = newTestState(t, db)
			}
		})
	}
}

func createPermissionlessValidatorTx(t testing.TB, subnetID ids.ID, validatorsData txs.Validator) *txs.AddPermissionlessValidatorTx {
	var sig signer.Signer = &signer.Empty{}
	if subnetID == constants.PrimaryNetworkID {
		sk, err := bls.NewSecretKey()
		require.NoError(t, err)
		sig = signer.NewProofOfPossession(sk)
	}

	return &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: constants.PlatformChainID,
				Outs:         []*avax.TransferableOutput{},
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID:        ids.GenerateTestID(),
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						In: &secp256k1fx.TransferInput{
							Amt: 2 * units.KiloAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{1},
							},
						},
					},
				},
				Memo: types.JSONByteSlice{},
			},
		},
		Validator: validatorsData,
		Subnet:    subnetID,
		Signer:    sig,

		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: ids.GenerateTestID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2 * units.KiloAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.GenerateTestShortID(),
						},
					},
				},
			},
		},
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
		DelegationShares: reward.PercentDenominator,
	}
}

func createPermissionlessDelegatorTx(subnetID ids.ID, delegatorData txs.Validator) *txs.AddPermissionlessDelegatorTx {
	return &txs.AddPermissionlessDelegatorTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: constants.PlatformChainID,
				Outs:         []*avax.TransferableOutput{},
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID:        ids.GenerateTestID(),
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						In: &secp256k1fx.TransferInput{
							Amt: 2 * units.KiloAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{1},
							},
						},
					},
				},
				Memo: types.JSONByteSlice{},
			},
		},
		Validator: delegatorData,
		Subnet:    subnetID,

		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: ids.GenerateTestID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2 * units.KiloAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.GenerateTestShortID(),
						},
					},
				},
			},
		},
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
	}
}

func TestValidatorWeightDiff(t *testing.T) {
	type test struct {
		name        string
		ops         []func(*ValidatorWeightDiff) error
		expected    *ValidatorWeightDiff
		expectedErr error
	}

	tests := []test{
		{
			name:        "no ops",
			ops:         []func(*ValidatorWeightDiff) error{},
			expected:    &ValidatorWeightDiff{},
			expectedErr: nil,
		},
		{
			name: "simple decrease",
			ops: []func(*ValidatorWeightDiff) error{
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1)
				},
			},
			expected: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   2,
			},
			expectedErr: nil,
		},
		{
			name: "decrease overflow",
			ops: []func(*ValidatorWeightDiff) error{
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, math.MaxUint64)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1)
				},
			},
			expected:    &ValidatorWeightDiff{},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "simple increase",
			ops: []func(*ValidatorWeightDiff) error{
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1)
				},
			},
			expected: &ValidatorWeightDiff{
				Decrease: false,
				Amount:   2,
			},
			expectedErr: nil,
		},
		{
			name: "increase overflow",
			ops: []func(*ValidatorWeightDiff) error{
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, math.MaxUint64)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1)
				},
			},
			expected:    &ValidatorWeightDiff{},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "varied use",
			ops: []func(*ValidatorWeightDiff) error{
				// Add to 0
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 2) // Value 2
				},
				// Subtract from positive number
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1) // Value 1
				},
				// Subtract from positive number
				// to make it negative
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 3) // Value -2
				},
				// Subtract from a negative number
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 3) // Value -5
				},
				// Add to a negative number
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1) // Value -4
				},
				// Add to a negative number
				// to make it positive
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 5) // Value 1
				},
				// Add to a positive number
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1) // Value 2
				},
				// Get to zero
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 2) // Value 0
				},
				// Subtract from zero
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 2) // Value -2
				},
			},
			expected: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   2,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			diff := &ValidatorWeightDiff{}
			errs := wrappers.Errs{}
			for _, op := range tt.ops {
				errs.Add(op(diff))
			}
			require.ErrorIs(errs.Err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, diff)
		})
	}
}

func TestState_ApplyValidatorDiffs(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())

	var (
		numNodes       = 5
		subnetID       = ids.GenerateTestID()
		startTime      = time.Now()
		endTime        = startTime.Add(24 * time.Hour)
		primaryStakers = make([]Staker, numNodes)
		subnetStakers  = make([]Staker, numNodes)
	)
	for i := range primaryStakers {
		sk, err := bls.NewSecretKey()
		require.NoError(err)

		timeOffset := time.Duration(i) * time.Second
		primaryStakers[i] = Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       bls.PublicFromSecretKey(sk),
			SubnetID:        constants.PrimaryNetworkID,
			Weight:          uint64(i + 1),
			StartTime:       startTime.Add(timeOffset),
			EndTime:         endTime.Add(timeOffset),
			PotentialReward: uint64(i + 1),
		}
	}
	for i, primaryStaker := range primaryStakers {
		subnetStakers[i] = Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          primaryStaker.NodeID,
			PublicKey:       nil, // Key is inherited from the primary network
			SubnetID:        subnetID,
			Weight:          uint64(i + 1),
			StartTime:       primaryStaker.StartTime,
			EndTime:         primaryStaker.EndTime,
			PotentialReward: uint64(i + 1),
		}
	}

	type diff struct {
		addedValidators   []Staker
		removedValidators []Staker

		expectedPrimaryValidatorSet map[ids.NodeID]*validators.GetValidatorOutput
		expectedSubnetValidatorSet  map[ids.NodeID]*validators.GetValidatorOutput
	}
	diffs := []diff{
		{
			// Do nothing
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Add primary validator 0
			addedValidators: []Staker{primaryStakers[0]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				primaryStakers[0].NodeID: {
					NodeID:    primaryStakers[0].NodeID,
					PublicKey: primaryStakers[0].PublicKey,
					Weight:    primaryStakers[0].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Add subnet validator 0
			addedValidators: []Staker{subnetStakers[0]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				primaryStakers[0].NodeID: {
					NodeID:    primaryStakers[0].NodeID,
					PublicKey: primaryStakers[0].PublicKey,
					Weight:    primaryStakers[0].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				subnetStakers[0].NodeID: {
					NodeID:    subnetStakers[0].NodeID,
					PublicKey: primaryStakers[0].PublicKey,
					Weight:    subnetStakers[0].Weight,
				},
			},
		},
		{
			// Remove subnet validator 0
			removedValidators: []Staker{subnetStakers[0]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				primaryStakers[0].NodeID: {
					NodeID:    primaryStakers[0].NodeID,
					PublicKey: primaryStakers[0].PublicKey,
					Weight:    primaryStakers[0].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Add primary network validator 1, and subnet validator 1
			addedValidators: []Staker{primaryStakers[1], subnetStakers[1]},
			// Remove primary network validator 0, and subnet validator 1
			removedValidators: []Staker{primaryStakers[0], subnetStakers[1]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				primaryStakers[1].NodeID: {
					NodeID:    primaryStakers[1].NodeID,
					PublicKey: primaryStakers[1].PublicKey,
					Weight:    primaryStakers[1].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Add primary network validator 2, and subnet validator 2
			addedValidators: []Staker{primaryStakers[2], subnetStakers[2]},
			// Remove primary network validator 1
			removedValidators: []Staker{primaryStakers[1]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				primaryStakers[2].NodeID: {
					NodeID:    primaryStakers[2].NodeID,
					PublicKey: primaryStakers[2].PublicKey,
					Weight:    primaryStakers[2].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				subnetStakers[2].NodeID: {
					NodeID:    subnetStakers[2].NodeID,
					PublicKey: primaryStakers[2].PublicKey,
					Weight:    subnetStakers[2].Weight,
				},
			},
		},
		{
			// Add primary network and subnet validators 3 & 4
			addedValidators: []Staker{primaryStakers[3], primaryStakers[4], subnetStakers[3], subnetStakers[4]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				primaryStakers[2].NodeID: {
					NodeID:    primaryStakers[2].NodeID,
					PublicKey: primaryStakers[2].PublicKey,
					Weight:    primaryStakers[2].Weight,
				},
				primaryStakers[3].NodeID: {
					NodeID:    primaryStakers[3].NodeID,
					PublicKey: primaryStakers[3].PublicKey,
					Weight:    primaryStakers[3].Weight,
				},
				primaryStakers[4].NodeID: {
					NodeID:    primaryStakers[4].NodeID,
					PublicKey: primaryStakers[4].PublicKey,
					Weight:    primaryStakers[4].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				subnetStakers[2].NodeID: {
					NodeID:    subnetStakers[2].NodeID,
					PublicKey: primaryStakers[2].PublicKey,
					Weight:    subnetStakers[2].Weight,
				},
				subnetStakers[3].NodeID: {
					NodeID:    subnetStakers[3].NodeID,
					PublicKey: primaryStakers[3].PublicKey,
					Weight:    subnetStakers[3].Weight,
				},
				subnetStakers[4].NodeID: {
					NodeID:    subnetStakers[4].NodeID,
					PublicKey: primaryStakers[4].PublicKey,
					Weight:    subnetStakers[4].Weight,
				},
			},
		},
		{
			// Remove primary network and subnet validators 2 & 3 & 4
			removedValidators: []Staker{
				primaryStakers[2], primaryStakers[3], primaryStakers[4],
				subnetStakers[2], subnetStakers[3], subnetStakers[4],
			},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Do nothing
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
	}
	for currentIndex, diff := range diffs {
		d, err := NewDiffOn(state)
		require.NoError(err)

		var expectedValidators set.Set[subnetIDNodeID]
		for _, added := range diff.addedValidators {
			require.NoError(d.PutCurrentValidator(&added))

			expectedValidators.Add(subnetIDNodeID{
				subnetID: added.SubnetID,
				nodeID:   added.NodeID,
			})
		}
		for _, removed := range diff.removedValidators {
			d.DeleteCurrentValidator(&removed)

			expectedValidators.Remove(subnetIDNodeID{
				subnetID: removed.SubnetID,
				nodeID:   removed.NodeID,
			})
		}

		require.NoError(d.Apply(state))

		currentHeight := uint64(currentIndex + 1)
		state.SetHeight(currentHeight)

		require.NoError(state.Commit())

		// Verify that the current state is as expected.
		for _, added := range diff.addedValidators {
			subnetNodeID := subnetIDNodeID{
				subnetID: added.SubnetID,
				nodeID:   added.NodeID,
			}
			if !expectedValidators.Contains(subnetNodeID) {
				continue
			}

			gotValidator, err := state.GetCurrentValidator(added.SubnetID, added.NodeID)
			require.NoError(err)
			require.Equal(added, *gotValidator)
		}

		for _, removed := range diff.removedValidators {
			_, err := state.GetCurrentValidator(removed.SubnetID, removed.NodeID)
			require.ErrorIs(err, database.ErrNotFound)
		}

		primaryValidatorSet := state.validators.GetMap(constants.PrimaryNetworkID)
		delete(primaryValidatorSet, defaultValidatorNodeID) // Ignore the genesis validator
		require.Equal(diff.expectedPrimaryValidatorSet, primaryValidatorSet)

		require.Equal(diff.expectedSubnetValidatorSet, state.validators.GetMap(subnetID))

		// Verify that applying diffs against the current state results in the
		// expected state.
		for i := 0; i < currentIndex; i++ {
			prevDiff := diffs[i]
			prevHeight := uint64(i + 1)

			{
				primaryValidatorSet := copyValidatorSet(diff.expectedPrimaryValidatorSet)
				require.NoError(state.ApplyValidatorWeightDiffs(
					context.Background(),
					primaryValidatorSet,
					currentHeight,
					prevHeight+1,
					constants.PrimaryNetworkID,
				))
				require.NoError(state.ApplyValidatorPublicKeyDiffs(
					context.Background(),
					primaryValidatorSet,
					currentHeight,
					prevHeight+1,
					constants.PrimaryNetworkID,
				))
				require.Equal(prevDiff.expectedPrimaryValidatorSet, primaryValidatorSet)
			}

			{
				legacySubnetValidatorSet := copyValidatorSet(diff.expectedSubnetValidatorSet)
				require.NoError(state.ApplyValidatorWeightDiffs(
					context.Background(),
					legacySubnetValidatorSet,
					currentHeight,
					prevHeight+1,
					subnetID,
				))

				// Update the public keys of the subnet validators with the current
				// primary network validator public keys
				for nodeID, vdr := range legacySubnetValidatorSet {
					if primaryVdr, ok := diff.expectedPrimaryValidatorSet[nodeID]; ok {
						vdr.PublicKey = primaryVdr.PublicKey
					} else {
						vdr.PublicKey = nil
					}
				}

				require.NoError(state.ApplyValidatorPublicKeyDiffs(
					context.Background(),
					legacySubnetValidatorSet,
					currentHeight,
					prevHeight+1,
					constants.PrimaryNetworkID,
				))
				require.Equal(prevDiff.expectedSubnetValidatorSet, legacySubnetValidatorSet)
			}

			{
				subnetValidatorSet := copyValidatorSet(diff.expectedSubnetValidatorSet)
				require.NoError(state.ApplyValidatorWeightDiffs(
					context.Background(),
					subnetValidatorSet,
					currentHeight,
					prevHeight+1,
					subnetID,
				))

				require.NoError(state.ApplyValidatorPublicKeyDiffs(
					context.Background(),
					subnetValidatorSet,
					currentHeight,
					prevHeight+1,
					subnetID,
				))
				require.Equal(prevDiff.expectedSubnetValidatorSet, subnetValidatorSet)
			}
		}
	}
}

func copyValidatorSet(
	input map[ids.NodeID]*validators.GetValidatorOutput,
) map[ids.NodeID]*validators.GetValidatorOutput {
	result := make(map[ids.NodeID]*validators.GetValidatorOutput, len(input))
	for nodeID, vdr := range input {
		vdrCopy := *vdr
		result[nodeID] = &vdrCopy
	}
	return result
}

func TestParsedStateBlock(t *testing.T) {
	var (
		require = require.New(t)
		blks    = makeBlocks(require)
	)

	for _, blk := range blks {
		stBlk := stateBlk{
			Bytes:  blk.Bytes(),
			Status: choices.Accepted,
		}

		stBlkBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &stBlk)
		require.NoError(err)

		gotBlk, isStateBlk, err := parseStoredBlock(stBlkBytes)
		require.NoError(err)
		require.True(isStateBlk)
		require.Equal(blk.ID(), gotBlk.ID())

		gotBlk, isStateBlk, err = parseStoredBlock(blk.Bytes())
		require.NoError(err)
		require.False(isStateBlk)
		require.Equal(blk.ID(), gotBlk.ID())
	}
}

func TestReindexBlocks(t *testing.T) {
	var (
		require = require.New(t)
		s       = newTestState(t, memdb.New())
		blks    = makeBlocks(require)
	)

	// Populate the blocks using the legacy format.
	for _, blk := range blks {
		stBlk := stateBlk{
			Bytes:  blk.Bytes(),
			Status: choices.Accepted,
		}
		stBlkBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &stBlk)
		require.NoError(err)

		blkID := blk.ID()
		require.NoError(s.blockDB.Put(blkID[:], stBlkBytes))
	}

	// Convert the indices to the new format.
	require.NoError(s.ReindexBlocks(&sync.Mutex{}, logging.NoLog{}))

	// Verify that the blocks are stored in the new format.
	for _, blk := range blks {
		blkID := blk.ID()
		blkBytes, err := s.blockDB.Get(blkID[:])
		require.NoError(err)

		parsedBlk, err := block.Parse(block.GenesisCodec, blkBytes)
		require.NoError(err)
		require.Equal(blkID, parsedBlk.ID())
	}

	// Verify that the flag has been written to disk to allow skipping future
	// reindexings.
	reindexed, err := s.singletonDB.Has(BlocksReindexedKey)
	require.NoError(err)
	require.True(reindexed)
}

func TestStateSubnetOwner(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())
	ctrl := gomock.NewController(t)

	var (
		owner1 = fxmock.NewOwner(ctrl)
		owner2 = fxmock.NewOwner(ctrl)

		createSubnetTx = &txs.Tx{
			Unsigned: &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{},
				Owner:  owner1,
			},
		}

		subnetID = createSubnetTx.ID()
	)

	owner, err := state.GetSubnetOwner(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(owner)

	state.AddSubnet(subnetID)
	state.SetSubnetOwner(subnetID, owner1)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	state.SetSubnetOwner(subnetID, owner2)
	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)
}

func TestStateSubnetConversion(t *testing.T) {
	tests := []struct {
		name  string
		setup func(s *state, subnetID ids.ID, c SubnetConversion)
	}{
		{
			name: "in-memory",
			setup: func(s *state, subnetID ids.ID, c SubnetConversion) {
				s.SetSubnetConversion(subnetID, c)
			},
		},
		{
			name: "cache",
			setup: func(s *state, subnetID ids.ID, c SubnetConversion) {
				s.subnetConversionCache.Put(subnetID, c)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				require            = require.New(t)
				state              = newTestState(t, memdb.New())
				subnetID           = ids.GenerateTestID()
				expectedConversion = SubnetConversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         []byte{'a', 'd', 'd', 'r'},
				}
			)

			actualConversion, err := state.GetSubnetConversion(subnetID)
			require.ErrorIs(err, database.ErrNotFound)
			require.Zero(actualConversion)

			test.setup(state, subnetID, expectedConversion)

			actualConversion, err = state.GetSubnetConversion(subnetID)
			require.NoError(err)
			require.Equal(expectedConversion, actualConversion)
		})
	}
}

func makeBlocks(require *require.Assertions) []block.Block {
	var blks []block.Block
	{
		blk, err := block.NewApricotAbortBlock(ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewApricotAtomicBlock(ids.GenerateTestID(), 1000, &txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{
				Time: 1000,
			},
		})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewApricotCommitBlock(ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		tx := &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		}
		require.NoError(tx.Initialize(txs.Codec))
		blk, err := block.NewApricotProposalBlock(ids.GenerateTestID(), 1000, tx)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		tx := &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		}
		require.NoError(tx.Initialize(txs.Codec))
		blk, err := block.NewApricotStandardBlock(ids.GenerateTestID(), 1000, []*txs.Tx{tx})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewBanffAbortBlock(time.Now(), ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewBanffCommitBlock(time.Now(), ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		tx := &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		}
		require.NoError(tx.Initialize(txs.Codec))

		blk, err := block.NewBanffProposalBlock(time.Now(), ids.GenerateTestID(), 1000, tx, []*txs.Tx{})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		tx := &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		}
		require.NoError(tx.Initialize(txs.Codec))

		blk, err := block.NewBanffStandardBlock(time.Now(), ids.GenerateTestID(), 1000, []*txs.Tx{tx})
		require.NoError(err)
		blks = append(blks, blk)
	}
	return blks
}

// Verify that committing the state writes the fee state to the database and
// that loading the state fetches the fee state from the database.
func TestStateFeeStateCommitAndLoad(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s := newTestState(t, db)

	expectedFeeState := gas.State{
		Capacity: 1,
		Excess:   2,
	}
	s.SetFeeState(expectedFeeState)
	require.NoError(s.Commit())

	s = newTestState(t, db)
	require.Equal(expectedFeeState, s.GetFeeState())
}

// Verify that committing the state writes the sov excess to the database and
// that loading the state fetches the sov excess from the database.
func TestStateSoVExcessCommitAndLoad(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s := newTestState(t, db)

	const expectedSoVExcess gas.Gas = 10
	s.SetSoVExcess(expectedSoVExcess)
	require.NoError(s.Commit())

	s = newTestState(t, db)
	require.Equal(expectedSoVExcess, s.GetSoVExcess())
}

// Verify that committing the state writes the accrued fees to the database and
// that loading the state fetches the accrued fees from the database.
func TestStateAccruedFeesCommitAndLoad(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s := newTestState(t, db)

	expectedAccruedFees := uint64(1)
	s.SetAccruedFees(expectedAccruedFees)
	require.NoError(s.Commit())

	s = newTestState(t, db)
	require.Equal(expectedAccruedFees, s.GetAccruedFees())
}

func TestMarkAndIsInitialized(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	defaultIsInitialized, err := isInitialized(db)
	require.NoError(err)
	require.False(defaultIsInitialized)

	require.NoError(markInitialized(db))

	isInitializedAfterMarking, err := isInitialized(db)
	require.NoError(err)
	require.True(isInitializedAfterMarking)
}

// Verify that reading from the database returns the same value that was written
// to it.
func TestPutAndGetFeeState(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	defaultFeeState, err := getFeeState(db)
	require.NoError(err)
	require.Equal(gas.State{}, defaultFeeState)

	//nolint:gosec // This does not require a secure random number generator
	expectedFeeState := gas.State{
		Capacity: gas.Gas(rand.Uint64()),
		Excess:   gas.Gas(rand.Uint64()),
	}
	require.NoError(putFeeState(db, expectedFeeState))

	actualFeeState, err := getFeeState(db)
	require.NoError(err)
	require.Equal(expectedFeeState, actualFeeState)
}

func TestGetFeeStateErrors(t *testing.T) {
	tests := []struct {
		value       []byte
		expectedErr error
	}{
		{
			value: []byte{
				// truncated codec version
				0x00,
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			value: []byte{
				// codec version
				0x00, 0x00,
				// truncated capacity
				0x12, 0x34, 0x56, 0x78,
			},
			expectedErr: wrappers.ErrInsufficientLength,
		},
		{
			value: []byte{
				// codec version
				0x00, 0x00,
				// capacity
				0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78,
				// excess
				0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78,
				// extra bytes
				0x00,
			},
			expectedErr: codec.ErrExtraSpace,
		},
	}
	for _, test := range tests {
		t.Run(test.expectedErr.Error(), func(t *testing.T) {
			var (
				require = require.New(t)
				db      = memdb.New()
			)
			require.NoError(db.Put(FeeStateKey, test.value))

			actualState, err := getFeeState(db)
			require.Equal(gas.State{}, actualState)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

// Verify that committing the state writes the expiry changes to the database
// and that loading the state fetches the expiry from the database.
func TestStateExpiryCommitAndLoad(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s := newTestState(t, db)

	// Populate an entry.
	expiry := ExpiryEntry{
		Timestamp: 1,
	}
	s.PutExpiry(expiry)
	require.NoError(s.Commit())

	// Verify that the entry was written and loaded correctly.
	s = newTestState(t, db)
	has, err := s.HasExpiry(expiry)
	require.NoError(err)
	require.True(has)

	// Delete an entry.
	s.DeleteExpiry(expiry)
	require.NoError(s.Commit())

	// Verify that the entry was deleted correctly.
	s = newTestState(t, db)
	has, err = s.HasExpiry(expiry)
	require.NoError(err)
	require.False(has)
}
