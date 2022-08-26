// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	errWeightTooSmall            = errors.New("weight of this validator is too low")
	errWeightTooLarge            = errors.New("weight of this validator is too large")
	errInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
	errStakeTooShort             = errors.New("staking period is too short")
	errStakeTooLong              = errors.New("staking period is too long")
	errFutureStakeTime           = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	errWrongNumberOfCredentials  = errors.New("should have the same number of credentials as inputs")
	errValidatorSubset           = errors.New("all subnets' staking period must be a subset of the primary network")
	errStakeOverflow             = errors.New("validator stake exceeds limit")
	errOverDelegated             = errors.New("validator would be over delegated")
)

// verifyAddValidatorTx carries out the validation for an AddValidatorTx.
// It returns the tx outputs that should be returned if this validator is not
// added to the staking set.
func verifyAddValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddValidatorTx,
) (
	[]*avax.TransferableOutput,
	error,
) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	duration := tx.Validator.Duration()

	switch {
	case tx.Validator.Wght < backend.Config.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, errWeightTooSmall

	case tx.Validator.Wght > backend.Config.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return nil, errWeightTooLarge

	case tx.Shares < backend.Config.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return nil, errInsufficientDelegationFee

	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, errStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, errStakeTooLong
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if !backend.Bootstrapped.GetValue() {
		return outs, nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current time
	startTime := tx.StartTime()
	if !currentTimestamp.Before(startTime) {
		return nil, fmt.Errorf(
			"validator's start time (%s) at or before current timestamp (%s)",
			startTime,
			currentTimestamp,
		)
	}

	_, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err == nil {
		return nil, fmt.Errorf(
			"attempted to issue duplicate validation for %s",
			tx.Validator.NodeID,
		)
	}
	if err != database.ErrNotFound {
		return nil, fmt.Errorf(
			"failed to find whether %s is a primary network validator: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.AddStakerTxFee,
		},
	); err != nil {
		return nil, fmt.Errorf("failed verifySpend: %w", err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return nil, errFutureStakeTime
	}

	return outs, nil
}

// verifyAddSubnetValidatorTx carries out the validation for an
// AddSubnetValidatorTx.
func verifyAddSubnetValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddSubnetValidatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case len(sTx.Creds) == 0:
		// Ensure there is at least one credential for the subnet authorization
		return errWrongNumberOfCredentials
	}

	if !backend.Bootstrapped.GetValue() {
		return nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current timestamp
	validatorStartTime := tx.StartTime()
	if !currentTimestamp.Before(validatorStartTime) {
		return fmt.Errorf(
			"validator's start time (%s) is at or after current chain timestamp (%s)",
			currentTimestamp,
			validatorStartTime,
		)
	}

	_, err := GetValidator(chainState, tx.Validator.Subnet, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"attempted to issue duplicate subnet validation for %s",
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

	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
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

	baseTxCredsLen := len(sTx.Creds) - 1
	baseTxCreds := sTx.Creds[:baseTxCredsLen]
	subnetCred := sTx.Creds[baseTxCredsLen]

	subnetIntf, _, err := chainState.GetTx(tx.Validator.Subnet)
	if err != nil {
		return fmt.Errorf(
			"couldn't find subnet %q: %w",
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

	if err := backend.Fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return err
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.TxFee,
		},
	); err != nil {
		return err
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if validatorStartTime.After(maxStartTime) {
		return errFutureStakeTime
	}

	return nil
}

// verifyAddDelegatorTx carries out the validation for an AddDelegatorTx.
// It returns the tx outputs that should be returned if this delegator is not
// added to the staking set.
func verifyAddDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddDelegatorTx,
) (
	[]*avax.TransferableOutput,
	error,
) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, errStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, errStakeTooLong

	case tx.Validator.Wght < backend.Config.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, errWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if !backend.Bootstrapped.GetValue() {
		return outs, nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current timestamp
	validatorStartTime := tx.StartTime()
	if !currentTimestamp.Before(validatorStartTime) {
		return nil, fmt.Errorf(
			"chain timestamp (%s) not before validator's start time (%s)",
			currentTimestamp,
			validatorStartTime,
		)
	}

	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	maximumWeight, err := math.Mul64(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
	if err != nil {
		return nil, errStakeOverflow
	}

	if backend.Config.IsApricotPhase3Activated(currentTimestamp) {
		maximumWeight = math.Min64(maximumWeight, backend.Config.MaxValidatorStake)
	}

	txID := sTx.ID()

	newStaker := state.NewPrimaryNetworkStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.PrimaryNetworkDelegatorPendingPriority

	canDelegate, err := canDelegate(chainState, primaryNetworkValidator, maximumWeight, newStaker)
	if err != nil {
		return nil, err
	}
	if !canDelegate {
		return nil, errOverDelegated
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.AddStakerTxFee,
		},
	); err != nil {
		return nil, fmt.Errorf("failed verifySpend: %w", err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if validatorStartTime.After(maxStartTime) {
		return nil, errFutureStakeTime
	}

	return outs, nil
}
