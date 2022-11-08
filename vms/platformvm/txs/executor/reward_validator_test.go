// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestRewardValidatorTxExecuteOnCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	currentStakerIterator, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)
	require.True(currentStakerIterator.Next())

	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	stakerToRemoveTxIntf, _, err := env.state.GetTx(stakerToRemove.TxID)
	require.NoError(err)
	stakerToRemoveTx := stakerToRemoveTxIntf.Unsigned.(*txs.AddValidatorTx)

	// Case 1: Chain timestamp is wrong
	tx, err := env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.Error(tx.Unsigned.Visit(&txExecutor))

	// Advance chain timestamp to time that next validator leaves
	env.state.SetTimestamp(stakerToRemove.EndTime)

	// Case 2: Wrong validator
	tx, err = env.txBuilder.NewRewardValidatorTx(ids.GenerateTestID())
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.Error(tx.Unsigned.Visit(&txExecutor))

	// Case 3: Happy path
	tx, err = env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	onCommitStakerIterator, err := txExecutor.OnCommitState.GetCurrentStakerIterator()
	require.NoError(err)
	require.True(onCommitStakerIterator.Next())

	nextToRemove := onCommitStakerIterator.Value()
	onCommitStakerIterator.Release()
	require.NotEqual(stakerToRemove.TxID, nextToRemove.TxID)

	// check that stake/reward is given back
	stakeOwners := stakerToRemoveTx.StakeOuts[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)

	txExecutor.OnCommitState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	onCommitBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)
	require.Equal(oldBalance+stakerToRemove.Weight+27, onCommitBalance)
}

func TestRewardValidatorTxExecuteOnAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	currentStakerIterator, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)
	require.True(currentStakerIterator.Next())

	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	stakerToRemoveTxIntf, _, err := env.state.GetTx(stakerToRemove.TxID)
	require.NoError(err)
	stakerToRemoveTx := stakerToRemoveTxIntf.Unsigned.(*txs.AddValidatorTx)

	// Case 1: Chain timestamp is wrong
	tx, err := env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.Error(tx.Unsigned.Visit(&txExecutor))

	// Advance chain timestamp to time that next validator leaves
	env.state.SetTimestamp(stakerToRemove.EndTime)

	// Case 2: Wrong validator
	tx, err = env.txBuilder.NewRewardValidatorTx(ids.GenerateTestID())
	require.NoError(err)

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.Error(tx.Unsigned.Visit(&txExecutor))

	// Case 3: Happy path
	tx, err = env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	onAbortStakerIterator, err := txExecutor.OnAbortState.GetCurrentStakerIterator()
	require.NoError(err)
	require.True(onAbortStakerIterator.Next())

	nextToRemove := onAbortStakerIterator.Value()
	onAbortStakerIterator.Release()
	require.NotEqual(stakerToRemove.TxID, nextToRemove.TxID)

	// check that stake/reward isn't given back
	stakeOwners := stakerToRemoveTx.StakeOuts[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)

	txExecutor.OnAbortState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	onAbortBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)
	require.Equal(oldBalance+stakerToRemove.Weight, onAbortBalance)
}

func TestRewardDelegatorTxExecuteOnCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		reward.PercentDenominator/4,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		delStartTime,
		delEndTime,
		vdrNodeID,
		delRewardAddress,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
		ids.ShortEmpty, // Change address
	)
	require.NoError(err)

	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		vdrTx.Unsigned.(*txs.AddValidatorTx),
		0,
	)
	require.NoError(err)

	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		delTx.Unsigned.(*txs.AddDelegatorTx),
		1000000,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// test validator stake
	set, ok := env.config.Validators.GetValidators(constants.PrimaryNetworkID)
	require.True(ok)
	stake, ok := set.GetWeight(vdrNodeID)
	require.True(ok)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&txExecutor)
	require.NoError(err)

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	txExecutor.OnCommitState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// If tx is committed, delegator and delegatee should get reward
	// and the delegator's reward should be greater because the delegatee's share is 25%
	commitVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.NotZero(vdrReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(vdrReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(expectedReward, delReward+vdrReward, "expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)

	stake, ok = set.GetWeight(vdrNodeID)
	require.True(ok)
	require.Equal(env.config.MinValidatorStake, stake)
}

func TestRewardDelegatorTxExecuteOnAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	initialSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		reward.PercentDenominator/4,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime
	delTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		delStartTime,
		delEndTime,
		vdrNodeID,
		delRewardAddress,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		vdrTx.Unsigned.(*txs.AddValidatorTx),
		0,
	)
	require.NoError(err)

	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		delTx.Unsigned.(*txs.AddDelegatorTx),
		1000000,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		t.Fatal(err)
	}

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&txExecutor)
	require.NoError(err)

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	txExecutor.OnAbortState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// If tx is aborted, delegator and delegatee shouldn't get reward
	newVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(newVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.Zero(vdrReward, "expected delegatee balance not to increase")

	newDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(newDelBalance, oldDelBalance)
	require.NoError(err)
	require.Zero(delReward, "expected delegator balance not to increase")

	newSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(initialSupply-expectedReward, newSupply, "should have removed un-rewarded tokens from the potential supply")
}
