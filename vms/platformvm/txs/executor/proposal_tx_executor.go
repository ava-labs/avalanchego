// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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
	ErrChildBlockNotAfterParent      = errors.New("proposed timestamp not after current chain time")
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
	Tx *txs.Tx
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

	// outputs populated by this struct's methods:
	//
	// [PrefersCommit] is true iff this node initially prefers to
	// commit this block transaction.
	PrefersCommit bool
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

func (*ProposalTxExecutor) AddContinuousValidatorTx(*txs.AddContinuousValidatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) AddContinuousDelegatorTx(*txs.AddContinuousDelegatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxExecutor) StopStakerTx(*txs.StopStakerTx) error {
	return ErrWrongTxType
}

func (e *ProposalTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	// AddValidatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddValidatorTxs must be issued into
	// StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.BanffTime,
		)
	}

	onAbortOuts, err := verifyAddValidatorTx(
		e.Backend,
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

	e.OnCommitState.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.OnAbortState, txID, onAbortOuts)

	e.PrefersCommit = tx.StartTime().After(e.Clk.Time())
	return nil
}

func (e *ProposalTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	// AddSubnetValidatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddSubnetValidatorTxs must be
	// issued into StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.BanffTime,
		)
	}

	if err := verifyAddSubnetValidatorTx(
		e.Backend,
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

	e.OnCommitState.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	avax.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOs
	avax.Produce(e.OnAbortState, txID, tx.Outs)

	e.PrefersCommit = tx.StartTime().After(e.Clk.Time())
	return nil
}

func (e *ProposalTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	// AddDelegatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddDelegatorTxs must be issued into
	// StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			ErrProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.BanffTime,
		)
	}

	onAbortOuts, err := verifyAddDelegatorTx(
		e.Backend,
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

	e.PrefersCommit = tx.StartTime().After(e.Clk.Time())
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
	if e.Config.IsBanffActivated(newChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s) >= Banff fork time (%s)",
			ErrAdvanceTimeTxIssuedAfterBanff,
			newChainTime,
			e.Config.BanffTime,
		)
	}

	parentChainTime := e.OnCommitState.GetTimestamp()
	if !newChainTime.After(parentChainTime) {
		return fmt.Errorf(
			"%w, proposed timestamp (%s), chain time (%s)",
			ErrChildBlockNotAfterParent,
			parentChainTime,
			parentChainTime,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	nextStakerChangeTime, err := GetNextStakerChangeTime(e.OnCommitState)
	if err != nil {
		return err
	}

	now := e.Clk.Time()
	if err := VerifyNewChainTime(
		newChainTime,
		nextStakerChangeTime,
		now,
	); err != nil {
		return err
	}

	changes, err := AdvanceTimeTo(e.OnCommitState, newChainTime)
	if err != nil {
		return err
	}

	// Update the state if this tx is committed
	e.OnCommitState.SetTimestamp(newChainTime)
	changes.Apply(e.OnCommitState)

	e.PrefersCommit = !newChainTime.After(now.Add(SyncBound))

	// Note that state doesn't change if this proposal is aborted
	return nil
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
	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	if stakerToRemove.TxID != tx.TxID {
		return fmt.Errorf(
			"%w: %s != %s",
			ErrRemoveWrongStaker,
			stakerToRemove.TxID,
			tx.TxID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentChainTime := e.OnCommitState.GetTimestamp()
	if !stakerToRemove.NextTime.Equal(currentChainTime) {
		return fmt.Errorf(
			"%w: TxID = %s with %s < %s",
			ErrRemoveStakerTooEarly,
			tx.TxID,
			currentChainTime,
			stakerToRemove.NextTime,
		)
	}

	primaryNetworkValidator, err := e.OnCommitState.GetCurrentValidator(
		constants.PrimaryNetworkID,
		stakerToRemove.NodeID,
	)
	if err != nil {
		// This should never error because the staker set is in memory and
		// primary network validators are removed last.
		return err
	}

	stakerTx, _, err := e.OnCommitState.GetTx(stakerToRemove.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	// Invariant: A [txs.DelegatorTx] does not also implement the
	//            [txs.ValidatorTx] interface.
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case txs.ValidatorTx:
		if err := e.rewardValidatorTx(uStakerTx, stakerToRemove, tx); err != nil {
			return err
		}
	case txs.DelegatorTx:
		if err := e.rewardDelegatorTx(uStakerTx, stakerToRemove, tx); err != nil {
			return err
		}
	default:
		// Invariant: Permissioned stakers are removed by the advancement of
		//            time and the current chain timestamp is == this staker's
		//            EndTime. This means only permissionless stakers should be
		//            left in the staker set.
		return ErrShouldBePermissionlessStaker
	}

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply, err := e.OnAbortState.GetCurrentSupply(stakerToRemove.SubnetID)
	if err != nil {
		return err
	}
	newSupply, err := math.Sub(currentSupply, stakerToRemove.PotentialReward)
	if err != nil {
		return err
	}
	e.OnAbortState.SetCurrentSupply(stakerToRemove.SubnetID, newSupply)

	var expectedUptimePercentage float64
	if stakerToRemove.SubnetID != constants.PrimaryNetworkID {
		transformSubnetIntf, err := e.OnCommitState.GetSubnetTransformation(stakerToRemove.SubnetID)
		if err != nil {
			return err
		}
		transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
		if !ok {
			return ErrIsNotTransformSubnetTx
		}

		expectedUptimePercentage = float64(transformSubnet.UptimeRequirement) / reward.PercentDenominator
	} else {
		expectedUptimePercentage = e.Config.UptimePercentage
	}

	// TODO: calculate subnet uptimes
	uptime, err := e.Uptimes.CalculateUptimePercentFrom(
		primaryNetworkValidator.NodeID,
		constants.PrimaryNetworkID,
		primaryNetworkValidator.StartTime,
	)
	if err != nil {
		return fmt.Errorf("failed to calculate uptime: %w", err)
	}

	e.PrefersCommit = uptime >= expectedUptimePercentage
	return nil
}

func (e *ProposalTxExecutor) rewardValidatorTx(
	uStakerTx txs.ValidatorTx,
	stakerToReward *state.Staker,
	tx *txs.RewardValidatorTx,
) error {
	var (
		stake   = uStakerTx.Stake()
		outputs = uStakerTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	if stakerToReward.ShouldRestake() {
		shiftedStaker := *stakerToReward
		state.ShiftStakerAheadInPlace(&shiftedStaker, mockable.MaxTime)
		if err := e.OnCommitState.UpdateCurrentValidator(&shiftedStaker); err != nil {
			return fmt.Errorf("failed updating current validator: %w", err)
		}
		if err := e.OnAbortState.UpdateCurrentValidator(&shiftedStaker); err != nil {
			return fmt.Errorf("failed updating current validator: %w", err)
		}
		// staked utxos will be returned only at the end of the staking period.
	} else {
		e.OnCommitState.DeleteCurrentValidator(stakerToReward)
		e.OnAbortState.DeleteCurrentValidator(stakerToReward)

		// Refund the stake only when validator is about to leave
		// the staking set
		for i, out := range stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(outputs) + i),
				},
				Asset: out.Asset,
				Out:   out.Output(),
			}
			e.OnCommitState.AddUTXO(utxo)
			e.OnAbortState.AddUTXO(utxo)
		}
	}

	// following Continuous staking fork activation multiple rewards UTXOS
	// can be cumulated, each related to a different staking period. We make
	// sure to index the reward UTXOs correctly by appending them to previous ones.
	utxosOffset := len(outputs) + len(stake)
	currentRewardUTXOs, err := e.OnCommitState.GetRewardUTXOs(tx.TxID)
	if err != nil {
		return fmt.Errorf("failed to create output: %w", err)
	}
	utxosOffset += len(currentRewardUTXOs)

	// Provide the reward here
	if stakerToReward.PotentialReward > 0 {
		validationRewardsOwner := uStakerTx.ValidationRewardsOwner()
		outIntf, err := e.Fx.CreateOutput(stakerToReward.PotentialReward, validationRewardsOwner)
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}

		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        tx.TxID,
				OutputIndex: uint32(utxosOffset),
			},
			Asset: stakeAsset,
			Out:   out,
		}
		e.OnCommitState.AddUTXO(utxo)
		e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)

		utxosOffset++
	}

	// Provide the accrued delegatee rewards from successful delegations here.
	delegateeReward, err := e.OnCommitState.GetDelegateeReward(
		stakerToReward.SubnetID,
		stakerToReward.NodeID,
	)
	if err != nil && err != database.ErrNotFound {
		return fmt.Errorf("failed to fetch accrued delegatee rewards: %w", err)
	}

	if delegateeReward > 0 {
		delegationRewardsOwner := uStakerTx.DelegationRewardsOwner()
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
				TxID:        tx.TxID,
				OutputIndex: uint32(utxosOffset),
			},
			Asset: stakeAsset,
			Out:   out,
		}
		e.OnCommitState.AddUTXO(onCommitUtxo)
		e.OnCommitState.AddRewardUTXO(tx.TxID, onCommitUtxo)

		// Note: There is no [offset] if the RewardValidatorTx is
		// aborted, because the validator reward is not awarded.
		onAbortUtxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID: tx.TxID,

				OutputIndex: uint32(utxosOffset - 1),
			},
			Asset: stakeAsset,
			Out:   out,
		}
		e.OnAbortState.AddUTXO(onAbortUtxo)
		e.OnAbortState.AddRewardUTXO(tx.TxID, onAbortUtxo)
	}
	return nil
}

func (e *ProposalTxExecutor) rewardDelegatorTx(
	uStakerTx txs.DelegatorTx,
	stakerToReward *state.Staker,
	tx *txs.RewardValidatorTx,
) error {
	var (
		stake   = uStakerTx.Stake()
		outputs = uStakerTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	if stakerToReward.ShouldRestake() {
		validator, err := e.OnCommitState.GetCurrentValidator(stakerToReward.SubnetID, stakerToReward.NodeID)
		if err != nil {
			return fmt.Errorf("could not find validator for subnetID %v, nodeID %v",
				stakerToReward.SubnetID,
				stakerToReward.NodeID,
			)
		}

		shiftedStaker := *stakerToReward
		state.ShiftStakerAheadInPlace(&shiftedStaker, validator.NextTime)
		if err := e.OnCommitState.UpdateCurrentDelegator(&shiftedStaker); err != nil {
			return fmt.Errorf("failed updating current delegator: %w", err)
		}
		if err := e.OnAbortState.UpdateCurrentDelegator(&shiftedStaker); err != nil {
			return fmt.Errorf("failed updating current delegator: %w", err)
		}
		// staked utxos will be returned only at the end of the staking period.
	} else {
		e.OnCommitState.DeleteCurrentDelegator(stakerToReward)
		e.OnAbortState.DeleteCurrentDelegator(stakerToReward)

		// Refund the stake only when delegator is about to leave
		// the staking set
		for i, out := range stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(outputs) + i),
				},
				Asset: out.Asset,
				Out:   out.Output(),
			}
			e.OnCommitState.AddUTXO(utxo)
			e.OnAbortState.AddUTXO(utxo)
		}
	}

	// We're (possibly) rewarding a delegator, so we need to fetch
	// the validator they are delegated to.
	vdrStaker, err := e.OnCommitState.GetCurrentValidator(
		stakerToReward.SubnetID,
		stakerToReward.NodeID,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to get whether %s is a validator: %w",
			stakerToReward.NodeID,
			err,
		)
	}

	vdrTxIntf, _, err := e.OnCommitState.GetTx(vdrStaker.TxID)
	if err != nil {
		return fmt.Errorf(
			"failed to get whether %s is a validator: %w",
			stakerToReward.NodeID,
			err,
		)
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
	// The delegator gives stake to the validatee
	validatorShares := vdrTx.Shares()
	delegatorShares := reward.PercentDenominator - uint64(validatorShares)
	delegatorReward := delegatorShares * (stakerToReward.PotentialReward / reward.PercentDenominator)
	// Delay rounding as long as possible for small numbers
	if optimisticReward, err := math.Mul64(delegatorShares, stakerToReward.PotentialReward); err == nil {
		delegatorReward = optimisticReward / reward.PercentDenominator
	}
	delegateeReward := stakerToReward.PotentialReward - delegatorReward

	// following Continuous staking fork activation multiple rewards UTXOS
	// can be cumulated, each related to a different staking period. We make
	// sure to index the reward UTXOs correctly by appending them to previous ones.
	utxosOffset := len(outputs) + len(stake)
	currentRewardUTXOs, err := e.OnCommitState.GetRewardUTXOs(stakerToReward.TxID)
	if err != nil {
		return fmt.Errorf("failed to create output: %w", err)
	}
	utxosOffset += len(currentRewardUTXOs)

	// Reward the delegator here
	if delegatorReward > 0 {
		rewardsOwner := uStakerTx.RewardsOwner()
		outIntf, err := e.Fx.CreateOutput(delegatorReward, rewardsOwner)
		if err != nil {
			return fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return ErrInvalidState
		}
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        tx.TxID,
				OutputIndex: uint32(utxosOffset),
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.OnCommitState.AddUTXO(utxo)
		e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)

		utxosOffset++
	}

	// Reward the delegatee here
	if delegateeReward > 0 {
		if vdrStaker.StartTime.After(e.Config.CortinaTime) {
			previousDelegateeReward, err := e.OnCommitState.GetDelegateeReward(
				vdrStaker.SubnetID,
				vdrStaker.NodeID,
			)
			if err != nil {
				return fmt.Errorf("failed to get delegatee reward: %w", err)
			}

			// Invariant: The rewards calculator can never return a
			//            [potentialReward] that would overflow the
			//            accumulated rewards.
			newDelegateeReward := previousDelegateeReward + delegateeReward

			// For any validators starting after [CortinaTime], we defer rewarding the
			// [delegateeReward] until their staking period is over.
			err = e.OnCommitState.SetDelegateeReward(
				vdrStaker.SubnetID,
				vdrStaker.NodeID,
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
					TxID:        tx.TxID,
					OutputIndex: uint32(utxosOffset),
				},
				Asset: stakeAsset,
				Out:   out,
			}

			e.OnCommitState.AddUTXO(utxo)
			e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)
		}
	}
	return nil
}
