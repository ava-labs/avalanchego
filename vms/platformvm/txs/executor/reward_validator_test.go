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

var defaultAddAutoRenewedValidatorCfg = addAutoRenewedValidatorCfg{
	weight:                   defaultMinValidatorStake,
	potentialReward:          10_000_000,
	delegateeReward:          5_000_000,
	accruedValidationRewards: 1_000_000,
	accruedDelegateeRewards:  500_000,
	delegationRewardShares:   reward.PercentDenominator / 10,
	autoCompoundRewardShares: 4 * reward.PercentDenominator / 10,
	restake:                  true,
}

func newRewardValidatorTx(t testing.TB, txID ids.ID) (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{TxID: txID}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(snowtest.Context(t, snowtest.PChainID))
}

func newRewardAutoRenewedValidatorTx(t testing.TB, txID ids.ID, time time.Time) *txs.Tx {
	t.Helper()

	utx := &txs.RewardAutoRenewedValidatorTx{TxID: txID, Timestamp: uint64(time.Unix())}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	require.NoError(t, err)
	return tx
}

func newProofOfPossession(t testing.TB) *signer.ProofOfPossession {
	t.Helper()

	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)
	return pop
}

// newAutoRenewedValidatorStaker builds the staker corresponding to an AddAutoRenewedValidator tx.
func newAutoRenewedValidatorStaker(t testing.TB, addTx *txs.Tx, potentialReward uint64) *state.Staker {
	t.Helper()

	uAddTx := addTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	duration := time.Duration(uAddTx.Period) * time.Second
	autoRenewedValidatorStartTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)

	staker, err := state.NewStaker(
		addTx.ID(),
		uAddTx,
		autoRenewedValidatorStartTime,
		autoRenewedValidatorStartTime.Add(duration),
		uAddTx.Weight(),
		potentialReward,
	)
	require.NoError(t, err)
	return staker
}

// assertOutputUTXO asserts that the UTXO at (txID, outputIndex) is an AVAX
// secp256k1fx.TransferOutput with the given amount and owner.
func assertOutputUTXO(
	t testing.TB,
	chain state.Chain,
	txID ids.ID,
	outputIndex int,
	wantAmount uint64,
	wantOwner *secp256k1fx.OutputOwners,
) {
	t.Helper()

	utxoID := avax.UTXOID{TxID: txID, OutputIndex: uint32(outputIndex)}
	utxo, err := chain.GetUTXO(utxoID.InputID())
	require.NoError(t, err)

	out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
	require.True(t, ok)
	require.Equal(t, snowtest.AVAXAssetID, utxo.Asset.AssetID())
	require.Equal(t, wantAmount, out.Amt)
	require.True(t, wantOwner.Equals(&out.OutputOwners))
}

// assertNoExtraUTXOs asserts that there is no UTXO at (txID, outputIndex).
func assertNoExtraUTXOs(t testing.TB, chain state.Chain, txID ids.ID, outputIndex int) {
	t.Helper()

	utxoID := avax.UTXOID{TxID: txID, OutputIndex: uint32(outputIndex)}
	_, err := chain.GetUTXO(utxoID.InputID())
	require.ErrorIs(t, err, database.ErrNotFound)
}

// assertStakeReturned asserts that each of the validator's stake outputs
// is returned as a UTXO with the original amount.
func assertStakeReturned(t testing.TB, chain state.Chain, addTxID ids.ID, uAddTx *txs.AddAutoRenewedValidatorTx) {
	t.Helper()

	for i, stakeOut := range uAddTx.StakeOuts {
		utxoID := avax.UTXOID{
			TxID:        addTxID,
			OutputIndex: uint32(len(uAddTx.Outputs()) + i),
		}
		utxo, err := chain.GetUTXO(utxoID.InputID())
		require.NoError(t, err)
		require.Equal(t, stakeOut.Out.Amount(), utxo.Out.(*secp256k1fx.TransferOutput).Amt)
	}
}

// addAutoRenewedValidatorCfg parameterizes addAutoRenewedValidator.
type addAutoRenewedValidatorCfg struct {
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

func newAddAutoRenewedValidatorTx(
	t testing.TB,
	env *environment,
	weight uint64,
	delegationRewardShares uint32,
	autoCompoundRewardShares uint32,
) *txs.Tx {
	t.Helper()

	wallet := newWallet(t, env, walletConfig{})
	addTx, err := wallet.IssueAddAutoRenewedValidatorTx(
		ids.GenerateTestNodeID(),
		weight,
		newProofOfPossession(t),
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{},
		delegationRewardShares,
		autoCompoundRewardShares,
		env.config.MinStakeDuration,
	)
	require.NoError(t, err)
	return addTx
}

// addAutoRenewedValidator adds addTx as a current validator at the end of its
// staking period and performs related state changes.
func addAutoRenewedValidator(t testing.TB, parent *state.State, addTx *txs.Tx, cfg addAutoRenewedValidatorCfg) {
	t.Helper()

	diff, err := state.NewDiffOn(parent, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	diff.AddTx(addTx, status.Committed)

	uAddTx := addTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	avax.Produce(diff, addTx.ID(), uAddTx.Outputs())

	staker := newAutoRenewedValidatorStaker(t, addTx, cfg.potentialReward)
	require.NoError(t, diff.PutCurrentValidator(staker))

	stakingInfo := state.StakingInfo{
		DelegateeReward:          cfg.delegateeReward,
		AccruedValidationRewards: cfg.accruedValidationRewards,
		AccruedDelegateeRewards:  cfg.accruedDelegateeRewards,
		AutoCompoundRewardShares: uAddTx.AutoCompoundRewardShares,
	}
	if cfg.restake {
		stakingInfo.NextPeriod = uAddTx.Period
	}
	require.NoError(t, diff.SetStakingInfo(staker.SubnetID, staker.NodeID, stakingInfo))

	diff.SetTimestamp(staker.EndTime)

	require.NoError(t, diff.Apply(parent))
	require.NoError(t, parent.Commit())
}

// wantReward is the pair of reward UTXOs a RewardAutoRenewedValidatorTx
type wantReward struct {
	// Rewards from completing a staking cycle
	validation uint64
	// Fees earned from delegators on this validator
	delegatee  uint64
}

// wantRewardAutoRenewedValidatorResult describes the expected commit and abort
// states after rewarding an auto-renewed validator.
type wantRewardAutoRenewedValidatorResult struct {
	// commitRestaked reports whether the validator remains a current validator
	// on commit. When false it is gracefully removed and its stake returned,
	// and the commit* fields below are ignored.
	commitRestaked                 bool
	commitWeight                   uint64
	commitAccruedValidationRewards uint64
	commitAccruedDelegateeRewards  uint64

	commitReward wantReward
	abortReward  wantReward
}

// assertRewardAutoRenewedValidator asserts the commit and abort states produced
// by rewarding the staged validator addTx, then applies the commit state and
// verifies the persisted reward UTXOs.
func assertRewardAutoRenewedValidator(
	t *testing.T,
	state *state.State,
	addTx *txs.Tx,
	rewardTx *txs.Tx,
	onCommitState *state.Diff,
	onAbortState *state.Diff,
	want wantRewardAutoRenewedValidatorResult,
) {
	t.Helper()

	uAddTx := addTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	currentSupply := must[uint64](t)(state.GetCurrentSupply(constants.PrimaryNetworkID))

	// The staked validator's potential reward is burned from the supply on abort.
	stakedValidator, err := state.GetCurrentValidator(uAddTx.SubnetID(), uAddTx.NodeID())
	require.NoError(t, err)

	// On abort the validator is always removed and its stake returned.
	assertValidatorRemoved(t, onAbortState, addTx, rewardTx.ID(), want.abortReward)
	abortSupply := must[uint64](t)(onAbortState.GetCurrentSupply(constants.PrimaryNetworkID))
	require.Equal(t, currentSupply-stakedValidator.PotentialReward, abortSupply)

	// On commit the validator is either restaked or gracefully removed.
	if want.commitRestaked {
		validator, err := onCommitState.GetCurrentValidator(uAddTx.SubnetID(), uAddTx.NodeID())
		require.NoError(t, err)
		stakingInfo, err := onCommitState.GetStakingInfo(uAddTx.SubnetID(), uAddTx.NodeID())
		require.NoError(t, err)

		require.Equal(t, want.commitWeight, validator.Weight)
		require.Equal(t, want.commitAccruedValidationRewards, stakingInfo.AccruedValidationRewards)
		require.Equal(t, want.commitAccruedDelegateeRewards, stakingInfo.AccruedDelegateeRewards)

		assertRewards(t, onCommitState, addTx, rewardTx.ID(), want.commitReward)
		// The stake is restaked rather than returned: no UTXO at the first stake index.
		assertNoExtraUTXOs(t, onCommitState, addTx.ID(), len(uAddTx.Outputs()))

		commitSupply := must[uint64](t)(onCommitState.GetCurrentSupply(constants.PrimaryNetworkID))
		require.Equal(t, currentSupply+validator.PotentialReward, commitSupply)
	} else {
		assertValidatorRemoved(t, onCommitState, addTx, rewardTx.ID(), want.commitReward)
		commitSupply := must[uint64](t)(onCommitState.GetCurrentSupply(constants.PrimaryNetworkID))
		require.Equal(t, currentSupply, commitSupply)
	}

	// The persisted reward UTXOs match the commit branch.
	require.NoError(t, onCommitState.Apply(state))
	require.NoError(t, state.Commit())

	rewardUTXOs, err := state.GetRewardUTXOs(rewardTx.ID())
	require.NoError(t, err)
	require.Len(t, rewardUTXOs, 2)
	require.Equal(t, want.commitReward.validation, rewardUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amount())
	require.Equal(t, want.commitReward.delegatee, rewardUTXOs[1].Out.(*secp256k1fx.TransferOutput).Amount())
}

// assertValidatorRemoved asserts that chain no longer has the validator from
// addTx, that its stake was returned, and that it produced the expected reward
// UTXOs.
func assertValidatorRemoved(
	t *testing.T,
	chain state.Chain,
	addTx *txs.Tx,
	rewardTxID ids.ID,
	rewards wantReward,
) {
	t.Helper()

	uAddTx := addTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)

	_, err := chain.GetCurrentValidator(uAddTx.SubnetID(), uAddTx.NodeID())
	require.ErrorIs(t, err, database.ErrNotFound)

	assertStakeReturned(t, chain, addTx.ID(), uAddTx)
	assertRewards(t, chain, addTx, rewardTxID, rewards)

	// No UTXO past the returned stake.
	assertNoExtraUTXOs(t, chain, addTx.ID(), len(uAddTx.Outputs())+len(uAddTx.StakeOuts))
}

// assertRewards asserts the validation and delegatee reward UTXOs of
// rewardTxID and that there are no further contiguous reward UTXOs.
func assertRewards(
	t *testing.T,
	chain state.Chain,
	addTx *txs.Tx,
	rewardTxID ids.ID,
	utxos wantReward,
) {
	t.Helper()

	// Output index layout of a RewardAutoRenewedValidatorTx's reward UTXOs. The tx
	// itself has no outputs, so its reward UTXOs occupy the first output indices.
	const (
		validationRewardOutputIndex = 0
		delegateeRewardOutputIndex  = 1
	)

	uAddTx := addTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	assertOutputUTXO(
		t,
		chain,
		rewardTxID,
		validationRewardOutputIndex,
		utxos.validation,
		uAddTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners),
	)
	assertOutputUTXO(
		t,
		chain,
		rewardTxID,
		delegateeRewardOutputIndex,
		utxos.delegatee,
		uAddTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners),
	)

	assertNoExtraUTXOs(t, chain, rewardTxID, delegateeRewardOutputIndex+1)
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
		// staker.
		stakerF func(t *testing.T, env *environment) (*txs.Tx, *state.Staker)
	}{
		{
			name: "auto_renewed_validator",
			stakerF: func(t *testing.T, env *environment) (*txs.Tx, *state.Staker) {
				tx := newAddAutoRenewedValidatorTx(t, env, env.config.MinValidatorStake, 1, 1)
				staker := newAutoRenewedValidatorStaker(t, tx, 1_000_000)
				return tx, staker
			},
		},
		{
			name: "permissioned_subnet_validator",
			stakerF: func(t *testing.T, env *environment) (*txs.Tx, *state.Staker) {
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

			addStakerTx, staker := test.stakerF(t, env)

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

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	addPermissionlessValidatorTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(genesistest.DefaultValidatorStartTime.Add(2 * env.config.MinStakeDuration).Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		newProofOfPossession(t),
		env.ctx.AVAXAssetID,
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		reward.PercentDenominator,
	)
	require.NoError(t, err)
	diff.AddTx(addPermissionlessValidatorTx, status.Committed)

	uAddPermissionlessValidatorTx := addPermissionlessValidatorTx.Unsigned.(*txs.AddPermissionlessValidatorTx)
	staker, err := state.NewCurrentStaker(
		addPermissionlessValidatorTx.ID(),
		uAddPermissionlessValidatorTx,
		time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0),
		uint64(1000000),
	)
	require.NoError(t, err)
	require.NoError(t, diff.PutCurrentValidator(staker))
	diff.SetTimestamp(staker.EndTime)

	require.NoError(t, diff.Apply(env.state))
	chainTime := env.state.GetTimestamp()

	tests := []struct {
		name    string
		txF     func(t testing.TB) *txs.Tx
		wantErr error
	}{
		{
			name: "wrong_staker",
			txF: func(t testing.TB) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, ids.GenerateTestID(), chainTime)
			},
			wantErr: ErrRemoveWrongStaker,
		},
		{
			name: "invalid_timestamp",
			txF: func(t testing.TB) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, staker.TxID, chainTime.Add(-time.Second))
			},
			wantErr: errInvalidTimestamp,
		},
		{
			name: "invalid_validator_tx",
			txF: func(t testing.TB) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, addPermissionlessValidatorTx.ID(), chainTime)
			},
			wantErr: errShouldBeAutoRenewedStaker,
		},
		{
			name: "wrong_number_of_credentials",
			txF: func(t testing.TB) *txs.Tx {
				rewardTx := newRewardAutoRenewedValidatorTx(t, staker.TxID, chainTime)
				rewardTx.Creds = append(rewardTx.Creds, &secp256k1fx.Credential{})
				return rewardTx
			},
			wantErr: errWrongNumberOfCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err = ProposalTx(
				&env.backend,
				feeCalculator,
				tt.txF(t),
				must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onCommitState
				must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)), // onAbortState
			)

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRewardAutoRenewedValidatorTx(t *testing.T) {
	tests := []struct {
		name   string
		addCfg addAutoRenewedValidatorCfg
		want   wantRewardAutoRenewedValidatorResult
	}{
		{
			name: "graceful_stop",
			addCfg: func() addAutoRenewedValidatorCfg {
				cfg := defaultAddAutoRenewedValidatorCfg
				cfg.restake = false

				return cfg
			}(),
			want: func() wantRewardAutoRenewedValidatorResult {
				cfg := defaultAddAutoRenewedValidatorCfg

				// The validator is removed on both branches; the only difference
				// is that commit pays out the potential reward and abort does not.
				return wantRewardAutoRenewedValidatorResult{
					commitRestaked: false,
					commitReward: wantReward{
						validation: cfg.potentialReward + cfg.accruedValidationRewards,
						delegatee:  cfg.delegateeReward + cfg.accruedDelegateeRewards,
					},
					abortReward: wantReward{
						validation: cfg.accruedValidationRewards,
						delegatee:  cfg.delegateeReward + cfg.accruedDelegateeRewards,
					},
				}
			}(),
		},
		{
			name:   "restake",
			addCfg: defaultAddAutoRenewedValidatorCfg,
			want: func() wantRewardAutoRenewedValidatorResult {
				cfg := defaultAddAutoRenewedValidatorCfg

				// autoCompoundRewardShares is 40%, so 40% of each reward is
				// restaked and the remaining 60% paid out. The new weight stays
				// below MaxValidatorStake, so nothing is capped.
				const (
					restakedValidationRewards uint64 = 4_000_000 // 40% of potentialReward (10_000_000)
					restakedDelegateeRewards  uint64 = 2_000_000 // 40% of delegateeReward (5_000_000)
				)

				return wantRewardAutoRenewedValidatorResult{
					commitRestaked:                 true,
					commitWeight:                   cfg.weight + restakedValidationRewards + restakedDelegateeRewards,
					commitAccruedValidationRewards: cfg.accruedValidationRewards + restakedValidationRewards,
					commitAccruedDelegateeRewards:  cfg.accruedDelegateeRewards + restakedDelegateeRewards,
					commitReward: wantReward{
						validation: cfg.potentialReward - restakedValidationRewards,
						delegatee:  cfg.delegateeReward - restakedDelegateeRewards,
					},
					abortReward: wantReward{
						validation: cfg.accruedValidationRewards,
						delegatee:  cfg.delegateeReward + cfg.accruedDelegateeRewards,
					},
				}
			}(),
		},
		{
			name: "overflow_max_validator_stake",
			addCfg: func() addAutoRenewedValidatorCfg {
				const restakingCapacity uint64 = 2_000_000
				stakingCfg := defaultConfig(upgradetest.Latest)

				cfg := defaultAddAutoRenewedValidatorCfg
				cfg.weight = stakingCfg.MaxValidatorStake - restakingCapacity

				return cfg
			}(),
			want: func() wantRewardAutoRenewedValidatorResult {
				stakingCfg := defaultConfig(upgradetest.Latest)
				const restakingCapacity uint64 = 2_000_000

				cfg := defaultAddAutoRenewedValidatorCfg
				cfg.weight = stakingCfg.MaxValidatorStake - restakingCapacity

				// autoCompoundRewardShares (40%) would restake 4M of the validation
				// reward and 2M of the delegatee reward (6M total), but only
				// restakingCapacity (2M) fits under MaxValidatorStake. The 2M is
				// split proportionally, 4M:2M.
				const (
					restakedValidationRewards uint64 = 1_333_333 // round(2_000_000 * 4M/6M)
					restakedDelegateeRewards  uint64 = 666_667   // restakingCapacity - 1_333_333
				)
				return wantRewardAutoRenewedValidatorResult{
					commitRestaked:                 true,
					commitWeight:                   stakingCfg.MaxValidatorStake, // restaked rewards capped at the max
					commitAccruedValidationRewards: cfg.accruedValidationRewards + restakedValidationRewards,
					commitAccruedDelegateeRewards:  cfg.accruedDelegateeRewards + restakedDelegateeRewards,
					commitReward: wantReward{
						validation: cfg.potentialReward - restakedValidationRewards,
						delegatee:  cfg.delegateeReward - restakedDelegateeRewards,
					},
					abortReward: wantReward{
						validation: cfg.accruedValidationRewards,
						delegatee:  cfg.delegateeReward + cfg.accruedDelegateeRewards,
					},
				}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)
			cfg := tt.addCfg

			addTx := newAddAutoRenewedValidatorTx(t, env, cfg.weight, cfg.delegationRewardShares, cfg.autoCompoundRewardShares)
			addAutoRenewedValidator(t, env.state, addTx, cfg)

			rewardTx := newRewardAutoRenewedValidatorTx(t, addTx.ID(), env.state.GetTimestamp())
			onCommitState := must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed))
			onAbortState := must[*state.Diff](t)(state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed))

			require.NoError(t, ProposalTx(
				&env.backend,
				state.PickFeeCalculator(env.config, env.state),
				rewardTx,
				onCommitState,
				onAbortState,
			))

			assertRewardAutoRenewedValidator(
				t,
				env.state,
				addTx,
				rewardTx,
				onCommitState,
				onAbortState,
				tt.want,
			)
		})
	}
}

// TestRewardDelegatorToAutoRenewedValidator tests the full delegator reward
// flow for a delegator to an auto-renewed validator: delegator gets their
// share, delegatee share is deferred to StakingInfo.DelegateeReward.
func TestRewardDelegatorToAutoRenewedValidator(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Latest)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)

		delRewardAddress   = ids.GenerateTestShortID()
		delegationShares   = uint32(reward.PercentDenominator / 4) // 25% to delegatee
		vdrWeight          = env.config.MinValidatorStake
		delRewardAmt       = uint64(1_000_000)
		vdrPotentialReward = uint64(2_000_000)
	)

	addAutoRenewedValidatorTx := newAddAutoRenewedValidatorTx(
		t,
		env,
		vdrWeight,
		delegationShares,
		reward.PercentDenominator, // auto compound 100%
	)
	addAutoRenewedValidator(t, env.state, addAutoRenewedValidatorTx, addAutoRenewedValidatorCfg{
		potentialReward: vdrPotentialReward,
		restake:         true,
	})

	vdrNodeID := addAutoRenewedValidatorTx.Unsigned.(*txs.AddAutoRenewedValidatorTx).NodeID()
	vdr, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, vdrNodeID)
	require.NoError(t, err)

	// Add a delegator running for the validator's full period.
	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	delStartTime := genesistest.DefaultValidatorStartTimeUnix + 1
	delEndTime := uint64(vdr.EndTime.Unix())

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

	require.NoError(t, diff.PutCurrentDelegator(delStaker))
	diff.AddTx(addDelegatorTx, status.Committed)
	diff.SetTimestamp(time.Unix(int64(delEndTime), 0))

	require.NoError(t, diff.Apply(env.state))

	// Reward the delegator via RewardValidatorTx.
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
	assertOutputUTXO(
		t,
		delOnCommitState,
		addDelegatorTx.ID(),
		delRewardOutputIndex,
		wantDelegatorReward,
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{delRewardAddress}},
	)

	// Verify delegatee reward is NOT distributed yet (deferred post-Cortina).
	assertNoExtraUTXOs(t, delOnCommitState, addDelegatorTx.ID(), delRewardOutputIndex+1)

	// Verify delegatee reward in StakingInfo.
	stakingInfo, err := delOnCommitState.GetStakingInfo(constants.PrimaryNetworkID, vdrNodeID)
	require.NoError(t, err)
	require.Equal(t, wantDelegateeReward, stakingInfo.DelegateeReward)

	stakingInfo, err = delOnAbortState.GetStakingInfo(constants.PrimaryNetworkID, vdrNodeID)
	require.NoError(t, err)
	require.Zero(t, stakingInfo.DelegateeReward)

	// Commit the delegator diff.
	require.NoError(t, delOnCommitState.Apply(env.state))

	// Verify reward UTXOs are correctly tracked via GetRewardUTXOs.
	rewardUTXOs, err := env.state.GetRewardUTXOs(addDelegatorTx.ID())
	require.NoError(t, err)
	require.Len(t, rewardUTXOs, 1)
	require.Equal(t, wantDelegatorReward, rewardUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amount())
}
