// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	initialTxID             = ids.GenerateTestID()
	initialNodeID           = ids.GenerateTestNodeID()
	initialTime             = time.Now().Round(time.Second)
	initialValidatorEndTime = initialTime.Add(28 * 24 * time.Hour)
)

func TestStateInitialization(t *testing.T) {
	assert := assert.New(t)
	state, db := newUninitializedState(assert)

	shouldInit, err := state.ShouldInit()
	assert.NoError(err)
	assert.True(shouldInit)

	assert.NoError(state.DoneInit())

	state = newStateFromDB(assert, db)

	shouldInit, err = state.ShouldInit()
	assert.NoError(err)
	assert.False(shouldInit)
}

func TestStateSyncGenesis(t *testing.T) {
	assert := assert.New(t)
	state, _ := newInitializedState(assert)

	staker, err := state.GetCurrentValidator(constants.PrimaryNetworkID, initialNodeID)
	assert.NoError(err)
	assert.NotNil(staker)
	assert.Equal(initialNodeID, staker.NodeID)

	delegatorIterator, err := state.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, initialNodeID)
	assert.NoError(err)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)

	stakerIterator, err := state.GetCurrentStakerIterator()
	assert.NoError(err)
	assertIteratorsEqual(t, NewSliceIterator(staker), stakerIterator)

	_, err = state.GetPendingValidator(constants.PrimaryNetworkID, initialNodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	delegatorIterator, err = state.GetPendingDelegatorIterator(constants.PrimaryNetworkID, initialNodeID)
	assert.NoError(err)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)
}

func TestGetValidatorWeightDiffs(t *testing.T) {
	assert := assert.New(t)
	stateIntf, _ := newInitializedState(assert)
	state := stateIntf.(*state)

	txID0 := ids.GenerateTestID()
	txID1 := ids.GenerateTestID()
	txID2 := ids.GenerateTestID()
	txID3 := ids.GenerateTestID()

	nodeID0 := ids.GenerateTestNodeID()

	subnetID0 := ids.GenerateTestID()

	type stakerDiff struct {
		validatorsToAdd    []*Staker
		delegatorsToAdd    []*Staker
		validatorsToRemove []*Staker
		delegatorsToRemove []*Staker

		expectedValidatorWeightDiffs map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff
	}
	stakerDiffs := []*stakerDiff{
		{
			validatorsToAdd: []*Staker{
				{
					TxID:     txID0,
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   1,
				},
			},
			expectedValidatorWeightDiffs: map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff{
				constants.PrimaryNetworkID: {
					nodeID0: {
						Decrease: false,
						Amount:   1,
					},
				},
			},
		},
		{
			validatorsToAdd: []*Staker{
				{
					TxID:     txID3,
					NodeID:   nodeID0,
					SubnetID: subnetID0,
					Weight:   10,
				},
			},
			delegatorsToAdd: []*Staker{
				{
					TxID:     txID1,
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   5,
				},
			},
			expectedValidatorWeightDiffs: map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff{
				constants.PrimaryNetworkID: {
					nodeID0: {
						Decrease: false,
						Amount:   5,
					},
				},
				subnetID0: {
					nodeID0: {
						Decrease: false,
						Amount:   10,
					},
				},
			},
		},
		{
			delegatorsToAdd: []*Staker{
				{
					TxID:     txID2,
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   15,
				},
			},
			delegatorsToRemove: []*Staker{
				{
					TxID:     txID1,
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   5,
				},
			},
			expectedValidatorWeightDiffs: map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff{
				constants.PrimaryNetworkID: {
					nodeID0: {
						Decrease: false,
						Amount:   10,
					},
				},
			},
		},
		{
			validatorsToRemove: []*Staker{
				{
					TxID:     txID0,
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   1,
				},
				{
					TxID:     txID3,
					NodeID:   nodeID0,
					SubnetID: subnetID0,
					Weight:   10,
				},
			},
			delegatorsToRemove: []*Staker{
				{
					TxID:     txID2,
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   15,
				},
			},
			expectedValidatorWeightDiffs: map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff{
				constants.PrimaryNetworkID: {
					nodeID0: {
						Decrease: true,
						Amount:   16,
					},
				},
				subnetID0: {
					nodeID0: {
						Decrease: true,
						Amount:   10,
					},
				},
			},
		},
		{},
	}

	for i, stakerDiff := range stakerDiffs {
		for _, validator := range stakerDiff.validatorsToAdd {
			state.PutCurrentValidator(validator)
		}
		for _, delegator := range stakerDiff.delegatorsToAdd {
			state.PutCurrentDelegator(delegator)
		}
		for _, validator := range stakerDiff.validatorsToRemove {
			state.DeleteCurrentValidator(validator)
		}
		for _, delegator := range stakerDiff.delegatorsToRemove {
			state.DeleteCurrentDelegator(delegator)
		}
		assert.NoError(state.Write(uint64(i + 1)))

		// Calling write again should not change the state.
		assert.NoError(state.Write(uint64(i + 1)))

		for j, stakerDiff := range stakerDiffs[:i+1] {
			for subnetID, expectedValidatorWeightDiffs := range stakerDiff.expectedValidatorWeightDiffs {
				validatorWeightDiffs, err := state.GetValidatorWeightDiffs(uint64(j+1), subnetID)
				assert.NoError(err)
				assert.Equal(expectedValidatorWeightDiffs, validatorWeightDiffs)
			}

			state.validatorDiffsCache.Flush()
		}
	}
}

func newInitializedState(assert *assert.Assertions) (State, database.Database) {
	state, db := newUninitializedState(assert)

	initialValidator := &txs.AddValidatorTx{
		Validator: validator.Validator{
			NodeID: initialNodeID,
			Start:  uint64(initialTime.Unix()),
			End:    uint64(initialValidatorEndTime.Unix()),
			Wght:   units.Avax,
		},
		Stake: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: initialTxID},
				Out: &secp256k1fx.TransferOutput{
					Amt: units.Avax,
				},
			},
		},
		RewardsOwner: &secp256k1fx.OutputOwners{},
		Shares:       reward.PercentDenominator,
	}
	initialValidatorTx := &txs.Tx{Unsigned: initialValidator}
	assert.NoError(initialValidatorTx.Sign(txs.Codec, nil))

	initialChain := &txs.CreateChainTx{
		SubnetID:   constants.PrimaryNetworkID,
		ChainName:  "x",
		VMID:       constants.AVMID,
		SubnetAuth: &secp256k1fx.Input{},
	}
	initialChainTx := &txs.Tx{Unsigned: initialChain}
	assert.NoError(initialChainTx.Sign(txs.Codec, nil))

	genesisBlkID := ids.GenerateTestID()
	genesisState := &genesis.State{
		UTXOs: []*avax.UTXO{
			{
				UTXOID: avax.UTXOID{
					TxID:        initialTxID,
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: initialTxID},
				Out: &secp256k1fx.TransferOutput{
					Amt: units.Schmeckle,
				},
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
	assert.NoError(state.SyncGenesis(genesisBlkID, genesisState))

	return state, db
}

func newUninitializedState(assert *assert.Assertions) (State, database.Database) {
	db := memdb.New()
	return newStateFromDB(assert, db), db
}

func newStateFromDB(assert *assert.Assertions, db database.Database) State {
	vdrs := validators.NewManager()
	assert.NoError(vdrs.Set(constants.PrimaryNetworkID, validators.NewSet()))

	state, err := New(
		db,
		prometheus.NewRegistry(),
		&config.Config{
			Validators: vdrs,
		},
		&snow.Context{},
		prometheus.NewGauge(prometheus.GaugeOpts{}),
		prometheus.NewGauge(prometheus.GaugeOpts{}),
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
	)
	assert.NoError(err)
	assert.NotNil(state)
	return state
}

func TestValidatorWeightDiff(t *testing.T) {
	type test struct {
		name      string
		ops       []func(*ValidatorWeightDiff) error
		shouldErr bool
		expected  ValidatorWeightDiff
	}

	tests := []test{
		{
			name:      "no ops",
			ops:       []func(*ValidatorWeightDiff) error{},
			shouldErr: false,
			expected:  ValidatorWeightDiff{},
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
			shouldErr: false,
			expected: ValidatorWeightDiff{
				Decrease: true,
				Amount:   2,
			},
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
			shouldErr: true,
			expected:  ValidatorWeightDiff{},
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
			shouldErr: false,
			expected: ValidatorWeightDiff{
				Decrease: false,
				Amount:   2,
			},
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
			shouldErr: true,
			expected:  ValidatorWeightDiff{},
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
			shouldErr: false,
			expected: ValidatorWeightDiff{
				Decrease: true,
				Amount:   2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			diff := &ValidatorWeightDiff{}
			errs := wrappers.Errs{}
			for _, op := range tt.ops {
				errs.Add(op(diff))
			}
			if tt.shouldErr {
				assert.Error(errs.Err)
				return
			}
			assert.NoError(errs.Err)
			assert.Equal(tt.expected, *diff)
		})
	}
}
