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
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	ErrWeightTooSmall                  = errors.New("weight of this validator is too low")
	ErrWeightTooLarge                  = errors.New("weight of this validator is too large")
	ErrInsufficientDelegationFee       = errors.New("staker charges an insufficient delegation fee")
	ErrStakeTooShort                   = errors.New("staking period is too short")
	ErrStakeTooLong                    = errors.New("staking period is too long")
	ErrFlowCheckFailed                 = errors.New("flow check failed")
	ErrNotValidator                    = errors.New("isn't a current or pending validator")
	ErrRemovePermissionlessValidator   = errors.New("attempting to remove permissionless validator")
	ErrStakeOverflow                   = errors.New("validator stake exceeds limit")
	ErrPeriodMismatch                  = errors.New("proposed staking period is not inside dependent staking period")
	ErrOverDelegated                   = errors.New("validator would be over delegated")
	ErrIsNotTransformSubnetTx          = errors.New("is not a transform subnet tx")
	ErrTimestampNotBeforeStartTime     = errors.New("chain timestamp not before start time")
	ErrAlreadyValidator                = errors.New("already a validator")
	ErrDuplicateValidator              = errors.New("duplicate validator")
	ErrDelegateToPermissionedValidator = errors.New("delegation to permissioned validator")
	ErrWrongStakedAssetID              = errors.New("incorrect staked assetID")
	ErrDurangoUpgradeNotActive         = errors.New("attempting to use a Durango-upgrade feature prior to activation")
	ErrAddValidatorTxPostDurango       = errors.New("AddValidatorTx is not permitted post-Durango")
	ErrAddDelegatorTxPostDurango       = errors.New("AddDelegatorTx is not permitted post-Durango")
	errInvalidStakerTxType             = errors.New("invalid staker tx type")
	errInvalidStakerTx                 = errors.New("invalid staker tx")
)

// verifySubnetValidatorPrimaryNetworkRequirements verifies the primary
// network requirements for [subnetValidator]. An error is returned if they
// are not fulfilled.
func verifySubnetValidatorPrimaryNetworkRequirements(
	isDurangoActive bool,
	chainState state.Chain,
	subnetValidator platform.Validator,
) error {
	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, subnetValidator.NodeID)
	if err == database.ErrNotFound {
		return fmt.Errorf(
			"%s %w of the primary network",
			subnetValidator.NodeID,
			ErrNotValidator,
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
	startTime := chainState.GetTimestamp()
	if !isDurangoActive {
		startTime = subnetValidator.StartTime()
	}
	if !platform.BoundedBy(
		startTime,
		subnetValidator.EndTime(),
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return ErrPeriodMismatch
	}

	return nil
}

// verifyAddValidatorTx carries out the validation for an [txs.AddValidatorTx].
func verifyAddValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.AddValidatorTx,
) error {
	currentTimestamp := chainState.GetTimestamp()
	if backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp) {
		return ErrAddValidatorTxPostDurango
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, false /*=isDurangoActive*/); err != nil {
		return err
	}

	startTime := tx.StartTime()
	duration := tx.EndTime().Sub(startTime)
	switch {
	case tx.Validator.Wght < backend.Config.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return ErrWeightTooSmall

	case tx.Validator.Wght > backend.Config.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return ErrWeightTooLarge

	case tx.DelegationShares < backend.Config.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return ErrInsufficientDelegationFee

	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	if err := verifyStakerStartTime(false /*=isDurangoActive*/, currentTimestamp, startTime); err != nil {
		return err
	}

	_, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"%s is %w of the primary network",
			tx.Validator.NodeID,
			ErrAlreadyValidator,
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

// verifyAddSubnetValidatorTx carries out the validation for an
// AddSubnetValidatorTx. It returns the credentials authorizing the spend of the
// tx inputs.
func verifyAddSubnetValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.AddSubnetValidatorTx,
) ([]verify.Verifiable, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return nil, err
	}

	startTime := currentTimestamp
	if !isDurangoActive {
		startTime = tx.StartTime()
	}
	duration := tx.EndTime().Sub(startTime)

	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, ErrStakeTooLong
	}

	if !backend.Bootstrapped.Get() {
		return nil, nil
	}

	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, startTime); err != nil {
		return nil, err
	}

	_, err := GetValidator(chainState, tx.SubnetValidator.Subnet, tx.Validator.NodeID)
	if err == nil {
		return nil, fmt.Errorf(
			"attempted to issue %w for %s on subnet %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.SubnetValidator.Subnet,
		)
	}
	if err != database.ErrNotFound {
		return nil, fmt.Errorf(
			"failed to find whether %s is a subnet validator: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	if err := verifySubnetValidatorPrimaryNetworkRequirements(isDurangoActive, chainState, tx.Validator); err != nil {
		return nil, err
	}

	return verifyPoASubnetAuthorization(backend.Fx, chainState, sTx, tx.SubnetValidator.Subnet, tx.SubnetAuth)
}

// Returns the representation of tx.NodeID validating tx.Subnet, which may
// be either a current or a pending validator, and the credentials authorizing
// the spend of the tx inputs.
// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * tx.NodeI] is a current/pending PoA validator of tx.Subnet.
// * sTx's creds authorize it to remove a validator from tx.Subnet.
func verifyRemoveSubnetValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.RemoveSubnetValidatorTx,
) (*state.Staker, []verify.Verifiable, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, nil, err
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return nil, nil, err
	}

	vdr, err := chainState.GetCurrentValidator(tx.Subnet, tx.NodeID)
	if err == database.ErrNotFound {
		vdr, err = chainState.GetPendingValidator(tx.Subnet, tx.NodeID)
	}
	if err != nil {
		// It isn't a current or pending validator.
		return nil, nil, fmt.Errorf(
			"%s %w of %s: %w",
			tx.NodeID,
			ErrNotValidator,
			tx.Subnet,
			err,
		)
	}

	if !vdr.Priority.IsPermissionedValidator() {
		return nil, nil, ErrRemovePermissionlessValidator
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return vdr, nil, nil
	}

	baseTxCreds, err := verifySubnetAuthorization(backend.Fx, chainState, sTx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return nil, nil, err
	}

	return vdr, baseTxCreds, nil
}

// verifyAddDelegatorTx carries out the validation for an AddDelegatorTx.
func verifyAddDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.AddDelegatorTx,
) error {
	currentTimestamp := chainState.GetTimestamp()
	if backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp) {
		return ErrAddDelegatorTxPostDurango
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, false /*=isDurangoActive*/); err != nil {
		return err
	}

	var (
		endTime   = tx.EndTime()
		startTime = tx.StartTime()
		duration  = endTime.Sub(startTime)
	)
	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong

	case tx.Validator.Wght < backend.Config.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return ErrWeightTooSmall
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

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

	maximumWeight, err := safemath.Mul(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
	if err != nil {
		return ErrStakeOverflow
	}

	if backend.Config.UpgradeConfig.IsApricotPhase3Activated(currentTimestamp) {
		maximumWeight = min(maximumWeight, backend.Config.MaxValidatorStake)
	}

	if !platform.BoundedBy(
		startTime,
		endTime,
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return ErrPeriodMismatch
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

// verifyAddPermissionlessValidatorTx carries out the validation for an
// AddPermissionlessValidatorTx.
func verifyAddPermissionlessValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.AddPermissionlessValidatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	startTime := currentTimestamp
	if !isDurangoActive {
		startTime = tx.StartTime()
	}
	duration := tx.EndTime().Sub(startTime)

	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, startTime); err != nil {
		return err
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	stakedAssetID := tx.StakeOuts[0].AssetID()
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

	case duration < validatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > validatorRules.maxStakeDuration:
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

	if tx.Subnet != constants.PrimaryNetworkID {
		if err := verifySubnetValidatorPrimaryNetworkRequirements(isDurangoActive, chainState, tx.Validator); err != nil {
			return err
		}
	}

	return nil
}

// verifyAddPermissionlessDelegatorTx carries out the validation for an
// AddPermissionlessDelegatorTx.
func verifyAddPermissionlessDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.AddPermissionlessDelegatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	var (
		endTime   = tx.EndTime()
		startTime = currentTimestamp
	)
	if !isDurangoActive {
		startTime = tx.StartTime()
	}
	duration := endTime.Sub(startTime)

	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, startTime); err != nil {
		return err
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	stakedAssetID := tx.StakeOuts[0].AssetID()
	switch {
	case tx.Validator.Wght < delegatorRules.minDelegatorStake:
		// Ensure delegator is staking at least the minimum amount
		return ErrWeightTooSmall

	case duration < delegatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > delegatorRules.maxStakeDuration:
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
		return ErrPeriodMismatch
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
			return ErrDelegateToPermissionedValidator
		}
	}

	return nil
}

// Returns the credentials authorizing the spend of the tx inputs. The flow
// check is left to the caller.
// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * [sTx]'s creds authorize it to transfer ownership of [tx.Subnet].
func verifyTransferSubnetOwnershipTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.TransferSubnetOwnershipTx,
) ([]verify.Verifiable, error) {
	var (
		currentTimestamp = chainState.GetTimestamp()
		upgrades         = backend.Config.UpgradeConfig
	)
	if !upgrades.IsDurangoActivated(currentTimestamp) {
		return nil, ErrDurangoUpgradeNotActive
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return nil, err
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return nil, nil
	}

	return verifySubnetAuthorization(backend.Fx, chainState, sTx, tx.Subnet, tx.SubnetAuth)
}

// verifyAddAutoRenewedValidatorTx carries out the validation for an
// AddAutoRenewedValidatorTx.
func verifyAddAutoRenewedValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.AddAutoRenewedValidatorTx,
) error {
	if !backend.Config.UpgradeConfig.IsHeliconActivated(chainState.GetTimestamp()) {
		return errHeliconUpgradeNotActive
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return nil
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.SubnetID())
	if err != nil {
		return err
	}

	switch {
	case tx.Weight() < validatorRules.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return ErrWeightTooSmall

	case tx.Weight() > validatorRules.maxValidatorStake:
		// Ensure validator isn't staking too much
		return ErrWeightTooLarge

	case tx.Shares() < validatorRules.minDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return ErrInsufficientDelegationFee

	case tx.Period < uint64(validatorRules.minStakeDuration/time.Second):
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case tx.Period > uint64(validatorRules.maxStakeDuration/time.Second):
		// Ensure staking length is not too long
		return ErrStakeTooLong
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

// verifySetAutoRenewedValidatorConfigTx carries out the validation for a
// SetAutoRenewedValidatorConfigTx. It returns the validator being configured
// and the credentials authorizing the spend of the tx inputs.
func verifySetAutoRenewedValidatorConfigTx(
	backend *Backend,
	chainState state.Chain,
	sTx *platform.Tx,
	tx *platform.SetAutoRenewedValidatorConfigTx,
) (*state.Staker, []verify.Verifiable, error) {
	if !backend.Config.UpgradeConfig.IsHeliconActivated(chainState.GetTimestamp()) {
		return nil, nil, errHeliconUpgradeNotActive
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, nil, err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return nil, nil, err
	}

	stakerTx, _, err := chainState.GetTx(tx.TxID)
	if err != nil {
		return nil, nil, fmt.Errorf("getting staker tx: %w", err)
	}

	autoRenewedStakerTx, ok := stakerTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %T", errInvalidStakerTxType, stakerTx.Unsigned)
	}

	validator, err := chainState.GetCurrentValidator(constants.PrimaryNetworkID, autoRenewedStakerTx.NodeID())
	if err != nil {
		return nil, nil, fmt.Errorf("getting validator %s from state: %w", autoRenewedStakerTx.NodeID(), err)
	}

	if tx.TxID != validator.TxID {
		// This can happen if a validator restaked with the same node id.
		// In this case, TxID should be the latest transaction of the auto-renewed validator.
		return nil, nil, fmt.Errorf("%w: wrong tx id", errInvalidStakerTx)
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return validator, nil, nil
	}

	validatorRules, err := getValidatorRules(backend, chainState, autoRenewedStakerTx.SubnetID())
	if err != nil {
		return nil, nil, fmt.Errorf("getting validator rules: %w", err)
	}

	switch {
	case tx.Period > 0 && tx.Period < uint64(validatorRules.minStakeDuration/time.Second):
		return nil, nil, ErrStakeTooShort
	case tx.Period > uint64(validatorRules.maxStakeDuration/time.Second):
		return nil, nil, ErrStakeTooLong
	}

	baseTxCreds, err := verifyAuthorization(backend.Fx, sTx, autoRenewedStakerTx.ValidatorAuthority, tx.Auth)
	if err != nil {
		return nil, nil, err
	}

	return validator, baseTxCreds, nil
}

// Ensure the proposed validator starts after the current time
func verifyStakerStartTime(isDurangoActive bool, chainTime, stakerTime time.Time) error {
	// Pre Durango activation, start time must be after current chain time.
	// Post Durango activation, start time is not validated
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

// verifySpend verifies that ins, authorized by creds, fund outs plus
// producedAVAX and the fee of tx for the current fee configuration.
func verifySpend(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	tx platform.UnsignedTx,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	producedAVAX uint64,
	creds []verify.Verifiable,
) error {
	txFee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return fmt.Errorf("calculating fee: %w", err)
	}

	producedAVAX, err = safemath.Add(producedAVAX, txFee)
	if err != nil {
		return fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}
