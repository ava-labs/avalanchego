// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/intmath"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// Maximum future start time for staking/delegating
	MaxFutureStartTime = 24 * 7 * 2 * time.Hour

	// SyncBound is the synchrony bound used for safe decision making
	SyncBound = 10 * time.Second

	MaxValidatorWeightFactor = 5
)

var (
	_ txs.Visitor = (*proposalTxExecutor)(nil)

	ErrRemoveStakerTooEarly          = errors.New("attempting to remove staker before their end time")
	ErrRemoveWrongStaker             = errors.New("attempting to remove wrong staker")
	ErrInvalidState                  = errors.New("generated output isn't valid state")
	ErrShouldBePermissionlessStaker  = errors.New("expected permissionless staker")
	ErrWrongTxType                   = errors.New("wrong transaction type")
	ErrInvalidID                     = errors.New("invalid ID")
	ErrProposedAddStakerTxAfterBanff = errors.New("staker transaction proposed after Banff")
	ErrAdvanceTimeTxIssuedAfterBanff = errors.New("AdvanceTimeTx issued after Banff")
	errShouldBeAutoRenewedStaker     = errors.New("expected auto renewed staker")
	errUnexpectedTxType              = errors.New("unexpected transaction type")
	errInvalidTimestamp              = errors.New("invalid timestamp")
	errDivideByZero                  = errors.New("divide by zero")
)

// ProposalTx executes the proposal transaction [tx].
//
// [onCommitState] will be modified to reflect the changes made to the state if
// the proposal is committed.
//
// [onAbortState] will be modified to reflect the changes made to the state if
// the proposal is aborted.
//
// Invariant: It is assumed that [onCommitState] and [onAbortState] represent
// the same state when passed into this function.
func ProposalTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	tx *txs.Tx,
	onCommitState *state.Diff,
	onAbortState *state.Diff,
) error {
	proposalExecutor := proposalTxExecutor{
		backend:       backend,
		feeCalculator: feeCalculator,
		tx:            tx,
		onCommitState: onCommitState,
		onAbortState:  onAbortState,
	}
	if err := tx.Unsigned.Visit(&proposalExecutor); err != nil {
		txID := tx.ID()
		return fmt.Errorf("proposal tx %s failed execution: %w", txID, err)
	}
	return nil
}

type proposalTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	backend       *Backend
	feeCalculator fee.Calculator
	tx            *txs.Tx
	// [onCommitState] is the state used for validation.
	// [onCommitState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is committed.
	onCommitState *state.Diff
	// [onAbortState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is aborted.
	onAbortState *state.Diff
}

func (*proposalTxExecutor) CreateChainTx(*txs.CreateChainTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) ImportTx(*txs.ImportTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) ExportTx(*txs.ExportTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) BaseTx(*txs.BaseTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) ConvertSubnetToL1Tx(*txs.ConvertSubnetToL1Tx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) RegisterL1ValidatorTx(*txs.RegisterL1ValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) SetL1ValidatorWeightTx(*txs.SetL1ValidatorWeightTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) IncreaseL1ValidatorBalanceTx(*txs.IncreaseL1ValidatorBalanceTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) DisableL1ValidatorTx(*txs.DisableL1ValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) AddAutoRenewedValidatorTx(*txs.AddAutoRenewedValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) SetAutoRenewedValidatorConfigTx(*txs.SetAutoRenewedValidatorConfigTx) error {
	return ErrWrongTxType
}

func (e *proposalTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	// AddValidatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddValidatorTxs must be issued into
	// StandardBlocks.
	currentTimestamp := e.onCommitState.GetTimestamp()
	if e.backend.Config.UpgradeConfig.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.backend.Config.UpgradeConfig.BanffTime,
		)
	}

	onAbortOuts, err := verifyAddValidatorTx(
		e.backend,
		e.feeCalculator,
		e.onCommitState,
		e.tx,
		tx,
	)
	if err != nil {
		return err
	}

	txID := e.tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	avax.Consume(e.onCommitState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	if err := e.onCommitState.PutPendingValidator(newStaker); err != nil {
		return err
	}

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.onAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onAbortState, txID, onAbortOuts)
	return nil
}

func (e *proposalTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	// AddSubnetValidatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddSubnetValidatorTxs must be
	// issued into StandardBlocks.
	currentTimestamp := e.onCommitState.GetTimestamp()
	if e.backend.Config.UpgradeConfig.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.backend.Config.UpgradeConfig.BanffTime,
		)
	}

	if err := verifyAddSubnetValidatorTx(
		e.backend,
		e.feeCalculator,
		e.onCommitState,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	avax.Consume(e.onCommitState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	if err := e.onCommitState.PutPendingValidator(newStaker); err != nil {
		return err
	}

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.onAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onAbortState, txID, tx.Outs)
	return nil
}

func (e *proposalTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	// AddDelegatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddDelegatorTxs must be issued into
	// StandardBlocks.
	currentTimestamp := e.onCommitState.GetTimestamp()
	if e.backend.Config.UpgradeConfig.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.backend.Config.UpgradeConfig.BanffTime,
		)
	}

	onAbortOuts, err := verifyAddDelegatorTx(
		e.backend,
		e.feeCalculator,
		e.onCommitState,
		e.tx,
		tx,
	)
	if err != nil {
		return err
	}

	txID := e.tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	avax.Consume(e.onCommitState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	e.onCommitState.PutPendingDelegator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.onAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onAbortState, txID, onAbortOuts)
	return nil
}

func (e *proposalTxExecutor) AdvanceTimeTx(tx *txs.AdvanceTimeTx) error {
	switch {
	case tx == nil:
		return txs.ErrNilTx
	case len(e.tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	// Validate [newChainTime]
	newChainTime := tx.Timestamp()
	if e.backend.Config.UpgradeConfig.IsBanffActivated(newChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s) >= Banff fork time (%s)",
			ErrAdvanceTimeTxIssuedAfterBanff,
			newChainTime,
			e.backend.Config.UpgradeConfig.BanffTime,
		)
	}

	now := e.backend.Clk.Time()
	if err := VerifyNewChainTime(
		e.backend.Config.ValidatorFeeConfig,
		newChainTime,
		now,
		e.onCommitState,
	); err != nil {
		return err
	}

	// Note that state doesn't change if this proposal is aborted
	_, err := AdvanceTimeTo(e.backend, e.onCommitState, newChainTime)
	return err
}

func (e *proposalTxExecutor) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	switch {
	case tx == nil:
		return txs.ErrNilTx
	case tx.TxID == ids.Empty:
		return ErrInvalidID
	case len(e.tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	stakerTx, stakerToReward, err := getNextStakerToReward(e.onCommitState, tx)
	if err != nil {
		return err
	}

	// Invariant: A [txs.DelegatorTx] does not also implement the
	//            [txs.ValidatorTx] interface.
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case txs.ValidatorTx:
		switch uStakerTx.(type) {
		case *txs.AddValidatorTx, *txs.AddPermissionlessValidatorTx:
		default:
			return fmt.Errorf("%w: %T", errUnexpectedTxType, uStakerTx)
		}

		if err := e.rewardValidatorTx(uStakerTx, stakerToReward); err != nil {
			return err
		}

		// Handle staker lifecycle.
		if err := e.onCommitState.DeleteCurrentValidator(stakerToReward); err != nil {
			return fmt.Errorf("deleting current validator from commit state: %w", err)
		}

		if err := e.onAbortState.DeleteCurrentValidator(stakerToReward); err != nil {
			return fmt.Errorf("deleting current validator from abort state: %w", err)
		}
	case txs.DelegatorTx:
		if err := e.rewardDelegatorTx(uStakerTx, stakerToReward); err != nil {
			return err
		}

		// Handle staker lifecycle.
		if err := e.onCommitState.DeleteCurrentDelegator(stakerToReward); err != nil {
			return fmt.Errorf("deleting current delegator from commit state: %w", err)
		}

		if err := e.onAbortState.DeleteCurrentDelegator(stakerToReward); err != nil {
			return fmt.Errorf("deleting current delegator from abort state: %w", err)
		}
	default:
		// Invariant: Permissioned stakers are removed by the advancement of
		//            time and the current chain timestamp is == this staker's
		//            EndTime. This means only permissionless stakers should be
		//            left in the staker set.
		return ErrShouldBePermissionlessStaker
	}

	return e.undoSupplyMintOnAbort(stakerToReward)
}

func (e *proposalTxExecutor) rewardValidatorTx(uValidatorTx txs.ValidatorTx, validator *state.Staker) error {
	var (
		txID    = validator.TxID
		stake   = uValidatorTx.Stake()
		outputs = uValidatorTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	unstakeUTXOs(uValidatorTx, txID, e.onCommitState, e.onAbortState)

	utxosOffset := 0

	// Provide the reward here
	reward := validator.PotentialReward
	if reward > 0 {
		utxo, err := e.newUTXO(
			reward,
			uValidatorTx.ValidationRewardsOwner(),
			txID,
			uint32(len(outputs)+len(stake)),
			stakeAsset,
		)
		if err != nil {
			return err
		}
		e.onCommitState.AddUTXO(utxo)
		e.onCommitState.AddRewardUTXO(txID, utxo)

		utxosOffset++
	}

	// Provide the accrued delegatee rewards from successful delegations here.
	stakingInfo, err := e.onCommitState.GetStakingInfo(
		validator.SubnetID,
		validator.NodeID,
	)
	if err != nil {
		return fmt.Errorf("failed to fetch accrued delegatee rewards: %w", err)
	}
	delegateeReward := stakingInfo.DelegateeReward

	if delegateeReward == 0 {
		return nil
	}

	delegationRewardsOwner := uValidatorTx.DelegationRewardsOwner()
	onCommitUtxo, err := e.newUTXO(
		delegateeReward,
		delegationRewardsOwner,
		txID,
		uint32(len(outputs)+len(stake)+utxosOffset),
		stakeAsset,
	)
	if err != nil {
		return err
	}
	e.onCommitState.AddUTXO(onCommitUtxo)
	e.onCommitState.AddRewardUTXO(txID, onCommitUtxo)

	// The delegatee reward is the validator's commission on rewards that were
	// already paid out to its delegators when those delegators were rewarded.
	// That commission was earned during the delegators' successful staking
	// periods, so it is owed to the validator regardless of whether the
	// validator met its own uptime requirement, which is why we also issue it on
	// the abort path.
	//
	// Note: There is no [offset] if the RewardValidatorTx is
	// aborted, because the validator reward is not awarded.
	onAbortUtxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: uint32(len(outputs) + len(stake)),
		},
		Asset: stakeAsset,
		Out:   onCommitUtxo.Out,
	}
	e.onAbortState.AddUTXO(onAbortUtxo)
	e.onAbortState.AddRewardUTXO(txID, onAbortUtxo)
	return nil
}

func (e *proposalTxExecutor) RewardAutoRenewedValidatorTx(tx *txs.RewardAutoRenewedValidatorTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	if len(e.tx.Creds) != 0 {
		return errWrongNumberOfCredentials
	}

	// Auto-renewed validators keep the original validator txID across cycles, so the
	// timestamp disambiguates reward txs for different cycles. Require it to match
	// the current chain timestamp so each rewardable cycle has a canonical reward tx.
	if tx.Timestamp != uint64(e.onCommitState.GetTimestamp().Unix()) {
		return errInvalidTimestamp
	}

	stakerTx, staker, err := getNextStakerToReward(e.onCommitState, tx)
	if err != nil {
		return err
	}

	addAutoRenewedValidatorTx, ok := stakerTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	if !ok {
		return errShouldBeAutoRenewedStaker
	}

	// The validator is unstaked even if it is not eligible for rewards
	if err := e.onAbortState.DeleteCurrentValidator(staker); err != nil {
		return fmt.Errorf("deleting current validator from abort state: %w", err)
	}

	if err := e.undoSupplyMintOnAbort(staker); err != nil {
		return err
	}

	stakingInfo, err := e.onCommitState.GetStakingInfo(staker.SubnetID, staker.NodeID)
	if err != nil {
		return fmt.Errorf("failed to get staking info: %w", err)
	}

	if stakingInfo.NextPeriod == 0 {
		// The validator requested to stop auto-staking.
		//
		// On commit (eligible for rewards) the validator is returned:
		//   - Initial staked tokens
		//   - All validation rewards (PotentialReward + AccruedRewards)
		//   - All delegatee rewards (AccruedDelegateeRewards + pending delegatee rewards)
		//
		// On abort (not eligible for rewards):
		//   - Initial Staked tokens
		//   - Only accrued validation rewards (AccruedRewards)
		//   - All delegatee rewards (AccruedDelegateeRewards + pending delegatee rewards)
		// Return stake + all rewards on both commit and abort paths.
		unstakeUTXOs(addAutoRenewedValidatorTx, staker.TxID, e.onCommitState, e.onAbortState)

		if err := e.mintRewardsOnAbort(addAutoRenewedValidatorTx, stakingInfo); err != nil {
			return err
		}

		totalRewards, err := safemath.Add(staker.PotentialReward, stakingInfo.AccruedValidationRewards)
		if err != nil {
			return err
		}

		// DelegateeReward is the validator's commission on rewards already paid out to
		// its delegators, earned during their successful staking periods. It is owed to
		// the validator even on the abort path, because the validator only forfeits its
		// own potential reward for failing to meet eligibility requirements this cycle.
		totalDelegateeRewards, err := safemath.Add(stakingInfo.DelegateeReward, stakingInfo.AccruedDelegateeRewards)
		if err != nil {
			return err
		}

		if err = e.mintRewards(
			addAutoRenewedValidatorTx,
			totalRewards,
			totalDelegateeRewards,
			e.onCommitState,
		); err != nil {
			return err
		}

		if err := e.onCommitState.DeleteCurrentValidator(staker); err != nil {
			return fmt.Errorf("deleting current validator from commit state: %w", err)
		}

		return nil
	}

	// The validator is eligible to auto-restake.
	//
	// If a validator is not eligible for rewards, unstake its assets and mint any
	// accrued rewards, forfeiting any potential rewards from the current cycle.
	unstakeUTXOs(addAutoRenewedValidatorTx, staker.TxID, e.onAbortState)
	if err := e.mintRewardsOnAbort(addAutoRenewedValidatorTx, stakingInfo); err != nil {
		return err
	}

	// If a validator is eligible for rewards, restake its assets for another
	// subsequent staking cycle.
	if err = e.restakeOnCommit(addAutoRenewedValidatorTx, staker, stakingInfo); err != nil {
		return err
	}

	return nil
}

// undoSupplyMintOnAbort removes the staker's potential reward from
// the abort branch of a reward proposal. Potential rewards are added
// optimistically to the current supply when the validator is added so they
// must be deducted on abort if the validator is not eligible for rewards.
func (e *proposalTxExecutor) undoSupplyMintOnAbort(staker *state.Staker) error {
	currentSupply, err := e.onAbortState.GetCurrentSupply(staker.SubnetID)
	if err != nil {
		return err
	}
	newSupply, err := safemath.Sub(currentSupply, staker.PotentialReward)
	if err != nil {
		return err
	}
	e.onAbortState.SetCurrentSupply(staker.SubnetID, newSupply)
	return nil
}

func (e *proposalTxExecutor) rewardDelegatorTx(uDelegatorTx txs.DelegatorTx, delegator *state.Staker) error {
	var (
		txID    = delegator.TxID
		stake   = uDelegatorTx.Stake()
		outputs = uDelegatorTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	unstakeUTXOs(uDelegatorTx, txID, e.onCommitState, e.onAbortState)

	// We're (possibly) rewarding a delegator, so we need to fetch
	// the validator they are delegated to.
	validator, err := e.onCommitState.GetCurrentValidator(delegator.SubnetID, delegator.NodeID)
	if err != nil {
		return fmt.Errorf("failed to get whether %s is a validator: %w", delegator.NodeID, err)
	}

	vdrTxIntf, _, err := e.onCommitState.GetTx(validator.TxID)
	if err != nil {
		return fmt.Errorf("failed to get whether %s is a validator: %w", delegator.NodeID, err)
	}

	// Invariant: Delegators must only be able to reference validator
	//            transactions that implement [txs.ValidatorTx]. All
	//            validator transactions implement this interface except the
	//            AddSubnetValidatorTx.
	vdrTx, ok := vdrTxIntf.Unsigned.(txs.ValidatorTx)
	if !ok {
		return ErrWrongTxType
	}

	// Calculate split of reward between delegator/delegatee
	delegateeReward, delegatorReward := reward.Split(delegator.PotentialReward, vdrTx.Shares())

	utxosOffset := 0

	// Reward the delegator here
	reward := delegatorReward
	if reward > 0 {
		utxo, err := e.newUTXO(
			reward,
			uDelegatorTx.RewardsOwner(),
			txID,
			uint32(len(outputs)+len(stake)),
			stakeAsset,
		)
		if err != nil {
			return err
		}

		e.onCommitState.AddUTXO(utxo)
		e.onCommitState.AddRewardUTXO(txID, utxo)

		utxosOffset++
	}

	if delegateeReward == 0 {
		return nil
	}

	// Reward the delegatee here
	if e.backend.Config.UpgradeConfig.IsCortinaActivated(validator.StartTime) {
		stakingInfo, err := e.onCommitState.GetStakingInfo(
			validator.SubnetID,
			validator.NodeID,
		)
		if err != nil {
			return fmt.Errorf("failed to get staking info: %w", err)
		}

		// Invariant: The rewards calculator can never return a
		//            [potentialReward] that would overflow the
		//            accumulated rewards.
		stakingInfo.DelegateeReward += delegateeReward

		// For any validators starting after [CortinaTime], we defer rewarding the
		// [reward] until their staking period is over.
		err = e.onCommitState.SetStakingInfo(
			validator.SubnetID,
			validator.NodeID,
			stakingInfo,
		)
		if err != nil {
			return fmt.Errorf("failed to update delegatee reward: %w", err)
		}
	} else {
		// For any validators who started prior to [CortinaTime], we issue the
		// [delegateeReward] immediately.
		utxo, err := e.newUTXO(
			delegateeReward,
			vdrTx.DelegationRewardsOwner(),
			txID,
			uint32(len(outputs)+len(stake)+utxosOffset),
			stakeAsset,
		)
		if err != nil {
			return err
		}

		e.onCommitState.AddUTXO(utxo)
		e.onCommitState.AddRewardUTXO(txID, utxo)
	}
	return nil
}

// mintRewardsOnAbort creates reward UTXOs on the abort state for an
// auto-renewed validator. This includes accrued validation rewards and
// all delegatee rewards (accrued + pending).
func (e *proposalTxExecutor) mintRewardsOnAbort(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	stakingInfo state.StakingInfo,
) error {
	// DelegateeReward tracks pending commission from completed delegator periods.
	// It is paid on the abort path along with accrued delegatee rewards, while the
	// validator forfeits its own potential reward for this cycle.
	totalDelegateeRewards, err := safemath.Add(stakingInfo.DelegateeReward, stakingInfo.AccruedDelegateeRewards)
	if err != nil {
		return err
	}

	if err = e.mintRewards(
		addAutoRenewedValidatorTx,
		stakingInfo.AccruedValidationRewards,
		totalDelegateeRewards,
		e.onAbortState,
	); err != nil {
		return err
	}

	return nil
}

// mintRewards adds reward UTXOs to chainState at output indices starting
// after the tx's own outputs.
//
// It must be called at most once per chainState per execution: the output
// indices are derived from len(e.tx.Unsigned.Outputs()) and do not account for
// UTXOs a prior call already added to the same diff, so a second call would
// reuse the same (txID, outputIndex) pairs and collide on UTXO IDs.
func (e *proposalTxExecutor) mintRewards(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	validationRewards uint64,
	delegateeRewards uint64,
	chainState *state.Diff,
) error {
	avaxAsset := avax.Asset{ID: e.backend.Ctx.AVAXAssetID}
	outputIndexOffset := uint32(len(e.tx.Unsigned.Outputs()))

	// Create UTXOs for validation rewards.
	if validationRewards > 0 {
		utxo, err := e.newUTXO(validationRewards, addAutoRenewedValidatorTx.ValidationRewardsOwner(), e.tx.ID(), outputIndexOffset, avaxAsset)
		if err != nil {
			return err
		}
		chainState.AddUTXO(utxo)
		chainState.AddRewardUTXO(e.tx.ID(), utxo)
		outputIndexOffset++
	}

	// Create UTXOs for delegatee rewards.
	if delegateeRewards > 0 {
		utxo, err := e.newUTXO(delegateeRewards, addAutoRenewedValidatorTx.DelegationRewardsOwner(), e.tx.ID(), outputIndexOffset, avaxAsset)
		if err != nil {
			return err
		}
		chainState.AddUTXO(utxo)
		chainState.AddRewardUTXO(e.tx.ID(), utxo)
	}

	return nil
}

// restakeOnCommit processes rewards for a running
// auto-renewed validator based on their AutoCompoundRewardShares configuration.
//
// The function:
//  1. Splits rewards (validation + delegatee) into restaking and withdrawing portions
//  2. Caps the restaking portion so the validator's weight stays within
//     MaxValidatorStake, withdrawing anything that doesn't fit
//  3. Creates UTXOs for the withdrawn portion
//  4. Increases validator weight and accrued rewards by the restaking portion
//  5. Updates the validator state
func (e *proposalTxExecutor) restakeOnCommit(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	validator *state.Staker,
	stakingInfo state.StakingInfo,
) error {
	// Ignore the withdrawn portions from reward.Split because the restaked
	// amounts may be capped below. Withdrawn rewards are computed later from the
	// difference between total rewards and the amounts actually restaked.
	restakingValidationRewards, _ := reward.Split(validator.PotentialReward, stakingInfo.AutoCompoundRewardShares)
	restakingDelegateeRewards, _ := reward.Split(stakingInfo.DelegateeReward, stakingInfo.AutoCompoundRewardShares)

	// Restaking grows the validator's weight, which must never exceed
	// MaxValidatorStake. If the restaked rewards wouldn't fit, only the remaining
	// capacity is restaked (split proportionally between validation and delegatee
	// rewards) and the rest is withdrawn below.
	restakingCapacity, err := safemath.Sub(e.backend.Config.MaxValidatorStake, validator.Weight)
	if err != nil {
		return err
	}

	totalRestakingRewards, err := safemath.Add(restakingValidationRewards, restakingDelegateeRewards)
	if err != nil {
		return err
	}

	if totalRestakingRewards > restakingCapacity {
		restakingValidationRewards, err = mulDivRound(restakingValidationRewards, restakingCapacity, totalRestakingRewards)
		if err != nil {
			return err
		}

		restakingDelegateeRewards, err = safemath.Sub(restakingCapacity, restakingValidationRewards)
		if err != nil {
			return err
		}
	}

	// Withdraw everything that isn't being restaked.
	withdrawingRewards, err := safemath.Sub(validator.PotentialReward, restakingValidationRewards)
	if err != nil {
		return err
	}

	withdrawingDelegateeRewards, err := safemath.Sub(stakingInfo.DelegateeReward, restakingDelegateeRewards)
	if err != nil {
		return err
	}

	if err := e.mintRewards(
		addAutoRenewedValidatorTx,
		withdrawingRewards,
		withdrawingDelegateeRewards,
		e.onCommitState,
	); err != nil {
		return err
	}

	// Compound the restaked rewards into the validator's weight and accrued
	// reward totals.
	newWeight, err := safemath.Add(validator.Weight, restakingValidationRewards)
	if err != nil {
		return err
	}

	newWeight, err = safemath.Add(newWeight, restakingDelegateeRewards)
	if err != nil {
		return err
	}

	newAccruedRewards, err := safemath.Add(stakingInfo.AccruedValidationRewards, restakingValidationRewards)
	if err != nil {
		return err
	}

	newAccruedDelegateeRewards, err := safemath.Add(stakingInfo.AccruedDelegateeRewards, restakingDelegateeRewards)
	if err != nil {
		return err
	}

	rewards, err := GetRewardsCalculator(e.backend, e.onCommitState, validator.SubnetID)
	if err != nil {
		return err
	}

	currentSupply, err := e.onCommitState.GetCurrentSupply(validator.SubnetID)
	if err != nil {
		return err
	}

	duration := time.Duration(stakingInfo.NextPeriod) * time.Second
	potentialReward := rewards.Calculate(
		duration,
		newWeight,
		currentSupply,
	)

	newCurrentSupply, err := safemath.Add(currentSupply, potentialReward)
	if err != nil {
		return err
	}

	e.onCommitState.SetCurrentSupply(validator.SubnetID, newCurrentSupply)

	endTime := validator.EndTime.Add(duration)

	// Update validator by deleting and putting back.
	renewedValidator := *validator
	renewedValidator.StartTime = renewedValidator.EndTime
	renewedValidator.EndTime = endTime
	renewedValidator.NextTime = endTime
	renewedValidator.PotentialReward = potentialReward
	renewedValidator.Weight = newWeight

	if err := e.onCommitState.DeleteCurrentValidator(validator); err != nil {
		return fmt.Errorf("failed to delete validator from commit state: %w", err)
	}

	if err := e.onCommitState.PutCurrentValidator(&renewedValidator); err != nil {
		return fmt.Errorf("putting renewed validator: %w", err)
	}

	// Update staking info
	stakingInfo.DelegateeReward = 0
	stakingInfo.AccruedValidationRewards = newAccruedRewards
	stakingInfo.AccruedDelegateeRewards = newAccruedDelegateeRewards

	if err := e.onCommitState.SetStakingInfo(validator.SubnetID, validator.NodeID, stakingInfo); err != nil {
		return fmt.Errorf("setting staking info for validator: %w", err)
	}

	return nil
}

// MulDiv computes (a * b) / c with full precision.
// The result is rounded to the nearest integer.
// Returns ErrDivideByZero if c is zero, or ErrOverflow if the result exceeds uint64.
func mulDivRound(a, b, c uint64) (uint64, error) {
	// intmath.MulDiv reports a zero denominator as an overflow, so check it
	// first to preserve the distinct error.
	if c == 0 {
		return 0, errDivideByZero
	}

	quo, rem, err := intmath.MulDiv(a, b, c)
	if err != nil {
		return 0, err
	}

	// Round to nearest by checking whether the fractional part rem/c is less
	// than 1/2. rem < c-rem ⟺ 2*rem < c, but can't overflow since rem < c.
	if rem < c-rem {
		return quo, nil
	}

	// The fractional part is at least 1/2, so round up. If quo is already
	// MaxUint64, rounding up would overflow the result.
	if quo == math.MaxUint64 {
		return 0, intmath.ErrOverflow
	}
	return quo + 1, nil
}

// newUTXO creates a reward UTXO with the specified parameters.
// The caller is responsible for adding the UTXO to the appropriate state(s)
// via AddUTXO and AddRewardUTXO.
func (e *proposalTxExecutor) newUTXO(
	amount uint64,
	owner fx.Owner,
	txID ids.ID,
	outputIndex uint32,
	asset avax.Asset,
) (*avax.UTXO, error) {
	outIntf, err := e.backend.Fx.CreateOutput(amount, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to create output: %w", err)
	}
	out, ok := outIntf.(verify.State)
	if !ok {
		return nil, ErrInvalidState
	}

	return &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: outputIndex,
		},
		Asset: asset,
		Out:   out,
	}, nil
}

// unstakeUTXOs creates UTXOs to unstakeUTXOs a staker's staked tokens.
// The UTXOs are added to all provided state diffs.
func unstakeUTXOs(stakerTx txs.PermissionlessStaker, txID ids.ID, states ...*state.Diff) {
	outputIndexOffset := len(stakerTx.Outputs())

	for i, out := range stakerTx.Stake() {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(outputIndexOffset + i),
			},
			Asset: out.Asset,
			Out:   out.Output(),
		}

		for _, s := range states {
			s.AddUTXO(utxo)
		}
	}
}
