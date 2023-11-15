// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	initialTxID             = ids.GenerateTestID()
	initialNodeID           = ids.GenerateTestNodeID()
	initialTime             = time.Now().Round(time.Second)
	initialValidatorEndTime = initialTime.Add(28 * 24 * time.Hour)
)

func TestStateInitialization(t *testing.T) {
	require := require.New(t)
	s, db := newUninitializedState(require)

	shouldInit, err := s.(*state).shouldInit()
	require.NoError(err)
	require.True(shouldInit)

	require.NoError(s.(*state).doneInit())
	require.NoError(s.Commit())

	s = newStateFromDB(require, db)

	shouldInit, err = s.(*state).shouldInit()
	require.NoError(err)
	require.False(shouldInit)
}

func TestStateSyncGenesis(t *testing.T) {
	require := require.New(t)
	state, _ := newInitializedState(require)

	staker, err := state.GetCurrentValidator(constants.PrimaryNetworkID, initialNodeID)
	require.NoError(err)
	require.NotNil(staker)
	require.Equal(initialNodeID, staker.NodeID)

	delegatorIterator, err := state.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, initialNodeID)
	require.NoError(err)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)

	stakerIterator, err := state.GetCurrentStakerIterator()
	require.NoError(err)
	assertIteratorsEqual(t, NewSliceIterator(staker), stakerIterator)

	_, err = state.GetPendingValidator(constants.PrimaryNetworkID, initialNodeID)
	require.ErrorIs(err, database.ErrNotFound)

	delegatorIterator, err = state.GetPendingDelegatorIterator(constants.PrimaryNetworkID, initialNodeID)
	require.NoError(err)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)
}

func newInitializedState(require *require.Assertions) (State, database.Database) {
	s, db := newUninitializedState(require)

	initialValidator := &txs.AddValidatorTx{
		Validator: txs.Validator{
			NodeID: initialNodeID,
			Start:  uint64(initialTime.Unix()),
			End:    uint64(initialValidatorEndTime.Unix()),
			Wght:   units.Avax,
		},
		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: initialTxID},
				Out: &secp256k1fx.TransferOutput{
					Amt: units.Avax,
				},
			},
		},
		RewardsOwner:     &secp256k1fx.OutputOwners{},
		DelegationShares: reward.PercentDenominator,
	}
	initialValidatorTx := &txs.Tx{Unsigned: initialValidator}
	require.NoError(initialValidatorTx.Initialize(txs.Codec))

	initialChain := &txs.CreateChainTx{
		SubnetID:   constants.PrimaryNetworkID,
		ChainName:  "x",
		VMID:       constants.AVMID,
		SubnetAuth: &secp256k1fx.Input{},
	}
	initialChainTx := &txs.Tx{Unsigned: initialChain}
	require.NoError(initialChainTx.Initialize(txs.Codec))

	genesisBlkID := ids.GenerateTestID()
	genesisState := &genesis.Genesis{
		UTXOs: []*genesis.UTXO{
			{
				UTXO: avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        initialTxID,
						OutputIndex: 0,
					},
					Asset: avax.Asset{ID: initialTxID},
					Out: &secp256k1fx.TransferOutput{
						Amt: units.Schmeckle,
					},
				},
				Message: nil,
			},
		},
		Validators: []*txs.Tx{
			initialValidatorTx,
		},
		Chains: []*txs.Tx{
			initialChainTx,
		},
		Timestamp:     uint64(initialTime.Unix()),
		InitialSupply: units.Schmeckle + units.Avax,
	}

	genesisBlk, err := block.NewApricotCommitBlock(genesisBlkID, 0)
	require.NoError(err)
	require.NoError(s.(*state).syncGenesis(genesisBlk, genesisState))

	return s, db
}

func newUninitializedState(require *require.Assertions) (State, database.Database) {
	db := memdb.New()
	return newStateFromDB(require, db), db
}

func newStateFromDB(require *require.Assertions, db database.Database) State {
	execCfg, _ := config.GetExecutionConfig(nil)
	state, err := newState(
		db,
		metrics.Noop,
		validators.NewManager(),
		execCfg,
		&snow.Context{},
		prometheus.NewRegistry(),
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
	)
	require.NoError(err)
	require.NotNil(state)
	return state
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

// Tests PutCurrentValidator, DeleteCurrentValidator, GetCurrentValidator,
// ApplyValidatorWeightDiffs, ApplyValidatorPublicKeyDiffs
func TestStateAddRemoveValidator(t *testing.T) {
	require := require.New(t)

	state, _ := newInitializedState(require)

	var (
		numNodes  = 3
		subnetID  = ids.GenerateTestID()
		startTime = time.Now()
		endTime   = startTime.Add(24 * time.Hour)
		stakers   = make([]Staker, numNodes)
	)
	for i := 0; i < numNodes; i++ {
		stakers[i] = Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			Weight:          uint64(i + 1),
			StartTime:       startTime.Add(time.Duration(i) * time.Second),
			EndTime:         endTime.Add(time.Duration(i) * time.Second),
			PotentialReward: uint64(i + 1),
		}
		if i%2 == 0 {
			stakers[i].SubnetID = subnetID
		} else {
			sk, err := bls.NewSecretKey()
			require.NoError(err)
			stakers[i].PublicKey = bls.PublicFromSecretKey(sk)
			stakers[i].SubnetID = constants.PrimaryNetworkID
		}
	}

	type diff struct {
		addedValidators   []Staker
		addedDelegators   []Staker
		removedDelegators []Staker
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
			// Add a subnet validator
			addedValidators:             []Staker{stakers[0]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[0].NodeID: {
					NodeID: stakers[0].NodeID,
					Weight: stakers[0].Weight,
				},
			},
		},
		{
			// Remove a subnet validator
			removedValidators:           []Staker{stakers[0]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{ // Add a primary network validator
			addedValidators: []Staker{stakers[1]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[1].NodeID: {
					NodeID:    stakers[1].NodeID,
					PublicKey: stakers[1].PublicKey,
					Weight:    stakers[1].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Do nothing
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[1].NodeID: {
					NodeID:    stakers[1].NodeID,
					PublicKey: stakers[1].PublicKey,
					Weight:    stakers[1].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{ // Remove a primary network validator
			removedValidators:           []Staker{stakers[1]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Add 2 subnet validators and a primary network validator
			addedValidators: []Staker{stakers[0], stakers[1], stakers[2]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[1].NodeID: {
					NodeID:    stakers[1].NodeID,
					PublicKey: stakers[1].PublicKey,
					Weight:    stakers[1].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[0].NodeID: {
					NodeID: stakers[0].NodeID,
					Weight: stakers[0].Weight,
				},
				stakers[2].NodeID: {
					NodeID: stakers[2].NodeID,
					Weight: stakers[2].Weight,
				},
			},
		},
		{
			// Remove 2 subnet validators and a primary network validator.
			removedValidators:           []Staker{stakers[0], stakers[1], stakers[2]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
	}
	for currentIndex, diff := range diffs {
		for _, added := range diff.addedValidators {
			added := added
			state.PutCurrentValidator(&added)
		}
		for _, added := range diff.addedDelegators {
			added := added
			state.PutCurrentDelegator(&added)
		}
		for _, removed := range diff.removedDelegators {
			removed := removed
			state.DeleteCurrentDelegator(&removed)
		}
		for _, removed := range diff.removedValidators {
			removed := removed
			state.DeleteCurrentValidator(&removed)
		}

		currentHeight := uint64(currentIndex + 1)
		state.SetHeight(currentHeight)

		require.NoError(state.Commit())

		for _, added := range diff.addedValidators {
			gotValidator, err := state.GetCurrentValidator(added.SubnetID, added.NodeID)
			require.NoError(err)
			require.Equal(added, *gotValidator)
		}

		for _, removed := range diff.removedValidators {
			_, err := state.GetCurrentValidator(removed.SubnetID, removed.NodeID)
			require.ErrorIs(err, database.ErrNotFound)
		}

		for i := 0; i < currentIndex; i++ {
			prevDiff := diffs[i]
			prevHeight := uint64(i + 1)

			primaryValidatorSet := copyValidatorSet(diff.expectedPrimaryValidatorSet)
			require.NoError(state.ApplyValidatorWeightDiffs(
				context.Background(),
				primaryValidatorSet,
				currentHeight,
				prevHeight+1,
				constants.PrimaryNetworkID,
			))
			requireEqualWeightsValidatorSet(require, prevDiff.expectedPrimaryValidatorSet, primaryValidatorSet)

			require.NoError(state.ApplyValidatorPublicKeyDiffs(
				context.Background(),
				primaryValidatorSet,
				currentHeight,
				prevHeight+1,
			))
			requireEqualPublicKeysValidatorSet(require, prevDiff.expectedPrimaryValidatorSet, primaryValidatorSet)

			subnetValidatorSet := copyValidatorSet(diff.expectedSubnetValidatorSet)
			require.NoError(state.ApplyValidatorWeightDiffs(
				context.Background(),
				subnetValidatorSet,
				currentHeight,
				prevHeight+1,
				subnetID,
			))
			requireEqualWeightsValidatorSet(require, prevDiff.expectedSubnetValidatorSet, subnetValidatorSet)
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

func requireEqualWeightsValidatorSet(
	require *require.Assertions,
	expected map[ids.NodeID]*validators.GetValidatorOutput,
	actual map[ids.NodeID]*validators.GetValidatorOutput,
) {
	require.Len(actual, len(expected))
	for nodeID, expectedVdr := range expected {
		require.Contains(actual, nodeID)

		actualVdr := actual[nodeID]
		require.Equal(expectedVdr.NodeID, actualVdr.NodeID)
		require.Equal(expectedVdr.Weight, actualVdr.Weight)
	}
}

func requireEqualPublicKeysValidatorSet(
	require *require.Assertions,
	expected map[ids.NodeID]*validators.GetValidatorOutput,
	actual map[ids.NodeID]*validators.GetValidatorOutput,
) {
	require.Len(actual, len(expected))
	for nodeID, expectedVdr := range expected {
		require.Contains(actual, nodeID)

		actualVdr := actual[nodeID]
		require.Equal(expectedVdr.NodeID, actualVdr.NodeID)
		require.Equal(expectedVdr.PublicKey, actualVdr.PublicKey)
	}
}

func TestParsedStateBlock(t *testing.T) {
	require := require.New(t)

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
		blk, err := block.NewApricotProposalBlock(ids.GenerateTestID(), 1000, &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewApricotStandardBlock(ids.GenerateTestID(), 1000, []*txs.Tx{
			{
				Unsigned: &txs.RewardValidatorTx{
					TxID: ids.GenerateTestID(),
				},
			},
		})
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
		blk, err := block.NewBanffProposalBlock(time.Now(), ids.GenerateTestID(), 1000, &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewBanffStandardBlock(time.Now(), ids.GenerateTestID(), 1000, []*txs.Tx{
			{
				Unsigned: &txs.RewardValidatorTx{
					TxID: ids.GenerateTestID(),
				},
			},
		})
		require.NoError(err)
		blks = append(blks, blk)
	}

	for _, blk := range blks {
		stBlk := stateBlk{
			Blk:    blk,
			Bytes:  blk.Bytes(),
			Status: choices.Accepted,
		}

		stBlkBytes, err := block.GenesisCodec.Marshal(block.Version, &stBlk)
		require.NoError(err)

		gotBlk, _, isStateBlk, err := parseStoredBlock(stBlkBytes)
		require.NoError(err)
		require.True(isStateBlk)
		require.Equal(blk.ID(), gotBlk.ID())

		gotBlk, _, isStateBlk, err = parseStoredBlock(blk.Bytes())
		require.NoError(err)
		require.False(isStateBlk)
		require.Equal(blk.ID(), gotBlk.ID())
	}
}

func TestStateSubnetOwner(t *testing.T) {
	require := require.New(t)

	state, _ := newInitializedState(require)
	ctrl := gomock.NewController(t)

	var (
		owner1 = fx.NewMockOwner(ctrl)
		owner2 = fx.NewMockOwner(ctrl)

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

	state.AddSubnet(createSubnetTx)
	state.SetSubnetOwner(subnetID, owner1)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	state.SetSubnetOwner(subnetID, owner2)
	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)
}
