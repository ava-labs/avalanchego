// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math/intmath"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
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
	_ platform.TxVisitor = (*proposalTxExecutor)(nil)

	ErrRemoveStakerTooEarly          = errors.New("attempting to remove staker before their end time")
	ErrRemoveWrongStaker             = errors.New("attempting to remove wrong staker")
	ErrInvalidState                  = errors.New("generated output isn't valid state")
	ErrWrongTxType                   = errors.New("wrong transaction type")
	ErrInvalidID                     = errors.New("invalid ID")
	ErrProposedAddStakerTxAfterBanff = errors.New("staker transaction proposed after Banff")
	ErrAdvanceTimeTxIssuedAfterBanff = errors.New("AdvanceTimeTx issued after Banff")
	errShouldBeAutoRenewedStaker     = errors.New("expected auto renewed staker")
	errInvalidTimestamp              = errors.New("invalid timestamp")
	errUnexpectedStakerTxType        = errors.New("unexpected staker transaction type")
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
	tx *platform.Tx,
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
	tx            *platform.Tx
	// [onCommitState] is the state used for validation.
	// [onCommitState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is committed.
	onCommitState *state.Diff
	// [onAbortState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is aborted.
	onAbortState *state.Diff
}

func (*proposalTxExecutor) CreateChainTx(*platform.CreateChainTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) CreateSubnetTx(*platform.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) ImportTx(*platform.ImportTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) ExportTx(*platform.ExportTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) RemoveSubnetValidatorTx(*platform.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) TransformSubnetTx(*platform.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) AddPermissionlessValidatorTx(*platform.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) AddPermissionlessDelegatorTx(*platform.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) TransferSubnetOwnershipTx(*platform.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) BaseTx(*platform.BaseTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) ConvertSubnetToL1Tx(*platform.ConvertSubnetToL1Tx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) RegisterL1ValidatorTx(*platform.RegisterL1ValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) SetL1ValidatorWeightTx(*platform.SetL1ValidatorWeightTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) IncreaseL1ValidatorBalanceTx(*platform.IncreaseL1ValidatorBalanceTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) DisableL1ValidatorTx(*platform.DisableL1ValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) AddAutoRenewedValidatorTx(*platform.AddAutoRenewedValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) SetAutoRenewedValidatorConfigTx(*platform.SetAutoRenewedValidatorConfigTx) error {
	return ErrWrongTxType
}

func (e *proposalTxExecutor) AddValidatorTx(tx *platform.AddValidatorTx) error {
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

	validator, err := state.NewPendingPrimaryNetworkValidator(txID, tx)
	if err != nil {
		return err
	}

	if err := state.NewAdapter(e.onCommitState).PutPendingPrimaryNetworkValidator(tx, validator); err != nil {
		return err
	}

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.onAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onAbortState, txID, onAbortOuts)
	return nil
}

func (e *proposalTxExecutor) AddSubnetValidatorTx(tx *platform.AddSubnetValidatorTx) error {
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

	validator, err := state.NewPendingSubnetValidator(txID, tx)
	if err != nil {
		return err
	}

	if err := state.NewAdapter(e.onCommitState).PutPendingSubnetValidator(tx, validator); err != nil {
		return err
	}

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.onAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onAbortState, txID, tx.Outs)
	return nil
}

func (e *proposalTxExecutor) AddDelegatorTx(tx *platform.AddDelegatorTx) error {
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

	delegator, err := state.NewPendingDelegator(txID, tx)
	if err != nil {
		return err
	}

	if err := state.NewAdapter(e.onCommitState).PutPendingDelegator(tx, delegator); err != nil {
		return err
	}

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.onAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.onAbortState, txID, onAbortOuts)
	return nil
}

func (e *proposalTxExecutor) AdvanceTimeTx(tx *platform.AdvanceTimeTx) error {
	switch {
	case tx == nil:
		return platform.ErrNilTx
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

func (e *proposalTxExecutor) RewardValidatorTx(tx *platform.RewardValidatorTx) error {
	switch {
	case tx == nil:
		return platform.ErrNilTx
	case tx.TxID == ids.Empty:
		return ErrInvalidID
	case len(e.tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	stakerTx, stakerToReward, err := getNextStakerToReward(e.onCommitState, tx)
	if err != nil {
		return err
	}

	// Dispatch on the concrete staker type. Only these four are rewarded here.
	//
	// [*platform.AddAutoRenewedValidatorTx] also implements [platform.ValidatorTx] but is
	// rewarded through [platform.RewardAutoRenewedValidatorTx].
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case *platform.AddValidatorTx, *platform.AddPermissionlessValidatorTx:
		validator, ok := stakerToReward.(state.CurrentValidator)
		if !ok {
			return fmt.Errorf("%w: %T", errUnexpectedStakerTxType, stakerToReward)
		}
		if err := e.rewardValidatorTx(uStakerTx.(platform.ValidatorTx), validator); err != nil {
			return err
		}

		// Handle staker lifecycle.
		if validator.SubnetID() == constants.PrimaryNetworkID {
			if err := state.NewAdapter(e.onCommitState).DeleteCurrentPrimaryNetworkValidator(validator.NodeID()); err != nil {
				return fmt.Errorf("deleting current validator from commit state: %w", err)
			}
			if err := state.NewAdapter(e.onAbortState).DeleteCurrentPrimaryNetworkValidator(validator.NodeID()); err != nil {
				return fmt.Errorf("deleting current validator from abort state: %w", err)
			}
		} else {
			if err := state.NewAdapter(e.onCommitState).DeleteCurrentSubnetValidator(validator.SubnetID(), validator.NodeID()); err != nil {
				return fmt.Errorf("deleting current validator from commit state: %w", err)
			}
			if err := state.NewAdapter(e.onAbortState).DeleteCurrentSubnetValidator(validator.SubnetID(), validator.NodeID()); err != nil {
				return fmt.Errorf("deleting current validator from abort state: %w", err)
			}
		}
	case *platform.AddDelegatorTx, *platform.AddPermissionlessDelegatorTx:
		delegator, ok := stakerToReward.(state.CurrentDelegator)
		if !ok {
			return fmt.Errorf("%w: %T", errUnexpectedStakerTxType, stakerToReward)
		}
		if err := e.rewardDelegatorTx(uStakerTx.(platform.DelegatorTx), delegator); err != nil {
			return err
		}

		// Handle staker lifecycle.
		if err := state.NewAdapter(e.onCommitState).DeleteCurrentDelegator(delegator.TxID); err != nil {
			return fmt.Errorf("deleting current delegator from commit state: %w", err)
		}

		if err := state.NewAdapter(e.onAbortState).DeleteCurrentDelegator(delegator.TxID); err != nil {
			return fmt.Errorf("deleting current delegator from abort state: %w", err)
		}
	default:
		return fmt.Errorf("%w: %T", errUnexpectedStakerTxType, uStakerTx)
	}

	return e.undoSupplyMintOnAbort(stakerToReward)
}

// RewardAutoRenewedValidatorTx processes the reward proposal for an auto-renewed
// validator at the end of a staking cycle, populating both the commit and abort
// branches.
//
// The abort branch is the same regardless of configuration: the validator is
// removed, its principal is returned, the optimistically-minted potential reward
// is removed from the supply, and only its accrued validation rewards and
// delegatee rewards are paid out (the current cycle's potential reward is
// forfeited).
//
// The commit branch depends on the configured next period:
//   - NextPeriod > 0: the validator continues. Its rewards are restaked per
//     AutoCompoundRewardShares (growing its weight up to MaxValidatorStake and
//     withdrawing any excess) and the next cycle begins immediately.
//   - NextPeriod == 0: the validator gracefully exits. It is removed, its
//     principal is returned, and it receives all rewards for the cycle (current
//     potential reward + accrued validation rewards + all delegatee rewards).
func (e *proposalTxExecutor) RewardAutoRenewedValidatorTx(tx *platform.RewardAutoRenewedValidatorTx) error {
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

	uStakerTx, ok := stakerTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)
	if !ok {
		return errShouldBeAutoRenewedStaker
	}

	validator, ok := staker.(state.CurrentPrimaryNetworkValidator)
	if !ok {
		return errShouldBeAutoRenewedStaker
	}
	continuousValidator, err := state.NewAdapter(e.onCommitState).GetCurrentContinuousPrimaryNetworkValidator(validator.NodeID())
	if err != nil {
		return fmt.Errorf("failed to get continuous validator: %w", err)
	}
	stakingInfo, err := e.onCommitState.GetStakingInfo(validator.SubnetID(), validator.NodeID())
	if err != nil {
		return fmt.Errorf("failed to get delegatee reward: %w", err)
	}

	// Build the abort branch (same for both configurations): remove the
	// validator, return its principal, and undo the optimistic supply mint since
	// the current cycle's potential reward is not granted on abort.
	if err := state.NewAdapter(e.onAbortState).DeleteCurrentPrimaryNetworkValidator(validator.NodeID()); err != nil {
		return fmt.Errorf("deleting current validator from abort state: %w", err)
	}

	unstakeUTXOs(uStakerTx, validator.TxID, e.onAbortState)

	if err := e.undoSupplyMintOnAbort(validator); err != nil {
		return err
	}

	// Abort: pay accrued validation + all delegatee rewards (the current
	// cycle's potential reward is forfeited).
	if err = e.mintRewardOnAbort(uStakerTx, continuousValidator.ContinuousValidatorMetadata, stakingInfo.DelegateeReward); err != nil {
		return fmt.Errorf("minting reward on abort: %w", err)
	}

	if continuousValidator.NextPeriod > 0 {
		// The validator continues to the next cycle.

		// Commit: restake rewards per AutoCompoundRewardShares and start the
		// next cycle. The validator stays in the set, so its stake is not
		// returned here.
		return e.restakeAutoRenewedValidatorOnCommit(uStakerTx, continuousValidator)
	}

	// Graceful exit (NextPeriod == 0): the validator stops after this cycle and
	// is removed on both branches.
	if err := state.NewAdapter(e.onCommitState).DeleteCurrentPrimaryNetworkValidator(validator.NodeID()); err != nil {
		return fmt.Errorf("deleting current validator from commit state: %w", err)
	}

	// Commit: return the principal. (The abort branch already returned it above.)
	unstakeUTXOs(uStakerTx, validator.TxID, e.onCommitState)

	// Commit: pay all rewards for the cycle — the current potential reward plus
	// any accrued validation rewards.
	totalValidationRewards, err := safemath.Add(validator.PotentialReward, continuousValidator.AccruedValidationRewards)
	if err != nil {
		return err
	}

	// On graceful exit the validator receives all delegatee rewards: the pending
	// commission from this cycle's completed delegations (DelegateeReward) plus the
	// commission accrued and restaked in prior cycles (AccruedDelegateeRewards).
	totalDelegateeRewards, err := safemath.Add(stakingInfo.DelegateeReward, continuousValidator.AccruedDelegateeRewards)
	if err != nil {
		return err
	}

	return e.mintRewards(
		uStakerTx,
		totalValidationRewards,
		totalDelegateeRewards,
		e.onCommitState,
	)
}

// undoSupplyMintOnAbort removes the staker's potential reward from
// the abort branch of a reward proposal. Potential rewards are added
// optimistically to the current supply when the validator is added so they
// must be deducted on abort if the validator is not eligible for rewards.
func (e *proposalTxExecutor) undoSupplyMintOnAbort(staker state.CurrentStaker) error {
	period := staker.Period()
	currentSupply, err := e.onAbortState.GetCurrentSupply(period.SubnetID())
	if err != nil {
		return err
	}
	newSupply, err := safemath.Sub(currentSupply, staker.Reward())
	if err != nil {
		return err
	}
	e.onAbortState.SetCurrentSupply(period.SubnetID(), newSupply)
	return nil
}

func (e *proposalTxExecutor) rewardValidatorTx(uValidatorTx platform.ValidatorTx, validator state.CurrentValidator) error {
	period := validator.Period()
	var (
		txID    = period.TxID
		stake   = uValidatorTx.Stake()
		outputs = uValidatorTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	unstakeUTXOs(uValidatorTx, txID, e.onCommitState)
	unstakeUTXOs(uValidatorTx, txID, e.onAbortState)

	utxosOffset := 0

	// Provide the reward here
	reward := validator.Reward()
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
		validator.SubnetID(),
		validator.NodeID(),
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

func (e *proposalTxExecutor) rewardDelegatorTx(uDelegatorTx platform.DelegatorTx, delegator state.CurrentDelegator) error {
	var (
		txID    = delegator.TxID
		stake   = uDelegatorTx.Stake()
		outputs = uDelegatorTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	unstakeUTXOs(uDelegatorTx, txID, e.onCommitState)
	unstakeUTXOs(uDelegatorTx, txID, e.onAbortState)

	// We're (possibly) rewarding a delegator, so we need to fetch
	// the validator they are delegated to.
	stakingState := state.NewAdapter(e.onCommitState)
	var validator state.StakingPeriod
	if delegator.SubnetID() == constants.PrimaryNetworkID {
		primaryNetworkValidator, err := stakingState.GetCurrentPrimaryNetworkValidator(delegator.NodeID())
		if err != nil {
			return fmt.Errorf("failed to get whether %s is a validator: %w", delegator.NodeID(), err)
		}
		validator = primaryNetworkValidator.StakingPeriod
	} else {
		subnetValidator, err := stakingState.GetCurrentSubnetValidator(delegator.SubnetID(), delegator.NodeID())
		if err != nil {
			return fmt.Errorf("failed to get whether %s is a validator: %w", delegator.NodeID(), err)
		}
		validator = subnetValidator.StakingPeriod
	}

	vdrTxIntf, _, err := e.onCommitState.GetTx(validator.TxID)
	if err != nil {
		return fmt.Errorf("failed to get whether %s is a validator: %w", delegator.NodeID(), err)
	}

	// Invariant: Delegators must only be able to reference validator
	//            transactions that implement [platform.ValidatorTx]. All
	//            validator transactions implement this interface except the
	//            AddSubnetValidatorTx.
	vdrTx, ok := vdrTxIntf.Unsigned.(platform.ValidatorTx)
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
			validator.SubnetID(),
			validator.NodeID(),
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
			validator.SubnetID(),
			validator.NodeID(),
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

// mintRewardOnAbort creates reward UTXOs on the abort state for an
// auto-renewed validator. This includes accrued validation rewards and
// all delegatee rewards (accrued + pending).
func (e *proposalTxExecutor) mintRewardOnAbort(
	addAutoRenewedValidatorTx *platform.AddAutoRenewedValidatorTx,
	metadata state.ContinuousValidatorMetadata,
	delegateeReward uint64,
) error {
	// DelegateeReward tracks pending commission from completed delegator periods.
	// It is paid on the abort path along with accrued delegatee rewards, while the
	// validator forfeits its own potential reward for this cycle.
	totalDelegateeRewards, err := safemath.Add(delegateeReward, metadata.AccruedDelegateeRewards)
	if err != nil {
		return err
	}

	return e.mintRewards(
		addAutoRenewedValidatorTx,
		metadata.AccruedValidationRewards,
		totalDelegateeRewards,
		e.onAbortState,
	)
}

// mintRewards adds reward UTXOs to chainState at output indices starting
// after the tx's own outputs.
//
// It must be called at most once per chainState per execution: the output
// indices are derived from len(e.tx.Unsigned.Outputs()) and do not account for
// UTXOs a prior call already added to the same diff, so a second call would
// reuse the same (txID, outputIndex) pairs and collide on UTXO IDs.
func (e *proposalTxExecutor) mintRewards(
	addAutoRenewedValidatorTx *platform.AddAutoRenewedValidatorTx,
	validationRewards uint64,
	delegateeRewards uint64,
	chainState *state.Diff,
) error {
	txID := e.tx.ID()
	avaxAsset := avax.Asset{ID: e.backend.Ctx.AVAXAssetID}
	outputIndexOffset := uint32(len(e.tx.Unsigned.Outputs()))

	// Create UTXOs for validation rewards.
	if validationRewards > 0 {
		utxo, err := e.newUTXO(validationRewards, addAutoRenewedValidatorTx.ValidationRewardsOwner(), txID, outputIndexOffset, avaxAsset)
		if err != nil {
			return err
		}
		chainState.AddUTXO(utxo)
		chainState.AddRewardUTXO(txID, utxo)
		outputIndexOffset++
	}

	// Create UTXOs for delegatee rewards.
	if delegateeRewards > 0 {
		utxo, err := e.newUTXO(delegateeRewards, addAutoRenewedValidatorTx.DelegationRewardsOwner(), txID, outputIndexOffset, avaxAsset)
		if err != nil {
			return err
		}
		chainState.AddUTXO(utxo)
		chainState.AddRewardUTXO(txID, utxo)
	}

	return nil
}

// restakeAutoRenewedValidatorOnCommit processes rewards for a running
// auto-renewed validator based on their AutoCompoundRewardShares configuration.
//
// The function:
//  1. Splits rewards (validation + delegatee) into restaking and withdrawing portions
//  2. Caps the restaking portion so the validator's weight stays within
//     MaxValidatorStake, withdrawing anything that doesn't fit
//  3. Creates UTXOs for the withdrawn portion
//  4. Increases validator weight and accrued rewards by the restaking portion
//  5. Updates the validator state
func (e *proposalTxExecutor) restakeAutoRenewedValidatorOnCommit(
	addAutoRenewedValidatorTx *platform.AddAutoRenewedValidatorTx,
	continuousValidator state.AutoRenewedValidator,
) error {
	validator := continuousValidator.Validator
	metadata := continuousValidator.ContinuousValidatorMetadata
	// Ignore the withdrawn portions from [reward.Split] because the restaked
	// amounts may be capped below. Withdrawn rewards are computed later from the
	// difference between total rewards and the amounts actually restaked.
	stakingInfo, err := e.onCommitState.GetStakingInfo(validator.SubnetID(), validator.NodeID())
	if err != nil {
		return fmt.Errorf("getting delegatee reward: %w", err)
	}
	restakingValidationRewards, _ := reward.Split(validator.PotentialReward, metadata.AutoCompoundRewardShares)
	restakingDelegateeRewards, _ := reward.Split(stakingInfo.DelegateeReward, metadata.AutoCompoundRewardShares)

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
		// Let V be the validation rewards, D be the delegatee rewards, C be the
		// restaking capacity, and T = V + D. This branch means C < T. After
		// computing V' = floor(V * C / T), the delegatee restake is D' = C - V'.
		//
		// Proving D' cannot exceed D:
		//   D' - D = (C - V') - (T - V)
		//          = (C - T) + (V - V')
		// Because V' = floor(V * C / T):
		//   V' > V * C / T - 1
		//   -V' < -(V * C / T - 1)
		//   -V' < -V * C / T + 1
		//   V - V' < V - V * C / T + 1
		//   V - V' < V * (1 - C / T) + 1
		//   V - V' < V * (T / T - C / T) + 1
		//   V - V' < V * (T - C) / T + 1
		// Since V <= T:
		//   V - V' < T - C + 1
		//   V - V' <= T - C
		// Substitute that bound into D' - D:
		//   D' - D <= (C - T) + (T - C)
		//          <= 0
		// Therefore D' <= D.
		//
		// Therefore assigning the remaining capacity to delegatee rewards cannot
		// increase them, and the later withdrawal subtraction cannot underflow.
		restakingValidationRewards, _, err = intmath.MulDiv(restakingValidationRewards, restakingCapacity, totalRestakingRewards)
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

	newAccruedRewards, err := safemath.Add(metadata.AccruedValidationRewards, restakingValidationRewards)
	if err != nil {
		return err
	}

	newAccruedDelegateeRewards, err := safemath.Add(metadata.AccruedDelegateeRewards, restakingDelegateeRewards)
	if err != nil {
		return err
	}

	rewards, err := GetRewardsCalculator(
		e.backend.Config.RewardConfig,
		e.backend.Config.UpgradeConfig,
		e.onCommitState,
		validator.SubnetID(),
	)
	if err != nil {
		return err
	}

	currentSupply, err := e.onCommitState.GetCurrentSupply(validator.SubnetID())
	if err != nil {
		return err
	}

	duration := time.Duration(metadata.NextPeriod) * time.Second
	// A renewed staking period starts when the current period ends.
	stakeStartTime := validator.EndTime
	newPotentialReward := rewards.Calculate(
		stakeStartTime,
		duration,
		newWeight,
		currentSupply,
	)

	newCurrentSupply, err := safemath.Add(currentSupply, newPotentialReward)
	if err != nil {
		return err
	}

	e.onCommitState.SetCurrentSupply(validator.SubnetID(), newCurrentSupply)

	newEndTime := stakeStartTime.Add(duration)

	// Update validator by deleting and putting back.
	renewedValidator := continuousValidator
	renewedValidator.Validator = validator
	renewedValidator.Validator.StartTime = stakeStartTime
	renewedValidator.Validator.EndTime = newEndTime
	renewedValidator.Validator.PotentialReward = newPotentialReward
	renewedValidator.Validator.Weight = newWeight

	stakingState := state.NewAdapter(e.onCommitState)
	if err := stakingState.DeleteCurrentPrimaryNetworkValidator(validator.NodeID()); err != nil {
		return fmt.Errorf("failed to delete validator from commit state: %w", err)
	}

	metadata.AccruedValidationRewards = newAccruedRewards
	metadata.AccruedDelegateeRewards = newAccruedDelegateeRewards
	renewedValidator.ContinuousValidatorMetadata = metadata
	if err := stakingState.PutAutoRenewedValidator(addAutoRenewedValidatorTx, renewedValidator); err != nil {
		return fmt.Errorf("putting renewed validator: %w", err)
	}

	return nil
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

// unstakeUTXOs creates UTXOs to unstake a staker's staked UTXOs.
// The UTXOs are added to all provided state diffs.
func unstakeUTXOs(stakerTx platform.PermissionlessStaker, txID ids.ID, state *state.Diff) {
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

		state.AddUTXO(utxo)
	}
}

func getNextStakerToReward(chainState state.Chain, tx platform.RewardTx) (*platform.Tx, state.CurrentStaker, error) {
	currentStakerIterator, err := state.NewAdapter(chainState).GetCurrentStakerIterator()
	if err != nil {
		return nil, nil, err
	}
	defer currentStakerIterator.Release()

	if !currentStakerIterator.Next() {
		return nil, nil, fmt.Errorf("failed to get next staker to remove: %w", database.ErrNotFound)
	}
	stakerToReward := currentStakerIterator.Value()
	period := stakerToReward.Period()

	if period.TxID != tx.StakerTxID() {
		return nil, nil, fmt.Errorf(
			"%w: %s != %s",
			ErrRemoveWrongStaker,
			period.TxID,
			tx.StakerTxID(),
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentChainTime := chainState.GetTimestamp()
	if !period.EndTime.Equal(currentChainTime) {
		return nil, nil, fmt.Errorf(
			"%w: TxID = %s with %s < %s",
			ErrRemoveStakerTooEarly,
			tx.StakerTxID(),
			currentChainTime,
			period.EndTime,
		)
	}

	stakerTx, _, err := chainState.GetTx(period.TxID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	return stakerTx, stakerToReward, nil
}
