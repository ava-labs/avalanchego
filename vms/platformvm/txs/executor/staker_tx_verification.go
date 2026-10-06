// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/database"
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
	errStakeOverflow                   = errors.New("validator stake exceeds limit")
	errPeriodMismatch                  = errors.New("proposed staking period is not inside dependent staking period")
	errAlreadyValidator                = errors.New("already a validator")
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
		return nil
	}

	validatorRules, err := getValidatorRules(backend, chainState, constants.PrimaryNetworkID)
	if err != nil {
		return err
	}

	startTime := tx.StartTime()
	endTime := tx.EndTime()
	duration := endTime.Sub(startTime)
	if err := validatorRules.verifyValidator(
		tx.Validator.Wght,
		tx.DelegationShares,
		duration,
	); err != nil {
		return err
	}

	if err := verifyStakerStartTime(
		false, /*=isDurangoActive*/
		chainState.GetTimestamp(),
		startTime,
	); err != nil {
		return err
	}

	_, err = GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"%s is %w of the primary network",
			tx.Validator.NodeID,
			errAlreadyValidator,
		)
	}
	if err != database.ErrNotFound {
		return fmt.Errorf(
			"failed to find whether %s is a primary network validator: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	return nil
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
		return nil
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)

	endTime := tx.EndTime()
	startTime := currentTimestamp
	if !isDurangoActive {
		startTime = tx.StartTime()
	}

	switch duration := endTime.Sub(startTime); {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, startTime); err != nil {
		return err
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

	if err := verifySubnetValidatorPrimaryNetworkRequirements(backend, chainState, tx.Validator); err != nil {
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
		return nil
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, constants.PrimaryNetworkID)
	if err != nil {
		return err
	}

	if err := delegatorRules.verifyDelegator(tx.Validator.Wght, tx.EndTime().Sub(tx.StartTime())); err != nil {
		return err
	}

	currentTimestamp := chainState.GetTimestamp()
	startTime := tx.StartTime()
	if err := verifyStakerStartTime(false /*=isDurangoActive*/, currentTimestamp, startTime); err != nil {
		return err
	}

	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	maximumWeight, err := safemath.Mul(primaryNetworkMaxValidatorWeightFactor, primaryNetworkValidator.Weight)
	if err != nil {
		return errStakeOverflow
	}

	if backend.Config.UpgradeConfig.IsApricotPhase3Activated(currentTimestamp) {
		maximumWeight = min(maximumWeight, backend.Config.MaxValidatorStake)
	}

	endTime := tx.EndTime()
	if !platform.BoundedBy(
		startTime,
		endTime,
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return errPeriodMismatch
	}
	overDelegated, err := overDelegated(
		chainState,
		primaryNetworkValidator,
		maximumWeight,
		tx.Validator.Wght,
		startTime,
		endTime,
	)
	if err != nil {
		return err
	}
	if overDelegated {
		return ErrOverDelegated
	}

	return nil
}

// verifyAddPermissionlessValidatorTx carries out the state-dependent
// validation for a [platform.AddPermissionlessValidatorTx].
func verifyAddPermissionlessValidatorTx(
	backend *Backend,
	chainState state.Chain,
	tx *platform.AddPermissionlessValidatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		return nil
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	startTime := currentTimestamp
	if !isDurangoActive {
		startTime = tx.StartTime()
	}
	duration := tx.EndTime().Sub(startTime)

	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, tx.StartTime()); err != nil {
		return err
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	if err := validatorRules.verifyValidator(tx.Validator.Wght, tx.DelegationShares, duration); err != nil {
		return err
	}

	stakedAssetID := tx.StakeOuts[0].AssetID()
	if stakedAssetID != validatorRules.assetID {
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			errWrongStakedAssetID,
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

	if tx.Subnet != constants.PrimaryNetworkID {
		if err := verifySubnetValidatorPrimaryNetworkRequirements(backend, chainState, tx.Validator); err != nil {
			return err
		}
	}

	return nil
}

// verifyAddPermissionlessDelegatorTx carries out the state-dependent
// validation for a [platform.AddPermissionlessDelegatorTx].
func verifyAddPermissionlessDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	tx *platform.AddPermissionlessDelegatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		return nil
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
		endTime          = tx.EndTime()
		startTime        = currentTimestamp
	)

	if !isDurangoActive {
		startTime = tx.StartTime()
	}
	duration := endTime.Sub(startTime)

	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, tx.StartTime()); err != nil {
		return err
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	if err := delegatorRules.verifyDelegator(tx.Validator.Wght, duration); err != nil {
		return err
	}

	stakedAssetID := tx.StakeOuts[0].AssetID()
	if stakedAssetID != delegatorRules.assetID {
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			errWrongStakedAssetID,
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

	maximumWeight, err := safemath.Mul(
		uint64(delegatorRules.maxValidatorWeightFactor),
		validator.Weight,
	)
	if err != nil {
		maximumWeight = math.MaxUint64
	}
	maximumWeight = min(maximumWeight, delegatorRules.maxValidatorStake)

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
		tx.Validator.Wght,
		startTime,
		endTime,
	)
	if err != nil {
		return err
	}
	if overDelegated {
		return ErrOverDelegated
	}

	if tx.Subnet != constants.PrimaryNetworkID {
		// Invariant: Delegators must only be able to reference validator
		//            transactions that implement [platform.ValidatorTx]. All
		//            validator transactions implement this interface except the
		//            AddSubnetValidatorTx. AddSubnetValidatorTx is the only
		//            permissioned validator, so we verify this delegator is
		//            pointing to a permissionless validator.
		if validator.Priority.IsPermissionedValidator() {
			return errDelegateToPermissionedValidator
		}
	}

	return nil
}

// verifyAddAutoRenewedValidatorTx carries out the state-dependent validation
// for a [platform.AddAutoRenewedValidatorTx].
func verifyAddAutoRenewedValidatorTx(
	backend *Backend,
	chainState state.Chain,
	tx *platform.AddAutoRenewedValidatorTx,
) error {
	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
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

	if err := validatorRules.verifyValidator(tx.Weight(), tx.Shares(), period); err != nil {
		return err
	}

	_, err = GetValidator(chainState, constants.PrimaryNetworkID, tx.NodeID())
	switch err {
	case nil:
		return fmt.Errorf(
			"%w: %s",
			ErrDuplicateValidator,
			tx.NodeID(),
		)
	case database.ErrNotFound:
		// OK: validator not found

	default:
		return fmt.Errorf(
			"failed to get primary network validator %s: %w",
			tx.NodeID(),
			err,
		)
	}

	return nil
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
		// Not bootstrapped yet -- don't need to do full verification.
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
// network requirements for subnetValidator. An error is returned if they
// are not fulfilled.
func verifySubnetValidatorPrimaryNetworkRequirements(
	backend *Backend,
	chainState state.Chain,
	subnetValidator platform.Validator,
) error {
	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, subnetValidator.NodeID)
	if err == database.ErrNotFound {
		return fmt.Errorf(
			"%s %w of the primary network",
			subnetValidator.NodeID,
			errNotValidator,
		)
	}
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			subnetValidator.NodeID,
			err,
		)
	}

	// Ensure that the period this validator validates the specified subnet
	// is a subset of the time they validate the primary network.
	chainTime := chainState.GetTimestamp()
	startTime := chainTime
	if !backend.Config.UpgradeConfig.IsDurangoActivated(chainTime) {
		startTime = subnetValidator.StartTime()
	}
	if !platform.BoundedBy(
		startTime,
		subnetValidator.EndTime(),
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return errPeriodMismatch
	}

	return nil
}

// verifyStakerStartTime ensures the proposed staker starts after the current
// chain time. Post-Durango the start time is not validated.
func verifyStakerStartTime(isDurangoActive bool, chainTime, stakerTime time.Time) error {
	if isDurangoActive {
		return nil
	}

	if !chainTime.Before(stakerTime) {
		return fmt.Errorf(
			"%w: %s >= %s",
			ErrTimestampNotBeforeStartTime,
			chainTime,
			stakerTime,
		)
	}
	return nil
}
