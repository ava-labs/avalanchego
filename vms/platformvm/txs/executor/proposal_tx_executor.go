// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	safemath "github.com/ava-labs/avalanchego/utils/math"
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
	newSupply, err := safemath.Sub(currentSupply, stakerToReward.PotentialReward)
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
		// Running continuous staker

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

	newSupply, err := safemath.Sub(currentSupply, stakerToReward.PotentialReward)
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
	delegateeReward, err := e.onCommitState.GetDelegateeReward(
		validator.SubnetID,
		validator.NodeID,
	)
	if err != nil {
		return fmt.Errorf("failed to fetch accrued delegatee rewards: %w", err)
	}

	if delegateeReward == 0 {
		return nil
	}

	delegationRewardsOwner := uValidatorTx.DelegationRewardsOwner()
	outIntf, err := e.backend.Fx.CreateOutput(delegateeReward, delegationRewardsOwner)
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
		previousDelegateeReward, err := e.onCommitState.GetDelegateeReward(
			validator.SubnetID,
			validator.NodeID,
		)
		if err != nil {
			return fmt.Errorf("failed to get delegatee reward: %w", err)
		}

		// Invariant: The rewards calculator can never return a
		//            [potentialReward] that would overflow the
		//            accumulated rewards.
		newDelegateeReward := previousDelegateeReward + delegateeReward

		// For any validators starting after [CortinaTime], we defer rewarding the
		// [reward] until their staking period is over.
		err = e.onCommitState.SetDelegateeReward(
			validator.SubnetID,
			validator.NodeID,
			newDelegateeReward,
		)
		if err != nil {
			return fmt.Errorf("failed to update delegatee reward: %w", err)
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

// creates the UTXOs for a running continuous validator which is not eligible for rewards.
func (e *proposalTxExecutor) setOnCommitStateContinuousValidatorRestake(validatorTx txs.ValidatorTx, validator *state.Staker) error {
	delegateeReward, err := e.onCommitState.GetDelegateeReward(
		validator.SubnetID,
		validator.NodeID,
	)
	if err != nil {
		return fmt.Errorf("failed to fetch delegatee rewards: %w", err)
	}

	newAccruedRewards, err := safemath.Add(validator.AccruedRewards, validator.PotentialReward)
	if err != nil {
		return err
	}

	newWeight, err := safemath.Add(validator.Weight, validator.PotentialReward)
	if err != nil {
		return err
	}

	newAccruedDelegateeRewards := validator.AccruedDelegateeRewards
	if delegateeReward > 0 {
		newAccruedDelegateeRewards, err = safemath.Add(validator.AccruedDelegateeRewards, delegateeReward)
		if err != nil {
			return err
		}

		newWeight, err = safemath.Add(newWeight, delegateeReward)
		if err != nil {
			return err
		}
	}

	// todo: can potentialrewards be 0 in any situation?
	if newWeight > e.backend.Config.MaxValidatorStake {
		excessValidationRewards, excessDelegateeRewards, err := e.createOverflowUTXOsContinuousValidator(
			validatorTx,
			newWeight,
			delegateeReward,
			validator,
		)
		if err != nil {
			return err
		}

		newAccruedRewards, err = safemath.Sub(newAccruedRewards, excessValidationRewards)
		if err != nil {
			return err
		}

		newWeight, err = safemath.Sub(newWeight, excessValidationRewards)
		if err != nil {
			return err
		}

		if excessDelegateeRewards > 0 {
			newAccruedDelegateeRewards, err = safemath.Sub(newAccruedDelegateeRewards, excessDelegateeRewards)
			if err != nil {
				return err
			}

			newWeight, err = safemath.Sub(newWeight, excessDelegateeRewards)
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

	newCurrentSupply, err := safemath.Add(currentSupply, potentialReward)
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

func (e *proposalTxExecutor) refundValidatorStake(txID ids.ID, stakeOuts []*avax.TransferableOutput) {
	for i, out := range stakeOuts {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(stakeOuts)) + uint32(i),
			},
			Asset: out.Asset,
			Out:   out.Output(),
		}
		e.onCommitState.AddUTXO(utxo)
		e.onAbortState.AddUTXO(utxo)
	}
}

// creates the UTXOs for a running continuous validator which is not eligible for rewards.
func (e *proposalTxExecutor) createUTXOsContinuousValidatorOnAbort(uValidatorTx txs.ValidatorTx, validator *state.Staker) error {
	txID := validator.TxID
	stake := uValidatorTx.Stake()
	stakeAsset := stake[0].Asset

	e.refundValidatorStake(txID, stake)

	outputIndexOffset := uint32(len(uValidatorTx.Outputs())) + uint32(len(stake))

	// Create UTXOs for accrued rewards for [onAbortState].
	utxosOffset := 0
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
				TxID:        txID,
				OutputIndex: outputIndexOffset,
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onAbortState.AddUTXO(utxo)
		e.onAbortState.AddRewardUTXO(txID, utxo)

		utxosOffset++
	}

	// Provide the delegatee rewards from successful delegations here.
	delegateeReward, err := e.onAbortState.GetDelegateeReward(
		validator.SubnetID,
		validator.NodeID,
	)
	if err != nil {
		return fmt.Errorf("failed to fetch accrued delegatee rewards: %w", err)
	}

	totalDelegateeRewards := delegateeReward + validator.AccruedDelegateeRewards
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
				TxID:        txID,
				OutputIndex: outputIndexOffset + uint32(len(stake)+utxosOffset),
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onAbortState.AddUTXO(onAbortUtxo)
		e.onAbortState.AddRewardUTXO(txID, onAbortUtxo)
	}

	return nil
}

// creates the UTXOs for a gracefully stopped continuous validator.
func (e *proposalTxExecutor) createUTXOsContinuousValidatorOnGracefulExit(uValidatorTx txs.ValidatorTx, validator *state.Staker) error {
	// todo: add test for a successful cycle and a failed one after. Check rewards amount!
	txID := validator.TxID
	stake := uValidatorTx.Stake()
	stakeAsset := stake[0].Asset

	e.refundValidatorStake(txID, stake)

	outputIndexOffset := uint32(len(uValidatorTx.Outputs())) + uint32(len(stake))

	// Create UTXOs for accrued rewards for [onAbortState].
	onAbortUTXOsOffset := 0
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
				TxID:        txID,
				OutputIndex: outputIndexOffset,
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onAbortState.AddUTXO(utxo)
		e.onAbortState.AddRewardUTXO(txID, utxo)

		onAbortUTXOsOffset++
	}

	// Create UTXOs for rewards for [onCommitState].
	onCommitUTXOsOffset := 0
	totalRewards := validator.PotentialReward + validator.AccruedRewards
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
				TxID:        txID,
				OutputIndex: outputIndexOffset,
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.onCommitState.AddUTXO(utxo)
		e.onCommitState.AddRewardUTXO(txID, utxo)

		onCommitUTXOsOffset++
	}

	// Provide the delegatee rewards from successful delegations here.
	delegateeReward, err := e.onCommitState.GetDelegateeReward(
		validator.SubnetID,
		validator.NodeID,
	)
	if err != nil {
		return fmt.Errorf("failed to fetch accrued delegatee rewards: %w", err)
	}

	totalDelegateeRewards := delegateeReward + validator.AccruedDelegateeRewards
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
			TxID:        txID,
			OutputIndex: outputIndexOffset + uint32(len(stake)+onCommitUTXOsOffset),
		},
		Asset: stakeAsset,
		Out:   out,
	}
	e.onCommitState.AddUTXO(onCommitUtxo)
	e.onCommitState.AddRewardUTXO(txID, onCommitUtxo)

	onAbortUtxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: outputIndexOffset + uint32(len(stake)+onAbortUTXOsOffset),
		},
		Asset: stakeAsset,
		Out:   out,
	}
	e.onAbortState.AddUTXO(onAbortUtxo)
	e.onAbortState.AddRewardUTXO(txID, onAbortUtxo)

	return nil
}

// Invariants:
//  1. [newWeight] > [e.backend.Config.MaxValidatorStake]
//  2. [newWeight] > [staker.Weight]
func (e *proposalTxExecutor) createOverflowUTXOsContinuousValidator(
	uValidatorTx txs.ValidatorTx,
	newWeight uint64,
	delegateeReward uint64,
	staker *state.Staker,
) (uint64, uint64, error) {
	// todo: think about using similar technique as rewards/calculator.Split
	// todo: think about having any of them 0
	asset := uValidatorTx.Stake()[0].Asset

	restakingRewards, err := safemath.Sub(newWeight, staker.Weight)
	if err != nil {
		return 0, 0, err
	}

	excess, err := safemath.Sub(newWeight, e.backend.Config.MaxValidatorStake)
	if err != nil {
		return 0, 0, err
	}

	excessRatio := float64(excess) / float64(restakingRewards) // < 0

	excessValidationReward := uint64(math.Round(excessRatio * float64(staker.PotentialReward)))
	excessDelegateeReward := uint64(math.Round(excessRatio * float64(delegateeReward)))

	if excessValidationReward > 0 {
		// Create UTXO for [excessValidationReward]
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
				OutputIndex: 0,
			},
			Asset: asset,
			Out:   out,
		}
		e.onCommitState.AddUTXO(excessValidationRewardUTXO)
		e.onCommitState.AddRewardUTXO(e.tx.ID(), excessValidationRewardUTXO)
	}

	if excessDelegateeReward > 0 {
		// Create UTXO for [excessDelegateeReward]
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
				OutputIndex: 1,
			},
			Asset: asset,
			Out:   out,
		}
		e.onCommitState.AddUTXO(excessDelegateeRewardUTXO)
		e.onCommitState.AddRewardUTXO(e.tx.ID(), excessDelegateeRewardUTXO)
	}

	return excessValidationReward, excessDelegateeReward, nil
}
