// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

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
	vm            *VM
	parentID      ids.ID
	stateVersions state.Versions
	tx            *txs.Tx

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

	parentState, ok := e.stateVersions.GetState(e.parentID)
	if !ok {
		return errInvalidBlockType
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if e.vm.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := tx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		_, err := GetValidator(parentState, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err == nil {
			return fmt.Errorf(
				"%s is already validating the primary network",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a primary network validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Verify the flowcheck
		if err := e.vm.utxoHandler.SemanticVerifySpend(
			tx,
			parentState,
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
	onCommit, err := state.NewDiff(e.parentID, e.vm.stateVersions)
	if err != nil {
		return err
	}
	e.onCommit = onCommit

	// Consume the UTXOS
	utxo.Consume(e.onCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onCommit, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	newStaker := state.NewPrimaryNetworkStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.PrimaryNetworkValidatorPendingPriority
	e.onCommit.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	onAbort, err := state.NewDiff(
		e.parentID,
		e.vm.stateVersions,
	)
	if err != nil {
		return err
	}
	e.onAbort = onAbort

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

	parentState, ok := e.stateVersions.GetState(e.parentID)
	if !ok {
		return errInvalidBlockType
	}

	if e.vm.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"validator's start time (%s) is at or after current chain timestamp (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		_, err := GetValidator(parentState, tx.Validator.Subnet, tx.Validator.NodeID)
		if err == nil {
			return fmt.Errorf(
				"%s is already validating the subnet",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a subnet validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		primaryNetworkValidator, err := GetValidator(parentState, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to fetch the primary network validator for %s: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if !tx.Validator.BoundedBy(primaryNetworkValidator.StartTime, primaryNetworkValidator.EndTime) {
			return errValidatorSubset
		}

		baseTxCredsLen := len(e.tx.Creds) - 1
		baseTxCreds := e.tx.Creds[:baseTxCredsLen]
		subnetCred := e.tx.Creds[baseTxCredsLen]

		subnetIntf, _, err := parentState.GetTx(tx.Validator.Subnet)
		if err != nil {
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
			parentState,
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
	onCommit, err := state.NewDiff(e.parentID, e.stateVersions)
	if err != nil {
		return err
	}
	e.onCommit = onCommit

	// Consume the UTXOS
	utxo.Consume(e.onCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onCommit, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	newStaker := state.NewSubnetStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.SubnetValidatorPendingPriority
	e.onCommit.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	onAbort, err := state.NewDiff(e.parentID, e.stateVersions)
	if err != nil {
		return err
	}
	e.onAbort = onAbort

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

	parentState, ok := e.stateVersions.GetState(e.parentID)
	if !ok {
		return errInvalidBlockType
	}

	txID := e.tx.ID()

	newStaker := state.NewPrimaryNetworkStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.PrimaryNetworkDelegatorPendingPriority
	e.onCommit.PutPendingDelegator(newStaker)

	if e.vm.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		primaryNetworkValidator, err := GetValidator(parentState, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to fetch the primary network validator for %s: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		maximumStake, err := math.Mul64(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
		if err != nil {
			return errStakeOverflow
		}

		if !currentTimestamp.Before(e.vm.ApricotPhase3Time) {
			maximumStake = math.Min64(maximumStake, e.vm.MaxValidatorStake)
		}

		canDelegate, err := CanDelegate2(parentState, primaryNetworkValidator, maximumStake, newStaker)
		if err != nil {
			return err
		}
		if !canDelegate {
			return errOverDelegated
		}

		// Ensure that the period this delegator delegates is a subset of the
		// time the validator validates.
		if !tx.Validator.BoundedBy(primaryNetworkValidator.StartTime, primaryNetworkValidator.EndTime) {
			return state.ErrDelegatorSubset
		}

		currentDelegators, err := parentState.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to fetch current delegators for %s: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		pendingDelegators, err := parentState.GetPendingDelegatorIterator(constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to fetch current delegators for %s: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Ensure that the period this delegator delegates wouldn't become over
		// delegated.
		vdrWeight := vdrTx.Weight()
		currentWeight, err := math.Add64(vdrWeight, currentDelegatorWeight)
		if err != nil {
			return err
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
			parentState,
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

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(e.tx)
	onCommit, err := state.NewDiffWithValidators(
		e.parentID,
		e.stateVersions,
		currentStakers,
		newlyPendingStakers,
	)
	if err != nil {
		return err
	}
	e.onCommit = onCommit

	// Consume the UTXOS
	utxo.Consume(e.onCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onCommit, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	onAbort, err := state.NewDiffWithValidators(
		e.parentID,
		e.stateVersions,
		currentStakers,
		pendingStakers,
	)
	if err != nil {
		return err
	}
	e.onAbort = onAbort

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

	parentState, ok := e.stateVersions.GetState(e.parentID)
	if !ok {
		return errInvalidBlockType
	}

	if chainTimestamp := parentState.GetTimestamp(); !txTimestamp.After(chainTimestamp) {
		return fmt.Errorf(
			"proposed timestamp (%s), not after current timestamp (%s)",
			txTimestamp,
			chainTimestamp,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	nextStakerChangeTime, err := parentState.GetNextStakerChangeTime()
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

	currentSupply := parentState.GetCurrentSupply()

	pendingStakers := parentState.PendingStakers()
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

	currentStakers := parentState.CurrentStakers()
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

	e.onCommit, err = state.NewDiffWithValidators(
		e.parentID,
		e.stateVersions,
		newlyCurrentStakers,
		newlyPendingStakers,
	)
	if err != nil {
		return err
	}

	e.onCommit.SetTimestamp(txTimestamp)
	e.onCommit.SetCurrentSupply(currentSupply)

	// State doesn't change if this proposal is aborted
	e.onAbort, err = state.NewDiffWithValidators(
		e.parentID,
		e.stateVersions,
		currentStakers,
		pendingStakers,
	)
	if err != nil {
		return err
	}

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

	parentState, ok := e.stateVersions.GetState(e.parentID)
	if !ok {
		return errInvalidBlockType
	}

	currentStakers := parentState.CurrentStakers()
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
	currentTime := parentState.GetTimestamp()
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

	pendingStakers := parentState.PendingStakers()
	e.onCommit, err = state.NewDiffWithValidators(e.parentID, e.vm.stateVersions, newlyCurrentStakers, pendingStakers)
	if err != nil {
		return err
	}

	e.onAbort, err = state.NewDiffWithValidators(e.parentID, e.vm.stateVersions, newlyCurrentStakers, pendingStakers)
	if err != nil {
		return err
	}

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

func GetNextStakerChangeTime(state state.Chain) (time.Time, error) {
	currentStakerIterator, err := state.GetCurrentStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer currentStakerIterator.Release()

	pendingStakers, err := state.GetPendingStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer pendingStakers.Release()

	earliest := mockable.MaxTime
	if currentStakerIterator.Next() {
		earliest = currentStakerIterator.Value().EndTime
	}
	if pendingStakers.Next() {
		startTime := pendingStakers.Value().StartTime
		if startTime.Before(earliest) {
			earliest = startTime
		}
	}
	return earliest, nil
}

func GetValidator(state state.Chain, subnetID ids.ID, nodeID ids.NodeID) (*state.Staker, error) {
	validator, err := state.GetCurrentValidator(subnetID, nodeID)
	if err == nil {
		// This node is currently validating the subnet.
		return validator, nil
	}
	if err != database.ErrNotFound {
		// Unexpected error occurred.
		return nil, err
	}
	return state.GetPendingValidator(subnetID, nodeID)
}

func CanDelegate(
	state state.Chain,
	validator *state.Staker,
	weightLimit uint64,
	delegator *state.Staker,
) (bool, error) {
	if delegator.StartTime.Before(validator.StartTime) {
		return false, nil
	}
	if delegator.EndTime.After(validator.EndTime) {
		return false, nil
	}

	maxWeight, err := getMaxWeight(state, validator, delegator.StartTime, delegator.EndTime)
	if err != nil {
		return false, err
	}
	return maxWeight <= weightLimit, nil
}

func getMaxWeight(
	state state.Chain,
	validator *state.Staker,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	// Keep track of which delegators should be removed next so that we can
	// efficiently remove delegators and keep the current stake updated.
	toRemoveHeap := validator.EndTimeHeap{}
	for _, currentDelegator := range current {
		toRemoveHeap.Add(&currentDelegator.Tx.Validator)
	}

	var (
		err error
		// [maxStake] is the max stake at any point between now [starTime] and [endTime]
		maxStake uint64
	)

	// Calculate what the amount staked will be when each pending delegation
	// starts.
	for _, nextPending := range pending { // Iterates in order of increasing start time
		// Calculate what the amount staked will be when this delegation starts.
		nextPendingStartTime := nextPending.Tx.StartTime()

		if nextPendingStartTime.After(endTime) {
			// This delegation starts after [endTime].
			// Since we're calculating the max amount staked in
			// [startTime, endTime], we can stop. (Recall that [pending] is
			// sorted in order of increasing end time.)
			break
		}

		// Subtract from [currentStake] all of the current delegations that will
		// have ended by the time that the delegation [nextPending] starts.
		for toRemoveHeap.Len() > 0 {
			// Get the next current delegation that will end.
			toRemove := toRemoveHeap.Peek()
			toRemoveEndTime := toRemove.EndTime()
			if toRemoveEndTime.After(nextPendingStartTime) {
				break
			}
			// This current delegation [toRemove] ends before [nextPending]
			// starts, so its stake should be subtracted from [currentStake].

			// Changed in AP3:
			// If the new delegator has started, then this current delegator
			// should have an end time that is > [startTime].
			newDelegatorHasStartedBeforeFinish := toRemoveEndTime.After(startTime)
			if newDelegatorHasStartedBeforeFinish && currentStake > maxStake {
				// Only update [maxStake] if it's after [startTime]
				maxStake = currentStake
			}

			currentStake, err = math.Sub64(currentStake, toRemove.Wght)
			if err != nil {
				return 0, err
			}

			// Changed in AP3:
			// Remove the delegator from the heap and update the heap so that
			// the top of the heap is the next delegator to remove.
			toRemoveHeap.Remove()
		}

		// Add to [currentStake] the stake of this pending delegator to
		// calculate what the stake will be when this pending delegation has
		// started.
		currentStake, err = math.Add64(currentStake, nextPending.Tx.Validator.Wght)
		if err != nil {
			return 0, err
		}

		// Changed in AP3:
		// If the new delegator has started, then this pending delegator should
		// have a start time that is >= [startTime]. Otherwise, the delegator
		// hasn't started yet and the [currentStake] shouldn't count towards the
		// [maximumStake] during the delegators delegation period.
		newDelegatorHasStarted := !nextPendingStartTime.Before(startTime)
		if newDelegatorHasStarted && currentStake > maxStake {
			// Only update [maxStake] if it's after [startTime]
			maxStake = currentStake
		}

		// This pending delegator is a current delegator relative
		// when considering later pending delegators that start late
		toRemoveHeap.Add(&nextPending.Tx.Validator)
	}

	// [currentStake] is now the amount staked before the next pending delegator
	// whose start time is after [endTime].

	// If there aren't any delegators that will be added before the end of our
	// delegation period, we should advance through time until our delegation
	// period starts.
	for toRemoveHeap.Len() > 0 {
		toRemove := toRemoveHeap.Peek()
		toRemoveEndTime := toRemove.EndTime()
		if toRemoveEndTime.After(startTime) {
			break
		}

		currentStake, err = math.Sub64(currentStake, toRemove.Wght)
		if err != nil {
			return 0, err
		}

		// Changed in AP3:
		// Remove the delegator from the heap and update the heap so that the
		// top of the heap is the next delegator to remove.
		toRemoveHeap.Remove()
	}

	// We have advanced time to be inside the delegation window.
	// Make sure that the max stake is updated accordingly.
	if currentStake > maxStake {
		maxStake = currentStake
	}
	return maxStake, nil
}
