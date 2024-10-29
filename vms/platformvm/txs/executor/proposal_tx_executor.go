// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	_ txs.Visitor = (*ProposalTxExecutor)(nil)

	ErrRemoveStakerTooEarly          = errors.New("attempting to remove staker before their end time")
	ErrRemoveWrongStaker             = errors.New("attempting to remove wrong staker")
	ErrInvalidState                  = errors.New("generated output isn't valid state")
	ErrShouldBePermissionlessStaker  = errors.New("expected permissionless staker")
	ErrWrongTxType                   = errors.New("wrong transaction type")
	ErrInvalidID                     = errors.New("invalid ID")
	ErrProposedAddStakerTxAfterBanff = errors.New("staker transaction proposed after Banff")
	ErrAdvanceTimeTxIssuedAfterBanff = errors.New("AdvanceTimeTx issued after Banff")
)

type ProposalTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	FeeCalculator fee.Calculator
	Tx            *txs.Tx
	// [OnCommitState] is the state used for validation.
	// [OnCommitState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is committed.
	//
	// Invariant: Both [OnCommitState] and [OnAbortState] represent the same
	//            state when provided to this struct.
	OnCommitState state.Diff
	// [OnAbortState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is aborted.
	OnAbortState state.Diff
}

func (*ProposalTxExecutor) CreateChainTx(*txs.CreateChainTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) ImportTx(*txs.ImportTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) ExportTx(*txs.ExportTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) BaseTx(*txs.BaseTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) ConvertSubnetTx(*txs.ConvertSubnetTx) error {
	return ErrWrongTxType
}

func (e *ProposalTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	// AddValidatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddValidatorTxs must be issued into
	// StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.UpgradeConfig.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.UpgradeConfig.BanffTime,
		)
	}

	onAbortOuts, err := verifyAddValidatorTx(
		e.Backend,
		e.FeeCalculator,
		e.OnCommitState,
		e.Tx,
		tx,
	)
	if err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	avax.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.OnCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	if err := e.OnCommitState.PutPendingValidator(newStaker); err != nil {
		return err
	}

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.OnAbortState, txID, onAbortOuts)
	return nil
}

func (e *ProposalTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	// AddSubnetValidatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddSubnetValidatorTxs must be
	// issued into StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.UpgradeConfig.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.UpgradeConfig.BanffTime,
		)
	}

	if err := verifyAddSubnetValidatorTx(
		e.Backend,
		e.FeeCalculator,
		e.OnCommitState,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	avax.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.OnCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	if err := e.OnCommitState.PutPendingValidator(newStaker); err != nil {
		return err
	}

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.OnAbortState, txID, tx.Outs)
	return nil
}

func (e *ProposalTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	// AddDelegatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddDelegatorTxs must be issued into
	// StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.UpgradeConfig.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.UpgradeConfig.BanffTime,
		)
	}

	onAbortOuts, err := verifyAddDelegatorTx(
		e.Backend,
		e.FeeCalculator,
		e.OnCommitState,
		e.Tx,
		tx,
	)
	if err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	avax.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.OnCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	e.OnCommitState.PutPendingDelegator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.OnAbortState, txID, onAbortOuts)
	return nil
}

func (e *ProposalTxExecutor) AdvanceTimeTx(tx *txs.AdvanceTimeTx) error {
	switch {
	case tx == nil:
		return txs.ErrNilTx
	case len(e.Tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	// Validate [newChainTime]
	newChainTime := tx.Timestamp()
	if e.Config.UpgradeConfig.IsBanffActivated(newChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s) >= Banff fork time (%s)",
			ErrAdvanceTimeTxIssuedAfterBanff,
			newChainTime,
			e.Config.UpgradeConfig.BanffTime,
		)
	}

	now := e.Clk.Time()
	if err := VerifyNewChainTime(
		newChainTime,
		now,
		e.OnCommitState,
	); err != nil {
		return err
	}

	// Note that state doesn't change if this proposal is aborted
	_, err := AdvanceTimeTo(e.Backend, e.OnCommitState, newChainTime)
	return err
}

func (e *ProposalTxExecutor) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	switch {
	case tx == nil:
		return txs.ErrNilTx
	case tx.TxID == ids.Empty:
		return ErrInvalidID
	case len(e.Tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	currentStakerIterator, err := e.OnCommitState.GetCurrentStakerIterator()
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
	currentChainTime := e.OnCommitState.GetTimestamp()
	if !stakerToReward.EndTime.Equal(currentChainTime) {
		return fmt.Errorf(
			"%w: TxID = %s with %s < %s",
			ErrRemoveStakerTooEarly,
			tx.TxID,
			currentChainTime,
			stakerToReward.EndTime,
		)
	}

	stakerTx, _, err := e.OnCommitState.GetTx(stakerToReward.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	// Invariant: A [txs.DelegatorTx] does not also implement the
	//            [txs.ValidatorTx] interface.
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case txs.ValidatorTx:
		if err := e.rewardValidatorTx(uStakerTx, stakerToReward); err != nil {
			return err
		}

		// Handle staker lifecycle.
		e.OnCommitState.DeleteCurrentValidator(stakerToReward)
		e.OnAbortState.DeleteCurrentValidator(stakerToReward)
	case txs.DelegatorTx:
		if err := e.rewardDelegatorTx(uStakerTx, stakerToReward); err != nil {
			return err
		}

		// Handle staker lifecycle.
		e.OnCommitState.DeleteCurrentDelegator(stakerToReward)
		e.OnAbortState.DeleteCurrentDelegator(stakerToReward)
	default:
		// Invariant: Permissioned stakers are removed by the advancement of
		//            time and the current chain timestamp is == this staker's
		//            EndTime. This means only permissionless stakers should be
		//            left in the staker set.
		return ErrShouldBePermissionlessStaker
	}

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply, err := e.OnAbortState.GetCurrentSupply(stakerToReward.SubnetID)
	if err != nil {
		return err
	}
	newSupply, err := math.Sub(currentSupply, stakerToReward.PotentialReward)
	if err != nil {
		return err
	}
	e.OnAbortState.SetCurrentSupply(stakerToReward.SubnetID, newSupply)
	return nil
}

func (e *ProposalTxExecutor) rewardValidatorTx(uValidatorTx txs.ValidatorTx, validator *state.Staker) error {
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
		e.OnCommitState.AddUTXO(utxo)
		e.OnAbortState.AddUTXO(utxo)
	}

	utxosOffset := 0

	// Provide the reward here
	reward := validator.PotentialReward
	if reward > 0 {
		validationRewardsOwner := uValidatorTx.ValidationRewardsOwner()
		outIntf, err := e.Fx.CreateOutput(reward, validationRewardsOwner)
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
		e.OnCommitState.AddUTXO(utxo)
		e.OnCommitState.AddRewardUTXO(txID, utxo)

		utxosOffset++
	}

	// Provide the accrued delegatee rewards from successful delegations here.
	delegateeReward, err := e.OnCommitState.GetDelegateeReward(
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
	outIntf, err := e.Fx.CreateOutput(delegateeReward, delegationRewardsOwner)
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
	e.OnCommitState.AddUTXO(onCommitUtxo)
	e.OnCommitState.AddRewardUTXO(txID, onCommitUtxo)

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
	e.OnAbortState.AddUTXO(onAbortUtxo)
	e.OnAbortState.AddRewardUTXO(txID, onAbortUtxo)
	return nil
}

func (e *ProposalTxExecutor) rewardDelegatorTx(uDelegatorTx txs.DelegatorTx, delegator *state.Staker) error {
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
		e.OnCommitState.AddUTXO(utxo)
		e.OnAbortState.AddUTXO(utxo)
	}

	// We're (possibly) rewarding a delegator, so we need to fetch
	// the validator they are delegated to.
	validator, err := e.OnCommitState.GetCurrentValidator(delegator.SubnetID, delegator.NodeID)
	if err != nil {
		return fmt.Errorf("failed to get whether %s is a validator: %w", delegator.NodeID, err)
	}

	vdrTxIntf, _, err := e.OnCommitState.GetTx(validator.TxID)
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
		outIntf, err := e.Fx.CreateOutput(reward, rewardsOwner)
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

		e.OnCommitState.AddUTXO(utxo)
		e.OnCommitState.AddRewardUTXO(txID, utxo)

		utxosOffset++
	}

	if delegateeReward == 0 {
		return nil
	}

	// Reward the delegatee here
	if e.Config.UpgradeConfig.IsCortinaActivated(validator.StartTime) {
		previousDelegateeReward, err := e.OnCommitState.GetDelegateeReward(
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
		err = e.OnCommitState.SetDelegateeReward(
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
		outIntf, err := e.Fx.CreateOutput(delegateeReward, delegationRewardsOwner)
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

		e.OnCommitState.AddUTXO(utxo)
		e.OnCommitState.AddRewardUTXO(txID, utxo)
	}
	return nil
}
