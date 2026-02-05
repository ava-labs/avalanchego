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
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
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

func newRewardContinuousValidatorTx(require *require.Assertions, txID ids.ID, timestamp uint64) *txs.Tx {
	utx := &txs.RewardContinuousValidatorTx{TxID: txID, Timestamp: timestamp}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	require.NoError(err)
	return tx
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

func TestRewardValidatorStakerType(t *testing.T) {
	require := require.New(t)

	var (
		env           = newEnvironment(t, upgradetest.Fortuna)
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
		wallet        = newWallet(t, env, walletConfig{})
	)

	sValidatorTx, err := wallet.IssueAddContinuousValidatorTx(
		ids.GenerateTestNodeID(),
		env.config.MinValidatorStake,
		must[*signer.ProofOfPossession](t)(signer.NewProofOfPossession(must[*localsigner.LocalSigner](t)(localsigner.New()))),
		env.ctx.AVAXAssetID,
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		reward.PercentDenominator,
		reward.PercentDenominator,
		env.config.MinStakeDuration,
	)
	require.NoError(err)
	env.state.AddTx(sValidatorTx, status.Committed)

	validatorTx := sValidatorTx.Unsigned.(*txs.AddContinuousValidatorTx)

	vdrStaker, err := state.NewContinuousStaker(
		sValidatorTx.ID(),
		sValidatorTx.Unsigned.(txs.Staker),
		time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0),
		uint64(1000000),
		0,
		0,
		0,
		0,
		validatorTx.PeriodDuration(),
	)
	require.NoError(err)
	require.NoError(env.state.PutCurrentValidator(vdrStaker))

	env.state.SetTimestamp(vdrStaker.EndTime)

	err = ProposalTx(
		&env.backend,
		feeCalculator,
		must[*txs.Tx](t)(newRewardValidatorTx(t, sValidatorTx.ID())),
		must[state.Diff](t)(state.NewDiffOn(env.state)), // onCommitState
		must[state.Diff](t)(state.NewDiffOn(env.state)), // onAbortState
	)
	require.ErrorIs(err, ErrShouldBeFixedStaker)
}

func TestRewardContinuousValidatorErrors(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Fortuna)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	vdrTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(genesistest.DefaultValidatorStartTime.Add(2 * env.config.MinStakeDuration).Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		must[*signer.ProofOfPossession](t)(signer.NewProofOfPossession(must[*localsigner.LocalSigner](t)(localsigner.New()))),
		env.ctx.AVAXAssetID,
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		reward.PercentDenominator,
	)
	require.NoError(t, err)
	env.state.AddTx(vdrTx, status.Committed)

	staker, err := state.NewCurrentValidator(
		vdrTx.ID(),
		vdrTx.Unsigned.(*txs.AddPermissionlessValidatorTx),
		time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0),
		uint64(1000000),
		0,
	)
	require.NoError(t, err)
	require.NoError(t, env.state.PutCurrentValidator(staker))
	env.state.SetTimestamp(staker.EndTime)

	tests := []struct {
		name                string
		expectedErr         error
		updateStateAndGetTx func(require *require.Assertions, state state.State) *txs.Tx
	}{
		{
			name:        "wrong staker",
			expectedErr: ErrRemoveWrongStaker,
			updateStateAndGetTx: func(require *require.Assertions, state state.State) *txs.Tx {
				return newRewardContinuousValidatorTx(require, ids.GenerateTestID(), uint64(state.GetTimestamp().Unix()))
			},
		},
		{
			name:        "invalid timestamp",
			expectedErr: ErrInvalidTimestamp,
			updateStateAndGetTx: func(require *require.Assertions, state state.State) *txs.Tx {
				return newRewardContinuousValidatorTx(require, staker.TxID, uint64(state.GetTimestamp().Unix())-1)
			},
		},
		{
			name:        "invalid validator tx",
			expectedErr: ErrShouldBeContinuousStaker,
			updateStateAndGetTx: func(require *require.Assertions, state state.State) *txs.Tx {
				return newRewardContinuousValidatorTx(require, vdrTx.ID(), uint64(state.GetTimestamp().Unix()))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			err = ProposalTx(
				&env.backend,
				feeCalculator,
				tt.updateStateAndGetTx(require, env.state),
				must[state.Diff](t)(state.NewDiffOn(env.state)), // onCommitState
				must[state.Diff](t)(state.NewDiffOn(env.state)), // onAbortState
			)

			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestRewardContinuousValidatorGracefulStop(t *testing.T) {
	require := require.New(t)

	var (
		env           = newEnvironment(t, upgradetest.Fortuna)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	vdrWeight := env.config.MaxValidatorStake - 500_000

	sValidatorTx, err := wallet.IssueAddContinuousValidatorTx(
		ids.GenerateTestNodeID(),
		vdrWeight,
		must[*signer.ProofOfPossession](t)(signer.NewProofOfPossession(must[*localsigner.LocalSigner](t)(localsigner.New()))),
		env.ctx.AVAXAssetID,
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{},
		100_000,
		400_000,
		env.config.MinStakeDuration,
	)
	require.NoError(err)
	env.state.AddTx(sValidatorTx, status.Committed)

	validatorTx := sValidatorTx.Unsigned.(*txs.AddContinuousValidatorTx)
	avax.Produce(env.state, sValidatorTx.ID(), validatorTx.Outputs())

	staker, err := state.NewContinuousStaker(
		sValidatorTx.ID(),
		sValidatorTx.Unsigned.(txs.Staker),
		time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0),
		10_000_000,
		5_000_000,
		1_000_000,
		500_000,
		validatorTx.AutoRestakeSharesAmount(),
		0,
	)

	require.NoError(err)
	require.NoError(env.state.PutCurrentValidator(staker))

	env.state.SetTimestamp(staker.EndTime)

	rewardTx := newRewardContinuousValidatorTx(require, sValidatorTx.ID(), uint64(env.state.GetTimestamp().Unix()))

	onCommitState, err := state.NewDiffOn(env.state)
	require.NoError(err)

	onAbortState, err := state.NewDiffOn(env.state)
	require.NoError(err)

	currentSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		rewardTx,
		onCommitState,
		onAbortState,
	))

	commitSupply, err := onCommitState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(commitSupply, currentSupply)

	abortSupply, err := onAbortState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(abortSupply, currentSupply-staker.PotentialReward)

	for _, stateTest := range []struct {
		diff                      state.Diff
		expectedValidationRewards uint64
	}{
		{diff: onAbortState, expectedValidationRewards: 1_000_000},
		{diff: onCommitState, expectedValidationRewards: 11_000_000},
	} {
		_, err = stateTest.diff.GetCurrentValidator(staker.SubnetID, staker.NodeID)
		require.ErrorIs(database.ErrNotFound, err)

		for i, stakeOut := range validatorTx.StakeOuts {
			utxoID := &avax.UTXOID{
				TxID:        sValidatorTx.ID(),
				OutputIndex: uint32(len(validatorTx.Outputs())) + uint32(i),
			}

			utxo, err := stateTest.diff.GetUTXO(utxoID.InputID())
			require.NoError(err)

			utxoOut := utxo.Out.(*secp256k1fx.TransferOutput)
			require.Equal(stakeOut.Out.Amount(), utxoOut.Amt)
		}

		rewardUTXOID := &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())),
		}
		rewardUTXO, err := stateTest.diff.GetUTXO(rewardUTXOID.InputID())
		require.NoError(err)
		require.Equal(env.ctx.AVAXAssetID, rewardUTXO.Asset.AssetID())
		require.Equal(stateTest.expectedValidationRewards, rewardUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
		require.True(validatorTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&rewardUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

		delegatingRewardsUTXOID := &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 1,
		}
		delegatingRewardsUTXO, err := stateTest.diff.GetUTXO(delegatingRewardsUTXOID.InputID())
		require.NoError(err)
		require.Equal(env.ctx.AVAXAssetID, delegatingRewardsUTXO.Asset.AssetID())
		require.Equal(uint64(5_500_000), delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
		require.True(validatorTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

		// No other UTXOs
		utxoID := &avax.UTXOID{
			TxID:        sValidatorTx.ID(),
			OutputIndex: uint32(len(validatorTx.Outputs()) + len(validatorTx.StakeOuts)),
		}
		_, err = stateTest.diff.GetUTXO(utxoID.InputID())
		require.ErrorIs(database.ErrNotFound, err)

		utxoID = &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 2,
		}
		_, err = stateTest.diff.GetUTXO(utxoID.InputID())
		require.ErrorIs(database.ErrNotFound, err)
	}
}

func TestRewardContinuousValidatorTxMaxValidatorStake(t *testing.T) {
	require := require.New(t)

	var (
		env           = newEnvironment(t, upgradetest.Fortuna)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	vdrWeight := env.config.MaxValidatorStake - 2_000_000

	sValidatorTx, err := wallet.IssueAddContinuousValidatorTx(
		ids.GenerateTestNodeID(),
		vdrWeight,
		must[*signer.ProofOfPossession](t)(signer.NewProofOfPossession(must[*localsigner.LocalSigner](t)(localsigner.New()))),
		env.ctx.AVAXAssetID,
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{},
		100_000,
		400_000,
		env.config.MinStakeDuration,
	)
	require.NoError(err)
	env.state.AddTx(sValidatorTx, status.Committed)

	validatorTx := sValidatorTx.Unsigned.(*txs.AddContinuousValidatorTx)
	avax.Produce(env.state, sValidatorTx.ID(), validatorTx.Outputs())

	staker, err := state.NewContinuousStaker(
		sValidatorTx.ID(),
		sValidatorTx.Unsigned.(txs.Staker),
		time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0),
		10_000_000,
		5_000_000,
		1_000_000,
		500_000,
		validatorTx.AutoRestakeSharesAmount(),
		validatorTx.PeriodDuration(),
	)

	require.NoError(err)
	require.NoError(env.state.PutCurrentValidator(staker))

	env.state.SetTimestamp(staker.EndTime)

	rewardTx := newRewardContinuousValidatorTx(require, sValidatorTx.ID(), uint64(env.state.GetTimestamp().Unix()))

	onCommitState, err := state.NewDiffOn(env.state)
	require.NoError(err)

	onAbortState, err := state.NewDiffOn(env.state)
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		rewardTx,
		onCommitState,
		onAbortState,
	))

	// Check [onAbortState].
	{
		_, err = onAbortState.GetCurrentValidator(staker.SubnetID, staker.NodeID)
		require.ErrorIs(database.ErrNotFound, err)

		for i, stakeOut := range validatorTx.StakeOuts {
			utxoID := &avax.UTXOID{
				TxID:        sValidatorTx.ID(),
				OutputIndex: uint32(len(validatorTx.Outputs())) + uint32(i),
			}

			utxo, err := onAbortState.GetUTXO(utxoID.InputID())
			require.NoError(err)

			utxoOut := utxo.Out.(*secp256k1fx.TransferOutput)
			require.Equal(stakeOut.Out.Amount(), utxoOut.Amt)
		}

		rewardUTXOID := &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())),
		}
		rewardUTXO, err := onAbortState.GetUTXO(rewardUTXOID.InputID())
		require.NoError(err)
		require.Equal(env.ctx.AVAXAssetID, rewardUTXO.Asset.AssetID())
		require.Equal(uint64(1_000_000), rewardUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
		require.True(validatorTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&rewardUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

		delegatingRewardsUTXOID := &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 1,
		}
		delegatingRewardsUTXO, err := onAbortState.GetUTXO(delegatingRewardsUTXOID.InputID())
		require.NoError(err)
		require.Equal(env.ctx.AVAXAssetID, delegatingRewardsUTXO.Asset.AssetID())
		require.Equal(uint64(5_500_000), delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
		require.True(validatorTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

		// No other UTXOs
		utxoID := &avax.UTXOID{
			TxID:        sValidatorTx.ID(),
			OutputIndex: uint32(len(validatorTx.Outputs()) + len(validatorTx.StakeOuts)),
		}
		_, err = onAbortState.GetUTXO(utxoID.InputID())
		require.ErrorIs(database.ErrNotFound, err)

		utxoID = &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 2,
		}
		_, err = onAbortState.GetUTXO(utxoID.InputID())
		require.ErrorIs(database.ErrNotFound, err)
	}

	// Check [onCommitState]
	{
		validator, err := onCommitState.GetCurrentValidator(staker.SubnetID, staker.NodeID)
		require.NoError(err)

		precisionDiff := uint64(1) // Important edge case, because we make sure [validator.Weight] <= [env.config.MaxValidatorStake]
		require.Equal(env.config.MaxValidatorStake-precisionDiff, validator.Weight)
		require.Equal(uint64(1_333_333), validator.AccruedRewards)
		require.Equal(uint64(666_666), validator.AccruedDelegateeRewards)

		// Check UTXOs for withdraws from auto-restake shares param
		{
			withdrawnRewardsUTXOID := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())),
			}
			withdrawRewardsUTXO, err := onCommitState.GetUTXO(withdrawnRewardsUTXOID.InputID())
			require.NoError(err)
			require.Equal(env.ctx.AVAXAssetID, withdrawRewardsUTXO.Asset.AssetID())
			require.Equal(uint64(6_000_000), withdrawRewardsUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
			require.True(validatorTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&withdrawRewardsUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

			withdrawDelegateeRewardsUTXO := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 1,
			}
			withdrawDelegateeRewards, err := onCommitState.GetUTXO(withdrawDelegateeRewardsUTXO.InputID())
			require.NoError(err)
			require.Equal(env.ctx.AVAXAssetID, withdrawDelegateeRewards.Asset.AssetID())
			require.Equal(uint64(3_000_000), withdrawDelegateeRewards.Out.(*secp256k1fx.TransferOutput).Amount())
			require.True(validatorTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&withdrawDelegateeRewards.Out.(*secp256k1fx.TransferOutput).OutputOwners))
		}

		// Check UTXOs for excess withdrawn
		{
			rewardUTXOID := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 2,
			}
			rewardUTXO, err := onCommitState.GetUTXO(rewardUTXOID.InputID())
			require.NoError(err)
			require.Equal(env.ctx.AVAXAssetID, rewardUTXO.Asset.AssetID())
			require.Equal(uint64(3_666_667), rewardUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
			require.True(validatorTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&rewardUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

			delegatingRewardsUTXOID := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 3,
			}
			delegatingRewardsUTXO, err := onCommitState.GetUTXO(delegatingRewardsUTXOID.InputID())
			require.NoError(err)
			require.Equal(env.ctx.AVAXAssetID, delegatingRewardsUTXO.Asset.AssetID())
			require.Equal(uint64(1_833_334), delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
			require.True(validatorTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

			// No other UTXOs
			utxoID := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 4,
			}
			_, err = onCommitState.GetUTXO(utxoID.InputID())
			require.ErrorIs(database.ErrNotFound, err)

			utxoID = &avax.UTXOID{
				TxID:        sValidatorTx.ID(),
				OutputIndex: uint32(len(validatorTx.Outputs())),
			}
			_, err = onCommitState.GetUTXO(utxoID.InputID())
			require.ErrorIs(database.ErrNotFound, err)
		}
	}
}
