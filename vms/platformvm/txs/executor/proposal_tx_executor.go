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

	changes, err := AdvanceTimeTo(e.Backend, e.OnCommitState, newChainTime)
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
	if !stakerToReward.NextTime.Equal(currentChainTime) {
		return fmt.Errorf(
			"%w: TxID = %s with %s < %s",
			ErrRemoveStakerTooEarly,
			tx.TxID,
			currentChainTime,
			stakerToReward.NextTime,
		)
	}

	// retrieve primaryNetworkValidator before possibly removing it.
	primaryNetworkValidator, err := e.OnCommitState.GetCurrentValidator(
		constants.PrimaryNetworkID,
		stakerToReward.NodeID,
	)
	if err != nil {
		// This should never error because the staker set is in memory and
		// primary network validators are removed last.
		return err
	}

	stakerTx, _, err := e.OnCommitState.GetTx(stakerToReward.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	// stop delegator if needed. We do this lazily here, instead of stopping all
	// delegators when a validator is stopped
	if err := e.lazyStakersStop(stakerToReward); err != nil {
		return err
	}

	rewardToRestake := uint64(0)

	// Invariant: A [txs.DelegatorTx] does not also implement the
	//            [txs.ValidatorTx] interface.
	switch uStakerTx := stakerTx.Unsigned.(type) {
	// TODO ABENEGIA: if a validator and a delegator terminates at the same time
	// the delegator will be handled first due to priority. This means that delegator
	// is shifted before its validator and there may be a few proposal and options blocks
	// where there is delegator outliving its validator. This is ugly and I wonder if this
	// should be done. Maybe we should introduce an ad-hoc priority (extending vs stopping)
	// to properly handle the case. It seems the simplest to me
	case txs.ValidatorTx:
		if rewardToRestake, err = e.rewardValidatorTx(uStakerTx, stakerToReward); err != nil {
			return err
		}
	case txs.DelegatorTx:
		if rewardToRestake, err = e.rewardDelegatorTx(uStakerTx, stakerToReward); err != nil {
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
	currentSupply, err := e.OnAbortState.GetCurrentSupply(stakerToReward.SubnetID)
	if err != nil {
		return err
	}
	newSupply, err := math.Sub(currentSupply, stakerToReward.PotentialReward)
	if err != nil {
		return err
	}
	e.OnAbortState.SetCurrentSupply(stakerToReward.SubnetID, newSupply)

	// here both commit and abort state supplies are correct. We can remove or
	// shift staker, with the right potential reward in the second case
	if _, ok := stakerTx.Unsigned.(txs.ValidatorTx); ok {
		if err := handleValidatorShift(e.Backend, e.OnCommitState, stakerToReward, rewardToRestake); err != nil {
			return err
		}

		// no reward to restake on abort
		if err := handleValidatorShift(e.Backend, e.OnAbortState, stakerToReward, 0); err != nil {
			return err
		}
	} else { // must be txs.DelegatorTx due to switch check above
		if err := handleDelegatorShift(e.Backend, e.OnCommitState, stakerToReward, rewardToRestake); err != nil {
			return err
		}
		if err := handleDelegatorShift(e.Backend, e.OnAbortState, stakerToReward, 0); err != nil {
			return err
		}
	}

	shouldCommit, err := e.calculateProposalPreference(stakerToReward, primaryNetworkValidator)
	if err != nil {
		return err
	}

	e.PrefersCommit = shouldCommit
	return nil
}

func (e *ProposalTxExecutor) lazyStakersStop(staker *state.Staker) error {
	if !staker.Priority.IsDelegator() {
		// no need to lazily stop validators
		return nil
	}

	vdr, err := e.OnCommitState.GetCurrentValidator(staker.SubnetID, staker.NodeID)
	if err != nil {
		return fmt.Errorf("can't find validator for delegator %v: %w", staker.TxID, err)
	}

	if vdr.ShouldRestake() {
		// validator not stopped yet, no need to stop the delegator
		return nil
	}

	state.MarkStakerForRemovalInPlaceBeforeTime(staker, vdr.NextTime)
	if err := e.OnCommitState.UpdateCurrentDelegator(staker); err != nil {
		return fmt.Errorf("failed lazily stopping delegator %v: %w", staker.TxID, err)
	}
	if err := e.OnAbortState.UpdateCurrentDelegator(staker); err != nil {
		return fmt.Errorf("failed lazily stopping delegator %v: %w", staker.TxID, err)
	}
	return nil
}

func (e *ProposalTxExecutor) rewardValidatorTx(
	uValidatorTx txs.ValidatorTx,
	validator *state.Staker,
) (uint64, error) {
	var (
		txID    = validator.TxID
		stake   = uValidatorTx.Stake()
		outputs = uValidatorTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	if !validator.ShouldRestake() {
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
	}

	// following Continuous staking fork activation multiple rewards UTXOS
	// can be cumulated, each related to a different staking period. We make
	// sure to index the reward UTXOs correctly by appending them to previous ones.
	utxosOffset := len(outputs) + len(stake)
	currentRewardUTXOs, err := e.OnCommitState.GetRewardUTXOs(txID)
	if err != nil {
		return 0, fmt.Errorf("failed to create output: %w", err)
	}
	utxosOffset += len(currentRewardUTXOs)

	// Provide the reward here
	rewardToPayBack := validator.PotentialReward
	rewardToRestake := uint64(0)
	if validator.ShouldRestake() {
		continuousStaker, ok := uValidatorTx.(txs.ContinuousStaker)
		if ok {
			rewardToPayBack, rewardToRestake = splitAmountByShares(rewardToPayBack, continuousStaker.RestakeShares())
		}
	}

	if rewardToPayBack > 0 {
		validationRewardsOwner := uValidatorTx.ValidationRewardsOwner()
		outIntf, err := e.Fx.CreateOutput(rewardToPayBack, validationRewardsOwner)
		if err != nil {
			return 0, fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return 0, ErrInvalidState
		}

		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(utxosOffset),
			},
			Asset: stakeAsset,
			Out:   out,
		}
		e.OnCommitState.AddUTXO(utxo)
		e.OnCommitState.AddRewardUTXO(txID, utxo)

		utxosOffset++
	}

	// Provide the accrued delegatee rewards from successful delegations here.
	delegateeRewardToPayBack, err := e.OnCommitState.GetDelegateeReward(
		validator.SubnetID,
		validator.NodeID,
	)
	if err != nil && err != database.ErrNotFound {
		return 0, fmt.Errorf("failed to fetch accrued delegatee rewards: %w", err)
	}

	if validator.ShouldRestake() {
		continuousStaker, ok := uValidatorTx.(txs.ContinuousStaker)
		if ok {
			delegateeRewardToRestake := uint64(0)
			delegateeRewardToPayBack, delegateeRewardToRestake = splitAmountByShares(delegateeRewardToPayBack, continuousStaker.RestakeShares())
			rewardToRestake += delegateeRewardToRestake
		}
	}

	if delegateeRewardToPayBack > 0 {
		delegationRewardsOwner := uValidatorTx.DelegationRewardsOwner()
		outIntf, err := e.Fx.CreateOutput(delegateeRewardToPayBack, delegationRewardsOwner)
		if err != nil {
			return 0, fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return 0, ErrInvalidState
		}

		onCommitUtxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(utxosOffset),
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
				OutputIndex: uint32(utxosOffset - 1),
			},
			Asset: stakeAsset,
			Out:   out,
		}
		e.OnAbortState.AddUTXO(onAbortUtxo)
		e.OnAbortState.AddRewardUTXO(txID, onAbortUtxo)
	}
	return rewardToRestake, nil
}

func handleValidatorShift(
	backend *Backend,
	baseState state.Chain,
	validator *state.Staker,
	rewardToRestake uint64,
) error {
	if !validator.ShouldRestake() {
		baseState.DeleteCurrentValidator(validator)
		return nil
	}

	shiftedStaker := *validator
	state.IncreaseStakerWeightInPlace(&shiftedStaker, shiftedStaker.Weight+rewardToRestake)
	state.ShiftStakerAheadInPlace(&shiftedStaker, shiftedStaker.NextTime)

	currentSupply, potentialReward, err := calculatePotentialReward(
		backend,
		baseState,
		shiftedStaker.SubnetID,
		shiftedStaker.StakingPeriod,
		shiftedStaker.Weight,
	)
	if err != nil {
		return err
	}

	shiftedStaker.PotentialReward = potentialReward
	if err := baseState.UpdateCurrentValidator(&shiftedStaker); err != nil {
		return fmt.Errorf("failed updating current validator: %w", err)
	}

	updatedSupply := currentSupply + potentialReward
	baseState.SetCurrentSupply(shiftedStaker.SubnetID, updatedSupply)
	return nil
}

func (e *ProposalTxExecutor) rewardDelegatorTx(
	uDelegatorTx txs.DelegatorTx,
	delegator *state.Staker,
) (uint64, error) {
	var (
		txID    = delegator.TxID
		stake   = uDelegatorTx.Stake()
		outputs = uDelegatorTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset = stake[0].Asset
	)

	if !delegator.ShouldRestake() {
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
	}

	// We're (possibly) rewarding a delegator, so we need to fetch
	// the validator they are delegated to.
	vdrStaker, err := e.OnCommitState.GetCurrentValidator(
		delegator.SubnetID,
		delegator.NodeID,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"failed to get whether %s is a validator: %w",
			delegator.NodeID,
			err,
		)
	}

	vdrTxIntf, _, err := e.OnCommitState.GetTx(vdrStaker.TxID)
	if err != nil {
		return 0, fmt.Errorf(
			"failed to get whether %s is a validator: %w",
			delegator.NodeID,
			err,
		)
	}

	// Invariant: Delegators must only be able to reference validator
	//            transactions that implement [txs.ValidatorTx]. All
	//            validator transactions implement this interface except the
	//            AddSubnetValidatorTx.
	vdrTx, ok := vdrTxIntf.Unsigned.(txs.ValidatorTx)
	if !ok {
		return 0, ErrWrongTxType
	}

	// Calculate split of reward between delegator/delegatee
	delegatorReward, delegateeReward := splitAmountByShares(delegator.PotentialReward, vdrTx.Shares())

	// following Continuous staking fork activation multiple rewards UTXOS
	// can be cumulated, each related to a different staking period. We make
	// sure to index the reward UTXOs correctly by appending them to previous ones.
	utxosOffset := len(outputs) + len(stake)
	currentRewardUTXOs, err := e.OnCommitState.GetRewardUTXOs(delegator.TxID)
	if err != nil {
		return 0, fmt.Errorf("failed to create output: %w", err)
	}
	utxosOffset += len(currentRewardUTXOs)

	// Reward the delegator here
	delRewardToPayBack := delegatorReward
	delRewardToRestake := uint64(0)
	if delegator.ShouldRestake() {
		continuousStaker, ok := uDelegatorTx.(txs.ContinuousStaker)
		if ok {
			delRewardToPayBack, delRewardToRestake = splitAmountByShares(delRewardToPayBack, continuousStaker.RestakeShares())
		}
	}
	if delRewardToPayBack > 0 {
		rewardsOwner := uDelegatorTx.RewardsOwner()
		outIntf, err := e.Fx.CreateOutput(delRewardToPayBack, rewardsOwner)
		if err != nil {
			return 0, fmt.Errorf("failed to create output: %w", err)
		}
		out, ok := outIntf.(verify.State)
		if !ok {
			return 0, ErrInvalidState
		}
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(utxosOffset),
			},
			Asset: stakeAsset,
			Out:   out,
		}

		e.OnCommitState.AddUTXO(utxo)
		e.OnCommitState.AddRewardUTXO(txID, utxo)

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
				return 0, fmt.Errorf("failed to get delegatee reward: %w", err)
			}

			// Invariant: The rewards calculator can never return a
			//            [potentialReward] that would overflow the
			//            accumulated rewards.
			newDelegateeReward := previousDelegateeReward + delegateeReward

			// For any validators starting after [CortinaTime], we defer rewarding the
			// [rewardToPayBack] until their staking period is over.
			err = e.OnCommitState.SetDelegateeReward(
				vdrStaker.SubnetID,
				vdrStaker.NodeID,
				newDelegateeReward,
			)
			if err != nil {
				return 0, fmt.Errorf("failed to update delegatee reward: %w", err)
			}
		} else {
			// For any validators who started prior to [CortinaTime], we issue the
			// [delegateeReward] immediately.
			delegationRewardsOwner := vdrTx.DelegationRewardsOwner()
			outIntf, err := e.Fx.CreateOutput(delegateeReward, delegationRewardsOwner)
			if err != nil {
				return 0, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return 0, ErrInvalidState
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(utxosOffset),
				},
				Asset: stakeAsset,
				Out:   out,
			}

			e.OnCommitState.AddUTXO(utxo)
			e.OnCommitState.AddRewardUTXO(txID, utxo)
		}
	}
	return delRewardToRestake, nil
}

func handleDelegatorShift(
	backend *Backend,
	baseState state.Chain,
	delegator *state.Staker,
	rewardToRestake uint64,
) error {
	if !delegator.ShouldRestake() {
		baseState.DeleteCurrentDelegator(delegator)
		return nil
	}

	// load delegator's validator to make sure to properly shift delegator. A delegator must not
	// outlive its validator, so delegator's staking time may be shorten up some staking cycles
	// to guarantee that.
	// One caveat: a delegator will be shifted before its validator due to staker priority. So there
	// may be temporary situation when delegator is shifted while validator is not.
	validator, err := baseState.GetCurrentValidator(delegator.SubnetID, delegator.NodeID)
	if err != nil {
		return fmt.Errorf("could not find validator for subnetID %v, nodeID %v",
			delegator.SubnetID,
			delegator.NodeID,
		)
	}

	shiftedStaker := *delegator
	state.IncreaseStakerWeightInPlace(&shiftedStaker, shiftedStaker.Weight+rewardToRestake)
	state.ShiftStakerAheadInPlace(&shiftedStaker, shiftedStaker.NextTime)

	timeBound := validator.NextTime
	if validator.NextTime.Equal(delegator.NextTime) {
		timeBound = timeBound.Add(validator.StakingPeriod)
	}

	if shiftedStaker.NextTime.After(timeBound) {
		stakingPeriod := timeBound.Sub(shiftedStaker.NextTime)
		state.UpdateStakingPeriodInPlace(&shiftedStaker, stakingPeriod)
	}

	currentSupply, potentialReward, err := calculatePotentialReward(
		backend,
		baseState,
		shiftedStaker.SubnetID,
		shiftedStaker.StakingPeriod,
		shiftedStaker.Weight,
	)
	if err != nil {
		return err
	}

	shiftedStaker.PotentialReward = potentialReward
	if err := baseState.UpdateCurrentDelegator(&shiftedStaker); err != nil {
		return fmt.Errorf("failed updating current delegator: %w", err)
	}

	updatedSupply := currentSupply + potentialReward
	baseState.SetCurrentSupply(shiftedStaker.SubnetID, updatedSupply)
	return nil
}

func (e *ProposalTxExecutor) calculateProposalPreference(stakerToReward, primaryNetworkValidator *state.Staker) (bool, error) {
	var expectedUptimePercentage float64
	if stakerToReward.SubnetID != constants.PrimaryNetworkID {
		transformSubnetIntf, err := e.OnCommitState.GetSubnetTransformation(stakerToReward.SubnetID)
		if err != nil {
			return false, fmt.Errorf("failed to calculate uptime: %w", err)
		}
		transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
		if !ok {
			return false, fmt.Errorf("failed to calculate uptime: %w", err)
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
		return false, fmt.Errorf("failed to calculate uptime: %w", err)
	}
	return uptime >= expectedUptimePercentage, nil
}

func splitAmountByShares(totalAmount uint64, shares uint32) (uint64, uint64) {
	remainderShares := reward.PercentDenominator - uint64(shares)
	remainderAmount := remainderShares * (totalAmount / reward.PercentDenominator)

	// Delay rounding as long as possible for small numbers
	if optimisticReward, err := math.Mul64(remainderShares, totalAmount); err == nil {
		remainderAmount = optimisticReward / reward.PercentDenominator
	}

	amountFromShares := totalAmount - remainderAmount
	return remainderAmount, amountFromShares
}
