// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"maps"
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
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/logging"
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
		&config.Default,
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
		expectedValidatorDiffs map[subnetIDNodeID]*validatorDiff
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
			expectedValidatorDiffs: map[subnetIDNodeID]*validatorDiff{
				{
					subnetID: constants.PrimaryNetworkID,
					nodeID:   primaryNetworkCurrentValidatorStaker.NodeID,
				}: {
					weightDiff: ValidatorWeightDiff{
						Decrease: false,
						Amount:   primaryNetworkCurrentValidatorStaker.Weight,
					},
					prevPublicKey: nil,
					newPublicKey:  bls.PublicKeyToUncompressedBytes(primaryNetworkCurrentValidatorStaker.PublicKey),
				},
			},
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
			expectedValidatorDiffs: map[subnetIDNodeID]*validatorDiff{
				{
					subnetID: constants.PrimaryNetworkID,
					nodeID:   primaryNetworkCurrentValidatorStaker.NodeID,
				}: {
					weightDiff: ValidatorWeightDiff{
						Decrease: false,
						Amount:   primaryNetworkCurrentDelegatorStaker.Weight,
					},
					prevPublicKey: bls.PublicKeyToUncompressedBytes(primaryNetworkCurrentValidatorStaker.PublicKey),
					newPublicKey:  bls.PublicKeyToUncompressedBytes(primaryNetworkCurrentValidatorStaker.PublicKey),
				},
			},
		},
		"add pending primary network validator": {
			staker:                   primaryNetworkPendingValidatorStaker,
			addStakerTx:              addPrimaryNetworkValidator,
			expectedPendingValidator: primaryNetworkPendingValidatorStaker,
			expectedValidatorDiffs:   map[subnetIDNodeID]*validatorDiff{},
		},
		"add pending primary network delegator": {
			initialStakers:            []*Staker{primaryNetworkPendingValidatorStaker},
			initialTxs:                []*txs.Tx{addPrimaryNetworkValidator},
			staker:                    primaryNetworkPendingDelegatorStaker,
			addStakerTx:               addPrimaryNetworkDelegator,
			expectedPendingValidator:  primaryNetworkPendingValidatorStaker,
			expectedPendingDelegators: []*Staker{primaryNetworkPendingDelegatorStaker},
			expectedValidatorDiffs:    map[subnetIDNodeID]*validatorDiff{},
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
			expectedValidatorDiffs: map[subnetIDNodeID]*validatorDiff{
				{
					subnetID: subnetID,
					nodeID:   subnetCurrentValidatorStaker.NodeID,
				}: {
					weightDiff: ValidatorWeightDiff{
						Decrease: false,
						Amount:   subnetCurrentValidatorStaker.Weight,
					},
					prevPublicKey: nil,
					newPublicKey:  bls.PublicKeyToUncompressedBytes(primaryNetworkCurrentValidatorStaker.PublicKey),
				},
			},
		},
		"delete current primary network validator": {
			initialStakers: []*Staker{primaryNetworkCurrentValidatorStaker},
			initialTxs:     []*txs.Tx{addPrimaryNetworkValidator},
			staker:         primaryNetworkCurrentValidatorStaker,
			expectedValidatorDiffs: map[subnetIDNodeID]*validatorDiff{
				{
					subnetID: constants.PrimaryNetworkID,
					nodeID:   primaryNetworkCurrentValidatorStaker.NodeID,
				}: {
					weightDiff: ValidatorWeightDiff{
						Decrease: true,
						Amount:   primaryNetworkCurrentValidatorStaker.Weight,
					},
					prevPublicKey: bls.PublicKeyToUncompressedBytes(primaryNetworkCurrentValidatorStaker.PublicKey),
					newPublicKey:  nil,
				},
			},
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
			expectedValidatorDiffs: map[subnetIDNodeID]*validatorDiff{
				{
					subnetID: constants.PrimaryNetworkID,
					nodeID:   primaryNetworkCurrentValidatorStaker.NodeID,
				}: {
					weightDiff: ValidatorWeightDiff{
						Decrease: true,
						Amount:   primaryNetworkCurrentDelegatorStaker.Weight,
					},
					prevPublicKey: bls.PublicKeyToUncompressedBytes(primaryNetworkCurrentValidatorStaker.PublicKey),
					newPublicKey:  bls.PublicKeyToUncompressedBytes(primaryNetworkCurrentValidatorStaker.PublicKey),
				},
			},
		},
		"delete pending primary network validator": {
			initialStakers:         []*Staker{primaryNetworkPendingValidatorStaker},
			initialTxs:             []*txs.Tx{addPrimaryNetworkValidator},
			staker:                 primaryNetworkPendingValidatorStaker,
			expectedValidatorDiffs: map[subnetIDNodeID]*validatorDiff{},
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
			expectedValidatorDiffs:   map[subnetIDNodeID]*validatorDiff{},
		},
		"delete current subnet validator": {
			initialStakers: []*Staker{primaryNetworkCurrentValidatorStaker, subnetCurrentValidatorStaker},
			initialTxs:     []*txs.Tx{addPrimaryNetworkValidator, addSubnetValidator},
			staker:         subnetCurrentValidatorStaker,
			expectedValidatorDiffs: map[subnetIDNodeID]*validatorDiff{
				{
					subnetID: subnetID,
					nodeID:   subnetCurrentValidatorStaker.NodeID,
				}: {
					weightDiff: ValidatorWeightDiff{
						Decrease: true,
						Amount:   subnetCurrentValidatorStaker.Weight,
					},
					prevPublicKey: bls.PublicKeyToUncompressedBytes(primaryNetworkCurrentValidatorStaker.PublicKey),
					newPublicKey:  nil,
				},
			},
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

			validatorDiffs, err := state.calculateValidatorDiffs()
			require.NoError(err)
			require.Equal(test.expectedValidatorDiffs, validatorDiffs)

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

				for subnetIDNodeID, expectedDiff := range test.expectedValidatorDiffs {
					requireValidDiff := func(
						diffKey []byte,
						weightDiffs database.Database,
						publicKeyDiffs database.Database,
					) {
						t.Helper()

						weightDiffBytes, err := weightDiffs.Get(diffKey)
						if expectedDiff.weightDiff.Amount == 0 {
							require.ErrorIs(err, database.ErrNotFound)
						} else {
							require.NoError(err)

							weightDiff, err := unmarshalWeightDiff(weightDiffBytes)
							require.NoError(err)
							require.Equal(&expectedDiff.weightDiff, weightDiff)
						}

						publicKeyDiffBytes, err := publicKeyDiffs.Get(diffKey)
						if bytes.Equal(expectedDiff.prevPublicKey, expectedDiff.newPublicKey) {
							require.ErrorIs(err, database.ErrNotFound)
						} else {
							require.NoError(err)

							require.Equal(expectedDiff.prevPublicKey, publicKeyDiffBytes)
						}
					}
					requireValidDiff(
						marshalDiffKeyBySubnetID(subnetIDNodeID.subnetID, 1, subnetIDNodeID.nodeID),
						state.validatorWeightDiffsBySubnetIDDB,
						state.validatorPublicKeyDiffsBySubnetIDDB,
					)
					requireValidDiff(
						marshalDiffKeyByHeight(1, subnetIDNodeID.subnetID, subnetIDNodeID.nodeID),
						state.validatorWeightDiffsByHeightDB,
						state.validatorPublicKeyDiffsByHeightDB,
					)
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
		sk, err := localsigner.New()
		require.NoError(t, err)
		sig, err = signer.NewProofOfPossession(sk)
		require.NoError(t, err)
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
	type op struct {
		op     func(*ValidatorWeightDiff, uint64) error
		amount uint64
	}
	type test struct {
		name        string
		ops         []op
		expected    *ValidatorWeightDiff
		expectedErr error
	}

	var (
		add = (*ValidatorWeightDiff).Add
		sub = (*ValidatorWeightDiff).Sub
	)
	tests := []test{
		{
			name:     "no ops",
			expected: &ValidatorWeightDiff{},
		},
		{
			name: "simple decrease",
			ops: []op{
				{sub, 1},
				{sub, 1},
			},
			expected: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   2,
			},
		},
		{
			name: "decrease overflow",
			ops: []op{
				{sub, math.MaxUint64},
				{sub, 1},
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "simple increase",
			ops: []op{
				{add, 1},
				{add, 1},
			},
			expected: &ValidatorWeightDiff{
				Decrease: false,
				Amount:   2,
			},
		},
		{
			name: "increase overflow",
			ops: []op{
				{add, math.MaxUint64},
				{add, 1},
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "varied use",
			ops: []op{
				{add, 2}, // = 2
				{sub, 1}, // = 1
				{sub, 3}, // = -2
				{sub, 3}, // = -5
				{add, 1}, // = -4
				{add, 5}, // = 1
				{add, 1}, // = 2
				{sub, 2}, // = 0
				{sub, 2}, // = -2
			},
			expected: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			var (
				diff = &ValidatorWeightDiff{}
				errs = wrappers.Errs{}
			)
			for _, op := range tt.ops {
				errs.Add(op.op(diff, op.amount))
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
		sk, err := localsigner.New()
		require.NoError(err)

		timeOffset := time.Duration(i) * time.Second
		primaryStakers[i] = Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       sk.PublicKey(),
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
					t.Context(),
					primaryValidatorSet,
					currentHeight,
					prevHeight+1,
					constants.PrimaryNetworkID,
				))
				require.NoError(state.ApplyValidatorPublicKeyDiffs(
					t.Context(),
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
					t.Context(),
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
					t.Context(),
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
					t.Context(),
					subnetValidatorSet,
					currentHeight,
					prevHeight+1,
					subnetID,
				))

				require.NoError(state.ApplyValidatorPublicKeyDiffs(
					t.Context(),
					subnetValidatorSet,
					currentHeight,
					prevHeight+1,
					subnetID,
				))
				require.Equal(prevDiff.expectedSubnetValidatorSet, subnetValidatorSet)
			}

			// Checks applying diffs to all validator sets using height-based indices
			{
				allValidatorSets := make(map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
				if len(diff.expectedPrimaryValidatorSet) != 0 {
					allValidatorSets[constants.PrimaryNetworkID] = copyValidatorSet(diff.expectedPrimaryValidatorSet)
				}
				if len(diff.expectedSubnetValidatorSet) != 0 {
					allValidatorSets[subnetID] = copyValidatorSet(diff.expectedSubnetValidatorSet)
				}
				require.NoError(state.ApplyAllValidatorWeightDiffs(
					t.Context(),
					allValidatorSets,
					currentHeight,
					prevHeight+1,
				))
				require.NoError(state.ApplyAllValidatorPublicKeyDiffs(
					t.Context(),
					allValidatorSets,
					currentHeight,
					prevHeight+1,
				))

				expectedAllValidatorSets := make(map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
				if len(prevDiff.expectedPrimaryValidatorSet) != 0 {
					expectedAllValidatorSets[constants.PrimaryNetworkID] = prevDiff.expectedPrimaryValidatorSet
				}
				if len(prevDiff.expectedSubnetValidatorSet) != 0 {
					expectedAllValidatorSets[subnetID] = prevDiff.expectedSubnetValidatorSet
				}
				require.Equal(expectedAllValidatorSets, allValidatorSets)
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

func TestStateSubnetToL1Conversion(t *testing.T) {
	tests := []struct {
		name  string
		setup func(s *state, subnetID ids.ID, c SubnetToL1Conversion)
	}{
		{
			name: "in-memory",
			setup: func(s *state, subnetID ids.ID, c SubnetToL1Conversion) {
				s.SetSubnetToL1Conversion(subnetID, c)
			},
		},
		{
			name: "cache",
			setup: func(s *state, subnetID ids.ID, c SubnetToL1Conversion) {
				s.subnetToL1ConversionCache.Put(subnetID, c)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				require            = require.New(t)
				state              = newTestState(t, memdb.New())
				subnetID           = ids.GenerateTestID()
				expectedConversion = SubnetToL1Conversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         []byte{'a', 'd', 'd', 'r'},
				}
			)

			actualConversion, err := state.GetSubnetToL1Conversion(subnetID)
			require.ErrorIs(err, database.ErrNotFound)
			require.Zero(actualConversion)

			test.setup(state, subnetID, expectedConversion)

			actualConversion, err = state.GetSubnetToL1Conversion(subnetID)
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

// Verify that committing the state writes the L1 validator excess to the
// database and that loading the state fetches the L1 validator excess from the
// database.
func TestStateL1ValidatorExcessCommitAndLoad(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s := newTestState(t, db)

	const expectedL1ValidatorExcess gas.Gas = 10
	s.SetL1ValidatorExcess(expectedL1ValidatorExcess)
	require.NoError(s.Commit())

	s = newTestState(t, db)
	require.Equal(expectedL1ValidatorExcess, s.GetL1ValidatorExcess())
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

func TestL1Validators(t *testing.T) {
	l1Validator := L1Validator{
		ValidationID: ids.GenerateTestID(),
		SubnetID:     ids.GenerateTestID(),
		NodeID:       ids.GenerateTestNodeID(),
	}

	sk, err := localsigner.New()
	require.NoError(t, err)
	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	otherSK, err := localsigner.New()
	require.NoError(t, err)
	otherPK := otherSK.PublicKey()
	otherPKBytes := bls.PublicKeyToUncompressedBytes(otherPK)

	tests := []struct {
		name         string
		initial      []L1Validator
		l1Validators []L1Validator
	}{
		{
			name: "empty noop",
		},
		{
			name: "initially active not modified",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
		},
		{
			name: "initially inactive not modified",
			initial: []L1Validator{
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          ids.GenerateTestID(),
					NodeID:            ids.GenerateTestNodeID(),
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 0, // Inactive
				},
			},
		},
		{
			name: "initially active removed",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID: l1Validator.ValidationID,
					SubnetID:     l1Validator.SubnetID,
					NodeID:       l1Validator.NodeID,
					PublicKey:    pkBytes,
					Weight:       0, // Removed
				},
			},
		},
		{
			name: "initially inactive removed",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 0, // Inactive
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID: l1Validator.ValidationID,
					SubnetID:     l1Validator.SubnetID,
					NodeID:       l1Validator.NodeID,
					PublicKey:    pkBytes,
					Weight:       0, // Removed
				},
			},
		},
		{
			name: "increase active weight",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            2, // Increased
					EndAccumulatedFee: 1, // Active
				},
			},
		},
		{
			name: "increase inactive weight",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 0, // Inactive
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            2, // Increased
					EndAccumulatedFee: 0, // Inactive
				},
			},
		},
		{
			name: "decrease active weight",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            2, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Decreased
					EndAccumulatedFee: 1, // Active
				},
			},
		},
		{
			name: "deactivate",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 0, // Inactive
				},
			},
		},
		{
			name: "reactivate",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 0, // Inactive
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
		},
		{
			name: "update multiple times",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            2, // Not removed
					EndAccumulatedFee: 1, // Active
				},
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            3, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
		},
		{
			name: "change validationID",
			initial: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID: l1Validator.ValidationID,
					SubnetID:     l1Validator.SubnetID,
					NodeID:       l1Validator.NodeID,
					PublicKey:    pkBytes,
					Weight:       0, // Removed
				},
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         otherPKBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
			},
		},
		{
			name: "added and removed",
			l1Validators: []L1Validator{
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 1, // Active
				},
				{
					ValidationID: l1Validator.ValidationID,
					SubnetID:     l1Validator.SubnetID,
					NodeID:       l1Validator.NodeID,
					PublicKey:    pkBytes,
					Weight:       0, // Removed
				},
			},
		},
		{
			name: "add multiple inactive",
			l1Validators: []L1Validator{
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          l1Validator.SubnetID,
					NodeID:            ids.GenerateTestNodeID(),
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 0, // Inactive
				},
				{
					ValidationID:      l1Validator.ValidationID,
					SubnetID:          l1Validator.SubnetID,
					NodeID:            l1Validator.NodeID,
					PublicKey:         pkBytes,
					Weight:            1, // Not removed
					EndAccumulatedFee: 0, // Inactive
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			state := newTestState(t, db)

			var (
				initialL1Validators = make(map[ids.ID]L1Validator)
				subnetIDs           set.Set[ids.ID]
			)
			for _, l1Validator := range test.initial {
				// The codec creates zero length slices rather than leaving them
				// as nil, so we need to populate the slices for later reflect
				// based equality checks.
				l1Validator.RemainingBalanceOwner = []byte{}
				l1Validator.DeactivationOwner = []byte{}

				require.NoError(state.PutL1Validator(l1Validator))
				initialL1Validators[l1Validator.ValidationID] = l1Validator
				subnetIDs.Add(l1Validator.SubnetID)
			}

			state.SetHeight(0)
			require.NoError(state.Commit())

			d, err := NewDiffOn(state)
			require.NoError(err)

			expectedL1Validators := maps.Clone(initialL1Validators)
			for _, l1Validator := range test.l1Validators {
				l1Validator.RemainingBalanceOwner = []byte{}
				l1Validator.DeactivationOwner = []byte{}

				require.NoError(d.PutL1Validator(l1Validator))
				expectedL1Validators[l1Validator.ValidationID] = l1Validator
				subnetIDs.Add(l1Validator.SubnetID)
			}

			verifyChain := func(chain Chain) {
				for _, expectedL1Validator := range expectedL1Validators {
					if !expectedL1Validator.isDeleted() {
						continue
					}

					l1Validator, err := chain.GetL1Validator(expectedL1Validator.ValidationID)
					require.ErrorIs(err, database.ErrNotFound)
					require.Zero(l1Validator)
				}

				var (
					weights        = make(map[ids.ID]uint64)
					expectedActive []L1Validator
				)
				for _, expectedL1Validator := range expectedL1Validators {
					if expectedL1Validator.isDeleted() {
						continue
					}

					l1Validator, err := chain.GetL1Validator(expectedL1Validator.ValidationID)
					require.NoError(err)
					require.Equal(expectedL1Validator, l1Validator)

					has, err := chain.HasL1Validator(expectedL1Validator.SubnetID, expectedL1Validator.NodeID)
					require.NoError(err)
					require.True(has)

					weights[l1Validator.SubnetID] += l1Validator.Weight
					if expectedL1Validator.IsActive() {
						expectedActive = append(expectedActive, expectedL1Validator)
					}
				}
				utils.Sort(expectedActive)

				activeIterator, err := chain.GetActiveL1ValidatorsIterator()
				require.NoError(err)
				require.Equal(
					expectedActive,
					iterator.ToSlice(activeIterator),
				)

				require.Equal(len(expectedActive), chain.NumActiveL1Validators())

				for subnetID, expectedWeight := range weights {
					weight, err := chain.WeightOfL1Validators(subnetID)
					require.NoError(err)
					require.Equal(expectedWeight, weight)
				}
			}

			verifyChain(d)
			require.NoError(d.Apply(state))
			verifyChain(d)
			verifyChain(state)
			assertChainsEqual(t, state, d)

			state.SetHeight(1)
			require.NoError(state.Commit())
			verifyChain(d)
			verifyChain(state)
			assertChainsEqual(t, state, d)

			// Verify that the subnetID+nodeID -> validationID mapping is correct.
			var populatedSubnetIDNodeIDs set.Set[subnetIDNodeID]
			for _, l1Validator := range expectedL1Validators {
				if l1Validator.isDeleted() {
					continue
				}

				subnetIDNodeID := subnetIDNodeID{
					subnetID: l1Validator.SubnetID,
					nodeID:   l1Validator.NodeID,
				}
				populatedSubnetIDNodeIDs.Add(subnetIDNodeID)

				subnetIDNodeIDKey := subnetIDNodeID.Marshal()
				validatorID, err := database.GetID(state.subnetIDNodeIDDB, subnetIDNodeIDKey)
				require.NoError(err)
				require.Equal(l1Validator.ValidationID, validatorID)
			}
			for _, l1Validator := range expectedL1Validators {
				if !l1Validator.isDeleted() {
					continue
				}

				subnetIDNodeID := subnetIDNodeID{
					subnetID: l1Validator.SubnetID,
					nodeID:   l1Validator.NodeID,
				}
				if populatedSubnetIDNodeIDs.Contains(subnetIDNodeID) {
					continue
				}

				subnetIDNodeIDKey := subnetIDNodeID.Marshal()
				has, err := state.subnetIDNodeIDDB.Has(subnetIDNodeIDKey)
				require.NoError(err)
				require.False(has)
			}

			l1ValdiatorsToValidatorSet := func(
				l1Validators map[ids.ID]L1Validator,
				subnetID ids.ID,
			) map[ids.NodeID]*validators.GetValidatorOutput {
				validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput)
				for _, l1Validator := range l1Validators {
					if l1Validator.SubnetID != subnetID || l1Validator.isDeleted() {
						continue
					}

					nodeID := l1Validator.effectiveNodeID()
					vdr, ok := validatorSet[nodeID]
					if !ok {
						pk := l1Validator.effectivePublicKey()
						vdr = &validators.GetValidatorOutput{
							NodeID:    nodeID,
							PublicKey: pk,
						}
						validatorSet[nodeID] = vdr
					}
					vdr.Weight += l1Validator.Weight
				}
				return validatorSet
			}

			reloadedState := newTestState(t, db)
			for subnetID := range subnetIDs {
				expectedEndValidatorSet := l1ValdiatorsToValidatorSet(expectedL1Validators, subnetID)
				endValidatorSet := state.validators.GetMap(subnetID)
				require.Equal(expectedEndValidatorSet, endValidatorSet)

				reloadedEndValidatorSet := reloadedState.validators.GetMap(subnetID)
				require.Equal(expectedEndValidatorSet, reloadedEndValidatorSet)

				require.NoError(state.ApplyValidatorWeightDiffs(t.Context(), endValidatorSet, 1, 1, subnetID))
				require.NoError(state.ApplyValidatorPublicKeyDiffs(t.Context(), endValidatorSet, 1, 1, subnetID))

				initialValidatorSet := l1ValdiatorsToValidatorSet(initialL1Validators, subnetID)
				require.Equal(initialValidatorSet, endValidatorSet)
			}
		})
	}
}

// TestLoadL1ValidatorAndLegacy tests that the state can be loaded when there is
// a mix of legacy validators and L1 validators in the same subnet.
func TestLoadL1ValidatorAndLegacy(t *testing.T) {
	var (
		require         = require.New(t)
		db              = memdb.New()
		state           = newTestState(t, db)
		subnetID        = ids.GenerateTestID()
		weight   uint64 = 1
	)

	unsignedAddSubnetValidator := createPermissionlessValidatorTx(
		t,
		subnetID,
		txs.Validator{
			NodeID: defaultValidatorNodeID,
			End:    genesistest.DefaultValidatorEndTimeUnix,
			Wght:   weight,
		},
	)
	addSubnetValidator := &txs.Tx{Unsigned: unsignedAddSubnetValidator}
	require.NoError(addSubnetValidator.Initialize(txs.Codec))
	state.AddTx(addSubnetValidator, status.Committed)

	legacyStaker := &Staker{
		TxID:            addSubnetValidator.ID(),
		NodeID:          defaultValidatorNodeID,
		PublicKey:       nil,
		SubnetID:        subnetID,
		Weight:          weight,
		StartTime:       genesistest.DefaultValidatorStartTime,
		EndTime:         genesistest.DefaultValidatorEndTime,
		PotentialReward: 0,
	}
	require.NoError(state.PutCurrentValidator(legacyStaker))

	sk, err := localsigner.New()
	require.NoError(err)
	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	l1Validator := L1Validator{
		ValidationID:          ids.GenerateTestID(),
		SubnetID:              legacyStaker.SubnetID,
		NodeID:                ids.GenerateTestNodeID(),
		PublicKey:             pkBytes,
		RemainingBalanceOwner: utils.RandomBytes(32),
		DeactivationOwner:     utils.RandomBytes(32),
		StartTime:             1,
		Weight:                2,
		MinNonce:              3,
		EndAccumulatedFee:     4,
	}
	require.NoError(state.PutL1Validator(l1Validator))

	state.SetHeight(1)
	require.NoError(state.Commit())

	expectedValidatorSet := state.validators.GetMap(subnetID)

	state = newTestState(t, db)

	validatorSet := state.validators.GetMap(subnetID)
	require.Equal(expectedValidatorSet, validatorSet)
}

// TestL1ValidatorAfterLegacyRemoval verifies that a legacy validator can be
// replaced by an L1 validator in the same block.
func TestL1ValidatorAfterLegacyRemoval(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	state := newTestState(t, db)

	legacyStaker := &Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          defaultValidatorNodeID,
		PublicKey:       nil,
		SubnetID:        ids.GenerateTestID(),
		Weight:          1,
		StartTime:       genesistest.DefaultValidatorStartTime,
		EndTime:         genesistest.DefaultValidatorEndTime,
		PotentialReward: 0,
	}
	require.NoError(state.PutCurrentValidator(legacyStaker))

	state.SetHeight(1)
	require.NoError(state.Commit())

	state.DeleteCurrentValidator(legacyStaker)

	l1Validator := L1Validator{
		ValidationID:          ids.GenerateTestID(),
		SubnetID:              legacyStaker.SubnetID,
		NodeID:                legacyStaker.NodeID,
		PublicKey:             utils.RandomBytes(bls.PublicKeyLen),
		RemainingBalanceOwner: utils.RandomBytes(32),
		DeactivationOwner:     utils.RandomBytes(32),
		StartTime:             1,
		Weight:                2,
		MinNonce:              3,
		EndAccumulatedFee:     4,
	}
	require.NoError(state.PutL1Validator(l1Validator))

	state.SetHeight(2)
	require.NoError(state.Commit())
}

func TestGetCurrentValidators(t *testing.T) {
	subnetID1 := ids.GenerateTestID()
	subnetID2 := ids.GenerateTestID()
	subnetIDs := []ids.ID{subnetID1, subnetID2}

	sk, err := localsigner.New()
	require.NoError(t, err)
	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	otherSK, err := localsigner.New()
	require.NoError(t, err)
	otherPK := otherSK.PublicKey()
	otherPKBytes := bls.PublicKeyToUncompressedBytes(otherPK)
	now := time.Now()

	tests := []struct {
		name         string
		initial      []*Staker
		l1Validators []L1Validator
	}{
		{
			name: "empty noop",
		},
		{
			name: "initial stakers in same subnet",
			initial: []*Staker{
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID1,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: pk,
					Weight:    1,
					StartTime: now,
				},
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID1,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: otherPK,
					Weight:    1,
					StartTime: now.Add(1 * time.Second),
				},
			},
		},
		{
			name: "initial stakers in different subnets",
			initial: []*Staker{
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID1,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: pk,
					Weight:    1,
					StartTime: now,
				},
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID2,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: otherPK,
					Weight:    1,
					StartTime: now.Add(1 * time.Second),
				},
			},
		},
		{
			name: "L1 validators with the same SubnetID",
			l1Validators: []L1Validator{
				{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID1,
					NodeID:       ids.GenerateTestNodeID(),
					StartTime:    uint64(now.Unix()),
					PublicKey:    pkBytes,
					Weight:       1,
				},
				{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID1,
					NodeID:       ids.GenerateTestNodeID(),
					PublicKey:    otherPKBytes,
					StartTime:    uint64(now.Unix()) + 1,
					Weight:       1,
				},
			},
		},
		{
			name: "L1 validators with different SubnetIDs",
			l1Validators: []L1Validator{
				{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID1,
					NodeID:       ids.GenerateTestNodeID(),
					StartTime:    uint64(now.Unix()),
					PublicKey:    pkBytes,
					Weight:       1,
				},
				{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID2,
					NodeID:       ids.GenerateTestNodeID(),
					PublicKey:    otherPKBytes,
					StartTime:    uint64(now.Unix()) + 1,
					Weight:       1,
				},
			},
		},
		{
			name: "initial stakers and L1 validators mixed",
			initial: []*Staker{
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID1,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: pk,
					Weight:    123123,
					StartTime: now,
				},
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID2,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: pk,
					Weight:    1,
					StartTime: now.Add(1 * time.Second),
				},
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID1,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: otherPK,
					Weight:    0,
					StartTime: now.Add(2 * time.Second),
				},
			},
			l1Validators: []L1Validator{
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          subnetID1,
					NodeID:            ids.GenerateTestNodeID(),
					StartTime:         uint64(now.Unix()),
					PublicKey:         pkBytes,
					Weight:            1,
					EndAccumulatedFee: 1,
					MinNonce:          2,
				},
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          subnetID2,
					NodeID:            ids.GenerateTestNodeID(),
					PublicKey:         otherPKBytes,
					StartTime:         uint64(now.Unix()) + 1,
					Weight:            0,
					EndAccumulatedFee: 0,
				},
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          subnetID1,
					NodeID:            ids.GenerateTestNodeID(),
					PublicKey:         pkBytes,
					StartTime:         uint64(now.Unix()) + 2,
					Weight:            1,
					EndAccumulatedFee: 0,
				},
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          subnetID1,
					NodeID:            ids.GenerateTestNodeID(),
					PublicKey:         otherPKBytes,
					StartTime:         uint64(now.Unix()) + 3,
					Weight:            0,
					EndAccumulatedFee: 1,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			state := newTestState(t, db)

			stakersLenBySubnetID := make(map[ids.ID]int)
			stakersByTxID := make(map[ids.ID]*Staker)
			for _, staker := range test.initial {
				primaryStaker := &Staker{
					TxID:      ids.GenerateTestID(),
					SubnetID:  constants.PrimaryNetworkID,
					NodeID:    staker.NodeID,
					PublicKey: staker.PublicKey,
					Weight:    5,
					// start primary network staker 1 second before the subnet staker
					StartTime: staker.StartTime.Add(-1 * time.Second),
				}
				require.NoError(state.PutCurrentValidator(primaryStaker))
				require.NoError(state.PutCurrentValidator(staker))

				stakersByTxID[staker.TxID] = staker
				stakersLenBySubnetID[staker.SubnetID]++
			}

			l1ValidatorsLenBySubnetID := make(map[ids.ID]int)
			l1ValidatorsByVID := make(map[ids.ID]L1Validator)
			for _, l1Validator := range test.l1Validators {
				// The codec creates zero length slices rather than leaving them
				// as nil, so we need to populate the slices for later reflect
				// based equality checks.
				l1Validator.RemainingBalanceOwner = []byte{}
				l1Validator.DeactivationOwner = []byte{}

				require.NoError(state.PutL1Validator(l1Validator))

				if l1Validator.Weight == 0 {
					continue
				}
				l1ValidatorsByVID[l1Validator.ValidationID] = l1Validator
				l1ValidatorsLenBySubnetID[l1Validator.SubnetID]++
			}

			state.SetHeight(0)
			require.NoError(state.Commit())

			for _, subnetID := range subnetIDs {
				baseStakers, currentValidators, height, err := state.GetCurrentValidators(t.Context(), subnetID)
				require.NoError(err)
				require.Equal(uint64(0), height)
				require.Len(baseStakers, stakersLenBySubnetID[subnetID])
				require.Len(currentValidators, l1ValidatorsLenBySubnetID[subnetID])

				for i, currentStaker := range baseStakers {
					require.Equalf(stakersByTxID[currentStaker.TxID], currentStaker, "index %d", i)
				}

				for i, currentValidator := range currentValidators {
					require.Equalf(l1ValidatorsByVID[currentValidator.ValidationID], currentValidator, "index %d", i)
				}
			}
		})
	}
}
