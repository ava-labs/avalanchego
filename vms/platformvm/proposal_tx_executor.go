// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

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
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
)

var (
	_ txs.Visitor = &proposalTxExecutor{}

	errWeightTooSmall            = errors.New("weight of this validator is too low")
	errWeightTooLarge            = errors.New("weight of this validator is too large")
	errStakeTooShort             = errors.New("staking period is too short")
	errStakeTooLong              = errors.New("staking period is too long")
	errInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
	errFutureStakeTime           = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", maxFutureStartTime)
	errWrongNumberOfCredentials  = errors.New("should have the same number of credentials as inputs")
	errValidatorSubset           = errors.New("all subnets' staking period must be a subset of the primary network")
	errStakeOverflow             = errors.New("validator stake exceeds limit")
	errInvalidState              = errors.New("generated output isn't valid state")
	errOverDelegated             = errors.New("validator would be over delegated")
	errShouldBeDSValidator       = errors.New("expected validator to be in the primary network")
	errWrongTxType               = errors.New("wrong transaction type")
)

type proposalTxExecutor struct {
	// inputs
	vm          *VM
	parentState state.Chain
	tx          *txs.Tx

	// outputs
	onCommit      state.Diff
	onAbort       state.Diff
	prefersCommit bool
}

func (*proposalTxExecutor) CreateChainTx(*txs.CreateChainTx) error   { return errWrongTxType }
func (*proposalTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error { return errWrongTxType }
func (*proposalTxExecutor) ImportTx(*txs.ImportTx) error             { return errWrongTxType }
func (*proposalTxExecutor) ExportTx(*txs.ExportTx) error             { return errWrongTxType }

func (e *proposalTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	// Verify the tx is well-formed
	if err := e.tx.SyntacticVerify(e.vm.ctx); err != nil {
		return err
	}

	switch {
	case tx.Validator.Wght < e.vm.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall

	case tx.Validator.Wght > e.vm.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return errWeightTooLarge

	case tx.Shares < e.vm.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return errInsufficientDelegationFee
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < e.vm.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.vm.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong
	}

	currentStakers := e.parentState.CurrentStakers()
	pendingStakers := e.parentState.PendingStakers()

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if e.vm.bootstrapped.GetValue() {
		currentTimestamp := e.parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := tx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		// Ensure this validator isn't currently a validator.
		_, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err == nil {
			return fmt.Errorf(
				"%s is already a primary network validator",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Ensure this validator isn't about to become a validator.
		_, _, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
		if err == nil {
			return fmt.Errorf(
				"%s is about to become a primary network validator",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is about to become a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Verify the flowcheck
		if err := e.vm.utxoHandler.SemanticVerifySpend(
			tx,
			e.parentState,
			tx.Ins,
			outs,
			e.tx.Creds,
			e.vm.AddStakerTxFee,
			e.vm.ctx.AVAXAssetID,
		); err != nil {
			return fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(maxFutureStartTime)
		if startTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.tx.ID()

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(e.tx)
	e.onCommit = state.NewDiff(e.parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	utxo.Consume(e.onCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onCommit, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	e.onAbort = state.NewDiff(e.parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	utxo.Consume(e.onAbort, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onAbort, txID, e.vm.ctx.AVAXAssetID, outs)

	e.prefersCommit = tx.StartTime().After(e.vm.clock.Time())
	return nil
}

func (e *proposalTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	// Verify the tx is well-formed
	if err := e.tx.SyntacticVerify(e.vm.ctx); err != nil {
		return err
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < e.vm.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.vm.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case len(e.tx.Creds) == 0:
		// Ensure there is at least one credential for the subnet authorization
		return errWrongNumberOfCredentials
	}

	currentStakers := e.parentState.CurrentStakers()
	pendingStakers := e.parentState.PendingStakers()

	if e.vm.bootstrapped.GetValue() {
		currentTimestamp := e.parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"validator's start time (%s) is at or after current chain timestamp (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		var vdrTx *txs.AddValidatorTx
		if err == nil {
			// This validator is attempting to validate with a currently
			// validing node.
			vdrTx, _ = currentValidator.AddValidatorTx()

			// Ensure that this transaction isn't a duplicate add validator tx.
			subnets := currentValidator.SubnetValidators()
			if _, validates := subnets[tx.Validator.Subnet]; validates {
				return fmt.Errorf(
					"already validating subnet %s",
					tx.Validator.Subnet,
				)
			}
		} else {
			// This validator is attempting to validate with a node that hasn't
			// started validating yet.
			vdrTx, _, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return errValidatorSubset
				}
				return fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return errValidatorSubset
		}

		// Ensure that this transaction isn't a duplicate add validator tx.
		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		subnets := pendingValidator.SubnetValidators()
		if _, validates := subnets[tx.Validator.Subnet]; validates {
			return fmt.Errorf(
				"already validating subnet %s",
				tx.Validator.Subnet,
			)
		}

		baseTxCredsLen := len(e.tx.Creds) - 1
		baseTxCreds := e.tx.Creds[:baseTxCredsLen]
		subnetCred := e.tx.Creds[baseTxCredsLen]

		subnetIntf, _, err := e.parentState.GetTx(tx.Validator.Subnet)
		if err != nil {
			if err == database.ErrNotFound {
				return errValidatorSubset
			}
			return fmt.Errorf(
				"couldn't find subnet %s with %w",
				tx.Validator.Subnet,
				err,
			)
		}

		subnet, ok := subnetIntf.Unsigned.(*txs.CreateSubnetTx)
		if !ok {
			return fmt.Errorf(
				"%s is not a subnet",
				tx.Validator.Subnet,
			)
		}

		if err := e.vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
			return err
		}

		// Verify the flowcheck
		if err := e.vm.utxoHandler.SemanticVerifySpend(
			tx,
			e.parentState,
			tx.Ins,
			tx.Outs,
			baseTxCreds,
			e.vm.TxFee,
			e.vm.ctx.AVAXAssetID,
		); err != nil {
			return err
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(maxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.tx.ID()

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(e.tx)
	e.onCommit = state.NewDiff(e.parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	utxo.Consume(e.onCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onCommit, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	e.onAbort = state.NewDiff(e.parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	utxo.Consume(e.onAbort, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onAbort, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	e.prefersCommit = tx.StartTime().After(e.vm.clock.Time())
	return nil
}

func (e *proposalTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	// Verify the tx is well-formed
	if err := e.tx.SyntacticVerify(e.vm.ctx); err != nil {
		return err
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < e.vm.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.vm.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case tx.Validator.Wght < e.vm.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	currentStakers := e.parentState.CurrentStakers()
	pendingStakers := e.parentState.PendingStakers()

	if e.vm.bootstrapped.GetValue() {
		currentTimestamp := e.parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		pendingDelegators := pendingValidator.Delegators()

		var (
			vdrTx                  *txs.AddValidatorTx
			currentDelegatorWeight uint64
			currentDelegators      []state.DelegatorAndID
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
			vdrTx, _, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return state.ErrDelegatorSubset
				}
				return fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this delegator delegates is a subset of the
		// time the validator validates.
		if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return state.ErrDelegatorSubset
		}

		// Ensure that the period this delegator delegates wouldn't become over
		// delegated.
		vdrWeight := vdrTx.Weight()
		currentWeight, err := math.Add64(vdrWeight, currentDelegatorWeight)
		if err != nil {
			return err
		}

		maximumWeight, err := math.Mul64(MaxValidatorWeightFactor, vdrWeight)
		if err != nil {
			return errStakeOverflow
		}

		if !currentTimestamp.Before(e.vm.ApricotPhase3Time) {
			maximumWeight = math.Min64(maximumWeight, e.vm.MaxValidatorStake)
		}

		canDelegate, err := state.CanDelegate(
			currentDelegators,
			pendingDelegators,
			tx,
			currentWeight,
			maximumWeight,
		)
		if err != nil {
			return err
		}
		if !canDelegate {
			return errOverDelegated
		}

		// Verify the flowcheck
		if err := e.vm.utxoHandler.SemanticVerifySpend(
			tx,
			e.parentState,
			tx.Ins,
			outs,
			e.tx.Creds,
			e.vm.AddStakerTxFee,
			e.vm.ctx.AVAXAssetID,
		); err != nil {
			return fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(maxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.tx.ID()

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(e.tx)
	e.onCommit = state.NewDiff(e.parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	utxo.Consume(e.onCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onCommit, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	e.onAbort = state.NewDiff(e.parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	utxo.Consume(e.onAbort, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onAbort, txID, e.vm.ctx.AVAXAssetID, outs)

	e.prefersCommit = tx.StartTime().After(e.vm.clock.Time())
	return nil
}

func (e *proposalTxExecutor) AdvanceTimeTx(tx *txs.AdvanceTimeTx) error {
	switch {
	case tx == nil:
		return txs.ErrNilTx
	case len(e.tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	txTimestamp := tx.Timestamp()
	localTimestamp := e.vm.clock.Time()
	localTimestampPlusSync := localTimestamp.Add(syncBound)
	if localTimestampPlusSync.Before(txTimestamp) {
		return fmt.Errorf(
			"proposed time (%s) is too far in the future relative to local time (%s)",
			txTimestamp,
			localTimestamp,
		)
	}

	if chainTimestamp := e.parentState.GetTimestamp(); !txTimestamp.After(chainTimestamp) {
		return fmt.Errorf(
			"proposed timestamp (%s), not after current timestamp (%s)",
			txTimestamp,
			chainTimestamp,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	nextStakerChangeTime, err := e.parentState.GetNextStakerChangeTime()
	if err != nil {
		return err
	}

	if txTimestamp.After(nextStakerChangeTime) {
		return fmt.Errorf(
			"proposed timestamp (%s) later than next staker change time (%s)",
			txTimestamp,
			nextStakerChangeTime,
		)
	}

	currentSupply := e.parentState.GetCurrentSupply()

	pendingStakers := e.parentState.PendingStakers()
	toAddValidatorsWithRewardToCurrent := []*state.ValidatorReward(nil)
	toAddDelegatorsWithRewardToCurrent := []*state.ValidatorReward(nil)
	toAddWithoutRewardToCurrent := []*txs.Tx(nil)
	numToRemoveFromPending := 0

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp. [pendingStakers.Stakers()] is sorted in order
	// of increasing startTime
pendingStakerLoop:
	for _, tx := range pendingStakers.Stakers() {
		switch staker := tx.Unsigned.(type) {
		case *txs.AddDelegatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := e.vm.rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = math.Add64(currentSupply, r)
			if err != nil {
				return err
			}

			toAddDelegatorsWithRewardToCurrent = append(toAddDelegatorsWithRewardToCurrent, &state.ValidatorReward{
				AddStakerTx:     tx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *txs.AddValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := e.vm.rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = math.Add64(currentSupply, r)
			if err != nil {
				return err
			}

			toAddValidatorsWithRewardToCurrent = append(toAddValidatorsWithRewardToCurrent, &state.ValidatorReward{
				AddStakerTx:     tx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *txs.AddSubnetValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			// If this staker should already be removed, then we should just
			// never add them.
			if staker.EndTime().After(txTimestamp) {
				toAddWithoutRewardToCurrent = append(toAddWithoutRewardToCurrent, tx)
			}
			numToRemoveFromPending++
		default:
			return fmt.Errorf("expected validator but got %T", tx.Unsigned)
		}
	}
	newlyPendingStakers := pendingStakers.DeleteStakers(numToRemoveFromPending)

	currentStakers := e.parentState.CurrentStakers()
	numToRemoveFromCurrent := 0

	// Remove from the staker set any subnet validators whose endTime is at or
	// before the new timestamp
currentStakerLoop:
	for _, tx := range currentStakers.Stakers() {
		switch staker := tx.Unsigned.(type) {
		case *txs.AddSubnetValidatorTx:
			if staker.EndTime().After(txTimestamp) {
				break currentStakerLoop
			}

			numToRemoveFromCurrent++
		case *txs.AddValidatorTx, *txs.AddDelegatorTx:
			// We shouldn't be removing any primary network validators here
			break currentStakerLoop
		default:
			return errWrongTxType
		}
	}
	newlyCurrentStakers, err := currentStakers.UpdateStakers(
		toAddValidatorsWithRewardToCurrent,
		toAddDelegatorsWithRewardToCurrent,
		toAddWithoutRewardToCurrent,
		numToRemoveFromCurrent,
	)
	if err != nil {
		return err
	}

	e.onCommit = state.NewDiff(e.parentState, newlyCurrentStakers, newlyPendingStakers)
	e.onCommit.SetTimestamp(txTimestamp)
	e.onCommit.SetCurrentSupply(currentSupply)

	// State doesn't change if this proposal is aborted
	e.onAbort = state.NewDiff(e.parentState, currentStakers, pendingStakers)

	e.prefersCommit = !txTimestamp.After(localTimestampPlusSync)
	return nil
}

func (e *proposalTxExecutor) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	switch {
	case tx == nil:
		return txs.ErrNilTx
	case tx.TxID == ids.Empty:
		return errInvalidID
	case len(e.tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	currentStakers := e.parentState.CurrentStakers()
	stakerTx, stakerReward, err := currentStakers.GetNextStaker()
	if err == database.ErrNotFound {
		return fmt.Errorf("failed to get next staker stop time: %w", err)
	}
	if err != nil {
		return err
	}

	stakerID := stakerTx.ID()
	if stakerID != tx.TxID {
		return fmt.Errorf(
			"attempting to remove TxID: %s. Should be removing %s",
			tx.TxID,
			stakerID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime := e.parentState.GetTimestamp()
	staker, ok := stakerTx.Unsigned.(txs.StakerTx)
	if !ok {
		return errWrongTxType
	}
	if endTime := staker.EndTime(); !endTime.Equal(currentTime) {
		return fmt.Errorf(
			"attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			endTime,
		)
	}

	newlyCurrentStakers, err := currentStakers.DeleteNextStaker()
	if err != nil {
		return err
	}

	pendingStakers := e.parentState.PendingStakers()
	e.onCommit = state.NewDiff(e.parentState, newlyCurrentStakers, pendingStakers)
	e.onAbort = state.NewDiff(e.parentState, newlyCurrentStakers, pendingStakers)

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply := e.onAbort.GetCurrentSupply()
	newSupply, err := math.Sub64(currentSupply, stakerReward)
	if err != nil {
		return err
	}
	e.onAbort.SetCurrentSupply(newSupply)

	var (
		nodeID    ids.NodeID
		startTime time.Time
	)
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case *txs.AddValidatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: e.vm.ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			e.onCommit.AddUTXO(utxo)
			e.onAbort.AddUTXO(utxo)
		}

		// Provide the reward here
		if stakerReward > 0 {
			outIntf, err := e.vm.fx.CreateOutput(stakerReward, uStakerTx.RewardsOwner)
			if err != nil {
				return fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return errInvalidState
			}

			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: e.vm.ctx.AVAXAssetID},
				Out:   out,
			}

			e.onCommit.AddUTXO(utxo)
			e.onCommit.AddRewardUTXO(tx.TxID, utxo)
		}

		// Handle reward preferences
		nodeID = uStakerTx.Validator.ID()
		startTime = uStakerTx.StartTime()
	case *txs.AddDelegatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: e.vm.ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			e.onCommit.AddUTXO(utxo)
			e.onAbort.AddUTXO(utxo)
		}

		// We're removing a delegator, so we need to fetch the validator they
		// are delgated to.
		vdr, err := currentStakers.GetValidator(uStakerTx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
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
		if optimisticReward, err := math.Mul64(delegatorShares, stakerReward); err == nil {
			delegatorReward = optimisticReward / reward.PercentDenominator
		}
		delegateeReward := stakerReward - delegatorReward // delegatorReward <= reward so no underflow

		offset := 0

		// Reward the delegator here
		if delegatorReward > 0 {
			outIntf, err := e.vm.fx.CreateOutput(delegatorReward, uStakerTx.RewardsOwner)
			if err != nil {
				return fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return errInvalidState
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: e.vm.ctx.AVAXAssetID},
				Out:   out,
			}

			e.onCommit.AddUTXO(utxo)
			e.onCommit.AddRewardUTXO(tx.TxID, utxo)

			offset++
		}

		// Reward the delegatee here
		if delegateeReward > 0 {
			outIntf, err := e.vm.fx.CreateOutput(delegateeReward, vdrTx.RewardsOwner)
			if err != nil {
				return fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return errInvalidState
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake) + offset),
				},
				Asset: avax.Asset{ID: e.vm.ctx.AVAXAssetID},
				Out:   out,
			}

			e.onCommit.AddUTXO(utxo)
			e.onCommit.AddRewardUTXO(tx.TxID, utxo)
		}

		nodeID = uStakerTx.Validator.ID()
		startTime = vdrTx.StartTime()
	default:
		return errShouldBeDSValidator
	}

	uptime, err := e.vm.uptimeManager.CalculateUptimePercentFrom(nodeID, startTime)
	if err != nil {
		return fmt.Errorf("failed to calculate uptime: %w", err)
	}

	e.prefersCommit = uptime >= e.vm.UptimePercentage
	return nil
}
