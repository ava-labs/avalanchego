// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	stdmath "math"

	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type activeFork uint8

const (
	apricotPhase3Fork     activeFork = 0
	apricotPhase5Fork     activeFork = 1
	banffFork             activeFork = 2
	cortinaFork           activeFork = 3
	continuousStakingFork activeFork = 4
	latestFork            activeFork = continuousStakingFork
)

var (
	initialTxID             = ids.GenerateTestID()
	initialNodeID           = ids.GenerateTestNodeID()
	initialTime             = time.Now().Round(time.Second)
	initialValidatorEndTime = initialTime.Add(28 * 24 * time.Hour)
)

func TestStateInitialization(t *testing.T) {
	require := require.New(t)
	cfg := defaultConfig(latestFork)
	s, db, err := newUninitializedState(cfg)
	require.NoError(err)
	require.NotNil(s)

	shouldInit, err := s.(*state).shouldInit()
	require.NoError(err)
	require.True(shouldInit)

	require.NoError(s.(*state).doneInit())
	require.NoError(s.Commit())

	s, err = newStateFromDB(db, cfg)
	require.NoError(err)

	shouldInit, err = s.(*state).shouldInit()
	require.NoError(err)
	require.False(shouldInit)
}

func TestStateSyncGenesis(t *testing.T) {
	require := require.New(t)
	cfg := defaultConfig(latestFork)
	state, _, err := newInitializedState(cfg)
	require.NoError(err)

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

func TestVariablePeriodDelegatorPersistence(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cfg := defaultConfig(latestFork)
	initialState, baseDB, err := newInitializedState(cfg)
	require.NoError(err)
	ctx := initialState.(*state).ctx

	var (
		nodeID        = ids.GenerateTestNodeID()
		preFundedKeys = secp256k1.TestKeys()
		addr          = preFundedKeys[0].PublicKey().Address()

		startTime       = time.Unix(0, 0)
		stakingPeriod   = defaultMaxStakingDuration
		potentialReward = uint64(1234)
	)

	sk, err := bls.NewSecretKey()
	require.NoError(err)
	blsPOP := signer.NewProofOfPossession(sk)

	// Add a continuous validator
	validatorData := txs.Validator{
		NodeID: nodeID,
		Start:  uint64(startTime.Unix()),
		End:    uint64(startTime.Add(stakingPeriod).Unix()),
		Wght:   cfg.MinValidatorStake,
	}
	uContinuousVal := &txs.AddContinuousValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          []*avax.TransferableInput{},
			Outs:         []*avax.TransferableOutput{},
		}},
		Validator: validatorData,
		Signer:    blsPOP,
		ValidatorAuthKey: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: ids.GenerateTestID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: validatorData.Weight(),
				},
			},
		},
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		ValidatorRewardRestakeShares: 0,
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		DelegationShares: 20_000,
	}
	valTx, err := txs.NewSigned(uContinuousVal, txs.Codec, nil)
	require.NoError(err)
	require.NoError(valTx.SyntacticVerify(ctx))

	validator, err := NewCurrentStaker(
		valTx.ID(),
		uContinuousVal,
		startTime,
		mockable.MaxTime,
		potentialReward,
	)
	require.NoError(err)

	initialState.PutCurrentValidator(validator)
	initialState.AddTx(valTx, status.Committed)
	require.NoError(initialState.Commit())

	// add a continuous delegator
	delegatorData := txs.Validator{
		NodeID: nodeID,
		Start:  uint64(startTime.Unix()),
		End:    uint64(startTime.Add(stakingPeriod).Unix()),
		Wght:   cfg.MinDelegatorStake,
	}
	uContinuousDel := &txs.AddContinuousDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          []*avax.TransferableInput{},
			Outs:         []*avax.TransferableOutput{},
		}},
		Validator: delegatorData,
		DelegatorAuthKey: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: ids.GenerateTestID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: delegatorData.Weight(),
				},
			},
		},
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		DelegatorRewardRestakeShares: 0,
	}
	delTx, err := txs.NewSigned(uContinuousDel, txs.Codec, nil)
	require.NoError(err)
	require.NoError(delTx.SyntacticVerify(ctx))

	delegator, err := NewCurrentStaker(
		delTx.ID(),
		uContinuousDel,
		startTime,
		mockable.MaxTime,
		potentialReward,
	)
	require.NoError(err)

	initialState.PutCurrentDelegator(delegator)
	initialState.AddTx(delTx, status.Committed)
	require.NoError(initialState.Commit())

	// shift delegator, without shortening its period, and store it
	shiftedDelegator := *delegator
	ShiftStakerAheadInPlace(&shiftedDelegator, delegator.NextTime)
	require.NoError(initialState.UpdateCurrentDelegator(&shiftedDelegator))
	require.NoError(initialState.Commit())

	// rebuild state
	cfg = defaultConfig(latestFork)
	newState, err := newStateFromDB(baseDB, cfg)
	require.NoError(err)
	require.NoError(newState.(*state).load())

	// check rebuilt validator and delegator
	require.NoError(err)
	retrievedVal, err := newState.GetCurrentValidator(validator.SubnetID, validator.NodeID)
	require.NoError(err)
	require.Equal(validator, retrievedVal)

	delIt, err := newState.GetCurrentDelegatorIterator(delegator.SubnetID, delegator.NodeID)
	require.NoError(err)
	require.True(delIt.Next())
	retrievedDel := delIt.Value()
	require.Equal(&shiftedDelegator, retrievedDel)
}

func TestGetValidatorWeightDiffs(t *testing.T) {
	require := require.New(t)
	cfg := defaultConfig(latestFork)
	stateIntf, _, err := newInitializedState(cfg)
	require.NoError(err)
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
	cfg := defaultConfig(latestFork)
	stateIntf, _, err := newInitializedState(cfg)
	require.NoError(err)
	state := stateIntf.(*state)

	var (
		numNodes = 6
		txIDs    = make([]ids.ID, numNodes)
		nodeIDs  = make([]ids.NodeID, numNodes)
		sks      = make([]*bls.SecretKey, numNodes)
		pks      = make([]*bls.PublicKey, numNodes)
		pkBytes  = make([][]byte, numNodes)
	)
	for i := 0; i < numNodes; i++ {
		txIDs[i] = ids.GenerateTestID()
		nodeIDs[i] = ids.GenerateTestNodeID()
		sks[i], err = bls.NewSecretKey()
		require.NoError(err)
		pks[i] = bls.PublicFromSecretKey(sks[i])
		pkBytes[i] = bls.PublicKeyToBytes(pks[i])
	}

	type stakerDiff struct {
		validatorsToAdd        []*Staker
		validatorsToRemove     []*Staker
		expectedPublicKeyDiffs map[ids.NodeID]*bls.PublicKey
	}
	stakerDiffs := []*stakerDiff{
		{
			// Add two validators
			validatorsToAdd: []*Staker{
				{
					TxID:      txIDs[0],
					NodeID:    nodeIDs[0],
					Weight:    1,
					PublicKey: pks[0],
				},
				{
					TxID:      txIDs[1],
					NodeID:    nodeIDs[1],
					Weight:    10,
					PublicKey: pks[1],
				},
			},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{},
		},
		{
			// Remove a validator
			validatorsToRemove: []*Staker{
				{
					TxID:      txIDs[0],
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
					TxID:      txIDs[2],
					NodeID:    nodeIDs[2],
					Weight:    10,
					PublicKey: pks[2],
				},
				{
					TxID:      txIDs[3],
					NodeID:    nodeIDs[3],
					Weight:    10,
					PublicKey: pks[3],
				},
			},
			validatorsToRemove: []*Staker{
				{
					TxID:      txIDs[1],
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
					TxID:      txIDs[4],
					NodeID:    nodeIDs[4],
					Weight:    10,
					PublicKey: pks[4],
				},
			},
			validatorsToRemove: []*Staker{
				{
					TxID:      txIDs[2],
					NodeID:    nodeIDs[2],
					Weight:    10,
					PublicKey: pks[2],
				},
				{
					TxID:      txIDs[3],
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
					TxID:      txIDs[5],
					NodeID:    nodeIDs[5],
					Weight:    10,
					PublicKey: nil,
				},
			},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{},
		},
		{
			// Remove a validator with no pub key
			validatorsToRemove: []*Staker{
				{
					TxID:      txIDs[5],
					NodeID:    nodeIDs[5],
					Weight:    10,
					PublicKey: nil,
				},
			},
			expectedPublicKeyDiffs: map[ids.NodeID]*bls.PublicKey{},
		},
	}

	for i, stakerDiff := range stakerDiffs {
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
			state.validatorPublicKeyDiffsCache.Flush()
		}
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

	cfg := defaultConfig(latestFork)
	state, _, err := newInitializedState(cfg)
	require.NoError(err)

	var (
		numNodes  = 3
		subnetID  = ids.GenerateTestID()
		startTime = time.Now()
		duration  = 24 * time.Hour
		endTime   = startTime.Add(duration)
		stakers   = make([]Staker, numNodes)
	)
	for i := 0; i < numNodes; i++ {
		stakers[i] = Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			Weight:          uint64(i + 1),
			StartTime:       startTime.Add(time.Duration(i) * time.Second),
			StakingPeriod:   duration,
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
		added                            []Staker
		removed                          []Staker
		expectedSubnetWeightDiff         map[ids.NodeID]*ValidatorWeightDiff
		expectedPrimaryNetworkWeightDiff map[ids.NodeID]*ValidatorWeightDiff
		expectedPublicKeyDiff            map[ids.NodeID]*bls.PublicKey
	}
	diffs := []diff{
		{
			// Add a subnet validator
			added:                            []Staker{stakers[0]},
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
			added: []Staker{stakers[1]},
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
			added: []Staker{stakers[0], stakers[1], stakers[2]},
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
		for _, added := range diff.added {
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

		for _, added := range diff.added {
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

func defaultGenesis(ctx *snow.Context) (*genesis.State, error) {
	initialValidator := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
		}},
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
	if err := initialValidatorTx.Initialize(txs.Codec); err != nil {
		return nil, err
	}

	initialChain := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
		}},
		SubnetID:   constants.PrimaryNetworkID,
		ChainName:  "x",
		VMID:       constants.AVMID,
		SubnetAuth: &secp256k1fx.Input{},
	}
	initialChainTx := &txs.Tx{Unsigned: initialChain}
	if err := initialChainTx.Initialize(txs.Codec); err != nil {
		return nil, err
	}

	return &genesis.State{
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
	}, nil
}

func defaultCtx() *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = 10
	ctx.XChainID = xChainID
	ctx.CChainID = cChainID
	ctx.AVAXAssetID = avaxAssetID
	return ctx
}

func newInitializedState(cfg *config.Config) (State, database.Database, error) {
	s, baseDB, err := newUninitializedState(cfg)
	if err != nil {
		return nil, nil, err
	}

	genesisState, err := defaultGenesis(s.(*state).ctx)
	if err != nil {
		return nil, nil, err
	}

	genesisBlkID := ids.GenerateTestID()
	genesisBlk, err := blocks.NewApricotCommitBlock(genesisBlkID, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := s.(*state).syncGenesis(genesisBlk, genesisState); err != nil {
		return nil, nil, err
	}

	return s, baseDB, nil
}

func newUninitializedState(cfg *config.Config) (State, database.Database, error) {
	db := memdb.New()
	s, err := newStateFromDB(db, cfg)
	if err != nil {
		return nil, nil, err
	}
	return s, db, nil
}

func newStateFromDB(db database.Database, cfg *config.Config) (State, error) {
	execCfg, _ := config.GetExecutionConfig(nil)
	return new(
		db,
		metrics.Noop,
		cfg,
		execCfg,
		defaultCtx(),
		prometheus.NewRegistry(),
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
		&utils.Atomic[bool]{},
	)
}

func TestParsedStateBlock(t *testing.T) {
	require := require.New(t)

	var blks []blocks.Block

	{
		blk, err := blocks.NewApricotAbortBlock(ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := blocks.NewApricotAtomicBlock(ids.GenerateTestID(), 1000, &txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{
				Time: 1000,
			},
		})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := blocks.NewApricotCommitBlock(ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := blocks.NewApricotProposalBlock(ids.GenerateTestID(), 1000, &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := blocks.NewApricotStandardBlock(ids.GenerateTestID(), 1000, []*txs.Tx{
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
		blk, err := blocks.NewBanffAbortBlock(time.Now(), ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := blocks.NewBanffCommitBlock(time.Now(), ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := blocks.NewBanffProposalBlock(time.Now(), ids.GenerateTestID(), 1000, &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := blocks.NewBanffStandardBlock(time.Now(), ids.GenerateTestID(), 1000, []*txs.Tx{
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

		stBlkBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, &stBlk)
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
