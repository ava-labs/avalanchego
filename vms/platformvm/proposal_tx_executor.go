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

		canDelegate, err := CanDelegate(parentState, primaryNetworkValidator, maximumStake, newStaker)
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
	onCommit, err := state.NewDiff(e.parentID, e.stateVersions)
	if err != nil {
		return err
	}
	e.onCommit = onCommit

	// Consume the UTXOS
	utxo.Consume(e.onCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.onCommit, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	e.onCommit.PutPendingDelegator(newStaker)

	// Set up the state if this tx is aborted
	onAbort, err := state.NewDiff(e.parentID, e.stateVersions)
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
	nextStakerChangeTime, err := GetNextStakerChangeTime(parentState)
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

	pendingStakerIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return err
	}

	var (
		currentSupply             = parentState.GetCurrentSupply()
		currentValidatorsToAdd    []*state.Staker
		pendingValidatorsToRemove []*state.Staker
		currentDelegatorsToAdd    []*state.Staker
		pendingDelegatorsToRemove []*state.Staker
	)

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp
	for pendingStakerIterator.Next() {
		stakerToRemove := pendingStakerIterator.Value()
		if stakerToRemove.StartTime.After(txTimestamp) {
			break
		}

		stakerToAdd := *stakerToRemove
		stakerToAdd.NextTime = stakerToRemove.EndTime
		stakerToAdd.Priority = state.PendingToCurrentPriorities[stakerToRemove.Priority]

		switch stakerToRemove.Priority {
		case state.PrimaryNetworkDelegatorPendingPriority:
			potentialReward := e.vm.rewards.Calculate(
				stakerToRemove.EndTime.Sub(stakerToRemove.StartTime),
				stakerToRemove.Weight,
				currentSupply,
			)
			currentSupply, err = math.Add64(currentSupply, potentialReward)
			if err != nil {
				pendingStakerIterator.Release()
				return err
			}

			stakerToAdd.PotentialReward = potentialReward

			currentDelegatorsToAdd = append(currentDelegatorsToAdd, &stakerToAdd)
			pendingDelegatorsToRemove = append(pendingDelegatorsToRemove, stakerToRemove)
		case state.PrimaryNetworkValidatorPendingPriority:
			potentialReward := e.vm.rewards.Calculate(
				stakerToRemove.EndTime.Sub(stakerToRemove.StartTime),
				stakerToRemove.Weight,
				currentSupply,
			)
			currentSupply, err = math.Add64(currentSupply, potentialReward)
			if err != nil {
				pendingStakerIterator.Release()
				return err
			}

			stakerToAdd.PotentialReward = potentialReward

			currentValidatorsToAdd = append(currentValidatorsToAdd, &stakerToAdd)
			pendingValidatorsToRemove = append(pendingValidatorsToRemove, stakerToRemove)
		case state.SubnetValidatorPendingPriority:
			currentValidatorsToAdd = append(currentValidatorsToAdd, &stakerToAdd)
			pendingValidatorsToRemove = append(pendingValidatorsToRemove, stakerToRemove)
		default:
			pendingStakerIterator.Release()
			return fmt.Errorf("expected staker priority got %d", stakerToRemove.Priority)
		}
	}
	pendingStakerIterator.Release()

	currentStakerIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var currentValidatorsToRemove []*state.Staker
	for currentStakerIterator.Next() {
		stakerToRemove := currentStakerIterator.Value()
		if stakerToRemove.EndTime.After(txTimestamp) {
			break
		}

		if stakerToRemove.PotentialReward != 0 {
			// We shouldn't be removing any permissionless stakers here
			break
		}

		currentValidatorsToRemove = append(currentValidatorsToRemove, stakerToRemove)
	}
	currentStakerIterator.Release()

	e.onCommit, err = state.NewDiff(e.parentID, e.stateVersions)
	if err != nil {
		return err
	}

	e.onCommit.SetTimestamp(txTimestamp)
	e.onCommit.SetCurrentSupply(currentSupply)

	for _, currentValidatorToAdd := range currentValidatorsToAdd {
		e.onCommit.PutCurrentValidator(currentValidatorToAdd)
	}
	for _, pendingValidatorToRemove := range pendingValidatorsToRemove {
		e.onCommit.DeletePendingValidator(pendingValidatorToRemove)
	}
	for _, currentDelegatorToAdd := range currentDelegatorsToAdd {
		e.onCommit.PutCurrentDelegator(currentDelegatorToAdd)
	}
	for _, pendingDelegatorToRemove := range pendingDelegatorsToRemove {
		e.onCommit.DeletePendingDelegator(pendingDelegatorToRemove)
	}
	for _, currentValidatorToRemove := range currentValidatorsToRemove {
		e.onCommit.DeleteCurrentValidator(currentValidatorToRemove)
	}

	// State doesn't change if this proposal is aborted
	e.onAbort, err = state.NewDiff(e.parentID, e.stateVersions)
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

	currentStakerIterator, err := parentState.GetCurrentStakerIterator()
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
			"attempting to remove TxID: %s. Should be removing %s",
			tx.TxID,
			stakerToRemove.TxID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime := parentState.GetTimestamp()
	if !stakerToRemove.EndTime.Equal(currentTime) {
		return fmt.Errorf(
			"attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			stakerToRemove.EndTime,
		)
	}

	stakerTx, _, err := parentState.GetTx(stakerToRemove.TxID)
	if err == database.ErrNotFound {
		return fmt.Errorf("failed to get next staker stop time: %w", err)
	}
	if err != nil {
		return err
	}

	e.onCommit, err = state.NewDiff(e.parentID, e.vm.stateVersions)
	if err != nil {
		return err
	}

	e.onCommit.DeleteCurrentValidator(stakerToRemove)

	e.onAbort, err = state.NewDiff(e.parentID, e.vm.stateVersions)
	if err != nil {
		return err
	}

	e.onAbort.DeleteCurrentValidator(stakerToRemove)

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply := e.onAbort.GetCurrentSupply()
	newSupply, err := math.Sub64(currentSupply, stakerToRemove.PotentialReward)
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
		if stakerToRemove.PotentialReward > 0 {
			outIntf, err := e.vm.fx.CreateOutput(stakerToRemove.PotentialReward, uStakerTx.RewardsOwner)
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
		vdrStaker, err := parentState.GetCurrentValidator(constants.PrimaryNetworkID, uStakerTx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				uStakerTx.Validator.NodeID,
				err,
			)
		}

		vdrTxIntf, _, err := parentState.GetTx(vdrStaker.TxID)
		if err != nil {
			return fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				uStakerTx.Validator.NodeID,
				err,
			)
		}

		vdrTx, ok := vdrTxIntf.Unsigned.(*txs.AddValidatorTx)
		if !ok {
			return errWrongTxType
		}

		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		delegatorShares := reward.PercentDenominator - uint64(vdrTx.Shares)                               // parentTx.Shares <= reward.PercentDenominator so no underflow
		delegatorReward := delegatorShares * (stakerToRemove.PotentialReward / reward.PercentDenominator) // delegatorShares <= reward.PercentDenominator so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := math.Mul64(delegatorShares, stakerToRemove.PotentialReward); err == nil {
			delegatorReward = optimisticReward / reward.PercentDenominator
		}
		delegateeReward := stakerToRemove.PotentialReward - delegatorReward // delegatorReward <= reward so no underflow

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

	maxWeight, err := GetMaxWeight(state, validator, delegator.StartTime, delegator.EndTime)
	if err != nil {
		return false, err
	}
	newMaxWeight, err := math.Add64(maxWeight, delegator.Weight)
	if err != nil {
		return false, err
	}
	return newMaxWeight <= weightLimit, nil
}

func GetMaxWeight(
	chainState state.Chain,
	validator *state.Staker,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	currentDelegatorIterator, err := chainState.GetCurrentDelegatorIterator(validator.SubnetID, validator.NodeID)
	if err != nil {
		return 0, err
	}

	// TODO: We can optimize this by moving the current total weight to be
	//       stored in the validator state.
	currentWeight := validator.Weight
	for currentDelegatorIterator.Next() {
		currentDelegator := currentDelegatorIterator.Value()

		currentWeight, err = math.Add64(currentWeight, currentDelegator.Weight)
		if err != nil {
			currentDelegatorIterator.Release()
			return 0, err
		}
	}
	currentDelegatorIterator.Release()

	currentDelegatorIterator, err = chainState.GetCurrentDelegatorIterator(validator.SubnetID, validator.NodeID)
	if err != nil {
		return 0, err
	}
	pendingDelegatorIterator, err := chainState.GetPendingDelegatorIterator(validator.SubnetID, validator.NodeID)
	if err != nil {
		currentDelegatorIterator.Release()
		return 0, err
	}
	delegatorChangesIterator := state.NewStakerDiffIterator(currentDelegatorIterator, pendingDelegatorIterator)
	defer delegatorChangesIterator.Release()

	var currentMax uint64
	for delegatorChangesIterator.Next() {
		delegator, isAdded := delegatorChangesIterator.Value()
		if delegator.NextTime.After(endTime) {
			// This delegation change (and all following changes) occur after
			// [endTime]. Since we're calculating the max amount staked in
			// [startTime, endTime], we can stop.
			break
		}

		if !delegator.NextTime.Before(startTime) {
			// We have advanced time to be at the inside of the delegation
			// window. Make sure that the max weight is updated accordingly.
			currentMax = math.Max64(currentMax, currentWeight)
		}

		var op func(uint64, uint64) (uint64, error)
		if isAdded {
			op = math.Add64
		} else {
			op = math.Sub64
		}
		currentWeight, err = op(currentWeight, delegator.Weight)
		if err != nil {
			return 0, err
		}
	}
	// We have advanced time to be at the end of the delegation window. Make
	// sure that the max weight is updated accordingly.
	return math.Max64(currentMax, currentWeight), nil
}
