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

var defaultAutoRenewedValidatorConfig = autoRenewedValidatorConfig{
	weight:                   defaultMinValidatorStake,
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

// assertUTXO asserts that the UTXO at (txID, outputIndex) is an AVAX
// secp256k1fx.TransferOutput with the given amount and owner.
func assertUTXO(
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
func assertStakeReturned(t testing.TB, chain state.Chain, addTxID ids.ID, tx *txs.AddAutoRenewedValidatorTx) {
	t.Helper()

	for i, stakeOut := range tx.StakeOuts {
		utxoID := avax.UTXOID{
			TxID:        addTxID,
			OutputIndex: uint32(len(tx.Outputs()) + i),
		}
		utxo, err := chain.GetUTXO(utxoID.InputID())
		require.NoError(t, err)

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		require.True(t, ok)

		require.Equal(t, stakeOut.Out.Amount(), out.Amt)
	}
}

// autoRenewedValidatorConfig parameterizes addAutoRenewedValidator.
type autoRenewedValidatorConfig struct {
	weight                   uint64
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
func (c autoRenewedValidatorConfig) restakedReward(amount uint64) uint64 {
	withdrawnShares := reward.PercentDenominator - uint64(c.autoCompoundRewardShares)
	withdrawnAmount := withdrawnShares * amount / reward.PercentDenominator
	return amount - withdrawnAmount
}

// newAddAutoRenewedValidatorTx issues an AddAutoRenewedValidator tx for a new
// random node with random reward owners, staking for the minimum duration.
func newAddAutoRenewedValidatorTx(
	t testing.TB,
	env *environment,
	weight uint64,
	delegationRewardShares uint32,
	autoCompoundRewardShares uint32,
) *txs.Tx {
	t.Helper()

	wallet := newWallet(t, env, walletConfig{})
	tx, err := wallet.IssueAddAutoRenewedValidatorTx(
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
	return tx
}

// addAutoRenewedValidator executes addTx as a current validator via StandardTx,
// attaches its staking info, advances the chain to the end of its staking
// period, and commits the result to env.state.
func addAutoRenewedValidator(t testing.TB, env *environment, tx *txs.Tx, cfg autoRenewedValidatorConfig) {
	t.Helper()

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		tx,
		diff,
	)
	require.NoError(t, err)
	diff.AddTx(tx, status.Committed)

	stakingInfo := state.StakingInfo{
		DelegateeReward:          cfg.delegateeReward,
		AccruedValidationRewards: cfg.accruedValidationRewards,
		AccruedDelegateeRewards:  cfg.accruedDelegateeRewards,
		AutoCompoundRewardShares: cfg.autoCompoundRewardShares,
	}
	if cfg.restake {
		stakingInfo.NextPeriod = env.config.MinValidatorStake
	}

	nodeID := (tx.Unsigned.(*txs.AddAutoRenewedValidatorTx)).NodeID()
	require.NoError(t, diff.SetStakingInfo(snowtest.PChainID, nodeID, stakingInfo))

	diff.SetTimestamp(env.state.GetTimestamp().Add(env.config.MinStakeDuration))

	require.NoError(t, diff.Apply(env.state))
	require.NoError(t, env.state.Commit())
}

// wantReward is the pair of reward UTXOs produced by a
// RewardAutoRenewedValidatorTx.
type wantReward struct {
	validation uint64 // reward from completing a staking cycle
	delegatee  uint64 // fees earned from delegators on this validator
}

// wantRewardAutoRenewedValidator describes the expected commit and abort states
// after rewarding an auto-renewed validator.
type wantRewardAutoRenewedValidator struct {
	// commitRestaked reports whether the validator remains a current validator on
	// commit. When false it is gracefully removed and its stake returned, and the
	// commit* fields below are ignored.
	commitRestaked                 bool
	commitWeight                   uint64
	commitAccruedValidationRewards uint64
	commitAccruedDelegateeRewards  uint64

	commitReward wantReward
	abortReward  wantReward
}

// assertRewards asserts the validation and delegatee reward UTXOs of rewardTxID
// and that there are no further contiguous reward UTXOs.
func assertRewards(t testing.TB, chain state.Chain, tx *txs.Tx, rewardTxID ids.ID, rewards wantReward) {
	t.Helper()

	// Output index layout of a RewardAutoRenewedValidatorTx's reward UTXOs. The
	// tx itself has no outputs, so its reward UTXOs occupy the first indices.
	const (
		validationRewardOutputIndex = 0
		delegateeRewardOutputIndex  = 1
	)

	stakerTx := tx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	assertUTXO(t, chain, rewardTxID, validationRewardOutputIndex, rewards.validation, stakerTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners))
	assertUTXO(t, chain, rewardTxID, delegateeRewardOutputIndex, rewards.delegatee, stakerTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners))

	assertNoExtraUTXOs(t, chain, rewardTxID, delegateeRewardOutputIndex+1)
}

// assertValidatorRemoved asserts that chain no longer has the
// validator from tx, that its stake was returned, and that it produced the
// expected reward UTXOs.
func assertValidatorRemoved(t testing.TB, chain state.Chain, tx *txs.Tx, rewardTxID ids.ID, rewards wantReward) {
	t.Helper()

	stakerTx := tx.Unsigned.(*txs.AddAutoRenewedValidatorTx)

	_, err := chain.GetCurrentValidator(stakerTx.SubnetID(), stakerTx.NodeID())
	require.ErrorIs(t, err, database.ErrNotFound)

	assertStakeReturned(t, chain, tx.ID(), stakerTx)
	assertRewards(t, chain, tx, rewardTxID, rewards)

	// No UTXO past the returned stake.
	assertNoExtraUTXOs(t, chain, tx.ID(), len(stakerTx.Outputs())+len(stakerTx.StakeOuts))
}

// assertRewardAutoRenewedValidator asserts the commit and abort states produced
// by rewarding the staged validator addTx, then applies the commit state and
// verifies the persisted reward UTXOs.
func assertRewardAutoRenewedValidator(
	t testing.TB,
	state *state.State,
	tx *txs.Tx,
	rewardTx *txs.Tx,
	onCommitState *state.Diff,
	onAbortState *state.Diff,
	want wantRewardAutoRenewedValidator,
) {
	t.Helper()

	stakerTx := tx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	currentSupply := must[uint64](t)(state.GetCurrentSupply(constants.PrimaryNetworkID))

	stakedValidator, err := state.GetCurrentValidator(stakerTx.SubnetID(), stakerTx.NodeID())
	require.NoError(t, err)

	// On abort the validator is always removed and its potential reward is
	// burned from the supply.
	assertValidatorRemoved(t, onAbortState, tx, rewardTx.ID(), want.abortReward)

	abortSupply, err := onAbortState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)
	require.Equal(t, currentSupply-stakedValidator.PotentialReward, abortSupply)

	commitSupply, err := onCommitState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	// On commit the validator is either restaked or gracefully removed.
	if want.commitRestaked {
		validator, err := onCommitState.GetCurrentValidator(stakerTx.SubnetID(), stakerTx.NodeID())
		require.NoError(t, err)
		stakingInfo, err := onCommitState.GetStakingInfo(stakerTx.SubnetID(), stakerTx.NodeID())
		require.NoError(t, err)

		require.Equal(t, want.commitWeight, validator.Weight)
		require.Equal(t, want.commitAccruedValidationRewards, stakingInfo.AccruedValidationRewards)
		require.Equal(t, want.commitAccruedDelegateeRewards, stakingInfo.AccruedDelegateeRewards)

		assertRewards(t, onCommitState, tx, rewardTx.ID(), want.commitReward)
		// The stake is restaked rather than returned: no UTXO at the first stake index.
		assertNoExtraUTXOs(t, onCommitState, tx.ID(), len(stakerTx.Outputs()))

		require.Equal(t, currentSupply+validator.PotentialReward, commitSupply)
	} else {
		assertValidatorRemoved(t, onCommitState, tx, rewardTx.ID(), want.commitReward)
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
			name: "auto_renewed_validator",
			setup: func(t *testing.T, env *environment) (*txs.Tx, *state.Staker) {
				tx := newAddAutoRenewedValidatorTx(t, env, env.config.MinValidatorStake, reward.PercentDenominator, reward.PercentDenominator)
				stakerTx := tx.Unsigned.(*txs.AddAutoRenewedValidatorTx)

				endTime := startTime.Add(time.Duration(stakerTx.Period) * time.Second)
				potentialReward := uint64(1)

				staker, err := state.NewStaker(
					tx.ID(),
					stakerTx,
					startTime,
					endTime,
					stakerTx.Weight(),
					potentialReward,
				)
				require.NoError(t, err)

				return tx, staker
			},
		},
		{
			name: "permissioned_subnet_validator",
			setup: func(t *testing.T, env *environment) (*txs.Tx, *state.Staker) {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				endTime := startTime.Add(env.config.MinStakeDuration)
				tx, err := wallet.IssueAddSubnetValidatorTx(
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

				stakerTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)

				vdrStaker, err := state.NewStaker(
					tx.ID(),
					stakerTx,
					startTime,
					endTime,
					stakerTx.Weight(),
					0,
				)
				require.NoError(t, err)

				return tx, vdrStaker
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)

			stakerTx, staker := test.setup(t, env)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			diff.AddTx(stakerTx, status.Committed)
			require.NoError(t, diff.PutCurrentValidator(staker))
			diff.SetTimestamp(staker.EndTime)
			require.NoError(t, diff.Apply(env.state))
			require.NoError(t, env.state.Commit())

			rewardTx, err := newRewardValidatorTx(t, staker.TxID)
			require.NoError(t, err)

			onCommitState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			err = ProposalTx(
				&env.backend,
				state.PickFeeCalculator(env.config, env.state),
				rewardTx,
				onCommitState,
				onAbortState,
			)
			require.ErrorIs(t, err, errUnexpectedStakerTxType)
		})
	}
}

func TestRewardAutoRenewedValidatorTxErrors(t *testing.T) {
	tests := []struct {
		name    string
		tx      func(t testing.TB, txID ids.ID, endTime time.Time) *txs.Tx
		wantErr error
	}{
		{
			name: "wrong_staker",
			tx: func(t testing.TB, _ ids.ID, endTime time.Time) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, ids.GenerateTestID(), endTime)
			},
			wantErr: ErrRemoveWrongStaker,
		},
		{
			name: "invalid_timestamp",
			tx: func(t testing.TB, txID ids.ID, endTime time.Time) *txs.Tx {
				return newRewardAutoRenewedValidatorTx(t, txID, endTime.Add(-time.Second))
			},
			wantErr: errInvalidTimestamp,
		},
		{
			name:    "invalid_validator_tx",
			tx:      newRewardAutoRenewedValidatorTx,
			wantErr: errShouldBeAutoRenewedStaker,
		},
		{
			name: "wrong_number_of_credentials",
			tx: func(t testing.TB, txID ids.ID, endTime time.Time) *txs.Tx {
				rewardTx := newRewardAutoRenewedValidatorTx(t, txID, endTime)
				rewardTx.Creds = append(rewardTx.Creds, &secp256k1fx.Credential{})
				return rewardTx
			},
			wantErr: errWrongNumberOfCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				env           = newEnvironment(t, upgradetest.Latest)
				wallet        = newWallet(t, env, walletConfig{})
				feeCalculator = state.PickFeeCalculator(env.config, env.state)
				endTime       = genesistest.DefaultValidatorStartTime.Add(2 * env.config.MinStakeDuration)
			)

			tx, err := wallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(endTime.Unix()),
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

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, diff)
			require.NoError(t, err)

			diff.AddTx(tx, status.Committed)
			diff.SetTimestamp(endTime)

			require.NoError(t, diff.Apply(env.state))

			onCommitState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)
			onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			rewardTx := tt.tx(t, tx.ID(), endTime)

			err = ProposalTx(
				&env.backend,
				feeCalculator,
				rewardTx,
				onCommitState,
				onAbortState,
			)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRewardAutoRenewedValidatorTx(t *testing.T) {
	const restakingCapacity uint64 = 2_000_000

	tests := []struct {
		name   string
		config func(env *environment) autoRenewedValidatorConfig
		want   func(cfg autoRenewedValidatorConfig, potentialReward uint64) wantRewardAutoRenewedValidator
	}{
		{
			// The validator gracefully stops, so it leaves the set on commit and
			// is paid the full potential reward plus accrued rewards.
			name: "graceful_stop",
			config: func(*environment) autoRenewedValidatorConfig {
				cfg := defaultAutoRenewedValidatorConfig
				cfg.restake = false
				return cfg
			},
			want: func(cfg autoRenewedValidatorConfig, potentialReward uint64) wantRewardAutoRenewedValidator {
				return wantRewardAutoRenewedValidator{
					commitRestaked: false,
					commitReward: wantReward{
						validation: potentialReward + cfg.accruedValidationRewards,
						delegatee:  cfg.delegateeReward + cfg.accruedDelegateeRewards,
					},
					abortReward: wantReward{
						validation: cfg.accruedValidationRewards,
						delegatee:  cfg.delegateeReward + cfg.accruedDelegateeRewards,
					},
				}
			},
		},
		{
			// The new weight stays below MaxValidatorStake, so the restaked
			// rewards are not capped. The remainder of each reward is paid out.
			name: "restake_below_max",
			config: func(*environment) autoRenewedValidatorConfig {
				return defaultAutoRenewedValidatorConfig // restake: true
			},
			want: func(cfg autoRenewedValidatorConfig, potentialReward uint64) wantRewardAutoRenewedValidator {
				restakedValidation := cfg.restakedReward(potentialReward)
				restakedDelegatee := cfg.restakedReward(cfg.delegateeReward)
				return wantRewardAutoRenewedValidator{
					commitRestaked:                 true,
					commitWeight:                   cfg.weight + restakedValidation + restakedDelegatee,
					commitAccruedValidationRewards: cfg.accruedValidationRewards + restakedValidation,
					commitAccruedDelegateeRewards:  cfg.accruedDelegateeRewards + restakedDelegatee,
					commitReward: wantReward{
						validation: potentialReward - restakedValidation,
						delegatee:  cfg.delegateeReward - restakedDelegatee,
					},
					abortReward: wantReward{
						validation: cfg.accruedValidationRewards,
						delegatee:  cfg.delegateeReward + cfg.accruedDelegateeRewards,
					},
				}
			},
		},
		{
			// The validator is configured to restake autoCompoundRewardShares of
			// the validation and pending delegatee rewards. Because this would
			// exceed MaxValidatorStake, the restaked portion is capped to
			// restakingCapacity and split proportionally between the two rewards.
			name: "restake_capped_at_max",
			config: func(env *environment) autoRenewedValidatorConfig {
				cfg := defaultAutoRenewedValidatorConfig
				cfg.weight = env.config.MaxValidatorStake - restakingCapacity
				return cfg
			},
			want: func(cfg autoRenewedValidatorConfig, potentialReward uint64) wantRewardAutoRenewedValidator {
				uncappedValidation := cfg.restakedReward(potentialReward)
				uncappedDelegatee := cfg.restakedReward(cfg.delegateeReward)
				uncapped := uncappedValidation + uncappedDelegatee

				restakedValidation := uint64(math.Round(float64(uncappedValidation*restakingCapacity) / float64(uncapped)))
				restakedDelegatee := restakingCapacity - restakedValidation
				return wantRewardAutoRenewedValidator{
					commitRestaked:                 true,
					commitWeight:                   cfg.weight + restakedValidation + restakedDelegatee, // == MaxValidatorStake
					commitAccruedValidationRewards: cfg.accruedValidationRewards + restakedValidation,
					commitAccruedDelegateeRewards:  cfg.accruedDelegateeRewards + restakedDelegatee,
					commitReward: wantReward{
						validation: potentialReward - restakedValidation,
						delegatee:  cfg.delegateeReward - restakedDelegatee,
					},
					abortReward: wantReward{
						validation: cfg.accruedValidationRewards,
						delegatee:  cfg.delegateeReward + cfg.accruedDelegateeRewards,
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)
			cfg := tt.config(env)

			tx := newAddAutoRenewedValidatorTx(t, env, cfg.weight, cfg.delegationRewardShares, cfg.autoCompoundRewardShares)
			addAutoRenewedValidator(t, env, tx, cfg)

			stakerTx := tx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
			staker, err := env.state.GetCurrentValidator(stakerTx.SubnetID(), stakerTx.NodeID())
			require.NoError(t, err)

			rewardTx := newRewardAutoRenewedValidatorTx(t, tx.ID(), env.state.GetTimestamp())

			onCommitState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)
			onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

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
				tx,
				rewardTx,
				onCommitState,
				onAbortState,
				tt.want(cfg, staker.PotentialReward),
			)
		})
	}
}

// TestRewardDelegatorToAutoRenewedValidator tests the full delegator reward
// flow for a delegator to an auto-renewed validator: delegator gets their
// share, delegatee share is deferred to StakingInfo.DelegateeReward.
func TestRewardDelegatorToAutoRenewedValidator(t *testing.T) {
	var (
		env    = newEnvironment(t, upgradetest.Latest)
		wallet = newWallet(t, env, walletConfig{})

		delegationShares = uint32(reward.PercentDenominator / 4) // 25% to delegatee
		vdrWeight        = env.config.MinValidatorStake
		delRewardAmt     = uint64(1_000_000)
	)

	// Create the auto-renewed validator.
	tx := newAddAutoRenewedValidatorTx(t, env, vdrWeight, delegationShares, reward.PercentDenominator)
	addAutoRenewedValidator(t, env, tx, autoRenewedValidatorConfig{
		delegationRewardShares:   delegationShares,
		autoCompoundRewardShares: reward.PercentDenominator,
		restake:                  true,
	})

	nodeID := tx.Unsigned.(*txs.AddAutoRenewedValidatorTx).NodeID()
	vdr, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)

	// Create a delegator running for the validator's full period.
	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	delStartTime := genesistest.DefaultValidatorStartTimeUnix + 1
	delEndTime := uint64(vdr.EndTime.Unix())

	tx, err = wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  delStartTime,
			End:    delEndTime,
			Wght:   env.config.MinDelegatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
	)
	require.NoError(t, err)

	delegatorTx := tx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		tx.ID(),
		delegatorTx,
		time.Unix(int64(delStartTime), 0),
		delRewardAmt,
	)
	require.NoError(t, err)

	require.NoError(t, diff.PutCurrentDelegator(delStaker))
	diff.AddTx(tx, status.Committed)
	diff.SetTimestamp(time.Unix(int64(delEndTime), 0))

	require.NoError(t, diff.Apply(env.state))

	// Step 3: Reward the delegator via RewardValidatorTx.
	rewardDelegatorTx, err := newRewardValidatorTx(t, tx.ID())
	require.NoError(t, err)

	commitState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	abortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	require.NoError(t, ProposalTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		rewardDelegatorTx,
		commitState,
		abortState,
	))

	// Verify delegator reward UTXO on commit: delegator gets 75% of delRewardAmt.
	wantDelegateeReward, wantDelegatorReward := reward.Split(delRewardAmt, delegationShares)
	delRewardOutputIndex := len(delegatorTx.Outputs()) + len(delegatorTx.Stake())
	assertUTXO(
		t,
		commitState,
		tx.ID(),
		delRewardOutputIndex,
		wantDelegatorReward,
		delegatorTx.RewardsOwner().(*secp256k1fx.OutputOwners),
	)

	// Verify delegatee reward is NOT distributed yet (deferred post-Cortina).
	assertNoExtraUTXOs(t, commitState, tx.ID(), delRewardOutputIndex+1)

	// Verify delegatee reward in StakingInfo.
	stakingInfo, err := commitState.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	require.Equal(t, wantDelegateeReward, stakingInfo.DelegateeReward)

	stakingInfo, err = abortState.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	require.Zero(t, stakingInfo.DelegateeReward)

	// Commit the delegator diff.
	require.NoError(t, commitState.Apply(env.state))

	// Verify reward UTXOs are correctly tracked via GetRewardUTXOs.
	rewardUTXOs, err := env.state.GetRewardUTXOs(tx.ID())
	require.NoError(t, err)
	require.Len(t, rewardUTXOs, 1)
	require.Equal(t, wantDelegatorReward, rewardUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amount())
}
