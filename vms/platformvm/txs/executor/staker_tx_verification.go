// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	ErrWeightTooSmall                  = errors.New("weight of this validator is too low")
	ErrWeightTooLarge                  = errors.New("weight of this validator is too large")
	ErrInsufficientDelegationFee       = errors.New("staker charges an insufficient delegation fee")
	ErrStakeTooShort                   = errors.New("staking period is too short")
	ErrStakeTooLong                    = errors.New("staking period is too long")
	ErrFlowCheckFailed                 = errors.New("flow check failed")
	ErrFutureStakeTime                 = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrNotValidator                    = errors.New("isn't a current or pending validator")
	ErrRemovePermissionlessValidator   = errors.New("attempting to remove permissionless validator")
	ErrStakeOverflow                   = errors.New("validator stake exceeds limit")
	ErrPeriodMismatch                  = errors.New("proposed staking period is not inside dependant staking period")
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
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddValidatorTx,
) (
	[]*avax.TransferableOutput,
	error,
) {
	currentTimestamp := chainState.GetTimestamp()
	if backend.Config.IsDurangoActivated(currentTimestamp) {
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

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	if !backend.Bootstrapped.Get() {
		return outs, nil
	}

	if err := verifyStakerStartTime(false /*=isDurangoActive*/, currentTimestamp, startTime); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return outs, verifyStakerStartsSoon(false /*=isDurangoActive*/, currentTimestamp, startTime)
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

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.IsDurangoActivated(currentTimestamp)
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
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return verifyStakerStartsSoon(isDurangoActive, currentTimestamp, startTime)
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
		isDurangoActive  = backend.Config.IsDurangoActivated(currentTimestamp)
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
		return nil, false, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
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
	currentTimestamp := chainState.GetTimestamp()
	if backend.Config.IsDurangoActivated(currentTimestamp) {
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

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

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

	maximumWeight, err := safemath.Mul64(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
	if err != nil {
		return nil, ErrStakeOverflow
	}

	if backend.Config.IsApricotPhase3Activated(currentTimestamp) {
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
		return nil, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return outs, verifyStakerStartsSoon(false /*=isDurangoActive*/, currentTimestamp, startTime)
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

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.IsDurangoActivated(currentTimestamp)
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

	var txFee uint64
	if tx.Subnet != constants.PrimaryNetworkID {
		if err := verifySubnetValidatorPrimaryNetworkRequirements(isDurangoActive, chainState, tx.Validator); err != nil {
			return err
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
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return verifyStakerStartsSoon(isDurangoActive, currentTimestamp, startTime)
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

	var (
		currentTimestamp = chainState.GetTimestamp()
		isDurangoActive  = backend.Config.IsDurangoActivated(currentTimestamp)
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

	maximumWeight, err := safemath.Mul64(
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
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return verifyStakerStartsSoon(isDurangoActive, currentTimestamp, startTime)
}

// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * [sTx]'s creds authorize it to spend the stated inputs.
// * [sTx]'s creds authorize it to transfer ownership of [tx.Subnet].
// * The flow checker passes.
func verifyTransferSubnetOwnershipTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.TransferSubnetOwnershipTx,
) error {
	if !backend.Config.IsDurangoActivated(chainState.GetTimestamp()) {
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

	baseTxCreds, err := verifySubnetAuthorization(backend, chainState, sTx, tx.Subnet, tx.SubnetAuth)
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
			backend.Ctx.AVAXAssetID: backend.Config.TxFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
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

func verifyStakerStartsSoon(isDurangoActive bool, chainTime, stakerStartTime time.Time) error {
	if isDurangoActive {
		return nil
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := chainTime.Add(MaxFutureStartTime)
	if stakerStartTime.After(maxStartTime) {
		return ErrFutureStakeTime
	}
	return nil
}
