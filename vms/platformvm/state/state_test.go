// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"strconv"
	"testing"
	"time"

	stdmath "math"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
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

	shouldInit, err := s.(*merkleState).shouldInit()
	require.NoError(err)
	require.True(shouldInit)

	require.NoError(s.(*merkleState).doneInit())
	require.NoError(s.Commit())

	s = newStateFromDB(require, db)

	shouldInit, err = s.(*merkleState).shouldInit()
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

func TestGetValidatorWeightDiffs(t *testing.T) {
	require := require.New(t)
	stateIntf, _ := newInitializedState(require)
	state := stateIntf.(*merkleState)

	tx0 := &txs.Tx{
		Unsigned: &txs.AddValidatorTx{}, // don't really care to fill it up
	}
	tx0.SetBytes([]byte{0x1}, []byte(strconv.Itoa(0)))

	tx1 := &txs.Tx{
		Unsigned: &txs.AddDelegatorTx{}, // don't really care to fill it up
	}
	tx1.SetBytes([]byte{0x1}, []byte(strconv.Itoa(1)))

	tx2 := &txs.Tx{
		Unsigned: &txs.AddDelegatorTx{}, // don't really care to fill it up
	}
	tx2.SetBytes([]byte{0x1}, []byte(strconv.Itoa(2)))

	tx3 := &txs.Tx{
		Unsigned: &txs.AddValidatorTx{}, // don't really care to fill it up
	}
	tx3.SetBytes([]byte{0x1}, []byte(strconv.Itoa(3)))

	nodeID0 := ids.GenerateTestNodeID()
	subnetID0 := ids.GenerateTestID()

	type stakerDiff struct {
		validatorsToAdd    []*Staker
		valTxs             []*txs.Tx
		delegatorsToAdd    []*Staker
		delTxs             []*txs.Tx
		validatorsToRemove []*Staker
		delegatorsToRemove []*Staker

		expectedValidatorWeightDiffs map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff
	}
	stakerDiffs := []*stakerDiff{
		{
			validatorsToAdd: []*Staker{
				{
					TxID:     tx0.ID(),
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   1,
				},
			},
			valTxs: []*txs.Tx{tx0},
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
					TxID:     tx3.ID(),
					NodeID:   nodeID0,
					SubnetID: subnetID0,
					Weight:   10,
				},
			},
			valTxs: []*txs.Tx{tx3},
			delegatorsToAdd: []*Staker{
				{
					TxID:     tx1.ID(),
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   5,
				},
			},
			delTxs: []*txs.Tx{tx1},
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
					TxID:     tx2.ID(),
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   15,
				},
			},
			delTxs: []*txs.Tx{tx2},
			delegatorsToRemove: []*Staker{
				{
					TxID:     tx1.ID(),
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
					TxID:     tx0.ID(),
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   1,
				},
				{
					TxID:     tx3.ID(),
					NodeID:   nodeID0,
					SubnetID: subnetID0,
					Weight:   10,
				},
			},
			valTxs: []*txs.Tx{tx0, tx3},
			delegatorsToRemove: []*Staker{
				{
					TxID:     tx2.ID(),
					NodeID:   nodeID0,
					SubnetID: constants.PrimaryNetworkID,
					Weight:   15,
				},
			},
			delTxs: []*txs.Tx{tx2},
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
		for _, valTx := range stakerDiff.valTxs {
			state.AddTx(valTx, status.Committed)
		}
		for _, delTx := range stakerDiff.delTxs {
			state.AddTx(delTx, status.Committed)
		}
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
		state.SetHeight(uint64(i + 1))
		require.NoError(state.Commit())

		// Calling write again should not change the state.
		state.SetHeight(uint64(i + 1))
		require.NoError(state.Commit())

		for j, stakerDiff := range stakerDiffs[:i+1] {
			for subnetID, expectedValidatorWeightDiffs := range stakerDiff.expectedValidatorWeightDiffs {
				validatorWeightDiffs, err := state.GetValidatorWeightDiffs(uint64(j+1), subnetID)
				require.NoError(err)
				require.Equal(expectedValidatorWeightDiffs, validatorWeightDiffs)
			}

			state.validatorWeightDiffsCache.Flush()
		}
	}
}

func TestGetValidatorPublicKeyDiffs(t *testing.T) {
	require := require.New(t)
	stateIntf, _ := newInitializedState(require)
	state := stateIntf.(*merkleState)

	var (
		numNodes = 6
		valTxs   = make([]*txs.Tx, numNodes)
		nodeIDs  = make([]ids.NodeID, numNodes)
		sks      = make([]*bls.SecretKey, numNodes)
		pks      = make([]*bls.PublicKey, numNodes)
		pkBytes  = make([][]byte, numNodes)
		err      error
	)
	for i := 0; i < numNodes; i++ {
		valTxs[i] = &txs.Tx{
			Unsigned: &txs.AddValidatorTx{}, // don't really care to fill it up
		}
		valTxs[i].SetBytes([]byte{0x1}, []byte(strconv.Itoa(i)))
		nodeIDs[i] = ids.GenerateTestNodeID()
		sks[i], err = bls.NewSecretKey()
		require.NoError(err)
		pks[i] = bls.PublicFromSecretKey(sks[i])
		pkBytes[i] = bls.PublicKeyToBytes(pks[i])
	}

	type stakerDiff struct {
		validatorsToAdd        []*Staker
		validatorsTxsToAdd     []*txs.Tx
		validatorsToRemove     []*Staker
		expectedPublicKeyDiffs map[ids.NodeID]*bls.PublicKey
	}
	stakerDiffs := []*stakerDiff{
		{
			// Add two validators
			validatorsToAdd: []*Staker{
				{
					TxID:      valTxs[0].ID(),
					NodeID:    nodeIDs[0],
					Weight:    1,
					PublicKey: pks[0],
				},
				{
					TxID:      valTxs[1].ID(),
					NodeID:    nodeIDs[1],
					Weight:    10,
					PublicKey: pks[1],
				},
			},
			validatorsTxsToAdd:     []*txs.Tx{valTxs[0], valTxs[1]},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{},
		},
		{
			// Remove a validator
			validatorsToRemove: []*Staker{
				{
					TxID:      valTxs[0].ID(),
					NodeID:    nodeIDs[0],
					Weight:    1,
					PublicKey: pks[0],
				},
			},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{
				nodeIDs[0]: pks[0],
			},
		},
		{
			// Add 2 validators and remove a validator
			validatorsToAdd: []*Staker{
				{
					TxID:      valTxs[2].ID(),
					NodeID:    nodeIDs[2],
					Weight:    10,
					PublicKey: pks[2],
				},
				{
					TxID:      valTxs[3].ID(),
					NodeID:    nodeIDs[3],
					Weight:    10,
					PublicKey: pks[3],
				},
			},
			validatorsTxsToAdd: []*txs.Tx{valTxs[2], valTxs[3]},
			validatorsToRemove: []*Staker{
				{
					TxID:      valTxs[1].ID(),
					NodeID:    nodeIDs[1],
					Weight:    10,
					PublicKey: pks[1],
				},
			},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{
				nodeIDs[1]: pks[1],
			},
		},
		{
			// Remove 2 validators and add a validator
			validatorsToAdd: []*Staker{
				{
					TxID:      valTxs[4].ID(),
					NodeID:    nodeIDs[4],
					Weight:    10,
					PublicKey: pks[4],
				},
			},
			validatorsTxsToAdd: []*txs.Tx{valTxs[4]},
			validatorsToRemove: []*Staker{
				{
					TxID:      valTxs[2].ID(),
					NodeID:    nodeIDs[2],
					Weight:    10,
					PublicKey: pks[2],
				},
				{
					TxID:      valTxs[3].ID(),
					NodeID:    nodeIDs[3],
					Weight:    10,
					PublicKey: pks[3],
				},
			},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{
				nodeIDs[2]: pks[2],
				nodeIDs[3]: pks[3],
			},
		},
		{
			// Add a validator with no pub key
			validatorsToAdd: []*Staker{
				{
					TxID:      valTxs[5].ID(),
					NodeID:    nodeIDs[5],
					Weight:    10,
					PublicKey: nil,
				},
			},
			validatorsTxsToAdd:     []*txs.Tx{valTxs[5]},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{},
		},
		{
			// Remove a validator with no pub key
			validatorsToRemove: []*Staker{
				{
					TxID:      valTxs[5].ID(),
					NodeID:    nodeIDs[5],
					Weight:    10,
					PublicKey: nil,
				},
			},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{},
		},
	}

	for i, stakerDiff := range stakerDiffs {
		for _, tx := range stakerDiff.validatorsTxsToAdd {
			state.AddTx(tx, status.Committed)
		}
		for _, validator := range stakerDiff.validatorsToAdd {
			state.PutCurrentValidator(validator)
		}
		for _, validator := range stakerDiff.validatorsToRemove {
			state.DeleteCurrentValidator(validator)
		}
		state.SetHeight(uint64(i + 1))
		require.NoError(state.Commit())

		// Calling write again should not change the state.
		state.SetHeight(uint64(i + 1))
		require.NoError(state.Commit())

		for j, stakerDiff := range stakerDiffs[:i+1] {
			pkDiffs, err := state.GetValidatorPublicKeyDiffs(uint64(j + 1))
			require.NoError(err)
			require.Equal(stakerDiff.expectedPublicKeyDiffs, pkDiffs)
			state.validatorBlsKeyDiffsCache.Flush()
		}
	}
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

	genesisBlk, err := blocks.NewApricotCommitBlock(genesisBlkID, 0)
	require.NoError(err)
	require.NoError(s.(*merkleState).syncGenesis(genesisBlk, genesisState))

	return s, db
}

func newUninitializedState(require *require.Assertions) (State, database.Database) {
	db := memdb.New()
	return newStateFromDB(require, db), db
}

func newStateFromDB(require *require.Assertions, db database.Database) State {
	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	execCfg, _ := config.GetExecutionConfig(nil)
	state, err := newMerklsState(
		db,
		metrics.Noop,
		&config.Config{
			Validators: vdrs,
		},
		execCfg,
		&snow.Context{},
		prometheus.NewRegistry(),
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
		&utils.Atomic[bool]{},
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
					return d.Add(true, stdmath.MaxUint64)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1)
				},
			},
			expected:    &ValidatorWeightDiff{},
			expectedErr: math.ErrOverflow,
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
					return d.Add(false, stdmath.MaxUint64)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1)
				},
			},
			expected:    &ValidatorWeightDiff{},
			expectedErr: math.ErrOverflow,
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
// GetValidatorWeightDiffs, GetValidatorPublicKeyDiffs
func TestStateAddRemoveValidator(t *testing.T) {
	require := require.New(t)

	state, _ := newInitializedState(require)

	var (
		numNodes  = 3
		subnetID  = ids.GenerateTestID()
		startTime = time.Now()
		endTime   = startTime.Add(24 * time.Hour)
		stakers   = make([]Staker, numNodes)
		addedTxs  = make([]*txs.Tx, numNodes)
	)
	for i := 0; i < numNodes; i++ {
		valTx := txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  uint64(startTime.Add(time.Duration(i) * time.Second).Unix()),
			End:    uint64(endTime.Add(time.Duration(i) * time.Second).Unix()),
			Wght:   uint64(i + 1),
		}
		addedTxs[i] = &txs.Tx{
			Unsigned: &txs.AddValidatorTx{
				Validator: valTx,
			},
		}
		addedTxs[i].SetBytes([]byte{0x1}, []byte(strconv.Itoa(10*i)))

		stakers[i] = Staker{
			TxID:            addedTxs[i].ID(),
			NodeID:          valTx.NodeID,
			Weight:          valTx.Wght,
			StartTime:       valTx.StartTime(),
			EndTime:         valTx.EndTime(),
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
		addedStakers                     []Staker
		addedTxs                         []*txs.Tx
		removed                          []Staker
		expectedSubnetWeightDiff         map[ids.NodeID]*ValidatorWeightDiff
		expectedPrimaryNetworkWeightDiff map[ids.NodeID]*ValidatorWeightDiff
		expectedPublicKeyDiff            map[ids.NodeID]*bls.PublicKey
	}
	diffs := []diff{
		{
			// Add a subnet validator
			addedStakers:                     []Staker{stakers[0]},
			addedTxs:                         []*txs.Tx{addedTxs[0]},
			expectedPrimaryNetworkWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{},
			expectedSubnetWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{
				stakers[0].NodeID: {
					Decrease: false,
					Amount:   stakers[0].Weight,
				},
			},
			// No diff because this is a subnet validator
			expectedPublicKeyDiff: map[ids.NodeID]*bls.PublicKey{},
		},
		{
			// Remove a subnet validator
			removed:                          []Staker{stakers[0]},
			expectedPrimaryNetworkWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{},
			expectedSubnetWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{
				stakers[0].NodeID: {
					Decrease: true,
					Amount:   stakers[0].Weight,
				},
			},
			// No diff because this is a subnet validator
			expectedPublicKeyDiff: map[ids.NodeID]*bls.PublicKey{},
		},
		{ // Add a primary network validator
			addedStakers: []Staker{stakers[1]},
			addedTxs:     []*txs.Tx{addedTxs[1]},
			expectedPrimaryNetworkWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{
				stakers[1].NodeID: {
					Decrease: false,
					Amount:   stakers[1].Weight,
				},
			},
			expectedSubnetWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{},
			expectedPublicKeyDiff:    map[ids.NodeID]*bls.PublicKey{},
		},
		{ // Remove a primary network validator
			removed: []Staker{stakers[1]},
			expectedPrimaryNetworkWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{
				stakers[1].NodeID: {
					Decrease: true,
					Amount:   stakers[1].Weight,
				},
			},
			expectedSubnetWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{},
			expectedPublicKeyDiff: map[ids.NodeID]*bls.PublicKey{
				stakers[1].NodeID: stakers[1].PublicKey,
			},
		},
		{
			// Add 2 subnet validators and a primary network validator
			addedStakers: []Staker{stakers[0], stakers[1], stakers[2]},
			addedTxs:     []*txs.Tx{addedTxs[0], addedTxs[1], addedTxs[2]},
			expectedPrimaryNetworkWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{
				stakers[1].NodeID: {
					Decrease: false,
					Amount:   stakers[1].Weight,
				},
			},
			expectedSubnetWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{
				stakers[0].NodeID: {
					Decrease: false,
					Amount:   stakers[0].Weight,
				},
				stakers[2].NodeID: {
					Decrease: false,
					Amount:   stakers[2].Weight,
				},
			},
			expectedPublicKeyDiff: map[ids.NodeID]*bls.PublicKey{},
		},
		{
			// Remove 2 subnet validators and a primary network validator.
			removed: []Staker{stakers[0], stakers[1], stakers[2]},
			expectedPrimaryNetworkWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{
				stakers[1].NodeID: {
					Decrease: true,
					Amount:   stakers[1].Weight,
				},
			},
			expectedSubnetWeightDiff: map[ids.NodeID]*ValidatorWeightDiff{
				stakers[0].NodeID: {
					Decrease: true,
					Amount:   stakers[0].Weight,
				},
				stakers[2].NodeID: {
					Decrease: true,
					Amount:   stakers[2].Weight,
				},
			},
			expectedPublicKeyDiff: map[ids.NodeID]*bls.PublicKey{
				stakers[1].NodeID: stakers[1].PublicKey,
			},
		},
	}

	for i, diff := range diffs {
		for _, tx := range diff.addedTxs {
			state.AddTx(tx, status.Committed)
		}

		for _, added := range diff.addedStakers {
			added := added
			state.PutCurrentValidator(&added)
		}
		for _, removed := range diff.removed {
			removed := removed
			state.DeleteCurrentValidator(&removed)
		}

		newHeight := uint64(i + 1)
		state.SetHeight(newHeight)

		require.NoError(state.Commit())

		for _, added := range diff.addedStakers {
			gotValidator, err := state.GetCurrentValidator(added.SubnetID, added.NodeID)
			require.NoError(err)
			require.Equal(added, *gotValidator)
		}

		for _, removed := range diff.removed {
			_, err := state.GetCurrentValidator(removed.SubnetID, removed.NodeID)
			require.ErrorIs(err, database.ErrNotFound)
		}

		// Assert that we get the expected weight diffs
		gotSubnetWeightDiffs, err := state.GetValidatorWeightDiffs(newHeight, subnetID)
		require.NoError(err)
		require.Equal(diff.expectedSubnetWeightDiff, gotSubnetWeightDiffs)

		gotWeightDiffs, err := state.GetValidatorWeightDiffs(newHeight, constants.PrimaryNetworkID)
		require.NoError(err)
		require.Equal(diff.expectedPrimaryNetworkWeightDiff, gotWeightDiffs)

		// Assert that we get the expected public key diff
		gotPublicKeyDiffs, err := state.GetValidatorPublicKeyDiffs(newHeight)
		require.NoError(err)
		require.Equal(diff.expectedPublicKeyDiff, gotPublicKeyDiffs)
	}
}
