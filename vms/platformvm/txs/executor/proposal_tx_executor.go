// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
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

	ErrRemoveStakerTooEarly          = errors.New("attempting to remove staker before their end time")
	ErrRemoveWrongStaker             = errors.New("attempting to remove wrong staker")
	ErrInvalidState                  = errors.New("generated output isn't valid state")
	ErrShouldBePermissionlessStaker  = errors.New("expected permissionless staker")
	ErrShouldBeContinuousStaker      = errors.New("expected continuous staker")
	ErrShouldBeFixedStaker           = errors.New("expected fixed staker")
	ErrInvalidTimestamp              = errors.New("invalid timestamp")
	ErrWrongTxType                   = errors.New("wrong transaction type")
	ErrInvalidID                     = errors.New("invalid ID")
	ErrProposedAddStakerTxAfterBanff = errors.New("staker transaction proposed after Banff")
	ErrAdvanceTimeTxIssuedAfterBanff = errors.New("AdvanceTimeTx issued after Banff")
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

func (*proposalTxExecutor) AddContinuousValidatorTx(*txs.AddContinuousValidatorTx) error {
	return ErrWrongTxType
}

func (*proposalTxExecutor) SetAutoRestakeConfigTx(*txs.SetAutoRestakeConfigTx) error {
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

	currentStakerIterator, err := e.onCommitState.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	if !currentStakerIterator.Next() {
		return fmt.Errorf("failed to get next staker to remove: %w", database.ErrNotFound)
	}
	stakerToReward := currentStakerIterator.Value()
	currentStakerIterator.Release()

	if stakerToReward.TxID != tx.TxID {
		return fmt.Errorf(
			"%w: %s != %s",
			ErrRemoveWrongStaker,
			stakerToReward.TxID,
			tx.TxID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentChainTime := e.onCommitState.GetTimestamp()
	if !stakerToReward.EndTime.Equal(currentChainTime) {
		return fmt.Errorf(
			"%w: TxID = %s with %s < %s",
			ErrRemoveStakerTooEarly,
			tx.TxID,
			currentChainTime,
			stakerToReward.EndTime,
		)
	}

	stakerTx, _, err := e.onCommitState.GetTx(stakerToReward.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	if _, ok := stakerTx.Unsigned.(txs.FixedStaker); !ok {
		return ErrShouldBeFixedStaker
	}

	// Invariant: A [txs.DelegatorTx] does not also implement the
	//            [txs.ValidatorTx] interface.
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case txs.ValidatorTx:
		if err := e.rewardValidatorTx(uStakerTx, stakerToReward); err != nil {
			return err
		}

		// Handle staker lifecycle.
		e.onCommitState.DeleteCurrentValidator(stakerToReward)
		e.onAbortState.DeleteCurrentValidator(stakerToReward)
	case txs.DelegatorTx:
		if err := e.rewardDelegatorTx(uStakerTx, stakerToReward); err != nil {
			return err
		}

		// Handle staker lifecycle.
		e.onCommitState.DeleteCurrentDelegator(stakerToReward)
		e.onAbortState.DeleteCurrentDelegator(stakerToReward)
	default:
		// Invariant: Permissioned stakers are removed by the advancement of
		//            time and the current chain timestamp is == this staker's
		//            EndTime. This means only permissionless stakers should be
		//            left in the staker set.
		return ErrShouldBePermissionlessStaker
	}

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

func (e *proposalTxExecutor) RewardContinuousValidatorTx(tx *txs.RewardContinuousValidatorTx) error {
	currentChainTime := e.onCommitState.GetTimestamp()
	if !time.Unix(int64(tx.Timestamp), 0).Equal(e.onCommitState.GetTimestamp()) {
		return ErrInvalidTimestamp
	}

	currentStakerIterator, err := e.onCommitState.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	defer currentStakerIterator.Release()

	if !currentStakerIterator.Next() {
		return fmt.Errorf("failed to get next staker to remove: %w", database.ErrNotFound)
	}

	stakerToReward := currentStakerIterator.Value()
	if stakerToReward.TxID != tx.TxID {
		return fmt.Errorf(
			"%w: %s != %s",
			ErrRemoveWrongStaker,
			stakerToReward.TxID,
			tx.TxID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	if !stakerToReward.EndTime.Equal(currentChainTime) {
		return fmt.Errorf(
			"%w: TxID = %s with %s < %s",
			ErrRemoveStakerTooEarly,
			tx.TxID,
			currentChainTime,
			stakerToReward.EndTime,
		)
	}

	stakerTx, _, err := e.onCommitState.GetTx(stakerToReward.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	validatorTx, ok := stakerTx.Unsigned.(txs.ValidatorTx)
	if !ok {
		return ErrShouldBePermissionlessStaker
	}

	continuousStaker, ok := stakerTx.Unsigned.(txs.ContinuousStaker)
	if !ok {
		return ErrShouldBeContinuousStaker
	}

	// in [onAbortState] the staker should be removed despite the continuation state.
	e.onAbortState.DeleteCurrentValidator(stakerToReward)

	if stakerToReward.ContinuationPeriod > 0 {
		// Running continuous staker: validator will continue to the next cycle.
		// On commit: restake rewards (based on AutoRestakeShares) and start new cycle.
		// On abort: return stake + accrued rewards, forfeit current cycle's rewards.

		// Create UTXOs for [onAbortState].
		if err = e.createUTXOsContinuousValidatorOnAbort(validatorTx, stakerToReward); err != nil {
			return fmt.Errorf("failed to create UTXO continuous validator on abort: %w", err)
		}

		// Set [onCommitState].
		if err = e.setOnCommitStateContinuousValidatorRestake(validatorTx, stakerToReward); err != nil {
			return err
		}

		// Early return because we don't need to do anything else.
		return nil
	}

	// Graceful exit: validator requested to stop (ContinuationPeriod == 0).
	// Return stake + all rewards on both commit and abort paths.
	if err := e.createUTXOsContinuousValidatorOnGracefulExit(continuousStaker.(txs.ValidatorTx), stakerToReward); err != nil {
		return err
	}

	// Handle staker lifecycle.
	e.onCommitState.DeleteCurrentValidator(stakerToReward)

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

	// Refund the stake only when validator is about to leave
	// the staking set
	for i, out := range stake {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(outputs) + i),
			},
			Asset: out.Asset,
			Out:   out.Output(),
		}
		e.onCommitState.AddUTXO(utxo)
		e.onAbortState.AddUTXO(utxo)
	}

	utxosOffset := 0

	// Provide the reward here
	reward := validator.PotentialReward
	if reward > 0 {
		validationRewardsOwner := uValidatorTx.ValidationRewardsOwner()
		outIntf, err := e.backend.Fx.CreateOutput(reward, validationRewardsOwner)
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}

		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(outputs) + len(stake)),
			},
			Asset: stakeAsset,
			Out:   out,
		}
		e.onCommitState.AddUTXO(utxo)
		e.onCommitState.AddRewardUTXO(txID, utxo)

		utxosOffset++
	}

	// Provide the accrued delegatee rewards from successful delegations here.
	if validator.DelegateeReward == 0 {
		return nil
	}

	delegationRewardsOwner := uValidatorTx.DelegationRewardsOwner()
	outIntf, err := e.backend.Fx.CreateOutput(validator.DelegateeReward, delegationRewardsOwner)
	if err != nil {
		return fmt.Errorf("failed to create output: %w", err)
	}
	out, ok := outIntf.(verify.State)
	if !ok {
		return ErrInvalidState
	}

	onCommitUtxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: uint32(len(outputs) + len(stake) + utxosOffset),
		},
		Asset: stakeAsset,
		Out:   out,
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
		Out:   out,
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

	// Refund the stake only when delegator is about to leave
	// the staking set
	for i, out := range stake {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(outputs) + i),
			},
			Asset: out.Asset,
			Out:   out.Output(),
		}
		e.onCommitState.AddUTXO(utxo)
		e.onAbortState.AddUTXO(utxo)
	}

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
		rewardsOwner := uDelegatorTx.RewardsOwner()
		outIntf, err := e.backend.Fx.CreateOutput(reward, rewardsOwner)
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(outputs) + len(stake)),
			},
			Asset: stakeAsset,
			Out:   out,
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
		// Invariant: The rewards calculator can never return a
		//            [potentialReward] that would overflow the
		//            accumulated rewards.
		newDelegateeReward, err := math.Add(validator.DelegateeReward, delegateeReward)
		if err != nil {
			return err
		}

		// Make a copy to avoid mutating the shared parent state object.
		// The commit and abort states share the same parent, so modifying
		// the validator directly would affect both states.
		validatorCopy := *validator

		// For any validators starting after [CortinaTime], we defer rewarding the
		// [reward] until their staking period is over.
		validatorCopy.DelegateeReward = newDelegateeReward
		if err := e.onCommitState.UpdateCurrentValidator(&validatorCopy); err != nil {
			return err
		}
	} else {
		// For any validators who started prior to [CortinaTime], we issue the
		// [delegateeReward] immediately.
		delegationRewardsOwner := vdrTx.DelegationRewardsOwner()
		outIntf, err := e.backend.Fx.CreateOutput(delegateeReward, delegationRewardsOwner)
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(outputs) + len(stake) + utxosOffset),
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onCommitState.AddUTXO(utxo)
		e.onCommitState.AddRewardUTXO(txID, utxo)
	}
	return nil
}

// createUTXOsContinuousValidatorOnAbort creates UTXOs for a continuous validator
// that failed to meet eligibility requirements.
//
// The validator receives:
//   - Staked tokens (returned via stake outputs)
//   - Accrued validation rewards (from previous successful periods)
//   - Accrued delegatee rewards + pending delegatee rewards
//
// The validator forfeits potential reward of the ended cycle.
func (e *proposalTxExecutor) createUTXOsContinuousValidatorOnAbort(uValidatorTx txs.ValidatorTx, validator *state.Staker) error {
	txID := validator.TxID
	stake := uValidatorTx.Stake()
	stakeAsset := stake[0].Asset

	createUTXOsStakeOut(uValidatorTx, validator, e.onAbortState)

	outputIndexOffset := uint32(len(e.tx.Unsigned.Outputs()))
	// Create UTXOs for accrued rewards.
	if validator.AccruedRewards > 0 {
		outIntf, err := e.backend.Fx.CreateOutput(validator.AccruedRewards, uValidatorTx.ValidationRewardsOwner())
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}

		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        e.tx.ID(),
				OutputIndex: outputIndexOffset,
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onAbortState.AddUTXO(utxo)
		e.onAbortState.AddRewardUTXO(txID, utxo)

		outputIndexOffset++
	}

	// Create UTXOs for accrued delegatee rewards.
	totalDelegateeRewards, err := math.Add(validator.DelegateeReward, validator.AccruedDelegateeRewards)
	if err != nil {
		return err
	}
	if totalDelegateeRewards > 0 {
		outIntf, err := e.backend.Fx.CreateOutput(totalDelegateeRewards, uValidatorTx.DelegationRewardsOwner())
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}

		onAbortUtxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        e.tx.ID(),
				OutputIndex: outputIndexOffset,
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onAbortState.AddUTXO(onAbortUtxo)
		e.onAbortState.AddRewardUTXO(txID, onAbortUtxo)
	}

	return nil
}

// setOnCommitStateContinuousValidatorRestake processes rewards for a running
// continuous validator based on their AutoRestakeShares configuration.
//
// The function:
//  1. Splits rewards (validation + delegatee) into restaking and withdrawing portions
//  2. Creates UTXOs for the withdrawn portion
//  3. Increases validator weight and accrued rewards by the restaking portion
//  4. If restaking would exceed MaxValidatorStake, the excess is withdrawn
//  5. Updates the validator state
func (e *proposalTxExecutor) setOnCommitStateContinuousValidatorRestake(validatorTx txs.ValidatorTx, validator *state.Staker) error {
	var err error

	outputIndexOffset := uint32(len(e.tx.Unsigned.Outputs()))
	asset := validatorTx.Stake()[0].Asset

	restakingRewards, withdrawingRewards := reward.Split(validator.PotentialReward, validator.AutoRestakeShares)
	restakingDelegateeRewards, withdrawingDelegateeRewards := reward.Split(validator.DelegateeReward, validator.AutoRestakeShares)

	if withdrawingRewards > 0 {
		outIntf, err := e.backend.Fx.CreateOutput(withdrawingRewards, validatorTx.ValidationRewardsOwner())
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}

		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}

		validationRewardUTXO := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        e.tx.ID(),
				OutputIndex: outputIndexOffset,
			},
			Asset: asset,
			Out:   out,
		}
		e.onCommitState.AddUTXO(validationRewardUTXO)
		e.onCommitState.AddRewardUTXO(e.tx.ID(), validationRewardUTXO)

		outputIndexOffset++
	}

	if withdrawingDelegateeRewards > 0 {
		outIntf, err := e.backend.Fx.CreateOutput(withdrawingDelegateeRewards, validatorTx.DelegationRewardsOwner())
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}

		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}

		delegateeRewardUTXO := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        e.tx.ID(),
				OutputIndex: outputIndexOffset,
			},
			Asset: asset,
			Out:   out,
		}
		e.onCommitState.AddUTXO(delegateeRewardUTXO)
		e.onCommitState.AddRewardUTXO(e.tx.ID(), delegateeRewardUTXO)

		outputIndexOffset++
	}

	newAccruedRewards := validator.AccruedRewards
	newWeight := validator.Weight
	if restakingRewards > 0 {
		newAccruedRewards, err = math.Add(validator.AccruedRewards, restakingRewards)
		if err != nil {
			return err
		}

		newWeight, err = math.Add(validator.Weight, restakingRewards)
		if err != nil {
			return err
		}
	}

	newAccruedDelegateeRewards := validator.AccruedDelegateeRewards
	if restakingDelegateeRewards > 0 {
		newAccruedDelegateeRewards, err = math.Add(validator.AccruedDelegateeRewards, restakingDelegateeRewards)
		if err != nil {
			return err
		}

		newWeight, err = math.Add(newWeight, restakingDelegateeRewards)
		if err != nil {
			return err
		}
	}

	if newWeight > e.backend.Config.MaxValidatorStake {
		excessValidationRewards, excessDelegateeRewards, err := e.createOverflowUTXOs(
			validatorTx,
			newWeight,
			restakingDelegateeRewards,
			restakingRewards,
			validator.Weight,
			outputIndexOffset,
		)
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

		// [newWeight] is equal to [e.backend.Config.MaxValidatorStake].
	}

	rewards, err := GetRewardsCalculator(e.backend, e.onCommitState, validator.SubnetID)
	if err != nil {
		return err
	}

	currentSupply, err := e.onCommitState.GetCurrentSupply(validator.SubnetID)
	if err != nil {
		return err
	}

	potentialReward := rewards.Calculate(
		validator.ContinuationPeriod,
		newWeight,
		currentSupply,
	)

	newCurrentSupply, err := math.Add(currentSupply, potentialReward)
	if err != nil {
		return err
	}

	e.onCommitState.SetCurrentSupply(validator.SubnetID, newCurrentSupply)

	if err = e.onCommitState.ResetContinuousValidatorCycle(
		validator,
		newWeight,
		potentialReward,
		newAccruedRewards,
		newAccruedDelegateeRewards,
	); err != nil {
		return err
	}

	return nil
}

// createUTXOsContinuousValidatorOnGracefulExit creates UTXOs for a continuous
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
func (e *proposalTxExecutor) createUTXOsContinuousValidatorOnGracefulExit(uValidatorTx txs.ValidatorTx, validator *state.Staker) error {
	txID := validator.TxID
	stake := uValidatorTx.Stake()
	stakeAsset := stake[0].Asset

	createUTXOsStakeOut(uValidatorTx, validator, e.onCommitState, e.onAbortState)

	// Create UTXOs for [onAbortState].
	onAbortUTXOsOffset := uint32(len(e.tx.Unsigned.Outputs()))
	if validator.AccruedRewards > 0 {
		outIntf, err := e.backend.Fx.CreateOutput(validator.AccruedRewards, uValidatorTx.ValidationRewardsOwner())
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}

		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        e.tx.ID(),
				OutputIndex: onAbortUTXOsOffset,
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onAbortState.AddUTXO(utxo)
		e.onAbortState.AddRewardUTXO(txID, utxo)

		onAbortUTXOsOffset++
	}

	// Create UTXOs for rewards for [onCommitState].
	onCommitUTXOsOffset := uint32(len(e.tx.Unsigned.Outputs()))
	totalRewards, err := math.Add(validator.PotentialReward, validator.AccruedRewards)
	if err != nil {
		return err
	}
	if totalRewards > 0 {
		outIntf, err := e.backend.Fx.CreateOutput(totalRewards, uValidatorTx.ValidationRewardsOwner())
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}

		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        e.tx.ID(),
				OutputIndex: onCommitUTXOsOffset,
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onCommitState.AddUTXO(utxo)
		e.onCommitState.AddRewardUTXO(txID, utxo)

		onCommitUTXOsOffset++
	}

	// Provide the delegatee rewards from successful delegations here.
	totalDelegateeRewards, err := math.Add(validator.DelegateeReward, validator.AccruedDelegateeRewards)
	if err != nil {
		return err
	}
	if totalDelegateeRewards == 0 {
		return nil
	}

	outIntf, err := e.backend.Fx.CreateOutput(totalDelegateeRewards, uValidatorTx.DelegationRewardsOwner())
	if err != nil {
		return fmt.Errorf("failed to create output: %w", err)
	}
	out, ok := outIntf.(verify.State)
	if !ok {
		return ErrInvalidState
	}

	onCommitUtxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        e.tx.ID(),
			OutputIndex: onCommitUTXOsOffset,
		},
		Asset: stakeAsset,
		Out:   out,
	}
	e.onCommitState.AddUTXO(onCommitUtxo)
	e.onCommitState.AddRewardUTXO(txID, onCommitUtxo)

	onAbortUtxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        e.tx.ID(),
			OutputIndex: onAbortUTXOsOffset,
		},
		Asset: stakeAsset,
		Out:   out,
	}
	e.onAbortState.AddUTXO(onAbortUtxo)
	e.onAbortState.AddRewardUTXO(txID, onAbortUtxo)

	return nil
}

// createOverflowUTXOs creates UTXOs for the excess rewards
// that cannot be restaked because doing so would exceed [MaxValidatorStake].
//
// When a continuous validator's restaked rewards would push their weight above
// [MaxValidatorStake], the excess is withdrawn.
//
// Returns the excess validation and delegatee rewards that were withdrawn.
func (e *proposalTxExecutor) createOverflowUTXOs(
	uValidatorTx txs.ValidatorTx,
	newWeight uint64,
	delegateeReward uint64,
	rewards uint64,
	oldWeight uint64,
	outputIndexOffset uint32,
) (excessValidationRewards uint64, excessDelegateeRewards uint64, err error) {
	asset := uValidatorTx.Stake()[0].Asset

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
	restakableValidationReward, err := reward.ProportionalAmount(rewards, restakingAvailability, totalRestakingRewards)
	if err != nil {
		return 0, 0, err
	}

	// rewards >= restakableValidationReward, but using math package as a defensive check.
	excessValidationReward, err := math.Sub(rewards, restakableValidationReward)
	if err != nil {
		return 0, 0, err
	}

	restakableDelegateeReward, err := reward.ProportionalAmount(delegateeReward, restakingAvailability, totalRestakingRewards)
	if err != nil {
		return 0, 0, err
	}

	// delegateeReward >= restakableDelegateeReward, but using math package as a defensive check.
	excessDelegateeReward, err := math.Sub(delegateeReward, restakableDelegateeReward)
	if err != nil {
		return 0, 0, err
	}

	if excessValidationReward > 0 {
		outIntf, err := e.backend.Fx.CreateOutput(excessValidationReward, uValidatorTx.ValidationRewardsOwner())
		if err != nil {
			return 0, 0, fmt.Errorf("failed to create output: %w", err)
		}

		out, ok := outIntf.(verify.State)
		if !ok {
			return 0, 0, ErrInvalidState
		}

		excessValidationRewardUTXO := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        e.tx.ID(),
				OutputIndex: outputIndexOffset,
			},
			Asset: asset,
			Out:   out,
		}
		e.onCommitState.AddUTXO(excessValidationRewardUTXO)
		e.onCommitState.AddRewardUTXO(e.tx.ID(), excessValidationRewardUTXO)

		outputIndexOffset++
	}

	if excessDelegateeReward > 0 {
		outIntf, err := e.backend.Fx.CreateOutput(excessDelegateeReward, uValidatorTx.DelegationRewardsOwner())
		if err != nil {
			return 0, 0, fmt.Errorf("failed to create output: %w", err)
		}

		out, ok := outIntf.(verify.State)
		if !ok {
			return 0, 0, ErrInvalidState
		}

		excessDelegateeRewardUTXO := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        e.tx.ID(),
				OutputIndex: outputIndexOffset,
			},
			Asset: asset,
			Out:   out,
		}
		e.onCommitState.AddUTXO(excessDelegateeRewardUTXO)
		e.onCommitState.AddRewardUTXO(e.tx.ID(), excessDelegateeRewardUTXO)
	}

	return excessValidationReward, excessDelegateeReward, nil
}

// createUTXOsStakeOut creates UTXOs to return a validator's staked tokens.
// The UTXOs are added to all provided state diffs.
func createUTXOsStakeOut(uValidatorTx txs.ValidatorTx, validator *state.Staker, states ...state.Diff) {
	for i, out := range uValidatorTx.Stake() {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        validator.TxID,
				OutputIndex: uint32(len(uValidatorTx.Outputs()) + i),
			},
			Asset: out.Asset,
			Out:   out.Output(),
		}

		for _, s := range states {
			s.AddUTXO(utxo)
		}
	}
}
