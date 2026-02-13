// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func newRewardValidatorTx(t testing.TB, txID ids.ID) (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{TxID: txID}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(snowtest.Context(t, snowtest.PChainID))
}

func TestRewardValidatorTxExecuteOnCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
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
	tx, err := newRewardValidatorTx(t, stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onAbortState)
	err = ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	)
	require.ErrorIs(err, ErrRemoveStakerTooEarly)

	// Advance chain timestamp to time that next validator leaves
	env.state.SetTimestamp(stakerToRemove.EndTime)

	// Case 2: Wrong validator
	tx, err = newRewardValidatorTx(t, ids.GenerateTestID())
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	err = ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	)
	require.ErrorIs(err, ErrRemoveWrongStaker)

	// Case 3: Happy path
	tx, err = newRewardValidatorTx(t, stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	onCommitStakerIterator, err := onCommitState.GetCurrentStakerIterator()
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

	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	onCommitBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)
	require.Equal(oldBalance+stakerToRemove.Weight+38944, onCommitBalance)
}

func TestRewardValidatorTxExecuteOnAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
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
	tx, err := newRewardValidatorTx(t, stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onAbortState)
	err = ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	)
	require.ErrorIs(err, ErrRemoveStakerTooEarly)

	// Advance chain timestamp to time that next validator leaves
	env.state.SetTimestamp(stakerToRemove.EndTime)

	// Case 2: Wrong validator
	tx, err = newRewardValidatorTx(t, ids.GenerateTestID())
	require.NoError(err)

	err = ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	)
	require.ErrorIs(err, ErrRemoveWrongStaker)

	// Case 3: Happy path
	tx, err = newRewardValidatorTx(t, stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	onAbortStakerIterator, err := onAbortState.GetCurrentStakerIterator()
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

	require.NoError(onAbortState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	onAbortBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)
	require.Equal(oldBalance+stakerToRemove.Weight, onAbortBalance)
}

func TestRewardDelegatorTxExecuteOnCommitPreDelegateeDeferral(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	dummyHeight := uint64(1)

	wallet := newWallet(t, env, walletConfig{})

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := genesistest.DefaultValidatorStartTimeUnix + 1
	vdrEndTime := uint64(genesistest.DefaultValidatorStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: vdrNodeID,
			Start:  vdrStartTime,
			End:    vdrEndTime,
			Wght:   env.config.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{vdrRewardAddress},
		},
		reward.PercentDenominator/4,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: vdrNodeID,
			Start:  delStartTime,
			End:    delEndTime,
			Wght:   env.config.MinDelegatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{delRewardAddress},
		},
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrStaker, err := state.NewCurrentValidator(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentDelegator(
		delTx.ID(),
		addDelTx,
		addDelTx.StartTime(),
		1000000,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(vdrStaker))
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// test validator stake
	stake := env.config.Validators.GetWeight(constants.PrimaryNetworkID, vdrNodeID)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx, err := newRewardValidatorTx(t, delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	vdrDestSet := set.Of(vdrRewardAddress)
	delDestSet := set.Of(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
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

	stake = env.config.Validators.GetWeight(constants.PrimaryNetworkID, vdrNodeID)
	require.Equal(env.config.MinValidatorStake, stake)
}

func TestRewardDelegatorTxExecuteOnCommitPostDelegateeDeferral(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Cortina)
	dummyHeight := uint64(1)

	wallet := newWallet(t, env, walletConfig{})

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := genesistest.DefaultValidatorStartTimeUnix + 1
	vdrEndTime := uint64(genesistest.DefaultValidatorStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: vdrNodeID,
			Start:  vdrStartTime,
			End:    vdrEndTime,
			Wght:   env.config.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{vdrRewardAddress},
		},
		reward.PercentDenominator/4,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: vdrNodeID,
			Start:  delStartTime,
			End:    delEndTime,
			Wght:   env.config.MinDelegatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{delRewardAddress},
		},
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrRewardAmt := uint64(2000000)
	vdrStaker, err := state.NewCurrentValidator(
		vdrTx.ID(),
		addValTx,
		time.Unix(int64(vdrStartTime), 0),
		vdrRewardAmt,
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delRewardAmt := uint64(1000000)
	delStaker, err := state.NewCurrentDelegator(
		delTx.ID(),
		addDelTx,
		time.Unix(int64(delStartTime), 0),
		delRewardAmt,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(vdrStaker))
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(vdrEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	vdrDestSet := set.Of(vdrRewardAddress)
	delDestSet := set.Of(delRewardAddress)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	// test validator stake
	stake := env.config.Validators.GetWeight(constants.PrimaryNetworkID, vdrNodeID)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx, err := newRewardValidatorTx(t, delTx.ID())
	require.NoError(err)

	// Create Delegator Diff
	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	// The delegator should be rewarded if the ProposalTx is committed. Since the
	// delegatee's share is 25%, we expect the delegator to receive 75% of the reward.
	// Since this is post [CortinaTime], the delegatee should not be rewarded until a
	// RewardValidatorTx is issued for the delegatee.
	numDelStakeUTXOs := uint32(len(delTx.Unsigned.InputIDs()))
	delRewardUTXOID := &avax.UTXOID{
		TxID:        delTx.ID(),
		OutputIndex: numDelStakeUTXOs + 1,
	}

	utxo, err := onCommitState.GetUTXO(delRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO := utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(delRewardAmt*3/4, castUTXO.Amt, "expected delegator balance to increase by 3/4 of reward amount")
	require.True(delDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to delDestSet")

	preCortinaVdrRewardUTXOID := &avax.UTXOID{
		TxID:        delTx.ID(),
		OutputIndex: numDelStakeUTXOs + 2,
	}
	_, err = onCommitState.GetUTXO(preCortinaVdrRewardUTXOID.InputID())
	require.ErrorIs(err, database.ErrNotFound)

	// Commit Delegator Diff
	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	tx, err = newRewardValidatorTx(t, vdrStaker.TxID)
	require.NoError(err)

	// Create Validator Diff
	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	require.NotEqual(vdrStaker.TxID, delStaker.TxID)

	numVdrStakeUTXOs := uint32(len(delTx.Unsigned.InputIDs()))

	// check for validator reward here
	vdrRewardUTXOID := &avax.UTXOID{
		TxID:        vdrTx.ID(),
		OutputIndex: numVdrStakeUTXOs + 1,
	}

	utxo, err = onCommitState.GetUTXO(vdrRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO = utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(vdrRewardAmt, castUTXO.Amt, "expected validator to be rewarded")
	require.True(vdrDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to vdrDestSet")

	// check for validator's batched delegator rewards here
	onCommitVdrDelRewardUTXOID := &avax.UTXOID{
		TxID:        vdrTx.ID(),
		OutputIndex: numVdrStakeUTXOs + 2,
	}

	utxo, err = onCommitState.GetUTXO(onCommitVdrDelRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO = utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(delRewardAmt/4, castUTXO.Amt, "expected validator to be rewarded with accrued delegator rewards")
	require.True(vdrDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to vdrDestSet")

	// aborted validator tx should still distribute accrued delegator rewards
	onAbortVdrDelRewardUTXOID := &avax.UTXOID{
		TxID:        vdrTx.ID(),
		OutputIndex: numVdrStakeUTXOs + 1,
	}

	utxo, err = onAbortState.GetUTXO(onAbortVdrDelRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO = utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(delRewardAmt/4, castUTXO.Amt, "expected validator to be rewarded with accrued delegator rewards")
	require.True(vdrDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to vdrDestSet")

	_, err = onCommitState.GetUTXO(preCortinaVdrRewardUTXOID.InputID())
	require.ErrorIs(err, database.ErrNotFound)

	// Commit Validator Diff
	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	delegateeReward, err := math.Sub(vdrReward, 2000000)
	require.NoError(err)
	require.NotZero(delegateeReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(delegateeReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(delRewardAmt, delReward+delegateeReward, "expected total reward to be %d but is %d", delRewardAmt, delReward+vdrReward)
}

func TestRewardDelegatorTxAndValidatorTxExecuteOnCommitPostDelegateeDeferral(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Cortina)
	dummyHeight := uint64(1)

	wallet := newWallet(t, env, walletConfig{})

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := genesistest.DefaultValidatorStartTimeUnix + 1
	vdrEndTime := uint64(genesistest.DefaultValidatorStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: vdrNodeID,
			Start:  vdrStartTime,
			End:    vdrEndTime,
			Wght:   env.config.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{vdrRewardAddress},
		},
		reward.PercentDenominator/4,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: vdrNodeID,
			Start:  delStartTime,
			End:    delEndTime,
			Wght:   env.config.MinDelegatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{delRewardAddress},
		},
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrRewardAmt := uint64(2000000)
	vdrStaker, err := state.NewCurrentValidator(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		vdrRewardAmt,
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delRewardAmt := uint64(1000000)
	delStaker, err := state.NewCurrentDelegator(
		delTx.ID(),
		addDelTx,
		time.Unix(int64(delStartTime), 0),
		delRewardAmt,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(vdrStaker))
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(vdrEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	vdrDestSet := set.Of(vdrRewardAddress)
	delDestSet := set.Of(delRewardAddress)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	tx, err := newRewardValidatorTx(t, delTx.ID())
	require.NoError(err)

	// Create Delegator Diffs
	delOnCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	delOnAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, delOnCommitState)
	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		delOnCommitState,
		delOnAbortState,
	))

	// Create Validator Diffs
	testID := ids.GenerateTestID()
	env.SetState(testID, delOnCommitState)

	vdrOnCommitState, err := state.NewDiff(testID, env)
	require.NoError(err)

	vdrOnAbortState, err := state.NewDiff(testID, env)
	require.NoError(err)

	tx, err = newRewardValidatorTx(t, vdrTx.ID())
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		vdrOnCommitState,
		vdrOnAbortState,
	))

	// aborted validator tx should still distribute accrued delegator rewards
	numVdrStakeUTXOs := uint32(len(delTx.Unsigned.InputIDs()))
	onAbortVdrDelRewardUTXOID := &avax.UTXOID{
		TxID:        vdrTx.ID(),
		OutputIndex: numVdrStakeUTXOs + 1,
	}

	utxo, err := vdrOnAbortState.GetUTXO(onAbortVdrDelRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO := utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(delRewardAmt/4, castUTXO.Amt, "expected validator to be rewarded with accrued delegator rewards")
	require.True(vdrDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to vdrDestSet")

	// Commit Delegator Diff
	require.NoError(delOnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Commit Validator Diff
	require.NoError(vdrOnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	delegateeReward, err := math.Sub(vdrReward, vdrRewardAmt)
	require.NoError(err)
	require.NotZero(delegateeReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(delegateeReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(delRewardAmt, delReward+delegateeReward, "expected total reward to be %d but is %d", delRewardAmt, delReward+vdrReward)
}

func TestRewardDelegatorTxExecuteOnAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	dummyHeight := uint64(1)

	wallet := newWallet(t, env, walletConfig{})

	initialSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := genesistest.DefaultValidatorStartTimeUnix + 1
	vdrEndTime := uint64(genesistest.DefaultValidatorStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: vdrNodeID,
			Start:  vdrStartTime,
			End:    vdrEndTime,
			Wght:   env.config.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{vdrRewardAddress},
		},
		reward.PercentDenominator/4,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: vdrNodeID,
			Start:  delStartTime,
			End:    delEndTime,
			Wght:   env.config.MinDelegatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{delRewardAddress},
		},
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrStaker, err := state.NewCurrentValidator(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentDelegator(
		delTx.ID(),
		addDelTx,
		addDelTx.StartTime(),
		1000000,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(vdrStaker))
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	tx, err := newRewardValidatorTx(t, delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	vdrDestSet := set.Of(vdrRewardAddress)
	delDestSet := set.Of(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	require.NoError(onAbortState.Apply(env.state))

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
