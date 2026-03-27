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

func newRewardAutoRenewedValidatorTx(t testing.TB, txID ids.ID, timestamp uint64) *txs.Tx {
	t.Helper()

	utx := &txs.RewardAutoRenewedValidatorTx{TxID: txID, Timestamp: timestamp}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	require.NoError(t, err)
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

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	onCommitState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	onCommitState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	onCommitState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		addDelTx.StartTime(),
		1000000,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(vdrStaker))
	env.state.AddTx(vdrTx, status.Committed)
	require.NoError(env.state.PutCurrentDelegator(delStaker))
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// test validator stake
	stake := env.config.Validators.GetWeight(constants.PrimaryNetworkID, vdrNodeID)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx, err := newRewardValidatorTx(t, delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		time.Unix(int64(vdrStartTime), 0),
		vdrRewardAmt,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delRewardAmt := uint64(1000000)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		time.Unix(int64(delStartTime), 0),
		delRewardAmt,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(vdrStaker))
	env.state.AddTx(vdrTx, status.Committed)
	require.NoError(env.state.PutCurrentDelegator(delStaker))
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
	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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
	onCommitState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		vdrRewardAmt,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delRewardAmt := uint64(1000000)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		time.Unix(int64(delStartTime), 0),
		delRewardAmt,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(vdrStaker))
	env.state.AddTx(vdrTx, status.Committed)
	require.NoError(env.state.PutCurrentDelegator(delStaker))
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
	delOnCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	delOnAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	vdrOnCommitState, err := state.NewDiff(testID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	vdrOnAbortState, err := state.NewDiff(testID, env, state.StakerAdditionAfterDeletionForbidden)
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
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		addDelTx.StartTime(),
		1000000,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(vdrStaker))
	env.state.AddTx(vdrTx, status.Committed)
	require.NoError(env.state.PutCurrentDelegator(delStaker))
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	tx, err := newRewardValidatorTx(t, delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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
	var (
		env           = newEnvironment(t, upgradetest.Fortuna)
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
		wallet        = newWallet(t, env, walletConfig{})
	)

	addAutoRenewedValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
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
	require.NoError(t, err)
	env.state.AddTx(addAutoRenewedValidatorTx, status.Committed)

	validatorTx := addAutoRenewedValidatorTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)

	startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)
	duration := time.Duration(validatorTx.Period) * time.Second
	vdrStaker, err := state.NewStaker(
		addAutoRenewedValidatorTx.ID(),
		validatorTx,
		startTime,
		startTime.Add(duration),
		validatorTx.Weight(),
		uint64(1000000),
	)
	require.NoError(t, err)

	require.NoError(t, env.state.PutCurrentValidator(vdrStaker))
	require.NoError(t, env.state.Commit())

	require.NoError(t, env.state.SetStakingInfo(vdrStaker.SubnetID, vdrStaker.NodeID, state.StakingInfo{
		Period: time.Duration(validatorTx.Period) * time.Second,
	}))

	env.state.SetTimestamp(vdrStaker.EndTime)

	err = ProposalTx(
		&env.backend,
		feeCalculator,
		must[*txs.Tx](t)(newRewardValidatorTx(t, addAutoRenewedValidatorTx.ID())),
		must[state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onCommitState
		must[state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onAbortState
	)
	require.ErrorIs(t, err, errShouldUseRewardAutoRenewedValidator)
}

func TestRewardAutoRenewedValidatorTxErrors(t *testing.T) {
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

	staker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		vdrTx.Unsigned.(*txs.AddPermissionlessValidatorTx),
		time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0),
		uint64(1000000),
	)
	require.NoError(t, err)
	require.NoError(t, env.state.PutCurrentValidator(staker))
	env.state.SetTimestamp(staker.EndTime)

	tests := []struct {
		name                string
		wantErr             error
		updateStateAndGetTx func(t testing.TB, state *state.State) *txs.Tx
	}{
		{
			name:    "wrong staker",
			wantErr: ErrRemoveWrongStaker,
			updateStateAndGetTx: func(t testing.TB, state *state.State) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, ids.GenerateTestID(), uint64(state.GetTimestamp().Unix()))
			},
		},
		{
			name:    "invalid timestamp",
			wantErr: ErrInvalidTimestamp,
			updateStateAndGetTx: func(t testing.TB, state *state.State) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, staker.TxID, uint64(state.GetTimestamp().Unix())-1)
			},
		},
		{
			name:    "invalid validator tx",
			wantErr: ErrShouldBeAutoRenewedStaker,
			updateStateAndGetTx: func(t testing.TB, state *state.State) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, vdrTx.ID(), uint64(state.GetTimestamp().Unix()))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err = ProposalTx(
				&env.backend,
				feeCalculator,
				tt.updateStateAndGetTx(t, env.state),
				must[state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onCommitState
				must[state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onAbortState
			)

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRewardAutoRenewedValidatorTxGracefulStop(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Fortuna)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	vdrWeight := env.config.MaxValidatorStake - 500_000

	sValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
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
	require.NoError(t, err)
	env.state.AddTx(sValidatorTx, status.Committed)

	validatorTx := sValidatorTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	avax.Produce(env.state, sValidatorTx.ID(), validatorTx.Outputs())

	startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)
	duration := time.Duration(validatorTx.Period) * time.Second
	staker, err := state.NewStaker(
		sValidatorTx.ID(),
		validatorTx,
		startTime,
		startTime.Add(duration),
		validatorTx.Weight(),
		10_000_000,
	)
	require.NoError(t, err)

	require.NoError(t, env.state.PutCurrentValidator(staker))
	require.NoError(t, env.state.Commit())

	require.NoError(t, env.state.SetStakingInfo(staker.SubnetID, staker.NodeID, state.StakingInfo{
		DelegateeReward:          5_000_000,
		AccruedRewards:           1_000_000,
		AccruedDelegateeRewards:  500_000,
		AutoCompoundRewardShares: validatorTx.AutoCompoundRewardShares,
		Period:                   0,
	}))

	env.state.SetTimestamp(staker.EndTime)

	rewardTx := newRewardAutoRenewedValidatorTx(t, sValidatorTx.ID(), uint64(env.state.GetTimestamp().Unix()))

	onCommitState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	currentSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	require.NoError(t, ProposalTx(
		&env.backend,
		feeCalculator,
		rewardTx,
		onCommitState,
		onAbortState,
	))

	commitSupply, err := onCommitState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)
	require.Equal(t, commitSupply, currentSupply)

	abortSupply, err := onAbortState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)
	require.Equal(t, abortSupply, currentSupply-staker.PotentialReward)

	for _, stateTest := range []struct {
		diff                  state.Diff
		wantValidationRewards uint64
	}{
		{diff: onAbortState, wantValidationRewards: 1_000_000},
		{diff: onCommitState, wantValidationRewards: 11_000_000},
	} {
		_, err = stateTest.diff.GetCurrentValidator(staker.SubnetID, staker.NodeID)
		require.ErrorIs(t, database.ErrNotFound, err)

		for i, stakeOut := range validatorTx.StakeOuts {
			utxoID := &avax.UTXOID{
				TxID:        sValidatorTx.ID(),
				OutputIndex: uint32(len(validatorTx.Outputs())) + uint32(i),
			}

			utxo, err := stateTest.diff.GetUTXO(utxoID.InputID())
			require.NoError(t, err)

			utxoOut := utxo.Out.(*secp256k1fx.TransferOutput)
			require.Equal(t, stakeOut.Out.Amount(), utxoOut.Amt)
		}

		rewardUTXOID := &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())),
		}
		rewardUTXO, err := stateTest.diff.GetUTXO(rewardUTXOID.InputID())
		require.NoError(t, err)
		require.Equal(t, env.ctx.AVAXAssetID, rewardUTXO.Asset.AssetID())
		require.Equal(t, stateTest.wantValidationRewards, rewardUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
		require.True(t, validatorTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&rewardUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

		delegatingRewardsUTXOID := &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 1,
		}
		delegatingRewardsUTXO, err := stateTest.diff.GetUTXO(delegatingRewardsUTXOID.InputID())
		require.NoError(t, err)
		require.Equal(t, env.ctx.AVAXAssetID, delegatingRewardsUTXO.Asset.AssetID())
		require.Equal(t, uint64(5_500_000), delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
		require.True(t, validatorTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

		// No other UTXOs
		utxoID := &avax.UTXOID{
			TxID:        sValidatorTx.ID(),
			OutputIndex: uint32(len(validatorTx.Outputs()) + len(validatorTx.StakeOuts)),
		}
		_, err = stateTest.diff.GetUTXO(utxoID.InputID())
		require.ErrorIs(t, database.ErrNotFound, err)

		utxoID = &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 2,
		}
		_, err = stateTest.diff.GetUTXO(utxoID.InputID())
		require.ErrorIs(t, database.ErrNotFound, err)
	}
}

func TestRewardAutoRenewedValidatorTxMaxValidatorStake(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Fortuna)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	vdrWeight := env.config.MaxValidatorStake - 2_000_000

	sValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
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
	require.NoError(t, err)
	env.state.AddTx(sValidatorTx, status.Committed)

	validatorTx := sValidatorTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	avax.Produce(env.state, sValidatorTx.ID(), validatorTx.Outputs())

	startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)
	duration := time.Duration(validatorTx.Period) * time.Second
	staker, err := state.NewStaker(
		sValidatorTx.ID(),
		validatorTx,
		startTime,
		startTime.Add(duration),
		validatorTx.Weight(),
		10_000_000,
	)
	require.NoError(t, err)

	require.NoError(t, env.state.PutCurrentValidator(staker))
	require.NoError(t, env.state.Commit())

	require.NoError(t, env.state.SetStakingInfo(staker.SubnetID, staker.NodeID, state.StakingInfo{
		DelegateeReward:          5_000_000,
		AccruedRewards:           1_000_000,
		AccruedDelegateeRewards:  500_000,
		AutoCompoundRewardShares: validatorTx.AutoCompoundRewardShares,
		Period:                   time.Duration(validatorTx.Period) * time.Second,
	}))

	env.state.SetTimestamp(staker.EndTime)

	rewardTx := newRewardAutoRenewedValidatorTx(t, sValidatorTx.ID(), uint64(env.state.GetTimestamp().Unix()))

	onCommitState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	currentSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	require.NoError(t, ProposalTx(
		&env.backend,
		feeCalculator,
		rewardTx,
		onCommitState,
		onAbortState,
	))

	// Check onAbortState.
	{
		_, err = onAbortState.GetCurrentValidator(staker.SubnetID, staker.NodeID)
		require.ErrorIs(t, database.ErrNotFound, err)

		for i, stakeOut := range validatorTx.StakeOuts {
			utxoID := &avax.UTXOID{
				TxID:        sValidatorTx.ID(),
				OutputIndex: uint32(len(validatorTx.Outputs())) + uint32(i),
			}

			utxo, err := onAbortState.GetUTXO(utxoID.InputID())
			require.NoError(t, err)

			utxoOut := utxo.Out.(*secp256k1fx.TransferOutput)
			require.Equal(t, stakeOut.Out.Amount(), utxoOut.Amt)
		}

		rewardUTXOID := &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())),
		}
		rewardUTXO, err := onAbortState.GetUTXO(rewardUTXOID.InputID())
		require.NoError(t, err)
		require.Equal(t, env.ctx.AVAXAssetID, rewardUTXO.Asset.AssetID())
		require.Equal(t, uint64(1_000_000), rewardUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
		require.True(t, validatorTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&rewardUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

		delegatingRewardsUTXOID := &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 1,
		}
		delegatingRewardsUTXO, err := onAbortState.GetUTXO(delegatingRewardsUTXOID.InputID())
		require.NoError(t, err)
		require.Equal(t, env.ctx.AVAXAssetID, delegatingRewardsUTXO.Asset.AssetID())
		require.Equal(t, uint64(5_500_000), delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
		require.True(t, validatorTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

		// No other UTXOs
		utxoID := &avax.UTXOID{
			TxID:        sValidatorTx.ID(),
			OutputIndex: uint32(len(validatorTx.Outputs()) + len(validatorTx.StakeOuts)),
		}
		_, err = onAbortState.GetUTXO(utxoID.InputID())
		require.ErrorIs(t, database.ErrNotFound, err)

		utxoID = &avax.UTXOID{
			TxID:        rewardTx.ID(),
			OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 2,
		}
		_, err = onAbortState.GetUTXO(utxoID.InputID())
		require.ErrorIs(t, database.ErrNotFound, err)

		abortSupply, err := onAbortState.GetCurrentSupply(constants.PrimaryNetworkID)
		require.NoError(t, err)
		require.Equal(t, currentSupply-staker.PotentialReward, abortSupply)
	}

	// Check onCommitState
	{
		validator, err := onCommitState.GetCurrentValidator(staker.SubnetID, staker.NodeID)
		require.NoError(t, err)

		validatorMutables, err := onCommitState.GetStakingInfo(staker.SubnetID, staker.NodeID)
		require.NoError(t, err)

		require.Equal(t, env.config.MaxValidatorStake, validator.Weight)
		require.Equal(t, uint64(2_333_333), validatorMutables.AccruedRewards)
		require.Equal(t, uint64(1_166_667), validatorMutables.AccruedDelegateeRewards)

		// Check UTXOs for withdraws from auto-restake shares param
		{
			withdrawnRewardsUTXOID := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())),
			}
			withdrawRewardsUTXO, err := onCommitState.GetUTXO(withdrawnRewardsUTXOID.InputID())
			require.NoError(t, err)
			require.Equal(t, env.ctx.AVAXAssetID, withdrawRewardsUTXO.Asset.AssetID())
			require.Equal(t, uint64(6_000_000), withdrawRewardsUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
			require.True(t, validatorTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&withdrawRewardsUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

			withdrawDelegateeRewardsUTXO := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 1,
			}
			withdrawDelegateeRewards, err := onCommitState.GetUTXO(withdrawDelegateeRewardsUTXO.InputID())
			require.NoError(t, err)
			require.Equal(t, env.ctx.AVAXAssetID, withdrawDelegateeRewards.Asset.AssetID())
			require.Equal(t, uint64(3_000_000), withdrawDelegateeRewards.Out.(*secp256k1fx.TransferOutput).Amount())
			require.True(t, validatorTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&withdrawDelegateeRewards.Out.(*secp256k1fx.TransferOutput).OutputOwners))
		}

		// Check UTXOs for excess withdrawn
		{
			rewardUTXOID := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 2,
			}
			rewardUTXO, err := onCommitState.GetUTXO(rewardUTXOID.InputID())
			require.NoError(t, err)
			require.Equal(t, env.ctx.AVAXAssetID, rewardUTXO.Asset.AssetID())
			require.Equal(t, uint64(2_666_667), rewardUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
			require.True(t, validatorTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&rewardUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

			delegatingRewardsUTXOID := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 3,
			}
			delegatingRewardsUTXO, err := onCommitState.GetUTXO(delegatingRewardsUTXOID.InputID())
			require.NoError(t, err)
			require.Equal(t, env.ctx.AVAXAssetID, delegatingRewardsUTXO.Asset.AssetID())
			require.Equal(t, uint64(1_333_333), delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).Amount())
			require.True(t, validatorTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners).Equals(&delegatingRewardsUTXO.Out.(*secp256k1fx.TransferOutput).OutputOwners))

			// No other UTXOs
			utxoID := &avax.UTXOID{
				TxID:        rewardTx.ID(),
				OutputIndex: uint32(len(rewardTx.Unsigned.Outputs())) + 4,
			}
			_, err = onCommitState.GetUTXO(utxoID.InputID())
			require.ErrorIs(t, database.ErrNotFound, err)

			utxoID = &avax.UTXOID{
				TxID:        sValidatorTx.ID(),
				OutputIndex: uint32(len(validatorTx.Outputs())),
			}
			_, err = onCommitState.GetUTXO(utxoID.InputID())
			require.ErrorIs(t, database.ErrNotFound, err)
		}

		commitSupply, err := onCommitState.GetCurrentSupply(constants.PrimaryNetworkID)
		require.NoError(t, err)
		require.Equal(t, currentSupply+validator.PotentialReward, commitSupply)
	}
}

// TestRewardDelegatorToAutoRenewedValidator tests the full delegator reward
// flow for a delegator to an auto-renewed validator: delegator gets their
// share, delegatee share is deferred to StakingInfo.DelegateeReward.
func TestRewardDelegatorToAutoRenewedValidator(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Fortuna)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)

		vdrNodeID          = ids.GenerateTestNodeID()
		vdrRewardAddress   = ids.GenerateTestShortID()
		delRewardAddress   = ids.GenerateTestShortID()
		delegationShares   = uint32(reward.PercentDenominator / 4) // 25% to delegatee
		vdrWeight          = env.config.MinValidatorStake
		delRewardAmt       = uint64(1_000_000)
		vdrPotentialReward = uint64(2_000_000)
	)

	// Step 1: Create the auto-renewed validator.
	sValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
		vdrNodeID,
		vdrWeight,
		must[*signer.ProofOfPossession](t)(signer.NewProofOfPossession(must[*localsigner.LocalSigner](t)(localsigner.New()))),
		env.ctx.AVAXAssetID,
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{vdrRewardAddress}},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{vdrRewardAddress}},
		&secp256k1fx.OutputOwners{},
		delegationShares,
		reward.PercentDenominator, // auto compound 100%
		env.config.MinStakeDuration,
	)
	require.NoError(t, err)
	env.state.AddTx(sValidatorTx, status.Committed)

	validatorTx := sValidatorTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	avax.Produce(env.state, sValidatorTx.ID(), validatorTx.Outputs())

	startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)
	duration := time.Duration(validatorTx.Period) * time.Second
	vdrStaker, err := state.NewStaker(
		sValidatorTx.ID(),
		validatorTx,
		startTime,
		startTime.Add(duration),
		validatorTx.Weight(),
		vdrPotentialReward,
	)
	require.NoError(t, err)

	require.NoError(t, env.state.PutCurrentValidator(vdrStaker))
	require.NoError(t, env.state.Commit())

	require.NoError(t, env.state.SetStakingInfo(vdrStaker.SubnetID, vdrStaker.NodeID, state.StakingInfo{
		AutoCompoundRewardShares: validatorTx.AutoCompoundRewardShares,
		Period:                   time.Duration(validatorTx.Period) * time.Second,
	}))

	// Step 2: Create a delegator to this auto-renewed validator.
	delStartTime := genesistest.DefaultValidatorStartTimeUnix + 1
	delEndTime := uint64(startTime.Add(duration).Unix())

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
	require.NoError(t, err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		time.Unix(int64(delStartTime), 0),
		delRewardAmt,
	)
	require.NoError(t, err)

	require.NoError(t, env.state.PutCurrentDelegator(delStaker))
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	require.NoError(t, env.state.Commit())

	// Step 3: Reward the delegator via RewardValidatorTx.
	rewardDelTx, err := newRewardValidatorTx(t, delTx.ID())
	require.NoError(t, err)

	delOnCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	delOnAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	require.NoError(t, ProposalTx(
		&env.backend,
		feeCalculator,
		rewardDelTx,
		delOnCommitState,
		delOnAbortState,
	))

	// Verify delegator reward UTXO on commit: delegator gets 75% of delRewardAmt.
	wantDelegateeReward, wantDelegatorReward := reward.Split(delRewardAmt, delegationShares)
	numDelStakeUTXOs := uint32(len(delTx.Unsigned.InputIDs()))
	delRewardUTXOID := &avax.UTXOID{
		TxID:        delTx.ID(),
		OutputIndex: numDelStakeUTXOs + 1,
	}
	utxo, err := delOnCommitState.GetUTXO(delRewardUTXOID.InputID())
	require.NoError(t, err)
	require.IsType(t, &secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO := utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(t, wantDelegatorReward, castUTXO.Amt)
	require.True(t, set.Of(delRewardAddress).Equals(castUTXO.AddressesSet()))

	// Verify delegatee reward is NOT distributed yet (deferred post-Cortina).
	preCortinaDelegateeUTXOID := &avax.UTXOID{
		TxID:        delTx.ID(),
		OutputIndex: numDelStakeUTXOs + 2,
	}
	_, err = delOnCommitState.GetUTXO(preCortinaDelegateeUTXOID.InputID())
	require.ErrorIs(t, err, database.ErrNotFound)

	// Verify delegatee reward in StakingInfo.
	stakingInfo, err := delOnCommitState.GetStakingInfo(constants.PrimaryNetworkID, vdrNodeID)
	require.NoError(t, err)
	require.Equal(t, wantDelegateeReward, stakingInfo.DelegateeReward)

	stakingInfo, err = delOnAbortState.GetStakingInfo(constants.PrimaryNetworkID, vdrNodeID)
	require.NoError(t, err)
	require.Zero(t, stakingInfo.DelegateeReward)

	// Commit the delegator diff.
	require.NoError(t, delOnCommitState.Apply(env.state))
	require.NoError(t, env.state.Commit())
}
