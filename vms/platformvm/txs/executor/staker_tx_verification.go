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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	ErrStakeTooLong                = errors.New("staking period is too long")
	ErrOverDelegated               = errors.New("validator would be over delegated")
	ErrTimestampNotBeforeStartTime = errors.New("chain timestamp not before start time")
	ErrDuplicateValidator          = errors.New("duplicate validator")

	errWeightTooSmall                  = errors.New("weight of this validator is too low")
	errWeightTooLarge                  = errors.New("weight of this validator is too large")
	errInsufficientDelegationFee       = errors.New("staker charges an insufficient delegation fee")
	errStakeTooShort                   = errors.New("staking period is too short")
	errNotValidator                    = errors.New("isn't a current or pending validator")
	errRemovePermissionlessValidator   = errors.New("attempting to remove permissionless validator")
	errPeriodMismatch                  = errors.New("proposed staking period is not inside dependent staking period")
	errDelegateToPermissionedValidator = errors.New("delegation to permissioned validator")
	errWrongStakedAssetID              = errors.New("incorrect staked assetID")
	errInvalidStakerTxType             = errors.New("invalid staker tx type")
	errInvalidStakerTx                 = errors.New("invalid staker tx")
)

// verifyAddValidatorTx carries out the state-dependent validation for a
// [platform.AddValidatorTx]. It is shared by the standard and proposal
// execution paths.
func verifyAddValidatorTx(
	backend *Backend,
	chainState state.Chain,
	tx *platform.AddValidatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet, don't need to do full verification.
		return nil
	}

	startTime, endTime, err := stakingPeriod(backend, chainState, tx)
	if err != nil {
		return err
	}

	return verifyNewValidator(backend, chainState, tx, startTime, endTime)
}

// verifyAddSubnetValidatorTx carries out the state-dependent validation for a
// [platform.AddSubnetValidatorTx]. It is shared by the standard and proposal
// execution paths.
func verifyAddSubnetValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.AddSubnetValidatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet, don't need to do full verification.
		return nil
	}

	startTime, endTime, err := stakingPeriod(backend, chainState, tx)
	if err != nil {
		return err
	}

	if err := verifyStakeDuration(
		endTime.Sub(startTime),
		backend.Config.MinStakeDuration,
		backend.Config.MaxStakeDuration,
	); err != nil {
		return err
	}

	_, err = GetValidator(chainState, tx.SubnetValidator.Subnet, tx.Validator.NodeID)
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

	if err := verifySubnetValidatorPrimaryNetworkRequirements(chainState, tx.Validator.NodeID, startTime, endTime); err != nil {
		return err
	}

	return verifyPoASubnetAuthorization(backend.Fx, chainState, sTx, tx.SubnetValidator.Subnet, tx.SubnetAuth)
}

// verifyAddDelegatorTx carries out the state-dependent validation for a
// [platform.AddDelegatorTx]. It is shared by the standard and proposal
// execution paths.
func verifyAddDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	tx *platform.AddDelegatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet, don't need to do full verification.
		return nil
	}

	return verifyNewDelegator(backend, chainState, tx)
}

// verifyAddPermissionlessValidatorTx carries out the state-dependent
// validation for a [platform.AddPermissionlessValidatorTx].
func verifyAddPermissionlessValidatorTx(
	backend *Backend,
	chainState state.Chain,
	tx *platform.AddPermissionlessValidatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet, don't need to do full verification.
		return nil
	}

	startTime, endTime, err := stakingPeriod(backend, chainState, tx)
	if err != nil {
		return err
	}

	return verifyNewValidator(backend, chainState, tx, startTime, endTime)
}

// verifyAddPermissionlessDelegatorTx carries out the state-dependent
// validation for a [platform.AddPermissionlessDelegatorTx].
func verifyAddPermissionlessDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	tx *platform.AddPermissionlessDelegatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet, don't need to do full verification.
		return nil
	}

	return verifyNewDelegator(backend, chainState, tx)
}

// verifyAddAutoRenewedValidatorTx carries out the state-dependent validation
// for a [platform.AddAutoRenewedValidatorTx].
func verifyAddAutoRenewedValidatorTx(
	backend *Backend,
	chainState state.Chain,
	tx *platform.AddAutoRenewedValidatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet, don't need to do full verification.
		return nil
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.SubnetID())
	if err != nil {
		return err
	}

	period, err := periodToDuration(tx.Period, validatorRules.maxStakeDuration)
	if err != nil {
		return err
	}

	startTime := chainState.GetTimestamp()
	endTime := startTime.Add(period)
	return verifyNewValidator(backend, chainState, tx, startTime, endTime)
}

// verifySetAutoRenewedValidatorConfigTx carries out the state-dependent
// validation for a [platform.SetAutoRenewedValidatorConfigTx]. It returns the
// validator being configured.
func verifySetAutoRenewedValidatorConfigTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.SetAutoRenewedValidatorConfigTx,
) (*state.Staker, error) {
	stakerTx, _, err := chainState.GetTx(tx.TxID)
	if err != nil {
		return nil, fmt.Errorf("getting staker tx: %w", err)
	}

	autoRenewedStakerTx, ok := stakerTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errInvalidStakerTxType, stakerTx.Unsigned)
	}

	validator, err := chainState.GetCurrentValidator(constants.PrimaryNetworkID, autoRenewedStakerTx.NodeID())
	if err != nil {
		return nil, fmt.Errorf("getting validator %s from state: %w", autoRenewedStakerTx.NodeID(), err)
	}

	if tx.TxID != validator.TxID {
		// This can happen if a validator restaked with the same node id.
		// In this case, TxID should be the latest transaction of the auto-renewed validator.
		return nil, fmt.Errorf("%w: wrong tx id", errInvalidStakerTx)
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet, don't need to do full verification.
		return validator, nil
	}

	validatorRules, err := getValidatorRules(backend, chainState, autoRenewedStakerTx.SubnetID())
	if err != nil {
		return nil, fmt.Errorf("getting validator rules: %w", err)
	}

	// A Period of 0 stops the validator at the end of the current cycle, so it
	// is not a stake duration.
	if tx.Period > 0 {
		period, err := periodToDuration(tx.Period, validatorRules.maxStakeDuration)
		if err != nil {
			return nil, err
		}
		if err := verifyStakeDuration(period, validatorRules.minStakeDuration, validatorRules.maxStakeDuration); err != nil {
			return nil, err
		}
	}

	if err := verifyAuthorization(backend.Fx, sTx, autoRenewedStakerTx.ValidatorAuthority, tx.Auth); err != nil {
		return nil, err
	}

	return validator, nil
}

// periodToDuration converts period, in seconds, to a [time.Duration]. A period
// longer than maxStakeDuration is rejected with [ErrStakeTooLong] before the
// conversion, so the conversion cannot overflow.
func periodToDuration(period uint64, maxStakeDuration time.Duration) (time.Duration, error) {
	// Comparing in whole seconds is exact: period > maxStakeDuration/time.Second
	// iff period*time.Second > maxStakeDuration.
	if period > uint64(maxStakeDuration/time.Second) {
		return 0, ErrStakeTooLong
	}
	return time.Duration(period) * time.Second, nil
}

// verifySubnetValidatorPrimaryNetworkRequirements verifies the primary
// network requirements for subnetValidator, which starts validating at
// startTime. An error is returned if they are not fulfilled.
func verifySubnetValidatorPrimaryNetworkRequirements(
	chainState state.Chain,
	nodeID ids.NodeID,
	startTime time.Time,
	endTime time.Time,
) error {
	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, nodeID)
	if err == database.ErrNotFound {
		return fmt.Errorf(
			"%s %w of the primary network",
			nodeID,
			errNotValidator,
		)
	}
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			nodeID,
			err,
		)
	}

	// Ensure that the period this validator validates the specified subnet
	// is a subset of the time they validate the primary network.
	if !platform.BoundedBy(
		startTime,
		endTime,
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return errPeriodMismatch
	}

	return nil
}

// verifyNewValidator verifies that tx, which validates from startTime to
// endTime, satisfies the validator rules of its subnet and is not already a
// validator of it.
func verifyNewValidator(
	backend *Backend,
	chainState state.Chain,
	tx platform.ValidatorTx,
	startTime time.Time,
	endTime time.Time,
) error {
	subnetID := tx.SubnetID()
	validatorRules, err := getValidatorRules(backend, chainState, subnetID)
	if err != nil {
		return err
	}

	if err := validatorRules.verifyValidator(tx.Weight(), tx.Shares(), endTime.Sub(startTime)); err != nil {
		return err
	}

	stakedAssetID := tx.Stake()[0].AssetID()
	if stakedAssetID != validatorRules.assetID {
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			errWrongStakedAssetID,
			validatorRules.assetID,
			stakedAssetID,
		)
	}

	nodeID := tx.NodeID()
	_, err = GetValidator(chainState, subnetID, nodeID)
	if err == nil {
		return fmt.Errorf(
			"%w: %s on %s",
			ErrDuplicateValidator,
			nodeID,
			subnetID,
		)
	}
	if err != database.ErrNotFound {
		return fmt.Errorf(
			"failed to find whether %s is a validator on %s: %w",
			nodeID,
			subnetID,
			err,
		)
	}

	if subnetID != constants.PrimaryNetworkID {
		return verifySubnetValidatorPrimaryNetworkRequirements(chainState, nodeID, startTime, endTime)
	}

	return nil
}

// stakingPeriod returns the period during which tx stakes. Post-Durango it
// starts at the current chain time; pre-Durango the start time of tx must be
// after the current chain time.
func stakingPeriod(
	backend *Backend,
	chainState state.Chain,
	tx platform.ScheduledStaker,
) (time.Time, time.Time, error) {
	currentTimestamp := chainState.GetTimestamp()
	if backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp) {
		return currentTimestamp, tx.EndTime(), nil
	}

	startTime := tx.StartTime()
	if !currentTimestamp.Before(startTime) {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"%w: %s >= %s",
			ErrTimestampNotBeforeStartTime,
			currentTimestamp,
			startTime,
		)
	}
	return startTime, tx.EndTime(), nil
}

type delegatorScheduledStakerTx interface {
	platform.DelegatorTx
	platform.ScheduledStaker
}

// verifyNewDelegator verifies that tx satisfies the delegator rules of its
// subnet and that the validator it delegates to can accept it.
func verifyNewDelegator(
	backend *Backend,
	chainState state.Chain,
	tx delegatorScheduledStakerTx,
) error {
	startTime, endTime, err := stakingPeriod(backend, chainState, tx)
	if err != nil {
		return err
	}

	subnetID := tx.SubnetID()
	delegatorRules, err := getDelegatorRules(backend, chainState, subnetID)
	if err != nil {
		return err
	}

	if err := delegatorRules.verifyDelegator(tx.Weight(), endTime.Sub(startTime)); err != nil {
		return err
	}

	stakedAssetID := tx.Stake()[0].AssetID()
	if stakedAssetID != delegatorRules.assetID {
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			errWrongStakedAssetID,
			delegatorRules.assetID,
			stakedAssetID,
		)
	}

	nodeID := tx.NodeID()
	validator, err := GetValidator(chainState, subnetID, nodeID)
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the validator for %s on %s: %w",
			nodeID,
			subnetID,
			err,
		)
	}

	maximumWeight, err := safemath.Mul(
		uint64(delegatorRules.maxValidatorWeightFactor),
		validator.Weight,
	)
	if err != nil {
		maximumWeight = math.MaxUint64
	}

	// Pre-ApricotPhase3, the delegation cap isn't bounded by the max validator
	// stake. Permissionless delegators are only accepted post-ApricotPhase3.
	if backend.Config.UpgradeConfig.IsApricotPhase3Activated(chainState.GetTimestamp()) {
		maximumWeight = min(maximumWeight, delegatorRules.maxValidatorStake)
	}

	if !platform.BoundedBy(
		startTime,
		endTime,
		validator.StartTime,
		validator.EndTime,
	) {
		return errPeriodMismatch
	}
	overDelegated, err := overDelegated(
		chainState,
		validator,
		maximumWeight,
		tx.Weight(),
		startTime,
		endTime,
	)
	if err != nil {
		return err
	}
	if overDelegated {
		return ErrOverDelegated
	}

	// Invariant: Delegators must only be able to reference validator
	//            transactions that implement [platform.ValidatorTx]. All
	//            validator transactions implement this interface except the
	//            AddSubnetValidatorTx. AddSubnetValidatorTx is the only
	//            permissioned validator, so we verify this delegator is
	//            pointing to a permissionless validator.
	if validator.Priority.IsPermissionedValidator() {
		return errDelegateToPermissionedValidator
	}

	return nil
}
