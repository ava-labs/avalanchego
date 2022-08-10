// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
	assert := assert.New(t)
	env := newEnvironment()
	defer func() {
		assert.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	currentStakerIterator, err := env.state.GetCurrentStakerIterator()
	assert.NoError(err)
	assert.True(currentStakerIterator.Next())

	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	stakerToRemoveTxIntf, _, err := env.state.GetTx(stakerToRemove.TxID)
	assert.NoError(err)
	stakerToRemoveTx := stakerToRemoveTxIntf.Unsigned.(*txs.AddValidatorTx)

	// Case 1: Chain timestamp is wrong
	tx, err := env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	assert.NoError(err)

	txExecutor := ProposalTxExecutor{
		Backend:       &env.backend,
		ParentID:      lastAcceptedID,
		StateVersions: env,
		Tx:            tx,
	}
	assert.Error(tx.Unsigned.Visit(&txExecutor))

	// Advance chain timestamp to time that next validator leaves
	env.state.SetTimestamp(stakerToRemove.EndTime)

	// Case 2: Wrong validator
	tx, err = env.txBuilder.NewRewardValidatorTx(ids.GenerateTestID())
	assert.NoError(err)

	txExecutor = ProposalTxExecutor{
		Backend:       &env.backend,
		ParentID:      lastAcceptedID,
		StateVersions: env,
		Tx:            tx,
	}
	assert.Error(tx.Unsigned.Visit(&txExecutor))

	// Case 3: Happy path
	tx, err = env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	assert.NoError(err)

	txExecutor = ProposalTxExecutor{
		Backend:       &env.backend,
		ParentID:      lastAcceptedID,
		StateVersions: env,
		Tx:            tx,
	}
	assert.NoError(tx.Unsigned.Visit(&txExecutor))

	onCommitStakerIterator, err := txExecutor.OnCommit.GetCurrentStakerIterator()
	assert.NoError(err)
	assert.True(onCommitStakerIterator.Next())

	nextToRemove := onCommitStakerIterator.Value()
	onCommitStakerIterator.Release()
	assert.NotEqual(stakerToRemove.TxID, nextToRemove.TxID)

	// check that stake/reward is given back
	stakeOwners := stakerToRemoveTx.Stake[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := avax.GetBalance(env.state, stakeOwners)
	assert.NoError(err)

	txExecutor.OnCommit.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	assert.NoError(env.state.Commit())

	onCommitBalance, err := avax.GetBalance(env.state, stakeOwners)
	assert.NoError(err)
	assert.Equal(oldBalance+stakerToRemove.Weight+27, onCommitBalance)
}

func TestRewardValidatorTxExecuteOnAbort(t *testing.T) {
	assert := assert.New(t)
	env := newEnvironment()
	defer func() {
		assert.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	currentStakerIterator, err := env.state.GetCurrentStakerIterator()
	assert.NoError(err)
	assert.True(currentStakerIterator.Next())

	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	stakerToRemoveTxIntf, _, err := env.state.GetTx(stakerToRemove.TxID)
	assert.NoError(err)
	stakerToRemoveTx := stakerToRemoveTxIntf.Unsigned.(*txs.AddValidatorTx)

	// Case 1: Chain timestamp is wrong
	tx, err := env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	assert.NoError(err)

	txExecutor := ProposalTxExecutor{
		Backend:       &env.backend,
		ParentID:      lastAcceptedID,
		StateVersions: env,
		Tx:            tx,
	}
	assert.Error(tx.Unsigned.Visit(&txExecutor))

	// Advance chain timestamp to time that next validator leaves
	env.state.SetTimestamp(stakerToRemove.EndTime)

	// Case 2: Wrong validator
	tx, err = env.txBuilder.NewRewardValidatorTx(ids.GenerateTestID())
	assert.NoError(err)

	txExecutor = ProposalTxExecutor{
		Backend:       &env.backend,
		ParentID:      lastAcceptedID,
		StateVersions: env,
		Tx:            tx,
	}
	assert.Error(tx.Unsigned.Visit(&txExecutor))

	// Case 3: Happy path
	tx, err = env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	assert.NoError(err)

	txExecutor = ProposalTxExecutor{
		Backend:       &env.backend,
		ParentID:      lastAcceptedID,
		StateVersions: env,
		Tx:            tx,
	}
	assert.NoError(tx.Unsigned.Visit(&txExecutor))

	onAbortStakerIterator, err := txExecutor.OnAbort.GetCurrentStakerIterator()
	assert.NoError(err)
	assert.True(onAbortStakerIterator.Next())

	nextToRemove := onAbortStakerIterator.Value()
	onAbortStakerIterator.Release()
	assert.NotEqual(stakerToRemove.TxID, nextToRemove.TxID)

	// check that stake/reward isn't given back
	stakeOwners := stakerToRemoveTx.Stake[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := avax.GetBalance(env.state, stakeOwners)
	assert.NoError(err)

	txExecutor.OnAbort.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	assert.NoError(env.state.Commit())

	onAbortBalance, err := avax.GetBalance(env.state, stakeOwners)
	assert.NoError(err)
	assert.Equal(oldBalance+stakerToRemove.Weight, onAbortBalance)
}

func TestRewardDelegatorTxExecuteOnCommit(t *testing.T) {
	assert := assert.New(t)
	env := newEnvironment()
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
	assert.NoError(err)

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
	assert.NoError(err)

	vdrStaker := state.NewPrimaryNetworkStaker(
		vdrTx.ID(),
		&vdrTx.Unsigned.(*txs.AddValidatorTx).Validator,
	)
	vdrStaker.PotentialReward = 0
	vdrStaker.NextTime = vdrStaker.EndTime
	vdrStaker.Priority = state.PrimaryNetworkValidatorCurrentPriority

	delStaker := state.NewPrimaryNetworkStaker(
		delTx.ID(),
		&delTx.Unsigned.(*txs.AddDelegatorTx).Validator,
	)
	delStaker.PotentialReward = 1000000
	delStaker.NextTime = delStaker.EndTime
	delStaker.Priority = state.PrimaryNetworkDelegatorCurrentPriority

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	assert.NoError(env.state.Commit())

	// test validator stake
	set, ok := env.config.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(ok)
	stake, ok := set.GetWeight(vdrNodeID)
	assert.True(ok)
	assert.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	assert.NoError(err)

	txExecutor := ProposalTxExecutor{
		Backend:       &env.backend,
		ParentID:      lastAcceptedID,
		StateVersions: env,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&txExecutor)
	assert.NoError(err)

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	assert.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	assert.NoError(err)

	txExecutor.OnCommit.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	assert.NoError(env.state.Commit())

	// If tx is committed, delegator and delegatee should get reward
	// and the delegator's reward should be greater because the delegatee's share is 25%
	commitVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	assert.NoError(err)
	vdrReward, err := math.Sub64(commitVdrBalance, oldVdrBalance)
	assert.NoError(err)
	assert.NotZero(vdrReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	assert.NoError(err)
	delReward, err := math.Sub64(commitDelBalance, oldDelBalance)
	assert.NoError(err)
	assert.NotZero(delReward, "expected delegator balance to increase because of reward")

	assert.Less(vdrReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	assert.Equal(expectedReward, delReward+vdrReward, "expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)

	stake, ok = set.GetWeight(vdrNodeID)
	assert.True(ok)
	assert.Equal(env.config.MinValidatorStake, stake)
}

func TestRewardDelegatorTxExecuteOnAbort(t *testing.T) {
	assert := assert.New(t)
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	initialSupply := env.state.GetCurrentSupply()

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
	assert.NoError(err)

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
	assert.NoError(err)

	vdrStaker := state.NewPrimaryNetworkStaker(
		vdrTx.ID(),
		&vdrTx.Unsigned.(*txs.AddValidatorTx).Validator,
	)
	vdrStaker.PotentialReward = 0
	vdrStaker.NextTime = vdrStaker.EndTime
	vdrStaker.Priority = state.PrimaryNetworkValidatorCurrentPriority

	delStaker := state.NewPrimaryNetworkStaker(
		delTx.ID(),
		&delTx.Unsigned.(*txs.AddDelegatorTx).Validator,
	)
	delStaker.PotentialReward = 1000000
	delStaker.NextTime = delStaker.EndTime
	delStaker.Priority = state.PrimaryNetworkDelegatorCurrentPriority

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	assert.NoError(env.state.Commit())

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	assert.NoError(err)

	txExecutor := ProposalTxExecutor{
		Backend:       &env.backend,
		ParentID:      lastAcceptedID,
		StateVersions: env,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&txExecutor)
	assert.NoError(err)

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	assert.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	assert.NoError(err)

	txExecutor.OnAbort.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	assert.NoError(env.state.Commit())

	// If tx is aborted, delegator and delegatee shouldn't get reward
	newVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	assert.NoError(err)
	vdrReward, err := math.Sub64(newVdrBalance, oldVdrBalance)
	assert.NoError(err)
	assert.Zero(vdrReward, "expected delegatee balance not to increase")

	newDelBalance, err := avax.GetBalance(env.state, delDestSet)
	assert.NoError(err)
	delReward, err := math.Sub64(newDelBalance, oldDelBalance)
	assert.NoError(err)
	assert.Zero(delReward, "expected delegator balance not to increase")

	newSupply := env.state.GetCurrentSupply()
	assert.Equal(initialSupply-expectedReward, newSupply, "should have removed un-rewarded tokens from the potential supply")
}
