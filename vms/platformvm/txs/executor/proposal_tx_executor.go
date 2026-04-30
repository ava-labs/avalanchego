// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
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

	ErrRemoveStakerTooEarly                = errors.New("attempting to remove staker before their end time")
	ErrRemoveWrongStaker                   = errors.New("attempting to remove wrong staker")
	ErrInvalidState                        = errors.New("generated output isn't valid state")
	ErrShouldBePermissionlessStaker        = errors.New("expected permissionless staker")
	ErrWrongTxType                         = errors.New("wrong transaction type")
	ErrInvalidID                           = errors.New("invalid ID")
	ErrProposedAddStakerTxAfterBanff       = errors.New("staker transaction proposed after Banff")
	ErrAdvanceTimeTxIssuedAfterBanff       = errors.New("AdvanceTimeTx issued after Banff")
	errShouldBeAutoRenewedStaker           = errors.New("expected auto renewed staker")
	errShouldUseRewardAutoRenewedValidator = errors.New("auto-renewed validators must be rewarded with RewardAutoRenewedValidatorTx")
	errInvalidTimestamp                    = errors.New("invalid timestamp")
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
	onCommitState state.Diff,
	onAbortState state.Diff,
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
	onCommitState state.Diff
	// [onAbortState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is aborted.
	onAbortState state.Diff
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
	}

	stakerTx, stakerToReward, err := verifyRewardTx(e.onCommitState, e.tx, tx)
	if err != nil {
		return err
	}

	// Invariant: A [txs.DelegatorTx] does not also implement the
	//            [txs.ValidatorTx] interface.
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case txs.ValidatorTx:
		if _, ok := uStakerTx.(*txs.AddAutoRenewedValidatorTx); ok {
			return errShouldUseRewardAutoRenewedValidator
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

	return e.decreaseAbortStateCurrentSupply(stakerToReward)
}

func (e *proposalTxExecutor) RewardAutoRenewedValidatorTx(tx *txs.RewardAutoRenewedValidatorTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	if tx.Timestamp != uint64(e.onCommitState.GetTimestamp().Unix()) {
		return errInvalidTimestamp
	}

	stakerTx, stakerToReward, err := verifyRewardTx(e.onCommitState, e.tx, tx)
	if err != nil {
		return err
	}

	addAutoRenewedValidatorTx, ok := stakerTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)
	if !ok {
		return errShouldBeAutoRenewedStaker
	}

	stakingInfo, err := e.onCommitState.GetStakingInfo(stakerToReward.SubnetID, stakerToReward.NodeID)
	if err != nil {
		return fmt.Errorf("failed to get staking info: %w", err)
	}

	// In onAbortState the staker is always removed.
	if err := e.onAbortState.DeleteCurrentValidator(stakerToReward); err != nil {
		return fmt.Errorf("deleting current validator from abort state: %w", err)
	}

	if err := e.decreaseAbortStateCurrentSupply(stakerToReward); err != nil {
		return err
	}

	if stakingInfo.Period > 0 {
		// Running auto-renewed staker: validator will continue to the next cycle.
		// On commit: restake rewards (based on AutoCompoundRewardShares) and start new cycle.
		// On abort: return stake + accrued rewards, forfeit current cycle's rewards.

		// Create UTXOs for onAbortState.
		if err = e.createUTXOsAutoRenewedValidatorOnAbort(addAutoRenewedValidatorTx, stakerToReward, stakingInfo); err != nil {
			return fmt.Errorf("failed to create UTXO auto-renewed validator on abort: %w", err)
		}

		// Set onCommitState.
		if err = e.setOnCommitStateAutoRenewedValidatorRestake(addAutoRenewedValidatorTx, stakerToReward, stakingInfo); err != nil {
			return err
		}

		// Early return because we don't need to do anything else.
		return nil
	}

	// Graceful exit: validator requested to stop (Period == 0).
	// Return stake + all rewards on both commit and abort paths.
	if err := e.createUTXOsAutoRenewedValidatorOnGracefulExit(addAutoRenewedValidatorTx, stakerToReward, stakingInfo); err != nil {
		return err
	}

	// Handle staker lifecycle.
	if err := e.onCommitState.DeleteCurrentValidator(stakerToReward); err != nil {
		return fmt.Errorf("deleting current validator from commit state: %w", err)
	}

	return nil
}

func (e *proposalTxExecutor) decreaseAbortStateCurrentSupply(stakerToReward *state.Staker) error {
	// If the reward is aborted, then the current supply should be decreased.
	currentSupply, err := e.onAbortState.GetCurrentSupply(stakerToReward.SubnetID)
	if err != nil {
		return err
	}
	newSupply, err := math.Sub(currentSupply, stakerToReward.PotentialReward)
	if err != nil {
		return err
	}
	e.onAbortState.SetCurrentSupply(stakerToReward.SubnetID, newSupply)
	return nil
}

func (e *proposalTxExecutor) rewardValidatorTx(uValidatorTx txs.ValidatorTx, validator *state.Staker) error {
	var (
		txID    = validator.TxID
		stake   = uValidatorTx.Stake()
		outputs = uValidatorTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	createUTXOsStakeOut(uValidatorTx, txID, e.onCommitState, e.onAbortState)

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

func (e *proposalTxExecutor) rewardDelegatorTx(uDelegatorTx txs.DelegatorTx, delegator *state.Staker) error {
	var (
		txID    = delegator.TxID
		stake   = uDelegatorTx.Stake()
		outputs = uDelegatorTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	createUTXOsStakeOut(uDelegatorTx, txID, e.onCommitState, e.onAbortState)

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

// createUTXOsAutoRenewedValidatorOnAbort creates UTXOs for an auto-renewed validator
// that failed to meet eligibility requirements.
//
// The validator receives:
//   - Staked tokens (returned via stake outputs)
//   - Accrued validation rewards (from previous successful periods)
//   - Accrued delegatee rewards + pending delegatee rewards
//
// The validator forfeits potential reward of the ended cycle.
func (e *proposalTxExecutor) createUTXOsAutoRenewedValidatorOnAbort(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	validator *state.Staker,
	stakingInfo state.StakingInfo,
) error {
	createUTXOsStakeOut(addAutoRenewedValidatorTx, validator.TxID, e.onAbortState)
	return e.createAbortRewardUTXOs(addAutoRenewedValidatorTx, stakingInfo)
}

// createAbortRewardUTXOs creates reward UTXOs on the abort state for an
// auto-renewed validator. This includes accrued validation rewards and
// all delegatee rewards (accrued + pending).
func (e *proposalTxExecutor) createAbortRewardUTXOs(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	stakingInfo state.StakingInfo,
) error {
	totalDelegateeRewards, err := math.Add(stakingInfo.DelegateeReward, stakingInfo.AccruedDelegateeRewards)
	if err != nil {
		return err
	}

	if _, err = e.createRewardsUTXOs(
		addAutoRenewedValidatorTx,
		stakingInfo.AccruedRewards,
		totalDelegateeRewards,
		e.onAbortState,
		uint32(len(e.tx.Unsigned.Outputs())),
	); err != nil {
		return err
	}

	return nil
}

func (e *proposalTxExecutor) createRewardsUTXOs(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	validationRewards uint64,
	delegateeRewards uint64,
	chainState state.Diff,
	outputIndexOffset uint32,
) (uint32, error) {
	avaxAsset := avax.Asset{ID: e.backend.Ctx.AVAXAssetID}

	// Create UTXOs for validation rewards.
	if validationRewards > 0 {
		utxo, err := e.newUTXO(validationRewards, addAutoRenewedValidatorTx.ValidationRewardsOwner(), e.tx.ID(), outputIndexOffset, avaxAsset)
		if err != nil {
			return 0, err
		}
		chainState.AddUTXO(utxo)
		chainState.AddRewardUTXO(e.tx.ID(), utxo)
		outputIndexOffset++
	}

	// Create UTXOs for delegatee rewards.
	if delegateeRewards > 0 {
		utxo, err := e.newUTXO(delegateeRewards, addAutoRenewedValidatorTx.DelegationRewardsOwner(), e.tx.ID(), outputIndexOffset, avaxAsset)
		if err != nil {
			return 0, err
		}
		chainState.AddUTXO(utxo)
		chainState.AddRewardUTXO(e.tx.ID(), utxo)
		outputIndexOffset++
	}

	return outputIndexOffset, nil
}

// setOnCommitStateAutoRenewedValidatorRestake processes rewards for a running
// auto-renewed validator based on their AutoCompoundRewardShares configuration.
//
// The function:
//  1. Splits rewards (validation + delegatee) into restaking and withdrawing portions
//  2. Creates UTXOs for the withdrawn portion
//  3. Increases validator weight and accrued rewards by the restaking portion
//  4. If restaking would exceed MaxValidatorStake, the excess is withdrawn
//  5. Updates the validator state
func (e *proposalTxExecutor) setOnCommitStateAutoRenewedValidatorRestake(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	validator *state.Staker,
	stakingInfo state.StakingInfo,
) error {
	restakingRewards, withdrawingRewards := reward.Split(validator.PotentialReward, stakingInfo.AutoCompoundRewardShares)
	restakingDelegateeRewards, withdrawingDelegateeRewards := reward.Split(stakingInfo.DelegateeReward, stakingInfo.AutoCompoundRewardShares)

	outputIndexOffset, err := e.createRewardsUTXOs(addAutoRenewedValidatorTx, withdrawingRewards, withdrawingDelegateeRewards, e.onCommitState, uint32(len(e.tx.Unsigned.Outputs())))
	if err != nil {
		return err
	}

	newAccruedRewards := stakingInfo.AccruedRewards
	newWeight := validator.Weight
	if restakingRewards > 0 {
		newAccruedRewards, err = math.Add(stakingInfo.AccruedRewards, restakingRewards)
		if err != nil {
			return err
		}

		newWeight, err = math.Add(validator.Weight, restakingRewards)
		if err != nil {
			return err
		}
	}

	newAccruedDelegateeRewards := stakingInfo.AccruedDelegateeRewards
	if restakingDelegateeRewards > 0 {
		newAccruedDelegateeRewards, err = math.Add(stakingInfo.AccruedDelegateeRewards, restakingDelegateeRewards)
		if err != nil {
			return err
		}

		newWeight, err = math.Add(newWeight, restakingDelegateeRewards)
		if err != nil {
			return err
		}
	}

	if newWeight > e.backend.Config.MaxValidatorStake {
		excessValidationRewards, excessDelegateeRewards, err := e.createOverflowUTXOs(addAutoRenewedValidatorTx, newWeight, restakingDelegateeRewards, restakingRewards, validator.Weight, outputIndexOffset)
		if err != nil {
			return err
		}

		newAccruedRewards, err = math.Sub(newAccruedRewards, excessValidationRewards)
		if err != nil {
			return err
		}

		newWeight, err = math.Sub(newWeight, excessValidationRewards)
		if err != nil {
			return err
		}

		if excessDelegateeRewards > 0 {
			newAccruedDelegateeRewards, err = math.Sub(newAccruedDelegateeRewards, excessDelegateeRewards)
			if err != nil {
				return err
			}

			newWeight, err = math.Sub(newWeight, excessDelegateeRewards)
			if err != nil {
				return err
			}
		}

		// newWeight is equal to e.backend.Config.MaxValidatorStake.
	}

	rewards, err := GetRewardsCalculator(e.backend, e.onCommitState, validator.SubnetID)
	if err != nil {
		return err
	}

	currentSupply, err := e.onCommitState.GetCurrentSupply(validator.SubnetID)
	if err != nil {
		return err
	}

	duration := time.Duration(stakingInfo.Period) * time.Second
	potentialReward := rewards.Calculate(
		duration,
		newWeight,
		currentSupply,
	)

	newCurrentSupply, err := math.Add(currentSupply, potentialReward)
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
	stakingInfo.AccruedRewards = newAccruedRewards
	stakingInfo.AccruedDelegateeRewards = newAccruedDelegateeRewards

	if err := e.onCommitState.SetStakingInfo(validator.SubnetID, validator.NodeID, stakingInfo); err != nil {
		return fmt.Errorf("setting staking info for validator: %w", err)
	}

	return nil
}

// createUTXOsAutoRenewedValidatorOnGracefulExit creates UTXOs for an auto-renewed
// validator that is stopping gracefully.
//
// On commit (validator eligible for rewards):
//   - Staked tokens (returned via stake outputs)
//   - All validation rewards (PotentialReward + AccruedRewards)
//   - All delegatee rewards (AccruedDelegateeRewards + pending delegatee rewards)
//
// On abort (validator not eligible for rewards):
//   - Staked tokens (returned via stake outputs)
//   - Only accrued validation rewards (AccruedRewards)
//   - All delegatee rewards (AccruedDelegateeRewards + pending delegatee rewards)
func (e *proposalTxExecutor) createUTXOsAutoRenewedValidatorOnGracefulExit(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	validator *state.Staker,
	stakingInfo state.StakingInfo,
) error {
	createUTXOsStakeOut(addAutoRenewedValidatorTx, validator.TxID, e.onCommitState, e.onAbortState)

	if err := e.createAbortRewardUTXOs(addAutoRenewedValidatorTx, stakingInfo); err != nil {
		return err
	}

	totalRewards, err := math.Add(validator.PotentialReward, stakingInfo.AccruedRewards)
	if err != nil {
		return err
	}

	totalDelegateeRewards, err := math.Add(stakingInfo.DelegateeReward, stakingInfo.AccruedDelegateeRewards)
	if err != nil {
		return err
	}

	if _, err = e.createRewardsUTXOs(addAutoRenewedValidatorTx, totalRewards, totalDelegateeRewards, e.onCommitState, uint32(len(e.tx.Unsigned.Outputs()))); err != nil {
		return err
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

// createOverflowUTXOs creates UTXOs for the excess rewards
// that cannot be restaked because doing so would exceed MaxValidatorStake.
//
// When an auto-renewed validator's restaked rewards would push their weight above
// MaxValidatorStake, the excess is withdrawn.
//
// Returns the excess validation and delegatee rewards that were withdrawn.
func (e *proposalTxExecutor) createOverflowUTXOs(
	addAutoRenewedValidatorTx *txs.AddAutoRenewedValidatorTx,
	newWeight uint64,
	delegateeReward uint64,
	rewards uint64,
	oldWeight uint64,
	outputIndexOffset uint32,
) (excessValidationRewards uint64, excessDelegateeRewards uint64, err error) {
	totalRestakingRewards, err := math.Sub(newWeight, oldWeight)
	if err != nil {
		return 0, 0, err
	}

	// Calculate how much room we have to grow before hitting max stake.
	restakingAvailability, err := math.Sub(e.backend.Config.MaxValidatorStake, oldWeight)
	if err != nil {
		return 0, 0, err
	}

	// Distribute available space proportionally between validation and delegatee rewards.
	restakingValidationReward, err := math.MulDiv(rewards, restakingAvailability, totalRestakingRewards)
	if err != nil {
		return 0, 0, err
	}

	restakingDelegateeReward, err := math.Sub(restakingAvailability, restakingValidationReward)
	if err != nil {
		return 0, 0, err
	}

	// rewards >= restakingValidationReward, but using math package as a defensive check.
	excessValidationReward, err := math.Sub(rewards, restakingValidationReward)
	if err != nil {
		return 0, 0, err
	}

	// delegateeReward >= restakingDelegateeReward, but using math package as a defensive check.
	excessDelegateeReward, err := math.Sub(delegateeReward, restakingDelegateeReward)
	if err != nil {
		return 0, 0, err
	}

	if _, err = e.createRewardsUTXOs(addAutoRenewedValidatorTx, excessValidationReward, excessDelegateeReward, e.onCommitState, outputIndexOffset); err != nil {
		return 0, 0, err
	}

	return excessValidationReward, excessDelegateeReward, nil
}

// createUTXOsStakeOut creates UTXOs to return a staker's staked tokens.
// The UTXOs are added to all provided state diffs.
func createUTXOsStakeOut(stakerTx txs.PermissionlessStaker, txID ids.ID, states ...state.Diff) {
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
