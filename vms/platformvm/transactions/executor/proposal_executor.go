// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/timed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	txstate "github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
	p_validator "github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var _ ProposalExecutor = &proposalExecutor{}

type ProposalExecutor interface {
	ExecuteProposal(
		stx *signed.Tx,
		parentState state.Mutable,
	) (state.Versioned, state.Versioned, error)

	InitiallyPrefersCommit(utx unsigned.Tx) bool

	semanticVerifyProposal(stx *signed.Tx, parentState state.Mutable) error
}

type proposalExecutor struct {
	*components
}

func (pe *proposalExecutor) ExecuteProposal(
	stx *signed.Tx,
	parentState state.Mutable,
) (state.Versioned, state.Versioned, error) {
	var (
		txID        = stx.ID()
		creds       = stx.Creds
		signedBytes = stx.Bytes()
	)

	switch utx := stx.Unsigned.(type) {
	case *unsigned.AddDelegatorTx:
		return pe.executeAddDelegator(parentState, utx, txID, signedBytes, creds)
	case *unsigned.AddValidatorTx:
		return pe.executeAddValidator(parentState, utx, txID, signedBytes, creds)
	case *unsigned.AddSubnetValidatorTx:
		return pe.executeAddSubnetValidator(parentState, utx, txID, signedBytes, creds)
	case *unsigned.AdvanceTimeTx:
		return pe.executeAdvanceTime(parentState, utx, creds)
	case *unsigned.RewardValidatorTx:
		return pe.executeRewardValidator(parentState, utx, creds)
	default:
		return nil, nil, fmt.Errorf("expected proposal tx but got %T", utx)
	}
}

func (pe *proposalExecutor) executeAddDelegator(
	parentState state.Mutable,
	utx *unsigned.AddDelegatorTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	if err := utx.SyntacticVerify(pe.ctx); err != nil {
		return nil, nil, err
	}

	duration := utx.Validator.Duration()
	switch {
	case duration < pe.cfg.MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, ErrStakeTooShort
	case duration > pe.cfg.MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, ErrStakeTooLong
	case utx.Validator.Wght < pe.cfg.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, nil, p_validator.ErrWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(utx.Outs)+len(utx.Stake))
	copy(outs, utx.Outs)
	copy(outs[len(utx.Outs):], utx.Stake)

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	if pe.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := utx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return nil, nil, fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(utx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				utx.Validator.NodeID,
				err,
			)
		}

		pendingValidator := pendingStakers.GetValidator(utx.Validator.NodeID)
		pendingDelegators := pendingValidator.Delegators()

		var (
			vdrTx                  *unsigned.AddValidatorTx
			currentDelegatorWeight uint64
			currentDelegators      []signed.DelegatorAndID
		)
		if err == nil {
			// This delegator is attempting to delegate to a currently validing
			// node.
			vdrTx, _ = currentValidator.AddValidatorTx()
			currentDelegatorWeight = currentValidator.DelegatorWeight()
			currentDelegators = currentValidator.Delegators()
		} else {
			// This delegator is attempting to delegate to a node that hasn't
			// started validating yet.
			vdrTx, _, err = pendingStakers.GetValidatorTx(utx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, unsigned.ErrDelegatorSubset
				}
				return nil, nil, fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					utx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this delegator delegates is a subset of the
		// time the validator validates.
		if !utx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return nil, nil, unsigned.ErrDelegatorSubset
		}

		// Ensure that the period this delegator delegates wouldn't become over
		// delegated.
		vdrWeight := vdrTx.Weight()
		currentWeight, err := safemath.Add64(vdrWeight, currentDelegatorWeight)
		if err != nil {
			return nil, nil, err
		}

		maximumWeight, err := safemath.Mul64(maxValidatorWeightFactor, vdrWeight)
		if err != nil {
			return nil, nil, api.ErrStakeOverflow
		}

		if !currentTimestamp.Before(pe.cfg.ApricotPhase3Time) {
			maximumWeight = safemath.Min64(maximumWeight, pe.cfg.MaxValidatorStake)
		}

		canDelegate, err := txstate.CanDelegate(
			currentDelegators,
			pendingDelegators,
			utx,
			currentWeight,
			maximumWeight,
		)
		if err != nil {
			return nil, nil, err
		}
		if !canDelegate {
			return nil, nil, ErrOverDelegated
		}

		// Verify the flowcheck
		if err := pe.spendHandler.SemanticVerifySpend(
			parentState,
			utx,
			utx.Ins,
			outs,
			creds,
			pe.cfg.AddStakerTxFee,
			pe.ctx.AVAXAssetID,
		); err != nil {
			return nil, nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return nil, nil, ErrFutureStakeTime
		}
	}

	// Set up the state if this tx is committed
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	newlyPendingStakers := pendingStakers.AddStaker(stx)

	onCommitState := state.NewVersioned(parentState, currentStakers, newlyPendingStakers)
	utxos.ConsumeInputs(onCommitState, utx.Ins)
	utxos.ProduceOutputs(onCommitState, txID, pe.ctx.AVAXAssetID, utx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)
	utxos.ConsumeInputs(onAbortState, utx.Ins)
	utxos.ProduceOutputs(onAbortState, txID, pe.ctx.AVAXAssetID, outs)

	return onCommitState, onAbortState, nil
}

func (pe *proposalExecutor) executeAddValidator(
	parentState state.Mutable,
	utx *unsigned.AddValidatorTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	// Verify the tx is well-formed
	if err := utx.SyntacticVerify(pe.ctx); err != nil {
		return nil, nil, err
	}

	switch {
	case utx.Validator.Wght < pe.cfg.MinValidatorStake: // Ensure validator is staking at least the minimum amount
		return nil, nil, p_validator.ErrWeightTooSmall
	case utx.Validator.Wght > pe.cfg.MaxValidatorStake: // Ensure validator isn't staking too much
		return nil, nil, ErrWeightTooLarge
	case utx.Shares < pe.cfg.MinDelegationFee:
		return nil, nil, ErrInsufficientDelegationFee
	}

	duration := utx.Validator.Duration()
	switch {
	case duration < pe.cfg.MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, ErrStakeTooShort
	case duration > pe.cfg.MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, ErrStakeTooLong
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	outs := make([]*avax.TransferableOutput, len(utx.Outs)+len(utx.Stake))
	copy(outs, utx.Outs)
	copy(outs[len(utx.Outs):], utx.Stake)

	if pe.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := utx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return nil, nil, fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		// Ensure this validator isn't currently a validator.
		_, err := currentStakers.GetValidator(utx.Validator.NodeID)
		if err == nil {
			return nil, nil, fmt.Errorf(
				"%s is already a primary network validator",
				utx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				utx.Validator.NodeID,
				err,
			)
		}

		// Ensure this validator isn't about to become a validator.
		_, _, err = pendingStakers.GetValidatorTx(utx.Validator.NodeID)
		if err == nil {
			return nil, nil, fmt.Errorf(
				"%s is about to become a primary network validator",
				utx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is about to become a validator: %w",
				utx.Validator.NodeID,
				err,
			)
		}

		// Verify the flowcheck
		if err := pe.spendHandler.SemanticVerifySpend(
			parentState,
			utx,
			utx.Ins,
			outs,
			creds,
			pe.cfg.AddStakerTxFee,
			pe.ctx.AVAXAssetID,
		); err != nil {
			return nil, nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if startTime.After(maxStartTime) {
			return nil, nil, ErrFutureStakeTime
		}
	}

	// Set up the state if this tx is committed
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := state.NewVersioned(parentState, currentStakers, newlyPendingStakers)

	utxos.ConsumeInputs(onCommitState, utx.Ins)
	utxos.ProduceOutputs(onCommitState, txID, pe.ctx.AVAXAssetID, utx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)
	utxos.ConsumeInputs(onAbortState, utx.Ins)
	utxos.ProduceOutputs(onAbortState, txID, pe.ctx.AVAXAssetID, outs)

	return onCommitState, onAbortState, nil
}

func (pe *proposalExecutor) executeAddSubnetValidator(
	parentState state.Mutable,
	utx *unsigned.AddSubnetValidatorTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	// Verify the tx is well-formed
	if err := utx.SyntacticVerify(pe.ctx); err != nil {
		return nil, nil, err
	}

	duration := utx.Validator.Duration()
	switch {
	case duration < pe.cfg.MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, ErrStakeTooShort
	case duration > pe.cfg.MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, ErrStakeTooLong
	case len(creds) == 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	if pe.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := utx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return nil, nil, fmt.Errorf(
				"validator's start time (%s) is at or after current chain timestamp (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(utx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				utx.Validator.NodeID,
				err,
			)
		}

		var vdrTx *unsigned.AddValidatorTx
		if err == nil {
			// This validator is attempting to validate with a currently
			// validing node.
			vdrTx, _ = currentValidator.AddValidatorTx()

			// Ensure that this transaction isn't a duplicate add validator tx.
			subnets := currentValidator.SubnetValidators()
			if _, validates := subnets[utx.Validator.Subnet]; validates {
				return nil, nil, fmt.Errorf(
					"already validating subnet %s",
					utx.Validator.Subnet,
				)
			}
		} else {
			// This validator is attempting to validate with a node that hasn't
			// started validating yet.
			vdrTx, _, err = pendingStakers.GetValidatorTx(utx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, unsigned.ErrDSValidatorSubset
				}
				return nil, nil, fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					utx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if !utx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return nil, nil, unsigned.ErrDSValidatorSubset
		}

		// Ensure that this transaction isn't a duplicate add validator tx.
		pendingValidator := pendingStakers.GetValidator(utx.Validator.NodeID)
		subnets := pendingValidator.SubnetValidators()
		if _, validates := subnets[utx.Validator.Subnet]; validates {
			return nil, nil, fmt.Errorf(
				"already validating subnet %s",
				utx.Validator.Subnet,
			)
		}

		baseTxCredsLen := len(creds) - 1
		baseTxCreds := creds[:baseTxCredsLen]
		subnetCred := creds[baseTxCredsLen]

		subnetIntf, _, err := parentState.GetTx(utx.Validator.Subnet)
		if err != nil {
			if err == database.ErrNotFound {
				return nil, nil, unsigned.ErrDSValidatorSubset
			}
			return nil, nil, fmt.Errorf(
				"couldn't find subnet %s with %w",
				utx.Validator.Subnet,
				err,
			)
		}

		subnet, ok := subnetIntf.Unsigned.(*unsigned.CreateSubnetTx)
		if !ok {
			return nil, nil, fmt.Errorf(
				"%s is not a subnet",
				utx.Validator.Subnet,
			)
		}

		if err := pe.fx.VerifyPermission(utx, utx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
			return nil, nil, err
		}

		// Verify the flowcheck
		if err := pe.spendHandler.SemanticVerifySpend(
			parentState,
			utx,
			utx.Ins,
			utx.Outs,
			baseTxCreds,
			pe.cfg.TxFee,
			pe.ctx.AVAXAssetID,
		); err != nil {
			return nil, nil, err
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return nil, nil, ErrFutureStakeTime
		}
	}

	// Set up the state if this tx is committed
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := state.NewVersioned(parentState, currentStakers, newlyPendingStakers)

	utxos.ConsumeInputs(onCommitState, utx.Ins)
	utxos.ProduceOutputs(onCommitState, txID, pe.ctx.AVAXAssetID, utx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)
	utxos.ConsumeInputs(onAbortState, utx.Ins)
	utxos.ProduceOutputs(onAbortState, txID, pe.ctx.AVAXAssetID, utx.Outs)

	return onCommitState, onAbortState, nil
}

func (pe *proposalExecutor) executeAdvanceTime(
	parentState state.Mutable,
	utx *unsigned.AdvanceTimeTx,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	switch {
	case utx == nil:
		return nil, nil, unsigned.ErrNilTx
	case len(creds) != 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
	}

	txTimestamp := utx.Timestamp()
	localTimestamp := pe.clk.Time()
	localTimestampPlusSync := localTimestamp.Add(SyncBound)
	if localTimestampPlusSync.Before(txTimestamp) {
		return nil, nil, fmt.Errorf(
			"proposed time (%s) is too far in the future relative to local time (%s)",
			txTimestamp,
			localTimestamp,
		)
	}

	if chainTimestamp := parentState.GetTimestamp(); !txTimestamp.After(chainTimestamp) {
		return nil, nil, fmt.Errorf(
			"proposed timestamp (%s), not after current timestamp (%s)",
			txTimestamp,
			chainTimestamp,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	nextStakerChangeTime, err := parentState.GetNextStakerChangeTime()
	if err != nil {
		return nil, nil, err
	}

	if txTimestamp.After(nextStakerChangeTime) {
		return nil, nil, fmt.Errorf(
			"proposed timestamp (%s) later than next staker change time (%s)",
			txTimestamp,
			nextStakerChangeTime,
		)
	}

	currentSupply := parentState.GetCurrentSupply()

	pendingStakers := parentState.PendingStakerChainState()
	toAddValidatorsWithRewardToCurrent := []*txstate.ValidatorReward(nil)
	toAddDelegatorsWithRewardToCurrent := []*txstate.ValidatorReward(nil)
	toAddWithoutRewardToCurrent := []*signed.Tx(nil)
	numToRemoveFromPending := 0

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp. [pendingStakers.Stakers()] is sorted in order
	// of increasing startTime
pendingStakerLoop:
	for _, stakerTx := range pendingStakers.Stakers() {
		switch staker := stakerTx.Unsigned.(type) {
		case *unsigned.AddDelegatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := pe.rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = safemath.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, err
			}

			toAddDelegatorsWithRewardToCurrent = append(toAddDelegatorsWithRewardToCurrent, &txstate.ValidatorReward{
				AddStakerTx:     stakerTx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *unsigned.AddValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := pe.rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = safemath.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, err
			}

			toAddValidatorsWithRewardToCurrent = append(toAddValidatorsWithRewardToCurrent, &txstate.ValidatorReward{
				AddStakerTx:     stakerTx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *unsigned.AddSubnetValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			// If this staker should already be removed, then we should just
			// never add them.
			if staker.EndTime().After(txTimestamp) {
				toAddWithoutRewardToCurrent = append(toAddWithoutRewardToCurrent, stakerTx)
			}
			numToRemoveFromPending++
		default:
			return nil, nil, fmt.Errorf("expected validator but got %T", stakerTx.Unsigned)
		}
	}
	newlyPendingStakers := pendingStakers.DeleteStakers(numToRemoveFromPending)

	currentStakers := parentState.CurrentStakerChainState()
	numToRemoveFromCurrent := 0

	// Remove from the staker set any subnet validators whose endTime is at or
	// before the new timestamp
currentStakerLoop:
	for _, tx := range currentStakers.Stakers() {
		switch staker := tx.Unsigned.(type) {
		case *unsigned.AddSubnetValidatorTx:
			if staker.EndTime().After(txTimestamp) {
				break currentStakerLoop
			}

			numToRemoveFromCurrent++
		case *unsigned.AddValidatorTx, *unsigned.AddDelegatorTx:
			// We shouldn't be removing any primary network validators here
			break currentStakerLoop
		default:
			return nil, nil, fmt.Errorf("expected tx type *unsigned.AddValidatorTx or *unsigned.AddDelegatorTx but got %T", staker)
		}
	}
	newlyCurrentStakers, err := currentStakers.UpdateStakers(
		toAddValidatorsWithRewardToCurrent,
		toAddDelegatorsWithRewardToCurrent,
		toAddWithoutRewardToCurrent,
		numToRemoveFromCurrent,
	)
	if err != nil {
		return nil, nil, err
	}

	onCommitState := state.NewVersioned(parentState, newlyCurrentStakers, newlyPendingStakers)
	onCommitState.SetTimestamp(txTimestamp)
	onCommitState.SetCurrentSupply(currentSupply)

	// State doesn't change if this proposal is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)

	return onCommitState, onAbortState, nil
}

// The current validating set must have at least one member.
// The next validator to be removed must be the validator specified in this block.
// The next validator to be removed must be have an end time equal to the current
// chain timestamp.
func (pe *proposalExecutor) executeRewardValidator(
	parentState state.Mutable,
	utx *unsigned.RewardValidatorTx,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	switch {
	case utx == nil:
		return nil, nil, unsigned.ErrNilTx
	case utx.TxID == ids.Empty:
		return nil, nil, ErrInvalidID
	case len(creds) != 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
	}

	currentStakers := parentState.CurrentStakerChainState()
	stakerTx, stakerReward, err := currentStakers.GetNextStaker()
	if err == database.ErrNotFound {
		return nil, nil, fmt.Errorf("failed to get next staker stop time: %w", err)
	}
	if err != nil {
		return nil, nil, err
	}

	stakerID := stakerTx.ID()
	if stakerID != utx.TxID {
		return nil, nil, fmt.Errorf(
			"attempting to remove TxID: %s. Should be removing %s",
			utx.TxID,
			stakerID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime := parentState.GetTimestamp()
	staker, ok := stakerTx.Unsigned.(timed.Tx)
	if !ok {
		return nil, nil, fmt.Errorf("expected tx type timed.Tx but got %T", stakerTx.Unsigned)
	}
	if endTime := staker.EndTime(); !endTime.Equal(currentTime) {
		return nil, nil, fmt.Errorf(
			"attempting to remove TxID: %s before their end time %s",
			utx.TxID,
			endTime,
		)
	}

	newlyCurrentStakers, err := currentStakers.DeleteNextStaker()
	if err != nil {
		return nil, nil, err
	}

	pendingStakers := parentState.PendingStakerChainState()
	onCommitState := state.NewVersioned(parentState, newlyCurrentStakers, pendingStakers)
	onAbortState := state.NewVersioned(parentState, newlyCurrentStakers, pendingStakers)

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply := onAbortState.GetCurrentSupply()
	newSupply, err := safemath.Sub64(currentSupply, stakerReward)
	if err != nil {
		return nil, nil, err
	}
	onAbortState.SetCurrentSupply(newSupply)

	var (
		nodeID    ids.NodeID
		startTime time.Time
	)
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: pe.ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			onCommitState.AddUTXO(utxo)
			onAbortState.AddUTXO(utxo)
		}

		// Provide the reward here
		if stakerReward > 0 {
			outIntf, err := pe.fx.CreateOutput(stakerReward, uStakerTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, fmt.Errorf("expected verify.State but got %T", outIntf)
			}

			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: pe.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(utx.TxID, utxo)
		}

		// Handle reward preferences
		nodeID = uStakerTx.Validator.ID()
		startTime = uStakerTx.StartTime()
	case *unsigned.AddDelegatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: pe.ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			onCommitState.AddUTXO(utxo)
			onAbortState.AddUTXO(utxo)
		}

		// We're removing a delegator, so we need to fetch the validator they
		// are delgated to.
		vdr, err := currentStakers.GetValidator(uStakerTx.Validator.NodeID)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				uStakerTx.Validator.NodeID,
				err,
			)
		}
		vdrTx, _ := vdr.AddValidatorTx()

		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		delegatorShares := reward.PercentDenominator - uint64(vdrTx.Shares)             // parentTx.Shares <= reward.PercentDenominator so no underflow
		delegatorReward := delegatorShares * (stakerReward / reward.PercentDenominator) // delegatorShares <= reward.PercentDenominator so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := safemath.Mul64(delegatorShares, stakerReward); err == nil {
			delegatorReward = optimisticReward / reward.PercentDenominator
		}
		delegateeReward := stakerReward - delegatorReward // delegatorReward <= reward so no underflow

		offset := 0

		// Reward the delegator here
		if delegatorReward > 0 {
			outIntf, err := pe.fx.CreateOutput(delegatorReward, uStakerTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, fmt.Errorf("expected verify.State but got %T", outIntf)
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: pe.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(utx.TxID, utxo)

			offset++
		}

		// Reward the delegatee here
		if delegateeReward > 0 {
			outIntf, err := pe.fx.CreateOutput(delegateeReward, vdrTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, fmt.Errorf("expected verify.State but got %T", outIntf)
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake) + offset),
				},
				Asset: avax.Asset{ID: pe.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(utx.TxID, utxo)
		}

		nodeID = uStakerTx.Validator.ID()
		startTime = vdrTx.StartTime()
	default:
		return nil, nil, ErrShouldBeDSValidator
	}

	uptime, err := pe.uptimeMan.CalculateUptimePercentFrom(nodeID, startTime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to calculate uptime: %w", err)
	}
	utx.ShouldPreferCommit = uptime >= pe.cfg.UptimePercentage

	return onCommitState, onAbortState, nil
}

func (pe *proposalExecutor) InitiallyPrefersCommit(utx unsigned.Tx) bool {
	switch tx := utx.(type) {
	case *unsigned.AddDelegatorTx:
		return tx.StartTime().After(pe.clk.Time())
	case *unsigned.AddValidatorTx:
		return tx.StartTime().After(pe.clk.Time())
	case *unsigned.AddSubnetValidatorTx:
		return tx.StartTime().After(pe.clk.Time())
	case *unsigned.AdvanceTimeTx:
		txTimestamp := tx.Timestamp()
		localTimestamp := pe.clk.Time()
		localTimestampPlusSync := localTimestamp.Add(SyncBound)
		return !txTimestamp.After(localTimestampPlusSync)
	case *unsigned.RewardValidatorTx:
		// InitiallyPrefersCommit returns true if this node thinks the validator
		// should receive a staking reward.
		//
		// TODO: A validator should receive a reward only if they are sufficiently
		// responsive and correct during the time they are validating.
		// Right now they receive a reward if they're up (but not necessarily
		// correct and responsive) for a sufficient amount of time
		return tx.ShouldPreferCommit
	}
	panic("TODO ABENEGIA FIND A BETTER WAY TO HANDLE THIS")
}

func (pe *proposalExecutor) semanticVerifyProposal(stx *signed.Tx, parentState state.Mutable) error {
	switch utx := stx.Unsigned.(type) {
	case *unsigned.AddDelegatorTx,
		*unsigned.AddValidatorTx,
		*unsigned.AddSubnetValidatorTx:
		startTime := utx.(timed.Tx).StartTime()
		maxLocalStartTime := pe.clk.Time().Add(MaxFutureStartTime)
		if startTime.After(maxLocalStartTime) {
			return ErrFutureStakeTime
		}

		_, _, err := pe.ExecuteProposal(stx, parentState)
		// We ignore [errFutureStakeTime] here because an advanceTimeTx will be
		// issued before this transaction is issued.
		if errors.Is(err, ErrFutureStakeTime) {
			return nil
		}
		return err

	case *unsigned.AdvanceTimeTx,
		*unsigned.RewardValidatorTx:
		_, _, err := pe.ExecuteProposal(stx, parentState)
		return err
	default:
		return fmt.Errorf("tx type %T could not be semantically verified", utx)
	}
}
