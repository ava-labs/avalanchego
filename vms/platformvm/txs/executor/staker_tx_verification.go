// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	stdmath "math"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	ErrWeightTooSmall                              = errors.New("weight of this validator is too low")
	ErrWeightTooLarge                              = errors.New("weight of this validator is too large")
	ErrInsufficientDelegationFee                   = errors.New("staker charges an insufficient delegation fee")
	ErrStakeTooShort                               = errors.New("staking period is too short")
	ErrStakeTooLong                                = errors.New("staking period is too long")
	ErrFlowCheckFailed                             = errors.New("flow check failed")
	ErrFutureStakeTime                             = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrValidatorSubset                             = errors.New("all subnets' staking period must be a subset of the primary network")
	ErrNotValidator                                = errors.New("isn't a current or pending validator")
	ErrRemovePermissionlessValidator               = errors.New("attempting to remove permissionless validator")
	ErrStakeOverflow                               = errors.New("validator stake exceeds limit")
	ErrPeriodMismatch                              = errors.New("proposed staking period is not inside dependant staking period")
	ErrOverDelegated                               = errors.New("validator would be over delegated")
	ErrIsNotTransformSubnetTx                      = errors.New("is not a transform subnet tx")
	ErrTimestampNotBeforeStartTime                 = errors.New("chain timestamp not before start time")
	ErrAlreadyValidator                            = errors.New("already a validator")
	ErrDuplicateValidator                          = errors.New("duplicate validator")
	ErrDelegateToPermissionedValidator             = errors.New("delegation to permissioned validator")
	ErrWrongStakedAssetID                          = errors.New("incorrect staked assetID")
	ErrUnauthorizedStakerStopping                  = errors.New("unauthorized staker stopping")
	ErrTxUnacceptableBeforeFork                    = errors.New("tx unacceptable before fork")
	ErrSubnetValidatorToContinuousValidator        = errors.New("cannot assign subnet validator to continuous validator")
	ErrFiniteDelegatorToContinuousValidator        = errors.New("cannot assign finite delegator to continuous validator")
	ErrContinuousDelegatorToNonContinuousValidator = errors.New("cannot assign continuous delegator to non continuous validator")
	ErrNoStakerToStop                              = errors.New("could not find staker to stop")
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

	stakingPeriod := tx.StakingPeriod()
	switch {
	case tx.Validator.Wght < backend.Config.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, ErrWeightTooSmall

	case tx.Validator.Wght > backend.Config.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return nil, ErrWeightTooLarge

	case tx.DelegationShares < backend.Config.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return nil, ErrInsufficientDelegationFee

	case stakingPeriod < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, ErrStakeTooShort

	case stakingPeriod > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, ErrStakeTooLong
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	if !backend.Bootstrapped.Get() {
		return outs, nil
	}

	// Pre Continuous Staking fork activation, start time must be after current chain time.
	// Post Continuous Staking fork activation, only staking period matters, hence start time
	// is not validated
	var (
		currentTimestamp              = chainState.GetTimestamp()
		preContinuousStakingStartTime = tx.StartTime()
		isContinuousStakingForkActive = backend.Config.IsContinuousStakingActivated(currentTimestamp)
	)
	if !isContinuousStakingForkActive {
		if !currentTimestamp.Before(preContinuousStakingStartTime) {
			return nil, fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				currentTimestamp,
				preContinuousStakingStartTime,
			)
		}
	}

	_, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err == nil {
		return nil, fmt.Errorf(
			"%s is %w of the primary network",
			tx.Validator.NodeID,
			ErrAlreadyValidator,
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
			backend.Ctx.AVAXAssetID: backend.Config.AddPrimaryNetworkValidatorFee,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	if !isContinuousStakingForkActive {
		// Make sure the tx doesn't start too far in the future. This is done last
		// to allow the verifier visitor to explicitly check for this error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if preContinuousStakingStartTime.After(maxStartTime) {
			return nil, ErrFutureStakeTime
		}
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

	stakingPeriod := tx.StakingPeriod()
	switch {
	case stakingPeriod < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case stakingPeriod > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	// Pre Continuous Staking fork activation, start time must be after current chain time.
	// Post Continuous Staking fork activation, only staking period matters, hence start time
	// is not validated
	var (
		currentTimestamp              = chainState.GetTimestamp()
		preContinuousStakingStartTime = tx.StartTime()
		isContinuousStakingForkActive = backend.Config.IsContinuousStakingActivated(currentTimestamp)
	)
	if !isContinuousStakingForkActive {
		if !currentTimestamp.Before(preContinuousStakingStartTime) {
			return fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				currentTimestamp,
				preContinuousStakingStartTime,
			)
		}
	}

	_, err := GetValidator(chainState, tx.SubnetValidator.Subnet, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"attempted to issue %w for %s on subnet %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.SubnetValidator.Subnet,
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
	if err == database.ErrNotFound {
		return fmt.Errorf(
			"%s %w of the primary network",
			tx.Validator.NodeID,
			ErrNotValidator,
		)
	}
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			tx.Validator.NodeID,
			err,
		)
	}
	if primaryNetworkValidator.Priority.IsContinuousValidator() {
		// TODO: consider activation in subsequent PRs
		return ErrSubnetValidatorToContinuousValidator
	}

	// Ensure that the period this validator validates the specified subnet
	// is a subset of the time they validate the primary network.
	stakerStart := currentTimestamp
	stakerEnd := stakerStart.Add(tx.StakingPeriod())
	if !isContinuousStakingForkActive {
		stakerStart = preContinuousStakingStartTime
		stakerEnd = tx.EndTime()
	}
	if !txs.BoundedBy(
		stakerStart,
		stakerEnd,
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return ErrPeriodMismatch
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(backend, chainState, sTx, tx.SubnetValidator.Subnet, tx.SubnetAuth)
	if err != nil {
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
			backend.Ctx.AVAXAssetID: backend.Config.AddSubnetValidatorFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	if !isContinuousStakingForkActive {
		// Make sure the tx doesn't start too far in the future. This is done last
		// to allow the verifier visitor to explicitly check for this error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if preContinuousStakingStartTime.After(maxStartTime) {
			return ErrFutureStakeTime
		}
	}

	return nil
}

// Returns the representation of [tx.NodeID] validating [tx.Subnet].
// Returns true if [tx.NodeID] is a current validator of [tx.Subnet].
// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * [tx.NodeID] is a current/pending PoA validator of [tx.Subnet].
// * [sTx]'s creds authorize it to spend the stated inputs.
// * [sTx]'s creds authorize it to remove a validator from [tx.Subnet].
// * The flow checker passes.
func removeSubnetValidatorValidation(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.RemoveSubnetValidatorTx,
) (*state.Staker, bool, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, false, err
	}

	isCurrentValidator := true
	vdr, err := chainState.GetCurrentValidator(tx.Subnet, tx.NodeID)
	if err == database.ErrNotFound {
		vdr, err = chainState.GetPendingValidator(tx.Subnet, tx.NodeID)
		isCurrentValidator = false
	}
	if err != nil {
		// It isn't a current or pending validator.
		return nil, false, fmt.Errorf(
			"%s %w of %s: %v",
			tx.NodeID,
			ErrNotValidator,
			tx.Subnet,
			err,
		)
	}

	if !vdr.Priority.IsPermissionedValidator() {
		return nil, false, ErrRemovePermissionlessValidator
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return vdr, isCurrentValidator, nil
	}

	baseTxCreds, err := verifySubnetAuthorization(backend, chainState, sTx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return nil, false, err
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
		return nil, false, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	return vdr, isCurrentValidator, nil
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

	stakingPeriod := tx.StakingPeriod()
	switch {
	case stakingPeriod < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, ErrStakeTooShort

	case stakingPeriod > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, ErrStakeTooLong

	case tx.Validator.Wght < backend.Config.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, ErrWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	if !backend.Bootstrapped.Get() {
		return outs, nil
	}

	// Pre Continuous Staking fork activation, start time must be after current chain time.
	// Post Continuous Staking fork activation, only staking period matters, hence start time
	// is not validated
	var (
		currentTimestamp              = chainState.GetTimestamp()
		preContinuousStakingStartTime = tx.StartTime()
		isContinuousStakingForkActive = backend.Config.IsContinuousStakingActivated(currentTimestamp)
	)
	if !isContinuousStakingForkActive {
		if !currentTimestamp.Before(preContinuousStakingStartTime) {
			return nil, fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				currentTimestamp,
				preContinuousStakingStartTime,
			)
		}
	}

	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			tx.Validator.NodeID,
			err,
		)
	}
	if primaryNetworkValidator.Priority.IsContinuousValidator() {
		// TODO: consider activation in subsequent PRs
		return nil, ErrFiniteDelegatorToContinuousValidator
	}

	maximumWeight, err := math.Mul64(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
	if err != nil {
		return nil, ErrStakeOverflow
	}

	if backend.Config.IsApricotPhase3Activated(currentTimestamp) {
		maximumWeight = math.Min(maximumWeight, backend.Config.MaxValidatorStake)
	}

	txID := sTx.ID()
	var newStaker *state.Staker
	if isContinuousStakingForkActive {
		// potential reward does not matter
		newStaker, err = state.NewCurrentStaker(txID, tx, currentTimestamp, currentTimestamp.Add(tx.StakingPeriod()), 0)
	} else {
		newStaker, err = state.NewPendingStaker(txID, tx)
	}
	if err != nil {
		return nil, err
	}

	if !txs.BoundedBy(
		newStaker.StartTime,
		newStaker.EndTime,
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return nil, ErrPeriodMismatch
	}
	overDelegated, err := overDelegated(chainState, primaryNetworkValidator, maximumWeight, newStaker)
	if err != nil {
		return nil, err
	}
	if overDelegated {
		return nil, ErrOverDelegated
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.AddPrimaryNetworkDelegatorFee,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	if !isContinuousStakingForkActive {
		// Make sure the tx doesn't start too far in the future. This is done last
		// to allow the verifier visitor to explicitly check for this error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if preContinuousStakingStartTime.After(maxStartTime) {
			return nil, ErrFutureStakeTime
		}
	}

	return outs, nil
}

// verifyAddPermissionlessValidatorTx carries out the validation for an
// AddPermissionlessValidatorTx.
func verifyAddPermissionlessValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessValidatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	// Pre Continuous Staking fork activation, start time must be after current chain time.
	// Post Continuous Staking fork activation, only staking period matters, hence start time
	// is not validated
	var (
		currentTimestamp              = chainState.GetTimestamp()
		preContinuousStakingStartTime = tx.StartTime()
		isContinuousStakingForkActive = backend.Config.IsContinuousStakingActivated(currentTimestamp)
	)
	if !isContinuousStakingForkActive {
		if !currentTimestamp.Before(preContinuousStakingStartTime) {
			return fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				currentTimestamp,
				preContinuousStakingStartTime,
			)
		}
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return fmt.Errorf("failed retrieving validator rules for subnet %v: %w", tx.Subnet, err)
	}

	var (
		stakingPeriod = tx.StakingPeriod()
		stakedAssetID = tx.StakeOuts[0].AssetID()
	)
	switch {
	case tx.Validator.Wght < validatorRules.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return ErrWeightTooSmall

	case tx.Validator.Wght > validatorRules.maxValidatorStake:
		// Ensure validator isn't staking too much
		return ErrWeightTooLarge

	case tx.DelegationShares < validatorRules.minDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return ErrInsufficientDelegationFee

	case stakingPeriod < validatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case stakingPeriod > validatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong

	case stakedAssetID != validatorRules.assetID:
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			validatorRules.assetID,
			stakedAssetID,
		)
	}

	_, err = GetValidator(chainState, tx.Subnet, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"%w: %s on %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.Subnet,
		)
	}
	if err != database.ErrNotFound {
		return fmt.Errorf(
			"failed to find whether %s is a validator on %s: %w",
			tx.Validator.NodeID,
			tx.Subnet,
			err,
		)
	}

	var txFee uint64
	if tx.Subnet != constants.PrimaryNetworkID {
		primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to fetch the primary network validator for %s: %w",
				tx.Validator.NodeID,
				err,
			)
		}
		if primaryNetworkValidator.Priority.IsContinuousValidator() {
			// TODO: consider activation in subsequent PRs
			return ErrSubnetValidatorToContinuousValidator
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		stakerStart := currentTimestamp
		stakerEnd := stakerStart.Add(stakingPeriod)
		if !isContinuousStakingForkActive {
			stakerStart = preContinuousStakingStartTime
			stakerEnd = tx.EndTime()
		}
		if !txs.BoundedBy(
			stakerStart,
			stakerEnd,
			primaryNetworkValidator.StartTime,
			primaryNetworkValidator.EndTime,
		) {
			return ErrPeriodMismatch
		}

		txFee = backend.Config.AddSubnetValidatorFee
	} else {
		txFee = backend.Config.AddPrimaryNetworkValidatorFee
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: txFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	if !isContinuousStakingForkActive {
		// Make sure the tx doesn't start too far in the future. This is done last
		// to allow the verifier visitor to explicitly check for this error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if preContinuousStakingStartTime.After(maxStartTime) {
			return ErrFutureStakeTime
		}
	}

	return nil
}

// verifyAddPermissionlessDelegatorTx carries out the validation for an
// AddPermissionlessDelegatorTx.
func verifyAddPermissionlessDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessDelegatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	// Pre Continuous Staking fork activation, start time must be after current chain time.
	// Post Continuous Staking fork activation, only staking period matters, hence start time
	// is not validated
	var (
		currentTimestamp              = chainState.GetTimestamp()
		preContinuousStakingStartTime = tx.StartTime()
		isContinuousStakingForkActive = backend.Config.IsContinuousStakingActivated(currentTimestamp)
	)
	if !isContinuousStakingForkActive {
		if !currentTimestamp.Before(preContinuousStakingStartTime) {
			return fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				preContinuousStakingStartTime,
			)
		}
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	var (
		stakingPeriod = tx.StakingPeriod()
		stakedAssetID = tx.StakeOuts[0].AssetID()
	)
	switch {
	case tx.Validator.Wght < delegatorRules.minDelegatorStake:
		// Ensure delegator is staking at least the minimum amount
		return ErrWeightTooSmall

	case stakingPeriod < delegatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case stakingPeriod > delegatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong

	case stakedAssetID != delegatorRules.assetID:
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			delegatorRules.assetID,
			stakedAssetID,
		)
	}

	validator, err := GetValidator(chainState, tx.Subnet, tx.Validator.NodeID)
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the validator for %s on %s: %w",
			tx.Validator.NodeID,
			tx.Subnet,
			err,
		)
	}
	if validator.Priority.IsContinuousValidator() {
		// TODO: consider activation in subsequent PRs
		return ErrFiniteDelegatorToContinuousValidator
	}

	maximumWeight, err := math.Mul64(
		uint64(delegatorRules.maxValidatorWeightFactor),
		validator.Weight,
	)
	if err != nil {
		maximumWeight = stdmath.MaxUint64
	}
	maximumWeight = math.Min(maximumWeight, delegatorRules.maxValidatorStake)

	txID := sTx.ID()
	var newStaker *state.Staker
	if isContinuousStakingForkActive {
		// potential reward does not matter
		newStaker, err = state.NewCurrentStaker(txID, tx, currentTimestamp, currentTimestamp.Add(stakingPeriod), 0)
	} else {
		newStaker, err = state.NewPendingStaker(txID, tx)
	}
	if err != nil {
		return err
	}

	if !txs.BoundedBy(
		newStaker.StartTime,
		newStaker.EndTime,
		validator.StartTime,
		validator.EndTime,
	) {
		return ErrPeriodMismatch
	}
	overDelegated, err := overDelegated(chainState, validator, maximumWeight, newStaker)
	if err != nil {
		return err
	}
	if overDelegated {
		return ErrOverDelegated
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	var txFee uint64
	if tx.Subnet != constants.PrimaryNetworkID {
		// Invariant: Delegators must only be able to reference validator
		//            transactions that implement [txs.ValidatorTx]. All
		//            validator transactions implement this interface except the
		//            AddSubnetValidatorTx. AddSubnetValidatorTx is the only
		//            permissioned validator, so we verify this delegator is
		//            pointing to a permissionless validator.
		if validator.Priority.IsPermissionedValidator() {
			return ErrDelegateToPermissionedValidator
		}

		txFee = backend.Config.AddSubnetDelegatorFee
	} else {
		txFee = backend.Config.AddPrimaryNetworkDelegatorFee
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: txFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	if !isContinuousStakingForkActive {
		// Make sure the tx doesn't start too far in the future. This is done last
		// to allow the verifier visitor to explicitly check for this error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if preContinuousStakingStartTime.After(maxStartTime) {
			return ErrFutureStakeTime
		}
	}

	return nil
}

// verifyAddContinuousValidatorTx carries out the validation for an
// AddContinuousValidatorTx.
func verifyAddContinuousValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddContinuousValidatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	currentTimestamp := chainState.GetTimestamp()
	if !backend.Config.IsContinuousStakingActivated(currentTimestamp) {
		return ErrTxUnacceptableBeforeFork
	}

	subnetID := constants.PlatformChainID
	validatorRules, err := getValidatorRules(backend, chainState, subnetID)
	if err != nil {
		return err
	}

	var (
		stakingPeriod = tx.StakingPeriod()
		stakedAssetID = tx.StakeOuts[0].AssetID()
	)
	switch {
	case tx.Validator.Wght < validatorRules.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return ErrWeightTooSmall

	case tx.Validator.Wght > validatorRules.maxValidatorStake:
		// Ensure validator isn't staking too much
		return ErrWeightTooLarge

	case tx.DelegationShares < validatorRules.minDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return ErrInsufficientDelegationFee

	case stakingPeriod < validatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case stakingPeriod > validatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong

	case stakedAssetID != validatorRules.assetID:
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			validatorRules.assetID,
			stakedAssetID,
		)
	}

	switch _, err = GetValidator(chainState, subnetID, tx.Validator.NodeID); err {
	case nil:
		return fmt.Errorf(
			"%w: %s on %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			subnetID,
		)
	case database.ErrNotFound:
		// not a double registration. It's fine
	default:
		return fmt.Errorf(
			"failed to find whether %s is a validator on %s: %w",
			tx.Validator.NodeID,
			subnetID,
			err,
		)
	}

	txFee := backend.Config.AddPrimaryNetworkValidatorFee

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: txFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	return nil
}

// verifyAddContinuousDelegatorTx carries out the validation for an
// AddContinuousDelegatorTx. It returns the validator start time and
// the end time for the delegator to be created if verification passes.
func verifyAddContinuousDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddContinuousDelegatorTx,
) (
	time.Time, // validator start time
	time.Time, // delegator end time
	time.Duration, // min staking duration
	error,
) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	var (
		subnetID         = constants.PrimaryNetworkID
		currentTimestamp = chainState.GetTimestamp()
	)

	if backend.Bootstrapped.Get() && !backend.Config.IsContinuousStakingActivated(currentTimestamp) {
		return time.Time{}, time.Time{}, 0, ErrTxUnacceptableBeforeFork
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, subnetID)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	var (
		stakingPeriod = tx.StakingPeriod()
		stakedAssetID = tx.StakeOuts[0].AssetID()
	)
	switch {
	case tx.Validator.Wght < delegatorRules.minDelegatorStake:
		// Ensure delegator is staking at least the minimum amount
		return time.Time{}, time.Time{}, 0, ErrWeightTooSmall

	case stakingPeriod < delegatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return time.Time{}, time.Time{}, 0, ErrStakeTooShort

	case stakingPeriod > delegatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return time.Time{}, time.Time{}, 0, ErrStakeTooLong

	case stakedAssetID != delegatorRules.assetID:
		// Wrong assetID used
		return time.Time{}, time.Time{}, 0, fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			delegatorRules.assetID,
			stakedAssetID,
		)
	}

	validator, err := GetValidator(chainState, subnetID, tx.Validator.NodeID)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf(
			"failed to fetch the validator for %s on %s: %w",
			tx.Validator.NodeID,
			subnetID,
			err,
		)
	}
	if !validator.Priority.IsContinuousValidator() {
		return time.Time{}, time.Time{}, 0, ErrContinuousDelegatorToNonContinuousValidator
	}
	delegatorEndTime := validator.EndTime

	if !backend.Bootstrapped.Get() {
		return validator.StartTime, delegatorEndTime, delegatorRules.minStakeDuration, nil
	}

	maximumWeight, err := math.Mul64(
		uint64(delegatorRules.maxValidatorWeightFactor),
		validator.Weight,
	)
	if err != nil {
		maximumWeight = stdmath.MaxUint64
	}
	maximumWeight = math.Min(maximumWeight, delegatorRules.maxValidatorStake)

	// potential reward does not matter
	newStaker, err := state.NewCurrentStaker(sTx.ID(), tx, currentTimestamp, delegatorEndTime, 0)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	// Candidate delegator period checks:
	// * delegator first period must be bounded by validator first period
	// * delegator period must be a power of two divisor of validator period
	if !txs.BoundedBy(
		newStaker.StartTime,
		newStaker.NextTime,
		validator.StartTime,
		validator.NextTime,
	) {
		return time.Time{}, time.Time{}, 0, ErrPeriodMismatch
	}
	if err := checkContinuousDelegatorPeriod(validator.StakingPeriod, newStaker.StakingPeriod); err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	overDelegated, err := overDelegated(chainState, validator, maximumWeight, newStaker)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	if overDelegated {
		return time.Time{}, time.Time{}, 0, ErrOverDelegated
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	txFee := backend.Config.AddPrimaryNetworkDelegatorFee

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: txFee,
		},
	); err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	return validator.StartTime, delegatorEndTime, delegatorRules.minStakeDuration, nil
}

func verifyStopStakerTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.StopStakerTx,
) ([]*state.Staker, time.Time, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, time.Time{}, err
	}

	currentTimestamp := chainState.GetTimestamp()
	if !backend.Config.IsContinuousStakingActivated(currentTimestamp) {
		return nil, time.Time{}, ErrTxUnacceptableBeforeFork
	}

	// retrieve staker to be stopped
	var (
		txID         = tx.TxID
		stakerToStop *state.Staker
	)
	theStakerIt, err := chainState.GetCurrentStakerIterator()
	if err != nil {
		return nil, time.Time{}, err
	}
	defer theStakerIt.Release()
	for theStakerIt.Next() {
		staker := theStakerIt.Value()
		if staker.TxID == txID {
			stakerToStop = staker
			break
		}
	}

	if stakerToStop == nil {
		return nil, time.Time{}, ErrNoStakerToStop
	}

	if backend.Bootstrapped.Get() {
		// We can skip full verification during bootstrap.
		baseTxCreds, err := verifyStopStakerAuthorization(backend, chainState, sTx, txID, tx.StakerAuth)
		if err != nil {
			return nil, time.Time{}, err
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
			return nil, time.Time{}, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
		}
	}

	candidateStopTime := stakerToStop.NextTime
	if !stakerToStop.Priority.IsValidator() {
		return []*state.Staker{stakerToStop}, candidateStopTime, nil
	}

	// primary network validators are special since, when stopping them, we need to handle
	// their delegators and subnet validators/delegator as well, to make sure they don't
	// outlive the primary network validators.
	res := []*state.Staker{stakerToStop}
	allStakersIt, err := chainState.GetCurrentStakerIterator()
	if err != nil {
		return nil, time.Time{}, err
	}
	defer allStakersIt.Release()
	for allStakersIt.Next() {
		staker := allStakersIt.Value()
		if staker.NodeID != stakerToStop.NodeID {
			continue
		}
		if staker.TxID != stakerToStop.TxID {
			res = append(res, staker)
		}
		if candidateStopTime.Before(staker.NextTime) {
			candidateStopTime = candidateStopTime.Add(stakerToStop.StakingPeriod)
		}
	}

	return res, candidateStopTime, nil
}

func verifyStopStakerAuthorization(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	stakerTxID ids.ID,
	stakerAuth verify.Verifiable,
) ([]verify.Verifiable, error) {
	if len(sTx.Creds) == 0 {
		// Ensure there is at least one credential for the subnet authorization
		return nil, errWrongNumberOfCredentials
	}

	baseTxCredsLen := len(sTx.Creds) - 1
	stakerCred := sTx.Creds[baseTxCredsLen]

	stakerTx, _, err := chainState.GetTx(stakerTxID)
	if err != nil {
		return nil, fmt.Errorf(
			"staker tx not found %q: %w",
			stakerTxID,
			err,
		)
	}

	continuousStakerTx, ok := stakerTx.Unsigned.(txs.ContinuousStaker)
	if !ok {
		return nil, ErrUnauthorizedStakerStopping
	}

	err = backend.Fx.VerifyPermission(sTx.Unsigned, stakerAuth, stakerCred, continuousStakerTx.ManagementKey())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnauthorizedStakerStopping, err)
	}

	return sTx.Creds[:baseTxCredsLen], nil
}
