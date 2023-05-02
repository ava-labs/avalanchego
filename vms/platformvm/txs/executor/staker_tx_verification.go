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
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	ErrWeightTooSmall                  = errors.New("weight of this validator is too low")
	ErrWeightTooLarge                  = errors.New("weight of this validator is too large")
	ErrInsufficientDelegationFee       = errors.New("staker charges an insufficient delegation fee")
	ErrStakeTooShort                   = errors.New("staking period is too short")
	ErrStakeTooLong                    = errors.New("staking period is too long")
	ErrFlowCheckFailed                 = errors.New("flow check failed")
	ErrFutureStakeTime                 = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrValidatorSubset                 = errors.New("all subnets' staking period must be a subset of the primary network")
	ErrNotValidator                    = errors.New("isn't a current or pending validator")
	ErrRemovePermissionlessValidator   = errors.New("attempting to remove permissionless validator")
	ErrStakeOverflow                   = errors.New("validator stake exceeds limit")
	ErrOverDelegated                   = errors.New("validator would be over delegated")
	ErrIsNotTransformSubnetTx          = errors.New("is not a transform subnet tx")
	ErrTimestampNotBeforeStartTime     = errors.New("chain timestamp not before start time")
	ErrStartTimeMustBeZero             = errors.New("staker start time must be zero")
	ErrAlreadyValidator                = errors.New("already a validator")
	ErrDuplicateValidator              = errors.New("duplicate validator")
	ErrDelegateToPermissionedValidator = errors.New("delegation to permissioned validator")
	ErrWrongStakedAssetID              = errors.New("incorrect staked assetID")
	ErrUnauthorizedStakerStopping      = errors.New("unauthorized staker stopping")
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

	duration := tx.StakingPeriod()
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

	currentTimestamp := chainState.GetTimestamp()
	startTime := tx.StartTime()
	if backend.Config.IsContinuousStakingActivated(currentTimestamp) {
		if startTime != txs.StakerZeroTime {
			return nil, fmt.Errorf(
				"%w: %s",
				ErrStartTimeMustBeZero,
				startTime,
			)
		}
	} else {
		// Ensure the proposed validator starts after the current time
		if !currentTimestamp.Before(startTime) {
			return nil, fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				currentTimestamp,
				startTime,
			)
		}
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	// Strickly speaking this check is needed for Pre Continuous Staking fork txs.
	// However Post Continuous Staking fork txs are guaranteed to satisfy the test
	// (start time is zero). I didn't bother guarding the check (which must be the
	// last one made).
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return nil, ErrFutureStakeTime
	}

	return outs, nil
}

// verifyAddSubnetValidatorTx carries out the validation for an
// AddSubnetValidatorTx.
// Returns the primary network validator EndTime, which bounds
// subnet staker endTime
func verifyAddSubnetValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddSubnetValidatorTx,
) (time.Time, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return time.Time{}, err
	}

	stakingPeriod := tx.StakingPeriod()
	switch {
	case stakingPeriod < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return time.Time{}, ErrStakeTooShort

	case stakingPeriod > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return time.Time{}, ErrStakeTooLong
	}

	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err == database.ErrNotFound {
		return time.Time{}, fmt.Errorf(
			"%s %w of the primary network",
			tx.Validator.NodeID,
			ErrNotValidator,
		)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	if !backend.Bootstrapped.Get() {
		return primaryNetworkValidator.EndTime, nil
	}

	currentChainTime := chainState.GetTimestamp()
	startTime := tx.StartTime()
	if backend.Config.IsContinuousStakingActivated(currentChainTime) {
		if startTime != txs.StakerZeroTime {
			return time.Time{}, fmt.Errorf(
				"%w: %s",
				ErrStartTimeMustBeZero,
				startTime,
			)
		}
	} else {
		// Ensure the proposed validator starts after the current timestamp
		if !currentChainTime.Before(startTime) {
			return time.Time{}, fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				currentChainTime,
				startTime,
			)
		}
	}

	if _, err = GetValidator(chainState, tx.SubnetValidator.Subnet, tx.Validator.NodeID); err == nil {
		return time.Time{}, fmt.Errorf(
			"attempted to issue %w for %s on subnet %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.SubnetValidator.Subnet,
		)
	}
	if err != database.ErrNotFound {
		return time.Time{}, fmt.Errorf(
			"failed to find whether %s is a subnet validator: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	if backend.Config.IsContinuousStakingActivated(currentChainTime) {
		if stakingPeriod > primaryNetworkValidator.StakingPeriod {
			return time.Time{}, ErrValidatorSubset
		}

		// TODO ABENEGIA: we assume that the subnet validator may be accepted
		// if its primary network counterpart will validate for at least another
		// period. We may change this
		firstStakinPeriodEndTime := currentChainTime.Add(stakingPeriod)
		if firstStakinPeriodEndTime.After(primaryNetworkValidator.EndTime) {
			return time.Time{}, ErrValidatorSubset
		}
	} else if !tx.Validator.BoundedBy(primaryNetworkValidator.StartTime, primaryNetworkValidator.EndTime) {
		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		return time.Time{}, ErrValidatorSubset
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(backend, chainState, sTx, tx.SubnetValidator.Subnet, tx.SubnetAuth)
	if err != nil {
		return time.Time{}, err
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
		return time.Time{}, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	// Strickly speaking this check is needed for Pre Continuous Staking fork txs.
	// However Post Continuous Staking fork txs are guaranteed to satisfy the test
	// (start time is zero). I didn't bother guarding the check (which must be the
	// last one made).
	maxStartTime := currentChainTime.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return time.Time{}, ErrFutureStakeTime
	}

	return primaryNetworkValidator.EndTime, nil
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
// added to the staking set; moreover it returns the primary validator end time.
func verifyAddDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddDelegatorTx,
) (
	[]*avax.TransferableOutput,
	time.Time,
	error,
) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, time.Time{}, err
	}

	duration := tx.StakingPeriod()
	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, time.Time{}, ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, time.Time{}, ErrStakeTooLong

	case tx.Validator.Wght < backend.Config.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, time.Time{}, ErrWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	if !backend.Bootstrapped.Get() {
		return outs, primaryNetworkValidator.EndTime, nil
	}

	currentTimestamp := chainState.GetTimestamp()
	startTime := tx.StartTime()
	if backend.Config.IsContinuousStakingActivated(currentTimestamp) {
		if startTime != txs.StakerZeroTime {
			return nil, time.Time{}, fmt.Errorf(
				"%w: %s",
				ErrStartTimeMustBeZero,
				startTime,
			)
		}
	} else {
		// Ensure the proposed validator starts after the current timestamp
		if !currentTimestamp.Before(startTime) {
			return nil, time.Time{}, fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				currentTimestamp,
				startTime,
			)
		}
	}

	maximumWeight, err := math.Mul64(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
	if err != nil {
		return nil, time.Time{}, ErrStakeOverflow
	}

	if backend.Config.IsApricotPhase3Activated(currentTimestamp) {
		maximumWeight = math.Min(maximumWeight, backend.Config.MaxValidatorStake)
	}

	txID := sTx.ID()
	var newStaker *state.Staker
	if backend.Config.IsContinuousStakingActivated(currentTimestamp) {
		// potential reward does not matter
		newStaker, err = state.NewCurrentStaker(txID, tx, currentTimestamp, 0)
	} else {
		newStaker, err = state.NewPendingStaker(txID, tx)
	}
	if err != nil {
		return nil, time.Time{}, err
	}

	canDelegate, err := canDelegate(chainState, primaryNetworkValidator, maximumWeight, newStaker)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !canDelegate {
		return nil, time.Time{}, ErrOverDelegated
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
		return nil, time.Time{}, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	// Strickly speaking this check is needed for Pre Continuous Staking fork txs.
	// However Post Continuous Staking fork txs are guaranteed to satisfy the test
	// (start time is zero). I didn't bother guarding the check (which must be the
	// last one made).
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return nil, time.Time{}, ErrFutureStakeTime
	}

	return outs, primaryNetworkValidator.EndTime, nil
}

// verifyAddPermissionlessValidatorTx carries out the validation for an
// AddPermissionlessValidatorTx.
func verifyAddPermissionlessValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessValidatorTx,
) (time.Time, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return time.Time{}, err
	}

	var (
		primaryNetworkValidator *state.Staker
		primaryValidatorEndTime = mockable.MaxTime
		err                     error
	)
	if tx.Subnet != constants.PlatformChainID {
		primaryNetworkValidator, err = GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return time.Time{}, fmt.Errorf(
				"failed to fetch the primary network validator for %s: %w",
				tx.Validator.NodeID,
				err,
			)
		}
		primaryValidatorEndTime = primaryNetworkValidator.EndTime
	}

	if !backend.Bootstrapped.Get() {
		return primaryValidatorEndTime, nil
	}

	currentChainTime := chainState.GetTimestamp()
	startTime := tx.StartTime()
	if backend.Config.IsContinuousStakingActivated(currentChainTime) {
		if startTime != txs.StakerZeroTime {
			return time.Time{}, fmt.Errorf(
				"%w: %s",
				ErrStartTimeMustBeZero,
				startTime,
			)
		}
	} else {
		// Ensure the proposed validator starts after the current time
		if !currentChainTime.Before(startTime) {
			return time.Time{}, fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				currentChainTime,
				startTime,
			)
		}
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return time.Time{}, err
	}

	var (
		stakingPeriod = tx.StakingPeriod()
		stakedAssetID = tx.StakeOuts[0].AssetID()
	)
	switch {
	case tx.Validator.Wght < validatorRules.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return time.Time{}, ErrWeightTooSmall

	case tx.Validator.Wght > validatorRules.maxValidatorStake:
		// Ensure validator isn't staking too much
		return time.Time{}, ErrWeightTooLarge

	case tx.DelegationShares < validatorRules.minDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return time.Time{}, ErrInsufficientDelegationFee

	case stakingPeriod < validatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return time.Time{}, ErrStakeTooShort

	case stakingPeriod > validatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return time.Time{}, ErrStakeTooLong

	case stakedAssetID != validatorRules.assetID:
		// Wrong assetID used
		return time.Time{}, fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			validatorRules.assetID,
			stakedAssetID,
		)
	}

	_, err = GetValidator(chainState, tx.Subnet, tx.Validator.NodeID)
	if err == nil {
		return time.Time{}, fmt.Errorf(
			"%w: %s on %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.Subnet,
		)
	}
	if err != database.ErrNotFound {
		return time.Time{}, fmt.Errorf(
			"failed to find whether %s is a validator on %s: %w",
			tx.Validator.NodeID,
			tx.Subnet,
			err,
		)
	}

	var txFee uint64
	if tx.Subnet != constants.PrimaryNetworkID {
		if backend.Config.IsContinuousStakingActivated(currentChainTime) {
			if stakingPeriod > primaryNetworkValidator.StakingPeriod {
				return time.Time{}, ErrValidatorSubset
			}

			// TODO ABENEGIA: we assume that the subnet validator may be accepted
			// if its primary network counterpart will validate for at least another
			// period. We may change this
			candidateEndTime := currentChainTime.Add(stakingPeriod)
			if candidateEndTime.After(primaryNetworkValidator.EndTime) {
				return time.Time{}, ErrValidatorSubset
			}
		} else if !tx.Validator.BoundedBy(primaryNetworkValidator.StartTime, primaryNetworkValidator.EndTime) {
			// Ensure that the period this validator validates the specified subnet
			// is a subset of the time they validate the primary network.
			return time.Time{}, ErrValidatorSubset
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
		return time.Time{}, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	// Strickly speaking this check is needed for Pre Continuous Staking fork txs.
	// However Post Continuous Staking fork txs are guaranteed to satisfy the test
	// (start time is zero). I didn't bother guarding the check (which must be the
	// last one made).
	maxStartTime := currentChainTime.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return time.Time{}, ErrFutureStakeTime
	}

	return primaryValidatorEndTime, nil
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
		return nil, ErrIsNotTransformSubnetTx
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
) (time.Time, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return time.Time{}, err
	}

	validator, err := GetValidator(chainState, tx.Subnet, tx.Validator.NodeID)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"failed to fetch the validator for %s on %s: %w",
			tx.Validator.NodeID,
			tx.Subnet,
			err,
		)
	}

	if !backend.Bootstrapped.Get() {
		return validator.EndTime, nil
	}

	currentTimestamp := chainState.GetTimestamp()
	startTime := tx.StartTime()
	if backend.Config.IsContinuousStakingActivated(currentTimestamp) {
		if startTime != txs.StakerZeroTime {
			return time.Time{}, fmt.Errorf(
				"%w: %s",
				ErrStartTimeMustBeZero,
				startTime,
			)
		}
	} else {
		// Ensure the proposed validator starts after the current timestamp
		if !currentTimestamp.Before(startTime) {
			return time.Time{}, fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				startTime,
			)
		}
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return time.Time{}, err
	}

	var (
		duration      = tx.StakingPeriod()
		stakedAssetID = tx.StakeOuts[0].AssetID()
	)
	switch {
	case tx.Validator.Wght < delegatorRules.minDelegatorStake:
		// Ensure delegator is staking at least the minimum amount
		return time.Time{}, ErrWeightTooSmall

	case duration < delegatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return time.Time{}, ErrStakeTooShort

	case duration > delegatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return time.Time{}, ErrStakeTooLong

	case stakedAssetID != delegatorRules.assetID:
		// Wrong assetID used
		return time.Time{}, fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			delegatorRules.assetID,
			stakedAssetID,
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
	var newStaker *state.Staker
	if backend.Config.IsContinuousStakingActivated(currentTimestamp) {
		// potential reward does not matter
		newStaker, err = state.NewCurrentStaker(txID, tx, currentTimestamp, 0)
	} else {
		newStaker, err = state.NewPendingStaker(txID, tx)
	}
	if err != nil {
		return time.Time{}, err
	}

	canDelegate, err := canDelegate(chainState, validator, maximumWeight, newStaker)
	if err != nil {
		return time.Time{}, err
	}
	if !canDelegate {
		return time.Time{}, ErrOverDelegated
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
			return time.Time{}, ErrDelegateToPermissionedValidator
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
		return time.Time{}, fmt.Errorf("%w: %v", ErrFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	// Strickly speaking this check is needed for Pre Continuous Staking fork txs.
	// However Post Continuous Staking fork txs are guaranteed to satisfy the test
	// (start time is zero). I didn't bother guarding the check (which must be the
	// last one made).
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return time.Time{}, ErrFutureStakeTime
	}

	return validator.EndTime, nil
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
		return nil, ErrIsNotTransformSubnetTx
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

func verifyStopStakerTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.StopStakerTx,
) ([]*state.Staker, time.Time, error) {
	if !backend.Config.IsContinuousStakingActivated(chainState.GetTimestamp()) {
		return nil, time.Time{}, errors.New("StopStakerTx cannot be accepted before continuous staking fork activation")
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, time.Time{}, err
	}

	// retrieve staker to be stopped
	var (
		txID         = tx.TxID
		stakerToStop *state.Staker
	)

	stakersIt, err := chainState.GetCurrentStakerIterator()
	if err != nil {
		return nil, time.Time{}, err
	}
	for stakersIt.Next() {
		if stakersIt.Value().TxID == txID {
			stakerToStop = stakersIt.Value()
			break
		}
	}
	if stakerToStop == nil {
		return nil, time.Time{}, errors.New("could not find staker to stop among current ones")
	}

	if backend.Bootstrapped.Get() {
		// Full verification only one bootstrapping is done. Otherwise only execution

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

	if !stakerToStop.Priority.IsValidator() || stakerToStop.SubnetID != constants.PrimaryNetworkID {
		return []*state.Staker{stakerToStop}, stakerToStop.EarliestStopTime(), nil
	}

	// primary network validators are special since, when stopping them, we need to handle
	// their delegators and subnet validators as well, to make sure they don't outlive the
	// primary network validators
	res := []*state.Staker{stakerToStop}
	stakersIt, err = chainState.GetCurrentStakerIterator()
	if err != nil {
		return nil, time.Time{}, err
	}
	for stakersIt.Next() {
		staker := stakersIt.Value()
		if staker.NodeID == stakerToStop.NodeID && staker.TxID != stakerToStop.TxID {
			res = append(res, staker)
		}
	}
	return res, stakerToStop.EarliestStopTime(), nil
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
			"staker tx not found %q: %v",
			stakerTxID,
			err,
		)
	}

	var stakerOwner fx.Owner
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case txs.ValidatorTx:
		stakerOwner = uStakerTx.ValidationRewardsOwner()
	case txs.DelegatorTx:
		stakerOwner = uStakerTx.RewardsOwner()
	case *txs.AddSubnetValidatorTx:
		signedSubnetTx, _, err := chainState.GetTx(uStakerTx.Subnet)
		if err != nil {
			return nil, fmt.Errorf(
				"tx creating subnet not found %q: %v",
				uStakerTx.Subnet,
				err,
			)
		}
		subnetTx, ok := signedSubnetTx.Unsigned.(*txs.CreateSubnetTx)
		if !ok {
			return nil, ErrWrongTxType
		}
		stakerOwner = subnetTx.Owner
	default:
		return nil, fmt.Errorf(
			"unhandled staker type: %t",
			uStakerTx,
		)
	}

	err = backend.Fx.VerifyPermission(sTx.Unsigned, stakerAuth, stakerCred, stakerOwner)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnauthorizedStakerStopping, err)
	}

	return sTx.Creds[:baseTxCredsLen], nil
}
