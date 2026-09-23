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
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// assertUTXO asserts that the UTXO at (txID, outputIndex) is an AVAX
// secp256k1fx.TransferOutput with the given amount and owner.
func assertUTXO(
	t testing.TB,
	diff *state.Diff,
	txID ids.ID,
	outputIndex int,
	wantAmount uint64,
	wantOwner *secp256k1fx.OutputOwners,
) {
	t.Helper()

	utxoID := avax.UTXOID{TxID: txID, OutputIndex: uint32(outputIndex)}
	utxo, err := diff.GetUTXO(utxoID.InputID())
	require.NoError(t, err)

	out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
	require.True(t, ok)
	require.Equal(t, snowtest.AVAXAssetID, utxo.Asset.AssetID())
	require.Equal(t, wantAmount, out.Amt)
	require.True(t, wantOwner.Equals(&out.OutputOwners))
}

// assertNoUTXO asserts that there is no UTXO at (txID, outputIndex).
func assertNoUTXO(t testing.TB, diff *state.Diff, txID ids.ID, outputIndex int) {
	t.Helper()

	utxoID := avax.UTXOID{TxID: txID, OutputIndex: uint32(outputIndex)}
	_, err := diff.GetUTXO(utxoID.InputID())
	require.ErrorIs(t, err, database.ErrNotFound)
}

// assertStakeReturned asserts that each of the validator's stake outputs
// is returned as a UTXO with the original amount.
func assertStakeReturned(t testing.TB, diff *state.Diff, addTxID ids.ID, tx *platform.AddAutoRenewedValidatorTx) {
	t.Helper()

	for i, stakeOut := range tx.StakeOuts {
		utxoID := avax.UTXOID{
			TxID:        addTxID,
			OutputIndex: uint32(len(tx.Outputs()) + i),
		}
		utxo, err := diff.GetUTXO(utxoID.InputID())
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
) *platform.Tx {
	t.Helper()

	// Only spend the first funded key so that txs built on top of this one
	// can use the other keys without conflicting on UTXOs.
	wallet := newWallet(t, env, walletConfig{
		keys: genesistest.DefaultFundedKeys[:1],
	})
	tx, err := wallet.IssueAddAutoRenewedValidatorTx(
		ids.GenerateTestNodeID(),
		weight,
		newProofOfPossession(t),
		newOwner(),
		newOwner(),
		newOwner(),
		delegationRewardShares,
		autoCompoundRewardShares,
		env.config.MinStakeDuration,
	)
	require.NoError(t, err)
	return tx
}

// addAutoRenewedValidator executes tx as a current validator via StandardTx
// and attaches its staking info. The result is returned as a diff on top of
// env.state so that env.state is left untouched.
func addAutoRenewedValidator(t testing.TB, env *environment, tx *platform.Tx, cfg autoRenewedValidatorConfig) *state.Diff {
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
		stakingInfo.NextPeriod = uint64(env.config.MinStakeDuration / time.Second)
	}

	nodeID := (tx.Unsigned.(*platform.AddAutoRenewedValidatorTx)).NodeID()
	require.NoError(t, diff.SetStakingInfo(constants.PrimaryNetworkID, nodeID, stakingInfo))
	return diff
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
func assertRewards(t testing.TB, diff *state.Diff, stakerTx *platform.Tx, rewardTxID ids.ID, rewards wantReward) {
	t.Helper()

	// Output index layout of a RewardAutoRenewedValidatorTx's reward UTXOs. The
	// tx itself has no outputs, so its reward UTXOs occupy the first indices.
	const (
		validationRewardOutputIndex = 0
		delegateeRewardOutputIndex  = 1
	)

	uStakerTx := stakerTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)
	assertUTXO(t, diff, rewardTxID, validationRewardOutputIndex, rewards.validation, uStakerTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners))
	assertUTXO(t, diff, rewardTxID, delegateeRewardOutputIndex, rewards.delegatee, uStakerTx.DelegatorRewardsOwner.(*secp256k1fx.OutputOwners))

	assertNoUTXO(t, diff, rewardTxID, delegateeRewardOutputIndex+1)
}

// assertValidatorRemoved asserts that diff no longer has the
// validator from tx, that its stake was returned, and that it produced the
// expected reward UTXOs.
func assertValidatorRemoved(t testing.TB, diff *state.Diff, stakerTx *platform.Tx, rewardTxID ids.ID, rewards wantReward) {
	t.Helper()

	uStakerTx := stakerTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)

	_, err := diff.GetCurrentValidator(uStakerTx.SubnetID(), uStakerTx.NodeID())
	require.ErrorIs(t, err, database.ErrNotFound)

	assertStakeReturned(t, diff, stakerTx.ID(), uStakerTx)
	assertRewards(t, diff, stakerTx, rewardTxID, rewards)

	// No UTXO past the returned stake.
	assertNoUTXO(t, diff, stakerTx.ID(), len(uStakerTx.Outputs())+len(uStakerTx.StakeOuts))
}

// assertRewardAutoRenewedValidator asserts the commit and abort states produced
// by rewarding the validator staked in stakerTx. diff is the state both
// onCommitState and onAbortState were built on.
func assertRewardAutoRenewedValidator(
	t testing.TB,
	diff *state.Diff,
	stakerTx *platform.Tx,
	rewardTx *platform.Tx,
	onCommitState *state.Diff,
	onAbortState *state.Diff,
	want wantRewardAutoRenewedValidator,
) {
	t.Helper()

	uStakerTx := stakerTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)
	currentSupply := must[uint64](t)(diff.GetCurrentSupply(constants.PrimaryNetworkID))

	stakedValidator, err := diff.GetCurrentValidator(uStakerTx.SubnetID(), uStakerTx.NodeID())
	require.NoError(t, err)

	// On abort the validator is always removed and its potential reward is
	// burned from the supply.
	assertValidatorRemoved(t, onAbortState, stakerTx, rewardTx.ID(), want.abortReward)

	abortSupply, err := onAbortState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)
	require.Equal(t, currentSupply-stakedValidator.PotentialReward, abortSupply)

	commitSupply, err := onCommitState.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	// On commit the validator is either restaked or gracefully removed.
	if want.commitRestaked {
		validator, err := onCommitState.GetCurrentValidator(uStakerTx.SubnetID(), uStakerTx.NodeID())
		require.NoError(t, err)
		stakingInfo, err := onCommitState.GetStakingInfo(uStakerTx.SubnetID(), uStakerTx.NodeID())
		require.NoError(t, err)

		require.Equal(t, want.commitWeight, validator.Weight)
		require.Equal(t, want.commitAccruedValidationRewards, stakingInfo.AccruedValidationRewards)
		require.Equal(t, want.commitAccruedDelegateeRewards, stakingInfo.AccruedDelegateeRewards)

		assertRewards(t, onCommitState, stakerTx, rewardTx.ID(), want.commitReward)
		// The stake is restaked rather than returned: no UTXO at the first stake index.
		assertNoUTXO(t, onCommitState, stakerTx.ID(), len(uStakerTx.Outputs()))

		require.Equal(t, currentSupply+validator.PotentialReward, commitSupply)
	} else {
		assertValidatorRemoved(t, onCommitState, stakerTx, rewardTx.ID(), want.commitReward)
		require.Equal(t, currentSupply, commitSupply)
	}
}

// nextStakerToRemove returns the current staker with the earliest end time.
func nextStakerToRemove(t testing.TB, diff *state.Diff) *state.Staker {
	t.Helper()

	it, err := diff.GetCurrentStakerIterator()
	require.NoError(t, err)
	defer it.Release()

	require.True(t, it.Next())
	return it.Value()
}

func TestRewardValidatorTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)
	stakerToRemove := nextStakerToRemove(t, diff)

	tests := []struct {
		name        string
		txID        ids.ID
		updateState func(*state.Diff)
		want        error
	}{
		{
			name: "chain_time_before_staker_end_time",
			txID: stakerToRemove.TxID,
			want: errRemoveStakerTooEarly,
		},
		{
			name: "wrong_staker",
			txID: ids.GenerateTestID(),
			updateState: func(diff *state.Diff) {
				diff.SetTimestamp(stakerToRemove.EndTime)
			},
			want: errRemoveWrongStaker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, err)

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			_, _, err = executeProposalTx(t, env, diff, newRewardValidatorTx(t, tt.txID))
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestRewardValidatorTx(t *testing.T) {
	tests := []struct {
		name       string
		commit     bool
		wantReward uint64
	}{
		{
			name:       "commit",
			commit:     true,
			wantReward: 38944,
		},
		{
			name:       "abort",
			wantReward: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, upgradetest.ApricotPhase5)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			stakerToRemove := nextStakerToRemove(t, diff)
			stakerToRemoveTx, _, err := diff.GetTx(stakerToRemove.TxID)
			require.NoError(err)

			// Advance chain timestamp to time that the validator leaves
			diff.SetTimestamp(stakerToRemove.EndTime)

			onCommitState, onAbortState, err := executeProposalTx(t, env, diff, newRewardValidatorTx(t, stakerToRemove.TxID))
			require.NoError(err)

			gotState := onAbortState
			if tt.commit {
				gotState = onCommitState
			}

			// assert the validator was removed
			require.NotEqual(stakerToRemove.TxID, nextStakerToRemove(t, gotState).TxID)

			// assert the stake, and the reward on commit, were returned
			stakeOuts := stakerToRemoveTx.Unsigned.(*platform.AddValidatorTx).StakeOuts
			stakeOwners := stakeOuts[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()
			oldBalance, err := avax.GetBalance(env.state, stakeOwners)
			require.NoError(err)

			require.NoError(gotState.Apply(env.state))
			dummyHeight := uint64(1)
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			gotBalance, err := avax.GetBalance(env.state, stakeOwners)
			require.NoError(err)
			require.Equal(oldBalance+stakerToRemove.Weight+tt.wantReward, gotBalance)
		})
	}
}

// delPotentialReward is the potential reward of the delegator added by
// addCurrentValidatorAndDelegator.
const delPotentialReward uint64 = 1_000_000

// addCurrentValidatorAndDelegator commits to env.state a primary network
// validator with a 25% delegation fee and a delegator to it, staking over the
// same period, and advances the chain time to their end time. It returns the
// staker txs and their rewards owners.
func addCurrentValidatorAndDelegator(
	t testing.TB,
	env *environment,
	vdrPotentialReward uint64,
) (vdrTx *platform.Tx, delTx *platform.Tx, vdrRewardsOwner *secp256k1fx.OutputOwners, delRewardsOwner *secp256k1fx.OutputOwners) {
	t.Helper()
	require := require.New(t)

	vdrRewardsOwner = newOwner()
	delRewardsOwner = newOwner()
	var (
		wallet    = newWallet(t, env, walletConfig{})
		validator = platform.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
			End:    uint64(genesistest.DefaultValidatorStartTime.Add(2 * env.config.MinStakeDuration).Unix()),
			Wght:   env.config.MinValidatorStake,
		}
		delegator = validator
	)
	delegator.Wght = env.config.MinDelegatorStake

	vdrTx, err := wallet.IssueAddValidatorTx(&validator, vdrRewardsOwner, reward.PercentDenominator/4)
	require.NoError(err)

	delTx, err = wallet.IssueAddDelegatorTx(&delegator, delRewardsOwner)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*platform.AddValidatorTx)
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		addValTx.EndTime(),
		addValTx.Weight(),
		vdrPotentialReward,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*platform.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		addDelTx.StartTime(),
		addDelTx.EndTime(),
		addDelTx.Weight(),
		delPotentialReward,
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	require.NoError(diff.PutCurrentValidator(vdrStaker))
	diff.AddTx(vdrTx, status.Committed)
	require.NoError(diff.PutCurrentDelegator(delStaker))
	diff.AddTx(delTx, status.Committed)
	diff.SetTimestamp(vdrStaker.EndTime)
	require.NoError(diff.Apply(env.state))
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	return vdrTx, delTx, vdrRewardsOwner, delRewardsOwner
}

func TestRewardDelegatorTxExecuteOnCommitPreDelegateeDeferral(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	dummyHeight := uint64(1)

	vdrTx, delTx, vdrRewardsOwner, delRewardsOwner := addCurrentValidatorAndDelegator(t, env, 0)
	vdrNodeID := vdrTx.Unsigned.(*platform.AddValidatorTx).NodeID()

	// test validator stake
	stake := env.config.Validators.GetWeight(constants.PrimaryNetworkID, vdrNodeID)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx := newRewardValidatorTx(t, delTx.ID())

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	onCommitState, _, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrRewardsOwner.AddressesSet())
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delRewardsOwner.AddressesSet())
	require.NoError(err)

	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrRewardsOwner.AddressesSet())
	require.NoError(err)
	vdrReward, err := safemath.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.NotZero(vdrReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delRewardsOwner.AddressesSet())
	require.NoError(err)
	delReward, err := safemath.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(vdrReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(delPotentialReward, delReward+vdrReward, "expected total reward to be %d but is %d", delPotentialReward, delReward+vdrReward)

	stake = env.config.Validators.GetWeight(constants.PrimaryNetworkID, vdrNodeID)
	require.Equal(env.config.MinValidatorStake, stake)
}

func TestRewardDelegatorTxExecuteOnCommitPostDelegateeDeferral(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Cortina)
	dummyHeight := uint64(1)

	const vdrRewardAmt = uint64(2000000)
	vdrTx, delTx, vdrRewardsOwner, delRewardsOwner := addCurrentValidatorAndDelegator(t, env, vdrRewardAmt)
	vdrNodeID := vdrTx.Unsigned.(*platform.AddValidatorTx).NodeID()

	oldVdrBalance, err := avax.GetBalance(env.state, vdrRewardsOwner.AddressesSet())
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delRewardsOwner.AddressesSet())
	require.NoError(err)

	// test validator stake
	stake := env.config.Validators.GetWeight(constants.PrimaryNetworkID, vdrNodeID)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx := newRewardValidatorTx(t, delTx.ID())

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	onCommitState, _, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)

	// The delegator should be rewarded if the ProposalTx is committed. Since the
	// delegatee's share is 25%, we expect the delegator to receive 75% of the reward.
	// Since this is post [CortinaTime], the delegatee should not be rewarded until a
	// RewardValidatorTx is issued for the delegatee.
	uDelTx := delTx.Unsigned.(*platform.AddDelegatorTx)
	delRewardOutputIndex := len(uDelTx.Outs) + len(uDelTx.StakeOuts)
	assertUTXO(t, onCommitState, delTx.ID(), delRewardOutputIndex, delPotentialReward*3/4, delRewardsOwner)
	// Pre-Cortina, the delegatee's reward would have been the next output.
	assertNoUTXO(t, onCommitState, delTx.ID(), delRewardOutputIndex+1)

	// Commit Delegator Diff
	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	tx = newRewardValidatorTx(t, vdrTx.ID())
	diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	onCommitState, onAbortState, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)

	uVdrTx := vdrTx.Unsigned.(*platform.AddValidatorTx)
	vdrRewardOutputIndex := len(uVdrTx.Outs) + len(uVdrTx.StakeOuts)
	// On commit, the validator is paid its reward and its accrued delegatee
	// rewards.
	assertUTXO(t, onCommitState, vdrTx.ID(), vdrRewardOutputIndex, vdrRewardAmt, vdrRewardsOwner)
	assertUTXO(t, onCommitState, vdrTx.ID(), vdrRewardOutputIndex+1, delPotentialReward/4, vdrRewardsOwner)
	// On abort, the validator is still paid its accrued delegatee rewards.
	assertUTXO(t, onAbortState, vdrTx.ID(), vdrRewardOutputIndex, delPotentialReward/4, vdrRewardsOwner)
	// The delegatee's reward is still not paid out on the delegator tx.
	assertNoUTXO(t, onCommitState, delTx.ID(), delRewardOutputIndex+1)

	// Commit Validator Diff
	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrRewardsOwner.AddressesSet())
	require.NoError(err)
	vdrReward, err := safemath.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	delegateeReward, err := safemath.Sub(vdrReward, vdrRewardAmt)
	require.NoError(err)
	require.NotZero(delegateeReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delRewardsOwner.AddressesSet())
	require.NoError(err)
	delReward, err := safemath.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(delegateeReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(delPotentialReward, delReward+delegateeReward, "expected total reward to be %d but is %d", delPotentialReward, delReward+vdrReward)
}

func TestRewardDelegatorTxAndValidatorTxExecuteOnCommitPostDelegateeDeferral(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Cortina)
	dummyHeight := uint64(1)

	const vdrRewardAmt = uint64(2000000)
	vdrTx, delTx, vdrRewardsOwner, delRewardsOwner := addCurrentValidatorAndDelegator(t, env, vdrRewardAmt)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrRewardsOwner.AddressesSet())
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delRewardsOwner.AddressesSet())
	require.NoError(err)

	tx := newRewardValidatorTx(t, delTx.ID())

	// Create Delegator Diffs
	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	delOnCommitState, _, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)

	// Create Validator Diffs
	require.NoError(delOnCommitState.Apply(env.state))

	tx = newRewardValidatorTx(t, vdrTx.ID())
	diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	vdrOnCommitState, vdrOnAbortState, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)

	// aborted validator tx should still distribute accrued delegator rewards
	uVdrTx := vdrTx.Unsigned.(*platform.AddValidatorTx)
	vdrRewardOutputIndex := len(uVdrTx.Outs) + len(uVdrTx.StakeOuts)
	assertUTXO(t, vdrOnAbortState, vdrTx.ID(), vdrRewardOutputIndex, delPotentialReward/4, vdrRewardsOwner)

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Commit Validator Diff
	require.NoError(vdrOnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrRewardsOwner.AddressesSet())
	require.NoError(err)
	vdrReward, err := safemath.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	delegateeReward, err := safemath.Sub(vdrReward, vdrRewardAmt)
	require.NoError(err)
	require.NotZero(delegateeReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delRewardsOwner.AddressesSet())
	require.NoError(err)
	delReward, err := safemath.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(delegateeReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(delPotentialReward, delReward+delegateeReward, "expected total reward to be %d but is %d", delPotentialReward, delReward+vdrReward)
}

func TestRewardDelegatorTxExecuteOnAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	dummyHeight := uint64(1)

	initialSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	_, delTx, vdrRewardsOwner, delRewardsOwner := addCurrentValidatorAndDelegator(t, env, 0)

	tx := newRewardValidatorTx(t, delTx.ID())

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	_, onAbortState, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrRewardsOwner.AddressesSet())
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delRewardsOwner.AddressesSet())
	require.NoError(err)

	require.NoError(onAbortState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// If tx is aborted, delegator and delegatee shouldn't get reward
	newVdrBalance, err := avax.GetBalance(env.state, vdrRewardsOwner.AddressesSet())
	require.NoError(err)
	vdrReward, err := safemath.Sub(newVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.Zero(vdrReward, "expected delegatee balance not to increase")

	newDelBalance, err := avax.GetBalance(env.state, delRewardsOwner.AddressesSet())
	require.NoError(err)
	delReward, err := safemath.Sub(newDelBalance, oldDelBalance)
	require.NoError(err)
	require.Zero(delReward, "expected delegator balance not to increase")

	newSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(initialSupply-delPotentialReward, newSupply, "should have removed un-rewarded tokens from the potential supply")
}

// TestRewardValidatorStakerTypeError verifies that RewardValidatorTx rejects stakers
// it does not reward: an auto-renewed validator (which must be rewarded through
// RewardAutoRenewedValidatorTx) and a permissioned subnet validator (which is
// never rewarded and should already have been removed by the advancement of
// time). Both reach the dispatch default and must surface errUnexpectedStakerTxType.
func TestRewardValidatorStakerTypeError(t *testing.T) {
	tests := []struct {
		name string
		// tx builds the staker tx to execute. The loop runs it via StandardTx so
		// the validator becomes current, then rewards it with a RewardValidatorTx.
		// Staking info is left at its zero value: both stakers hit the dispatch
		// default and are rejected with errUnexpectedStakerTxType before any
		// reward state is read.
		tx func(t *testing.T, env *environment) *platform.Tx
	}{
		{
			name: "auto_renewed_validator",
			tx: func(t *testing.T, env *environment) *platform.Tx {
				return newAddAutoRenewedValidatorTx(
					t,
					env,
					env.config.MinValidatorStake,
					reward.PercentDenominator,
					reward.PercentDenominator,
				)
			},
		},
		{
			name: "permissioned_subnet_validator",
			tx: func(t *testing.T, env *environment) *platform.Tx {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})

				startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)
				endTime := startTime.Add(env.config.MinStakeDuration)
				tx, err := wallet.IssueAddSubnetValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: genesistest.DefaultNodeIDs[0],
							Start:  uint64(startTime.Unix()),
							End:    uint64(endTime.Unix()),
							Wght:   genesistest.DefaultValidatorWeight,
						},
						Subnet: subnetID,
					},
				)
				require.NoError(t, err)

				return tx
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)

			stakerTx := test.tx(t, env)
			uStakerTx := stakerTx.Unsigned.(platform.Staker)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			_, _, _, err = StandardTx(
				&env.backend,
				state.PickFeeCalculator(env.config, diff),
				stakerTx,
				diff,
			)
			require.NoError(t, err)
			diff.AddTx(stakerTx, status.Committed)

			staker, err := diff.GetCurrentValidator(uStakerTx.SubnetID(), uStakerTx.NodeID())
			require.NoError(t, err)

			diff.SetTimestamp(staker.EndTime)

			rewardTx := newRewardValidatorTx(t, staker.TxID)

			_, _, err = executeProposalTx(t, env, diff, rewardTx)
			require.ErrorIs(t, err, errUnexpectedStakerTxType)
		})
	}
}

func TestRewardAutoRenewedValidatorTxErrors(t *testing.T) {
	tests := []struct {
		name string
		tx   func(t testing.TB, txID ids.ID, endTime time.Time) *platform.Tx
		want error
	}{
		{
			name: "wrong_staker",
			tx: func(t testing.TB, _ ids.ID, endTime time.Time) *platform.Tx {
				return newRewardAutoRenewedValidatorTx(t, ids.GenerateTestID(), endTime)
			},
			want: errRemoveWrongStaker,
		},
		{
			name: "invalid_timestamp",
			tx: func(t testing.TB, txID ids.ID, endTime time.Time) *platform.Tx {
				return newRewardAutoRenewedValidatorTx(t, txID, endTime.Add(-time.Second))
			},
			want: errInvalidTimestamp,
		},
		{
			name: "invalid_validator_tx",
			tx:   newRewardAutoRenewedValidatorTx,
			want: errShouldBeAutoRenewedStaker,
		},
		{
			name: "wrong_number_of_credentials",
			tx: func(t testing.TB, txID ids.ID, endTime time.Time) *platform.Tx {
				rewardTx := newRewardAutoRenewedValidatorTx(t, txID, endTime)
				rewardTx.Creds = append(rewardTx.Creds, &secp256k1fx.Credential{})
				return rewardTx
			},
			want: errWrongNumberOfCredentials,
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
				&platform.SubnetValidator{
					Validator: platform.Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(endTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				newProofOfPossession(t),
				env.ctx.AVAXAssetID,
				newOwner(),
				newOwner(),
				reward.PercentDenominator,
			)
			require.NoError(t, err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, diff)
			require.NoError(t, err)

			diff.AddTx(tx, status.Committed)
			diff.SetTimestamp(endTime)

			_, _, err = executeProposalTx(t, env, diff, tt.tx(t, tx.ID(), endTime))
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestRewardAutoRenewedValidatorTx(t *testing.T) {
	const restakingCapacity uint64 = 2_000_000

	env := newEnvironment(t, upgradetest.Latest)

	validatorConfig := autoRenewedValidatorConfig{
		weight:                   env.config.MinValidatorStake,
		delegateeReward:          5_000_000,
		accruedValidationRewards: 1_000_000,
		accruedDelegateeRewards:  500_000,
		delegationRewardShares:   reward.PercentDenominator / 10,
		autoCompoundRewardShares: 4 * reward.PercentDenominator / 10,
		restake:                  true,
	}

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
				cfg := validatorConfig
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
				return validatorConfig // restake: true
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
				cfg := validatorConfig
				cfg.weight = env.config.MaxValidatorStake - restakingCapacity
				return cfg
			},
			want: func(cfg autoRenewedValidatorConfig, potentialReward uint64) wantRewardAutoRenewedValidator {
				uncappedValidation := cfg.restakedReward(potentialReward)
				uncappedDelegatee := cfg.restakedReward(cfg.delegateeReward)
				uncapped := uncappedValidation + uncappedDelegatee

				// Production floors the proportional validation share and gives the
				// remainder of the capacity to the delegatee share (V' + D' == C).
				restakedValidation := uncappedValidation * restakingCapacity / uncapped
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
			cfg := tt.config(env)

			stakerTx := newAddAutoRenewedValidatorTx(t, env, cfg.weight, cfg.delegationRewardShares, cfg.autoCompoundRewardShares)
			diff := addAutoRenewedValidator(t, env, stakerTx, cfg)

			uStakerTx := stakerTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)
			staker, err := diff.GetCurrentValidator(uStakerTx.SubnetID(), uStakerTx.NodeID())
			require.NoError(t, err)
			diff.SetTimestamp(staker.EndTime)

			rewardTx := newRewardAutoRenewedValidatorTx(t, stakerTx.ID(), diff.GetTimestamp())

			onCommitState, onAbortState, err := executeProposalTx(t, env, diff, rewardTx)
			require.NoError(t, err)

			assertRewardAutoRenewedValidator(
				t,
				diff,
				stakerTx,
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
		env = newEnvironment(t, upgradetest.Latest)

		delegationShares = uint32(reward.PercentDenominator / 4) // 25% to delegatee
		vdrWeight        = env.config.MinValidatorStake
	)

	// Create the auto-renewed validator.
	stakerTx := newAddAutoRenewedValidatorTx(t, env, vdrWeight, delegationShares, reward.PercentDenominator)
	diff := addAutoRenewedValidator(t, env, stakerTx, autoRenewedValidatorConfig{
		delegationRewardShares:   delegationShares,
		autoCompoundRewardShares: reward.PercentDenominator,
		restake:                  true,
	})

	nodeID := stakerTx.Unsigned.(*platform.AddAutoRenewedValidatorTx).NodeID()
	vdr, err := diff.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)

	// Create a delegator running for the validator's full period. The wallet
	// reads UTXOs from env.state, which does not include the validator tx
	// staged in diff, so use a key the validator did not spend from.
	wallet := newWallet(t, env, walletConfig{
		keys: genesistest.DefaultFundedKeys[1:2],
	})
	delegatorTx, err := wallet.IssueAddPermissionlessDelegatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: nodeID,
				Start:  uint64(env.state.GetTimestamp().Add(time.Second).Unix()),
				End:    uint64(vdr.EndTime.Unix()),
				Wght:   env.config.MinDelegatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		snowtest.AVAXAssetID,
		newOwner(),
	)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		delegatorTx,
		diff,
	)
	require.NoError(t, err)
	diff.AddTx(delegatorTx, status.Committed)
	diff.SetTimestamp(vdr.EndTime)

	// Reward the delegator via RewardValidatorTx.
	rewardDelegatorTx := newRewardValidatorTx(t, delegatorTx.ID())

	commitState, abortState, err := executeProposalTx(t, env, diff, rewardDelegatorTx)
	require.NoError(t, err)

	// Verify delegator reward UTXO on commit: delegator gets 75% of its reward.
	uDelegatorTx := delegatorTx.Unsigned.(*platform.AddPermissionlessDelegatorTx)
	wantOwner := uDelegatorTx.RewardsOwner().(*secp256k1fx.OutputOwners)

	delegatorIt, err := diff.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	require.True(t, delegatorIt.Next())
	potentialReward := delegatorIt.Value().PotentialReward

	delegatorIt.Release()

	wantDelegateeReward, wantDelegatorReward := reward.Split(potentialReward, delegationShares)
	delRewardOutputIndex := len(uDelegatorTx.Outputs()) + len(uDelegatorTx.Stake())
	assertUTXO(
		t,
		commitState,
		delegatorTx.ID(),
		delRewardOutputIndex,
		wantDelegatorReward,
		wantOwner,
	)

	// Verify delegatee reward is NOT distributed yet (deferred post-Cortina).
	assertNoUTXO(t, commitState, delegatorTx.ID(), delRewardOutputIndex+1)

	// Verify delegatee reward in StakingInfo.
	stakingInfo, err := commitState.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	require.Equal(t, wantDelegateeReward, stakingInfo.DelegateeReward)

	stakingInfo, err = abortState.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	require.Zero(t, stakingInfo.DelegateeReward)
}
