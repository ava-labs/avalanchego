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
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"

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
	ErrMissingStakerTx                 = errors.New("missing staker tx")
	ErrInvalidStakerTxType             = errors.New("invalid staker tx type")
	ErrInvalidStakerTx                 = errors.New("invalid staker tx")
	ErrMissingValidator                = errors.New("missing validator")
)

// verifySubnetValidatorPrimaryNetworkRequirements verifies the primary
// network requirements for [subnetValidator]. An error is returned if they
// are not fulfilled.
func verifySubnetValidatorPrimaryNetworkRequirements(
	isDurangoActive bool,
	chainState state.Chain,
	subnetValidator txs.Validator,
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
	if !txs.BoundedBy(
		startTime,
		subnetValidator.EndTime(),
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return ErrPeriodMismatch
	}

	return nil
}

// verifyAddValidatorTx carries out the validation for an AddValidatorTx.
// It returns the tx outputs that should be returned if this validator is not
// added to the staking set.
func verifyAddValidatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddValidatorTx,
) (
	[]*avax.TransferableOutput,
	error,
) {
	currentTimestamp := chainState.GetTimestamp()
	if backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp) {
		return nil, ErrAddValidatorTxPostDurango
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, false /*=isDurangoActive*/); err != nil {
		return nil, err
	}

	startTime := tx.StartTime()
	duration := tx.EndTime().Sub(startTime)
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

	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, ErrStakeTooLong
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return nil, fmt.Errorf("getting utxos %w", err)
	}

	if !backend.Bootstrapped.Get() {
		return outs, nil
	}

	if err := verifyStakerStartTime(false /*=isDurangoActive*/, currentTimestamp, startTime); err != nil {
		return nil, err
	}

	_, err = GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
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
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return nil, err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return nil, fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return outs, nil
}

// verifyAddSubnetValidatorTx carries out the validation for an
// AddSubnetValidatorTx.
func verifyAddSubnetValidatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddSubnetValidatorTx,
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

	startTime := currentTimestamp
	if !isDurangoActive {
		startTime = tx.StartTime()
	}
	duration := tx.EndTime().Sub(startTime)

	switch {
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

	if err := verifySubnetValidatorPrimaryNetworkRequirements(isDurangoActive, chainState, tx.Validator); err != nil {
		return err
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(backend.Fx, chainState, sTx, tx.SubnetValidator.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return fmt.Errorf("getting utxos %w", err)
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
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
func verifyRemoveSubnetValidatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.RemoveSubnetValidatorTx,
) (*state.Staker, bool, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, false, err
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
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
			"%s %w of %s: %w",
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

	baseTxCreds, err := verifySubnetAuthorization(backend.Fx, chainState, sTx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return nil, false, err
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return nil, false, fmt.Errorf("getting utxos: %w", err)
	}

	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return nil, false, err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return nil, false, fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return vdr, isCurrentValidator, nil
}

// verifyAddDelegatorTx carries out the validation for an AddDelegatorTx.
// It returns the tx outputs that should be returned if this delegator is not
// added to the staking set.
func verifyAddDelegatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddDelegatorTx,
) (
	[]*avax.TransferableOutput,
	error,
) {
	currentTimestamp := chainState.GetTimestamp()
	if backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp) {
		return nil, ErrAddDelegatorTxPostDurango
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, false /*=isDurangoActive*/); err != nil {
		return nil, err
	}

	var (
		endTime   = tx.EndTime()
		startTime = tx.StartTime()
		duration  = endTime.Sub(startTime)
	)
	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, ErrStakeTooLong

	case tx.Validator.Wght < backend.Config.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, ErrWeightTooSmall
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return nil, fmt.Errorf("getting utxos: %w", err)
	}

	if !backend.Bootstrapped.Get() {
		return outs, nil
	}

	if err := verifyStakerStartTime(false /*=isDurangoActive*/, currentTimestamp, startTime); err != nil {
		return nil, err
	}

	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	maximumWeight, err := safemath.Mul(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
	if err != nil {
		return nil, ErrStakeOverflow
	}

	if backend.Config.UpgradeConfig.IsApricotPhase3Activated(currentTimestamp) {
		maximumWeight = min(maximumWeight, backend.Config.MaxValidatorStake)
	}

	if !txs.BoundedBy(
		startTime,
		endTime,
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return nil, ErrPeriodMismatch
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
		return nil, err
	}
	if overDelegated {
		return nil, ErrOverDelegated
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return nil, err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return nil, fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return outs, nil
}

// verifyAddPermissionlessValidatorTx carries out the validation for an
// AddPermissionlessValidatorTx.
func verifyAddPermissionlessValidatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessValidatorTx,
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

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return fmt.Errorf("getting utxos %w", err)
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// verifyAddPermissionlessDelegatorTx carries out the validation for an
// AddPermissionlessDelegatorTx.
func verifyAddPermissionlessDelegatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessDelegatorTx,
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

	if !txs.BoundedBy(
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
		//            transactions that implement [txs.ValidatorTx]. All
		//            validator transactions implement this interface except the
		//            AddSubnetValidatorTx. AddSubnetValidatorTx is the only
		//            permissioned validator, so we verify this delegator is
		//            pointing to a permissionless validator.
		if validator.Priority.IsPermissionedValidator() {
			return ErrDelegateToPermissionedValidator
		}
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return fmt.Errorf("getting utxos %w", err)
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * [sTx]'s creds authorize it to spend the stated inputs.
// * [sTx]'s creds authorize it to transfer ownership of [tx.Subnet].
// * The flow checker passes.
func verifyTransferSubnetOwnershipTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.TransferSubnetOwnershipTx,
) error {
	var (
		currentTimestamp = chainState.GetTimestamp()
		upgrades         = backend.Config.UpgradeConfig
	)
	if !upgrades.IsDurangoActivated(currentTimestamp) {
		return ErrDurangoUpgradeNotActive
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

	baseTxCreds, err := verifySubnetAuthorization(backend.Fx, chainState, sTx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return fmt.Errorf("getting utxos %w", err)
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// verifyAddContinuousValidatorTx carries out the validation for an AddContinuousValidatorTx.
func verifyAddContinuousValidatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddContinuousValidatorTx,
) error {
	var (
		currentTimestamp = chainState.GetTimestamp()
		upgrades         = backend.Config.UpgradeConfig
	)
	if !upgrades.IsHeliconActivated(currentTimestamp) {
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

	duration := tx.PeriodDuration()

	stakedAssetID := tx.StakeOuts[0].AssetID()
	switch {
	case tx.Weight() < validatorRules.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return ErrWeightTooSmall

	case tx.Weight() > validatorRules.maxValidatorStake:
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

	_, err = GetValidator(chainState, tx.SubnetID(), tx.NodeID())
	switch {
	case err == nil:
		return fmt.Errorf(
			"%w: %s on %s",
			ErrDuplicateValidator,
			tx.NodeID(),
			tx.SubnetID(),
		)
	case errors.Is(err, database.ErrNotFound):
		// OK: validator not found

	default:
		return fmt.Errorf(
			"failed to check if validator %s is on subnet %s: %w",
			tx.NodeID(),
			tx.SubnetID(),
			err,
		)
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return fmt.Errorf("getting utxos %w", err)
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// verifySetAutoRestakeConfigTx carries out the validation for an AutoRestakeConfigTx.
func verifySetAutoRestakeConfigTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.SetAutoRestakeConfigTx,
) (*state.Staker, error) {
	var (
		currentTimestamp = chainState.GetTimestamp()
		upgrades         = backend.Config.UpgradeConfig
	)
	if !upgrades.IsHeliconActivated(currentTimestamp) {
		return nil, errHeliconUpgradeNotActive
	}

	if err := tx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return nil, err
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return nil, nil
	}

	stakerTx, _, err := chainState.GetTx(tx.TxID)
	if err != nil {
		return nil, ErrMissingStakerTx
	}

	continuousStakerTx, ok := stakerTx.Unsigned.(txs.ContinuousStaker)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrInvalidStakerTxType, stakerTx.Unsigned)
	}

	validatorRules, err := getValidatorRules(backend, chainState, continuousStakerTx.SubnetID())
	if err != nil {
		return nil, err
	}

	if tx.HasPeriod {
		duration := time.Duration(tx.Period) * time.Second
		switch {
		case duration > 0 && duration < validatorRules.minStakeDuration:
			return nil, ErrStakeTooShort

		case duration > validatorRules.maxStakeDuration:
			return nil, ErrStakeTooLong
		}
	}

	validator, err := chainState.GetCurrentValidator(continuousStakerTx.SubnetID(), continuousStakerTx.NodeID())
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrMissingValidator
		}

		return nil, fmt.Errorf("failed to get validator %s from state: %w", continuousStakerTx.NodeID(), err)
	}

	if tx.TxID != validator.TxID {
		// This can happen if a validator restaked with the same node id.
		// In this case, TxID should be the latest transaction of the continuous validator.
		return nil, fmt.Errorf("%w: wrong tx id", ErrInvalidStakerTx)
	}

	baseTxCreds, err := verifyAuthorization(backend.Fx, sTx, continuousStakerTx.Owner(), tx.Auth)
	if err != nil {
		return nil, err
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return nil, fmt.Errorf("getting utxos %w", err)
	}

	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return nil, err
	}

	producedAVAX, err = safemath.Add(producedAVAX, fee)
	if err != nil {
		return nil, fmt.Errorf("adding fee: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return validator, nil
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
