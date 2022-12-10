// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	errWeightTooSmall                  = errors.New("weight of this validator is too low")
	errWeightTooLarge                  = errors.New("weight of this validator is too large")
	errInsufficientDelegationFee       = errors.New("staker charges an insufficient delegation fee")
	errStakeTooShort                   = errors.New("staking period is too short")
	errStakeTooLong                    = errors.New("staking period is too long")
	errFlowCheckFailed                 = errors.New("flow check failed")
	errFutureStakeTime                 = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	errValidatorSubset                 = errors.New("all subnets' staking period must be a subset of the primary network")
	errNotValidator                    = errors.New("isn't a current or pending validator")
	errRemovePermissionlessValidator   = errors.New("attempting to remove permissionless validator")
	errStakeOverflow                   = errors.New("validator stake exceeds limit")
	errOverDelegated                   = errors.New("validator would be over delegated")
	errIsNotTransformSubnetTx          = errors.New("is not a transform subnet tx")
	errTimestampNotBeforeStartTime     = errors.New("chain timestamp not before start time")
	errDuplicateValidator              = errors.New("duplicate validator")
	errDelegateToPermissionedValidator = errors.New("delegation to permissioned validator")
	errWrongStakedAssetID              = errors.New("incorrect staked assetID")
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

	case tx.DelegationShares < backend.Config.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return nil, errInsufficientDelegationFee

	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, errStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, errStakeTooLong
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	if !backend.Bootstrapped.GetValue() {
		return outs, nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current time
	startTime := tx.StartTime()
	if !currentTimestamp.Before(startTime) {
		return nil, fmt.Errorf(
			"%w: %s >= %s",
			errTimestampNotBeforeStartTime,
			currentTimestamp,
			startTime,
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
			backend.Ctx.AVAXAssetID: backend.Config.AddPrimaryNetworkValidatorFee,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %s", errFlowCheckFailed, err)
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
	}

	if !backend.Bootstrapped.GetValue() {
		return nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current timestamp
	validatorStartTime := tx.StartTime()
	if !currentTimestamp.Before(validatorStartTime) {
		return fmt.Errorf(
			"%w: %s >= %s",
			errTimestampNotBeforeStartTime,
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

	baseTxCreds, err := verifyPoASubnetAuthorization(backend, chainState, sTx, tx.Validator.Subnet, tx.SubnetAuth)
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
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if validatorStartTime.After(maxStartTime) {
		return errFutureStakeTime
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
			"%s %w of %s: %s",
			tx.NodeID,
			errNotValidator,
			tx.Subnet,
			err,
		)
	}

	if vdr.Priority != txs.SubnetPermissionedValidatorCurrentPriority &&
		vdr.Priority != txs.SubnetPermissionedValidatorPendingPriority {
		return nil, false, errRemovePermissionlessValidator
	}

	if !backend.Bootstrapped.GetValue() {
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
		return nil, false, fmt.Errorf("%w: %s", errFlowCheckFailed, err)
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

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	if !backend.Bootstrapped.GetValue() {
		return outs, nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current timestamp
	validatorStartTime := tx.StartTime()
	if !currentTimestamp.Before(validatorStartTime) {
		return nil, fmt.Errorf(
			"%w: %s >= %s",
			errTimestampNotBeforeStartTime,
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
		maximumWeight = math.Min(maximumWeight, backend.Config.MaxValidatorStake)
	}

	txID := sTx.ID()
	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return nil, err
	}

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
			backend.Ctx.AVAXAssetID: backend.Config.AddPrimaryNetworkDelegatorFee,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if validatorStartTime.After(maxStartTime) {
		return nil, errFutureStakeTime
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

	if !backend.Bootstrapped.GetValue() {
		return nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current time
	startTime := tx.StartTime()
	if !currentTimestamp.Before(startTime) {
		return fmt.Errorf(
			"%w: %s >= %s",
			errTimestampNotBeforeStartTime,
			currentTimestamp,
			startTime,
		)
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	duration := tx.Validator.Duration()
	stakedAssetID := tx.StakeOuts[0].AssetID()
	switch {
	case tx.Validator.Wght < validatorRules.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall

	case tx.Validator.Wght > validatorRules.maxValidatorStake:
		// Ensure validator isn't staking too much
		return errWeightTooLarge

	case tx.DelegationShares < validatorRules.minDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return errInsufficientDelegationFee

	case duration < validatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > validatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case stakedAssetID != validatorRules.assetID:
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
			errDuplicateValidator,
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

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if !tx.Validator.BoundedBy(primaryNetworkValidator.StartTime, primaryNetworkValidator.EndTime) {
			return errValidatorSubset
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
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return errFutureStakeTime
	}

	return nil
}

type addValidatorRules struct {
	assetID           ids.ID
	minValidatorStake uint64
	maxValidatorStake uint64
	minStakeDuration  time.Duration
	maxStakeDuration  time.Duration
	minDelegationFee  uint32
}

func getValidatorRules(
	backend *Backend,
	chainState state.Chain,
	subnetID ids.ID,
) (*addValidatorRules, error) {
	if subnetID == constants.PrimaryNetworkID {
		return &addValidatorRules{
			assetID:           backend.Ctx.AVAXAssetID,
			minValidatorStake: backend.Config.MinValidatorStake,
			maxValidatorStake: backend.Config.MaxValidatorStake,
			minStakeDuration:  backend.Config.MinStakeDuration,
			maxStakeDuration:  backend.Config.MaxStakeDuration,
			minDelegationFee:  backend.Config.MinDelegationFee,
		}, nil
	}

	transformSubnetIntf, err := chainState.GetSubnetTransformation(subnetID)
	if err != nil {
		return nil, err
	}
	transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
	if !ok {
		return nil, errIsNotTransformSubnetTx
	}

	return &addValidatorRules{
		assetID:           transformSubnet.AssetID,
		minValidatorStake: transformSubnet.MinValidatorStake,
		maxValidatorStake: transformSubnet.MaxValidatorStake,
		minStakeDuration:  time.Duration(transformSubnet.MinStakeDuration) * time.Second,
		maxStakeDuration:  time.Duration(transformSubnet.MaxStakeDuration) * time.Second,
		minDelegationFee:  transformSubnet.MinDelegationFee,
	}, nil
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

	if !backend.Bootstrapped.GetValue() {
		return nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current timestamp
	startTime := tx.StartTime()
	if !currentTimestamp.Before(startTime) {
		return fmt.Errorf(
			"chain timestamp (%s) not before validator's start time (%s)",
			currentTimestamp,
			startTime,
		)
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	duration := tx.Validator.Duration()
	stakedAssetID := tx.StakeOuts[0].AssetID()
	switch {
	case tx.Validator.Wght < delegatorRules.minDelegatorStake:
		// Ensure delegator is staking at least the minimum amount
		return errWeightTooSmall

	case duration < delegatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > delegatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case stakedAssetID != delegatorRules.assetID:
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

	maximumWeight, err := math.Mul64(
		uint64(delegatorRules.maxValidatorWeightFactor),
		validator.Weight,
	)
	if err != nil {
		maximumWeight = stdmath.MaxUint64
	}
	maximumWeight = math.Min(maximumWeight, delegatorRules.maxValidatorStake)

	txID := sTx.ID()
	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	canDelegate, err := canDelegate(chainState, validator, maximumWeight, newStaker)
	if err != nil {
		return err
	}
	if !canDelegate {
		return errOverDelegated
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
		if validator.Priority == txs.SubnetPermissionedValidatorCurrentPriority ||
			validator.Priority == txs.SubnetPermissionedValidatorPendingPriority {
			return errDelegateToPermissionedValidator
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
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return errFutureStakeTime
	}

	return nil
}

type addDelegatorRules struct {
	assetID                  ids.ID
	minDelegatorStake        uint64
	maxValidatorStake        uint64
	minStakeDuration         time.Duration
	maxStakeDuration         time.Duration
	maxValidatorWeightFactor byte
}

func getDelegatorRules(
	backend *Backend,
	chainState state.Chain,
	subnetID ids.ID,
) (*addDelegatorRules, error) {
	if subnetID == constants.PrimaryNetworkID {
		return &addDelegatorRules{
			assetID:                  backend.Ctx.AVAXAssetID,
			minDelegatorStake:        backend.Config.MinDelegatorStake,
			maxValidatorStake:        backend.Config.MaxValidatorStake,
			minStakeDuration:         backend.Config.MinStakeDuration,
			maxStakeDuration:         backend.Config.MaxStakeDuration,
			maxValidatorWeightFactor: MaxValidatorWeightFactor,
		}, nil
	}

	transformSubnetIntf, err := chainState.GetSubnetTransformation(subnetID)
	if err != nil {
		return nil, err
	}
	transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
	if !ok {
		return nil, errIsNotTransformSubnetTx
	}

	return &addDelegatorRules{
		assetID:                  transformSubnet.AssetID,
		minDelegatorStake:        transformSubnet.MinDelegatorStake,
		maxValidatorStake:        transformSubnet.MaxValidatorStake,
		minStakeDuration:         time.Duration(transformSubnet.MinStakeDuration) * time.Second,
		maxStakeDuration:         time.Duration(transformSubnet.MaxStakeDuration) * time.Second,
		maxValidatorWeightFactor: transformSubnet.MaxValidatorWeightFactor,
	}, nil
}
