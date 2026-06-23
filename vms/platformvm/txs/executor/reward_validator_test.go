// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
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

// requireOutputUTXO asserts that the UTXO at (txID, outputIndex) is a
// secp256k1fx.TransferOutput with the given asset, amount, and owner.
func requireOutputUTXO(
	t testing.TB,
	chain state.Chain,
	txID ids.ID,
	outputIndex int,
	wantAssetID ids.ID,
	wantAmount uint64,
	wantOwner *secp256k1fx.OutputOwners,
) {
	t.Helper()

	utxoID := avax.UTXOID{TxID: txID, OutputIndex: uint32(outputIndex)}
	utxo, err := chain.GetUTXO(utxoID.InputID())
	require.NoError(t, err)

	out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
	require.True(t, ok)
	require.Equal(t, wantAssetID, utxo.Asset.AssetID())
	require.Equal(t, wantAmount, out.Amt)
	require.True(t, wantOwner.Equals(&out.OutputOwners))
}

// requireNoOutputUTXO asserts that there is no UTXO at (txID, outputIndex).
func requireNoOutputUTXO(t testing.TB, chain state.Chain, txID ids.ID, outputIndex int) {
	t.Helper()

	utxoID := avax.UTXOID{TxID: txID, OutputIndex: uint32(outputIndex)}
	_, err := chain.GetUTXO(utxoID.InputID())
	require.ErrorIs(t, err, database.ErrNotFound)
}

// requireStakeReturned asserts that each of the validator's stake outputs
// is returned as a UTXO with the original amount.
func requireStakeReturned(t testing.TB, chain state.Chain, addTx *txs.Tx, uAddTx *txs.AddAutoRenewedValidatorTx) {
	t.Helper()

	for i, stakeOut := range uAddTx.StakeOuts {
		utxoID := avax.UTXOID{
			TxID:        addTx.ID(),
			OutputIndex: uint32(len(uAddTx.Outputs()) + i),
		}
		utxo, err := chain.GetUTXO(utxoID.InputID())
		require.NoError(t, err)
		require.Equal(t, stakeOut.Out.Amount(), utxo.Out.(*secp256k1fx.TransferOutput).Amt)
	}
}

// rewardAutoRenewedValidatorConfig parameterizes rewardAutoRenewedValidator.
type rewardAutoRenewedValidatorConfig struct {
	weight                   uint64
	potentialReward          uint64
	delegateeReward          uint64
	accruedValidationRewards uint64
	accruedDelegateeRewards  uint64
	delegationRewardShares   uint32
	autoCompoundRewardShares uint32
	// restake reports whether the validator is configured to auto-renew. When
	// false the validator gracefully stops (NextPeriod stays 0).
	restake bool
}

// restakedReward returns the portion of amount that is restaked based on the
// configured auto-compound shares, before any MaxValidatorStake capping.
func (c rewardAutoRenewedValidatorConfig) restakedReward(amount uint64) uint64 {
	withdrawnShares := reward.PercentDenominator - uint64(c.autoCompoundRewardShares)
	withdrawnAmount := withdrawnShares * amount / reward.PercentDenominator
	return amount - withdrawnAmount
}

// rewardAutoRenewedValidatorResult holds the results produced while rewarding an
// auto-renewed validator at the end of its period.
type rewardAutoRenewedValidatorResult struct {
	addTx         *txs.Tx
	uAddTx        *txs.AddAutoRenewedValidatorTx
	rewardTx      *txs.Tx
	onCommitState *state.Diff
	onAbortState  *state.Diff
}

// rewardAutoRenewedValidator sets up an auto-renewed validator at the end
// of its period, executes the RewardAutoRenewedValidatorTx against fresh commit
// and abort diffs, and returns the resulting artifacts for assertion.
func rewardAutoRenewedValidator(
	t testing.TB,
	env *environment,
	cfg rewardAutoRenewedValidatorConfig,
) rewardAutoRenewedValidatorResult {
	t.Helper()

	var (
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	addTx, err := wallet.IssueAddAutoRenewedValidatorTx(
		ids.GenerateTestNodeID(),
		cfg.weight,
		must[*signer.ProofOfPossession](t)(signer.NewProofOfPossession(must[*localsigner.LocalSigner](t)(localsigner.New()))),
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{},
		cfg.delegationRewardShares,
		cfg.autoCompoundRewardShares,
		env.config.MinStakeDuration,
	)
	require.NoError(t, err)
	env.state.AddTx(addTx, status.Committed)

	uAddTx := addTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	avax.Produce(env.state, addTx.ID(), uAddTx.Outputs())

	startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)
	duration := time.Duration(uAddTx.Period) * time.Second
	staker, err := state.NewStaker(
		addTx.ID(),
		uAddTx,
		startTime,
		startTime.Add(duration),
		uAddTx.Weight(),
		cfg.potentialReward,
	)
	require.NoError(t, err)

	require.NoError(t, env.state.PutCurrentValidator(staker))

	stakingInfo := state.StakingInfo{
		DelegateeReward:          cfg.delegateeReward,
		AccruedValidationRewards: cfg.accruedValidationRewards,
		AccruedDelegateeRewards:  cfg.accruedDelegateeRewards,
		AutoCompoundRewardShares: uAddTx.AutoCompoundRewardShares,
	}
	if cfg.restake {
		stakingInfo.NextPeriod = uAddTx.Period
	}
	require.NoError(t, env.state.SetStakingInfo(staker.SubnetID, staker.NodeID, stakingInfo))

	env.state.SetTimestamp(staker.EndTime)
	require.NoError(t, env.state.Commit())

	rewardTx := newRewardAutoRenewedValidatorTx(t, addTx.ID(), uint64(env.state.GetTimestamp().Unix()))

	onCommitState := must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed))
	onAbortState := must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed))

	require.NoError(t, ProposalTx(
		&env.backend,
		feeCalculator,
		rewardTx,
		onCommitState,
		onAbortState,
	))

	return rewardAutoRenewedValidatorResult{
		addTx:         addTx,
		uAddTx:        uAddTx,
		rewardTx:      rewardTx,
		onCommitState: onCommitState,
		onAbortState:  onAbortState,
	}
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
	vdrReward, err := safemath.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.NotZero(vdrReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := safemath.Sub(commitDelBalance, oldDelBalance)
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
	vdrReward, err := safemath.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	delegateeReward, err := safemath.Sub(vdrReward, 2000000)
	require.NoError(err)
	require.NotZero(delegateeReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := safemath.Sub(commitDelBalance, oldDelBalance)
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
	vdrReward, err := safemath.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	delegateeReward, err := safemath.Sub(vdrReward, vdrRewardAmt)
	require.NoError(err)
	require.NotZero(delegateeReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := safemath.Sub(commitDelBalance, oldDelBalance)
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
	vdrReward, err := safemath.Sub(newVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.Zero(vdrReward, "expected delegatee balance not to increase")

	newDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := safemath.Sub(newDelBalance, oldDelBalance)
	require.NoError(err)
	require.Zero(delReward, "expected delegator balance not to increase")

	newSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(initialSupply-expectedReward, newSupply, "should have removed un-rewarded tokens from the potential supply")
}

// TestRewardValidatorStakerTypeError verifies that RewardValidatorTx rejects stakers
// it does not reward: an auto-renewed validator (which must be rewarded through
// RewardAutoRenewedValidatorTx) and a permissioned subnet validator (which is
// never rewarded and should already have been removed by the advancement of
// time). Both reach the dispatch default and must surface errUnexpectedStakerTxType.
func TestRewardValidatorStakerTypeError(t *testing.T) {
	startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)

	tests := []struct {
		name string
		// setup builds the staker tx and the corresponding current-validator
		// staker. The caller adds them to state. Staking info is intentionally
		// left at its zero value: both stakers are rejected at the dispatch
		// default before it is ever read.
		setup func(t *testing.T, env *environment) (*txs.Tx, *state.Staker)
	}{
		{
			name: "auto-renewed validator",
			setup: func(t *testing.T, env *environment) (*txs.Tx, *state.Staker) {
				wallet := newWallet(t, env, walletConfig{})

				addAutoRenewedValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
					ids.GenerateTestNodeID(),
					env.config.MinValidatorStake,
					must[*signer.ProofOfPossession](t)(signer.NewProofOfPossession(must[*localsigner.LocalSigner](t)(localsigner.New()))),
					&secp256k1fx.OutputOwners{},
					&secp256k1fx.OutputOwners{},
					&secp256k1fx.OutputOwners{},
					reward.PercentDenominator,
					reward.PercentDenominator,
					env.config.MinStakeDuration,
				)
				require.NoError(t, err)

				uAddAutoRenewedValidatorTx := addAutoRenewedValidatorTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)

				duration := time.Duration(uAddAutoRenewedValidatorTx.Period) * time.Second
				vdrStaker, err := state.NewStaker(
					addAutoRenewedValidatorTx.ID(),
					uAddAutoRenewedValidatorTx,
					startTime,
					startTime.Add(duration),
					uAddAutoRenewedValidatorTx.Weight(),
					uint64(1000000),
				)
				require.NoError(t, err)

				return addAutoRenewedValidatorTx, vdrStaker
			},
		},
		{
			name: "permissioned subnet validator",
			setup: func(t *testing.T, env *environment) (*txs.Tx, *state.Staker) {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				endTime := startTime.Add(env.config.MinStakeDuration)
				addSubnetValidatorTx, err := wallet.IssueAddSubnetValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: ids.GenerateTestNodeID(),
							Start:  uint64(startTime.Unix()),
							End:    uint64(endTime.Unix()),
							Wght:   genesistest.DefaultValidatorWeight,
						},
						Subnet: subnetID,
					},
				)
				require.NoError(t, err)

				uAddSubnetValidatorTx := addSubnetValidatorTx.Unsigned.(*txs.AddSubnetValidatorTx)

				vdrStaker, err := state.NewStaker(
					addSubnetValidatorTx.ID(),
					uAddSubnetValidatorTx,
					startTime,
					endTime,
					uAddSubnetValidatorTx.Weight(),
					0,
				)
				require.NoError(t, err)

				return addSubnetValidatorTx, vdrStaker
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)
			feeCalculator := state.PickFeeCalculator(env.config, env.state)

			addStakerTx, staker := test.setup(t, env)

			diff := must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed))
			diff.AddTx(addStakerTx, status.Committed)
			require.NoError(t, diff.PutCurrentValidator(staker))
			diff.SetTimestamp(staker.EndTime)
			require.NoError(t, diff.Apply(env.state))
			require.NoError(t, env.state.Commit())

			err := ProposalTx(
				&env.backend,
				feeCalculator,
				must[*txs.Tx](t)(newRewardValidatorTx(t, addStakerTx.ID())),
				must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onCommitState
				must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onAbortState
			)
			require.ErrorIs(t, err, errUnexpectedStakerTxType)
		})
	}
}

func TestRewardAutoRenewedValidatorTxErrors(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Latest)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	addPermissionlessValidatorTx, err := wallet.IssueAddPermissionlessValidatorTx(
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
	env.state.AddTx(addPermissionlessValidatorTx, status.Committed)

	uAddPermissionlessValidatorTx := addPermissionlessValidatorTx.Unsigned.(*txs.AddPermissionlessValidatorTx)
	staker, err := state.NewCurrentStaker(
		addPermissionlessValidatorTx.ID(),
		uAddPermissionlessValidatorTx,
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
			wantErr: errInvalidTimestamp,
			updateStateAndGetTx: func(t testing.TB, state *state.State) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, staker.TxID, uint64(state.GetTimestamp().Unix())-1)
			},
		},
		{
			name:    "invalid validator tx",
			wantErr: errShouldBeAutoRenewedStaker,
			updateStateAndGetTx: func(t testing.TB, state *state.State) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, addPermissionlessValidatorTx.ID(), uint64(state.GetTimestamp().Unix()))
			},
		},
		{
			name:    "wrong number of credentials",
			wantErr: errWrongNumberOfCredentials,
			updateStateAndGetTx: func(t testing.TB, state *state.State) *txs.Tx {
				rewardTx := newRewardAutoRenewedValidatorTx(t, staker.TxID, uint64(state.GetTimestamp().Unix()))
				rewardTx.Creds = append(rewardTx.Creds, &secp256k1fx.Credential{})
				return rewardTx
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err = ProposalTx(
				&env.backend,
				feeCalculator,
				tt.updateStateAndGetTx(t, env.state),
				must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onCommitState
				must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onAbortState
			)

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRewardAutoRenewedValidatorTxGracefulStop(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	cfg := rewardAutoRenewedValidatorConfig{
		weight:                   env.config.MaxValidatorStake - 500_000,
		potentialReward:          10_000_000,
		delegateeReward:          5_000_000,
		accruedValidationRewards: 1_000_000,
		accruedDelegateeRewards:  500_000,
		delegationRewardShares:   100_000,
		autoCompoundRewardShares: 400_000,
		restake:                  false, // graceful stop
	}
	rewardResult := rewardAutoRenewedValidator(t, env, cfg)

	var (
		valOwner      = rewardResult.uAddTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners)
		delOwner      = rewardResult.uAddTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners)
		numRewardOuts = len(rewardResult.rewardTx.Unsigned.Outputs())
	)

	currentSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	commitSupply, err := rewardResult.onCommitState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)
	require.Equal(t, commitSupply, currentSupply)

	abortSupply, err := rewardResult.onAbortState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)
	require.Equal(t, abortSupply, currentSupply-cfg.potentialReward)

	for _, stateTest := range []struct {
		diff                  *state.Diff
		wantValidationRewards uint64
	}{
		{diff: rewardResult.onAbortState, wantValidationRewards: cfg.accruedValidationRewards},
		{diff: rewardResult.onCommitState, wantValidationRewards: cfg.potentialReward + cfg.accruedValidationRewards},
	} {
		_, err = stateTest.diff.GetCurrentValidator(rewardResult.uAddTx.SubnetID(), rewardResult.uAddTx.NodeID())
		require.ErrorIs(t, err, database.ErrNotFound)

		requireStakeReturned(t, stateTest.diff, rewardResult.addTx, rewardResult.uAddTx)

		requireOutputUTXO(t, stateTest.diff, rewardResult.rewardTx.ID(), numRewardOuts, env.ctx.AVAXAssetID, stateTest.wantValidationRewards, valOwner)
		requireOutputUTXO(t, stateTest.diff, rewardResult.rewardTx.ID(), numRewardOuts+1, env.ctx.AVAXAssetID, cfg.delegateeReward+cfg.accruedDelegateeRewards, delOwner)

		// No additional contiguous UTXOs for the validator or reward txs.
		requireNoOutputUTXO(t, stateTest.diff, rewardResult.addTx.ID(), len(rewardResult.uAddTx.Outputs())+len(rewardResult.uAddTx.StakeOuts))
		requireNoOutputUTXO(t, stateTest.diff, rewardResult.rewardTx.ID(), numRewardOuts+2)
	}

	// Verify reward UTXOs are correctly tracked via GetRewardUTXOs.
	require.NoError(t, rewardResult.onCommitState.Apply(env.state))
	require.NoError(t, env.state.Commit())

	rewardUTXOs, err := env.state.GetRewardUTXOs(rewardResult.rewardTx.ID())
	require.NoError(t, err)
	require.Len(t, rewardUTXOs, 2)
	require.Equal(t, cfg.potentialReward+cfg.accruedValidationRewards, rewardUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amount())
	require.Equal(t, cfg.delegateeReward+cfg.accruedDelegateeRewards, rewardUTXOs[1].Out.(*secp256k1fx.TransferOutput).Amount())
}

func TestRewardAutoRenewedValidatorTxRestake(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	cfg := rewardAutoRenewedValidatorConfig{
		weight:                   env.config.MinValidatorStake,
		potentialReward:          10_000_000,
		delegateeReward:          5_000_000,
		accruedValidationRewards: 1_000_000,
		accruedDelegateeRewards:  500_000,
		delegationRewardShares:   100_000,
		autoCompoundRewardShares: 400_000,
		restake:                  true,
	}
	rewardResult := rewardAutoRenewedValidator(t, env, cfg)

	var (
		rewardTx      = rewardResult.rewardTx
		valOwner      = rewardResult.uAddTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners)
		delOwner      = rewardResult.uAddTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners)
		numRewardOuts = len(rewardTx.Unsigned.Outputs())
	)

	// The new weight stays below MaxValidatorStake, so the restaked rewards are
	// not capped. The remainder of each reward is paid out as a UTXO.
	restakedValidationRewards := cfg.restakedReward(cfg.potentialReward)
	restakedDelegateeRewards := cfg.restakedReward(cfg.delegateeReward)
	wantWeight := cfg.weight + restakedValidationRewards + restakedDelegateeRewards
	wantAccruedValidationRewards := cfg.accruedValidationRewards + restakedValidationRewards
	wantAccruedDelegateeRewards := cfg.accruedDelegateeRewards + restakedDelegateeRewards
	wantValidationRewardUTXOAmount := cfg.potentialReward - restakedValidationRewards
	wantDelegateeRewardUTXOAmount := cfg.delegateeReward - restakedDelegateeRewards
	wantAbortDelegateeRewardUTXOAmount := cfg.delegateeReward + cfg.accruedDelegateeRewards

	currentSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	// Check onAbortState.
	{
		_, err := rewardResult.onAbortState.GetCurrentValidator(rewardResult.uAddTx.SubnetID(), rewardResult.uAddTx.NodeID())
		require.ErrorIs(t, err, database.ErrNotFound)

		requireStakeReturned(t, rewardResult.onAbortState, rewardResult.addTx, rewardResult.uAddTx)

		requireOutputUTXO(t, rewardResult.onAbortState, rewardTx.ID(), numRewardOuts, env.ctx.AVAXAssetID, cfg.accruedValidationRewards, valOwner)
		requireOutputUTXO(t, rewardResult.onAbortState, rewardTx.ID(), numRewardOuts+1, env.ctx.AVAXAssetID, wantAbortDelegateeRewardUTXOAmount, delOwner)

		// No additional contiguous UTXOs for the validator or reward txs.
		requireNoOutputUTXO(t, rewardResult.onAbortState, rewardResult.addTx.ID(), len(rewardResult.uAddTx.Outputs())+len(rewardResult.uAddTx.StakeOuts))
		requireNoOutputUTXO(t, rewardResult.onAbortState, rewardTx.ID(), numRewardOuts+2)

		abortSupply, err := rewardResult.onAbortState.GetCurrentSupply(constants.PrimaryNetworkID)
		require.NoError(t, err)
		require.Equal(t, currentSupply-cfg.potentialReward, abortSupply)
	}

	// Check onCommitState
	{
		validator, err := rewardResult.onCommitState.GetCurrentValidator(rewardResult.uAddTx.SubnetID(), rewardResult.uAddTx.NodeID())
		require.NoError(t, err)

		stakingInfo, err := rewardResult.onCommitState.GetStakingInfo(rewardResult.uAddTx.SubnetID(), rewardResult.uAddTx.NodeID())
		require.NoError(t, err)

		require.Equal(t, wantWeight, validator.Weight)
		require.Equal(t, wantAccruedValidationRewards, stakingInfo.AccruedValidationRewards)
		require.Equal(t, wantAccruedDelegateeRewards, stakingInfo.AccruedDelegateeRewards)

		// Check UTXOs for withdraws from auto-restake shares param.
		requireOutputUTXO(t, rewardResult.onCommitState, rewardTx.ID(), numRewardOuts, env.ctx.AVAXAssetID, wantValidationRewardUTXOAmount, valOwner)
		requireOutputUTXO(t, rewardResult.onCommitState, rewardTx.ID(), numRewardOuts+1, env.ctx.AVAXAssetID, wantDelegateeRewardUTXOAmount, delOwner)

		// No overflow UTXOs — new weight is below MaxValidatorStake.
		requireNoOutputUTXO(t, rewardResult.onCommitState, rewardTx.ID(), numRewardOuts+2)
		requireNoOutputUTXO(t, rewardResult.onCommitState, rewardResult.addTx.ID(), len(rewardResult.uAddTx.Outputs()))

		commitSupply, err := rewardResult.onCommitState.GetCurrentSupply(constants.PrimaryNetworkID)
		require.NoError(t, err)
		require.Equal(t, currentSupply+validator.PotentialReward, commitSupply)
	}

	// Verify reward UTXOs are correctly tracked via GetRewardUTXOs.
	require.NoError(t, rewardResult.onCommitState.Apply(env.state))
	require.NoError(t, env.state.Commit())

	rewardUTXOs, err := env.state.GetRewardUTXOs(rewardTx.ID())
	require.NoError(t, err)
	require.Len(t, rewardUTXOs, 2)
	require.Equal(t, wantValidationRewardUTXOAmount, rewardUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amount())
	require.Equal(t, wantDelegateeRewardUTXOAmount, rewardUTXOs[1].Out.(*secp256k1fx.TransferOutput).Amount())
}

func TestRewardAutoRenewedValidatorTxMaxValidatorStake(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	const restakingCapacity uint64 = 2_000_000
	vdrWeight := env.config.MaxValidatorStake - restakingCapacity

	cfg := rewardAutoRenewedValidatorConfig{
		weight:                   vdrWeight,
		potentialReward:          10_000_000,
		delegateeReward:          5_000_000,
		accruedValidationRewards: 1_000_000,
		accruedDelegateeRewards:  500_000,
		delegationRewardShares:   reward.PercentDenominator / 10,
		autoCompoundRewardShares: 4 * reward.PercentDenominator / 10,
		restake:                  true,
	}

	// On commit, the validator is configured to restake autoCompoundRewardShares
	// of the validation and pending delegatee rewards. Because this would exceed
	// MaxValidatorStake, the restaked portion is capped to restakingCapacity and
	// split proportionally between validation and delegatee rewards.
	uncappedRestakedValidationRewards := cfg.restakedReward(cfg.potentialReward)
	uncappedRestakedDelegateeRewards := cfg.restakedReward(cfg.delegateeReward)
	uncappedRestakedRewards := uncappedRestakedValidationRewards + uncappedRestakedDelegateeRewards

	wantRestakedValidationRewards := uint64(math.Round(float64(uncappedRestakedValidationRewards*restakingCapacity) / float64(uncappedRestakedRewards)))
	wantRestakedDelegateeRewards := restakingCapacity - wantRestakedValidationRewards
	wantAccruedValidationRewards := cfg.accruedValidationRewards + wantRestakedValidationRewards
	wantAccruedDelegateeRewards := cfg.accruedDelegateeRewards + wantRestakedDelegateeRewards
	wantValidationRewardUTXOAmount := cfg.potentialReward - wantRestakedValidationRewards
	wantDelegateeRewardUTXOAmount := cfg.delegateeReward - wantRestakedDelegateeRewards
	wantAbortDelegateeRewardUTXOAmount := cfg.delegateeReward + cfg.accruedDelegateeRewards

	rewardResult := rewardAutoRenewedValidator(t, env, cfg)

	var (
		rewardTx      = rewardResult.rewardTx
		valOwner      = rewardResult.uAddTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners)
		delOwner      = rewardResult.uAddTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners)
		numRewardOuts = len(rewardTx.Unsigned.Outputs())
	)

	currentSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	// Check onAbortState.
	{
		_, err := rewardResult.onAbortState.GetCurrentValidator(rewardResult.uAddTx.SubnetID(), rewardResult.uAddTx.NodeID())
		require.ErrorIs(t, err, database.ErrNotFound)

		requireStakeReturned(t, rewardResult.onAbortState, rewardResult.addTx, rewardResult.uAddTx)

		requireOutputUTXO(t, rewardResult.onAbortState, rewardTx.ID(), numRewardOuts, env.ctx.AVAXAssetID, cfg.accruedValidationRewards, valOwner)
		requireOutputUTXO(t, rewardResult.onAbortState, rewardTx.ID(), numRewardOuts+1, env.ctx.AVAXAssetID, wantAbortDelegateeRewardUTXOAmount, delOwner)

		// No additional contiguous UTXOs for the validator or reward txs.
		requireNoOutputUTXO(t, rewardResult.onAbortState, rewardResult.addTx.ID(), len(rewardResult.uAddTx.Outputs())+len(rewardResult.uAddTx.StakeOuts))
		requireNoOutputUTXO(t, rewardResult.onAbortState, rewardTx.ID(), numRewardOuts+2)

		abortSupply, err := rewardResult.onAbortState.GetCurrentSupply(constants.PrimaryNetworkID)
		require.NoError(t, err)
		require.Equal(t, currentSupply-cfg.potentialReward, abortSupply)
	}

	// Check onCommitState
	{
		validator, err := rewardResult.onCommitState.GetCurrentValidator(rewardResult.uAddTx.SubnetID(), rewardResult.uAddTx.NodeID())
		require.NoError(t, err)

		stakingInfo, err := rewardResult.onCommitState.GetStakingInfo(rewardResult.uAddTx.SubnetID(), rewardResult.uAddTx.NodeID())
		require.NoError(t, err)

		require.Equal(t, vdrWeight+wantRestakedValidationRewards+wantRestakedDelegateeRewards, validator.Weight)
		require.Equal(t, env.config.MaxValidatorStake, validator.Weight)
		require.Equal(t, wantAccruedValidationRewards, stakingInfo.AccruedValidationRewards)
		require.Equal(t, wantAccruedDelegateeRewards, stakingInfo.AccruedDelegateeRewards)

		// Rewards not restaked are paid as one validation reward UTXO and one
		// delegatee reward UTXO. These amounts include the shares the validator
		// chose not to restake and the rewards above MaxValidatorStake.
		requireOutputUTXO(t, rewardResult.onCommitState, rewardTx.ID(), numRewardOuts, env.ctx.AVAXAssetID, wantValidationRewardUTXOAmount, valOwner)
		requireOutputUTXO(t, rewardResult.onCommitState, rewardTx.ID(), numRewardOuts+1, env.ctx.AVAXAssetID, wantDelegateeRewardUTXOAmount, delOwner)

		// No additional contiguous UTXOs for the validator or reward txs.
		requireNoOutputUTXO(t, rewardResult.onCommitState, rewardTx.ID(), numRewardOuts+2)
		requireNoOutputUTXO(t, rewardResult.onCommitState, rewardResult.addTx.ID(), len(rewardResult.uAddTx.Outputs()))

		commitSupply, err := rewardResult.onCommitState.GetCurrentSupply(constants.PrimaryNetworkID)
		require.NoError(t, err)
		require.Equal(t, currentSupply+validator.PotentialReward, commitSupply)
	}

	// Verify reward UTXOs are correctly tracked via GetRewardUTXOs.
	require.NoError(t, rewardResult.onCommitState.Apply(env.state))
	require.NoError(t, env.state.Commit())

	rewardUTXOs, err := env.state.GetRewardUTXOs(rewardTx.ID())
	require.NoError(t, err)
	require.Len(t, rewardUTXOs, 2)
	require.Equal(t, wantValidationRewardUTXOAmount, rewardUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amount())
	require.Equal(t, wantDelegateeRewardUTXOAmount, rewardUTXOs[1].Out.(*secp256k1fx.TransferOutput).Amount())
}

// TestRewardDelegatorToAutoRenewedValidator tests the full delegator reward
// flow for a delegator to an auto-renewed validator: delegator gets their
// share, delegatee share is deferred to StakingInfo.DelegateeReward.
func TestRewardDelegatorToAutoRenewedValidator(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Latest)
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
	addAutoRenewedValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
		vdrNodeID,
		vdrWeight,
		must[*signer.ProofOfPossession](t)(signer.NewProofOfPossession(must[*localsigner.LocalSigner](t)(localsigner.New()))),
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{vdrRewardAddress}},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{vdrRewardAddress}},
		&secp256k1fx.OutputOwners{},
		delegationShares,
		reward.PercentDenominator, // auto compound 100%
		env.config.MinStakeDuration,
	)
	require.NoError(t, err)
	env.state.AddTx(addAutoRenewedValidatorTx, status.Committed)

	uAddAutoRenewedValidatorTx := addAutoRenewedValidatorTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	avax.Produce(env.state, addAutoRenewedValidatorTx.ID(), uAddAutoRenewedValidatorTx.Outputs())

	startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)
	duration := time.Duration(uAddAutoRenewedValidatorTx.Period) * time.Second
	vdrStaker, err := state.NewStaker(
		addAutoRenewedValidatorTx.ID(),
		uAddAutoRenewedValidatorTx,
		startTime,
		startTime.Add(duration),
		uAddAutoRenewedValidatorTx.Weight(),
		vdrPotentialReward,
	)
	require.NoError(t, err)

	require.NoError(t, env.state.PutCurrentValidator(vdrStaker))
	require.NoError(t, env.state.Commit())

	require.NoError(t, env.state.SetStakingInfo(vdrStaker.SubnetID, vdrStaker.NodeID, state.StakingInfo{
		AutoCompoundRewardShares: uAddAutoRenewedValidatorTx.AutoCompoundRewardShares,
		NextPeriod:               uAddAutoRenewedValidatorTx.Period,
	}))

	// Step 2: Create a delegator to this auto-renewed validator.
	delStartTime := genesistest.DefaultValidatorStartTimeUnix + 1
	delEndTime := uint64(startTime.Add(duration).Unix())

	addDelegatorTx, err := wallet.IssueAddDelegatorTx(
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

	uAddDelegatorTx := addDelegatorTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		addDelegatorTx.ID(),
		uAddDelegatorTx,
		time.Unix(int64(delStartTime), 0),
		delRewardAmt,
	)
	require.NoError(t, err)

	require.NoError(t, env.state.PutCurrentDelegator(delStaker))
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	require.NoError(t, env.state.Commit())

	// Step 3: Reward the delegator via RewardValidatorTx.
	rewardDelegatorTx, err := newRewardValidatorTx(t, addDelegatorTx.ID())
	require.NoError(t, err)

	delOnCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	delOnAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	require.NoError(t, ProposalTx(
		&env.backend,
		feeCalculator,
		rewardDelegatorTx,
		delOnCommitState,
		delOnAbortState,
	))

	// Verify delegator reward UTXO on commit: delegator gets 75% of delRewardAmt.
	wantDelegateeReward, wantDelegatorReward := reward.Split(delRewardAmt, delegationShares)
	delRewardOutputIndex := len(uAddDelegatorTx.Outputs()) + len(uAddDelegatorTx.Stake())
	requireOutputUTXO(
		t,
		delOnCommitState,
		addDelegatorTx.ID(),
		delRewardOutputIndex,
		env.ctx.AVAXAssetID,
		wantDelegatorReward,
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{delRewardAddress}},
	)

	// Verify delegatee reward is NOT distributed yet (deferred post-Cortina).
	requireNoOutputUTXO(t, delOnCommitState, addDelegatorTx.ID(), delRewardOutputIndex+1)

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

	// Verify reward UTXOs are correctly tracked via GetRewardUTXOs.
	rewardUTXOs, err := env.state.GetRewardUTXOs(addDelegatorTx.ID())
	require.NoError(t, err)
	require.Len(t, rewardUTXOs, 1)
	require.Equal(t, wantDelegatorReward, rewardUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amount())
}
