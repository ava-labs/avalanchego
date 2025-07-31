// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
)

var (
	ErrChildBlockEarlierThanParent     = errors.New("proposed timestamp before current chain time")
	ErrChildBlockAfterStakerChangeTime = errors.New("proposed timestamp later than next staker change time")
	ErrChildBlockBeyondSyncBound       = errors.New("proposed timestamp is too far in the future relative to local time")
)

// VerifyNewChainTime returns nil if the [newChainTime] is a valid chain time.
// Requires:
//   - [newChainTime] >= [currentChainTime]: to ensure chain time advances
//     monotonically.
//   - [newChainTime] <= [now] + [SyncBound]: to ensure chain time approximates
//     "real" time.
//   - [newChainTime] <= [nextStakerChangeTime]: so that no staking set changes
//     are skipped.
func VerifyNewChainTime(
	config fee.Config,
	newChainTime time.Time,
	now time.Time,
	currentState state.Chain,
) error {
	currentChainTime := currentState.GetTimestamp()
	if newChainTime.Before(currentChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s), chain time (%s)",
			ErrChildBlockEarlierThanParent,
			newChainTime,
			currentChainTime,
		)
	}

	// Only allow timestamp to be reasonably far forward
	maxNewChainTime := now.Add(SyncBound)
	if newChainTime.After(maxNewChainTime) {
		return fmt.Errorf(
			"%w, proposed time (%s), local time (%s)",
			ErrChildBlockBeyondSyncBound,
			newChainTime,
			now,
		)
	}

	// nextStakerChangeTime is calculated last to ensure that the function is
	// able to be calculated efficiently.
	nextStakerChangeTime, err := state.GetNextStakerChangeTime(
		config,
		currentState,
		newChainTime,
	)
	if err != nil {
		return fmt.Errorf("could not verify block timestamp: %w", err)
	}

	// Only allow timestamp to move as far forward as the time of the next
	// staker set change
	if newChainTime.After(nextStakerChangeTime) {
		return fmt.Errorf(
			"%w, proposed timestamp (%s), next staker change time (%s)",
			ErrChildBlockAfterStakerChangeTime,
			newChainTime,
			nextStakerChangeTime,
		)
	}
	return nil
}

// AdvanceTimeTo applies all state changes to [parentState] resulting from
// advancing the chain time to [newChainTime].
//
// Returns true iff the validator set changed.
func AdvanceTimeTo(
	backend *Backend,
	parentState state.Chain,
	newChainTime time.Time,
) (bool, error) {
	diff, changed, err := advanceTimeTo(backend, parentState, newChainTime)
	if err != nil {
		return false, err
	}
	return changed, diff.Apply(parentState)
}

// advanceTimeTo returns the state diff on top of parentState resulting from
// advancing the chain time to newChainTime. It also returns a boolean
// indicating if the validator set changed.
//
// parentState is not modified.
func advanceTimeTo(
	backend *Backend,
	parentState state.Chain,
	newChainTime time.Time,
) (state.Diff, bool, error) {
	// We promote pending stakers to current stakers first and remove
	// completed stakers from the current staker set. We assume that any
	// promoted staker will not immediately be removed from the current staker
	// set. This is guaranteed by the following invariants.
	//
	// Invariant: MinStakeDuration > 0 => guarantees [StartTime] != [EndTime]
	// Invariant: [newChainTime] <= nextStakerChangeTime.

	changes, err := state.NewDiffOn(parentState)
	if err != nil {
		return nil, false, err
	}

	// Promote any pending stakers to current if [StartTime] <= [newChainTime].
	//
	// Invariant: It is not safe to modify the state while iterating over it,
	// so we use the parentState's iterator rather than the changes iterator.
	// ParentState must not be modified before this iterator is released.
	pendingStakerIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return nil, false, err
	}
	defer pendingStakerIterator.Release()

	var changed bool
	for pendingStakerIterator.Next() {
		stakerToRemove := pendingStakerIterator.Value()
		if stakerToRemove.StartTime.After(newChainTime) {
			break
		}

		stakerToAdd := *stakerToRemove
		stakerToAdd.NextTime = stakerToRemove.EndTime
		stakerToAdd.Priority = txs.PendingToCurrentPriorities[stakerToRemove.Priority]

		if stakerToRemove.Priority == txs.SubnetPermissionedValidatorPendingPriority {
			if err := changes.PutCurrentValidator(&stakerToAdd); err != nil {
				return nil, false, err
			}
			changes.DeletePendingValidator(stakerToRemove)
			changed = true
			continue
		}

		supply, err := changes.GetCurrentSupply(stakerToRemove.SubnetID)
		if err != nil {
			return nil, false, err
		}

		rewards, err := GetRewardsCalculator(backend, parentState, stakerToRemove.SubnetID)
		if err != nil {
			return nil, false, err
		}

		potentialReward := rewards.Calculate(
			stakerToRemove.EndTime.Sub(stakerToRemove.StartTime),
			stakerToRemove.Weight,
			supply,
		)
		stakerToAdd.PotentialReward = potentialReward

		// Invariant: [rewards.Calculate] can never return a [potentialReward]
		//            such that [supply + potentialReward > maximumSupply].
		changes.SetCurrentSupply(stakerToRemove.SubnetID, supply+potentialReward)

		switch stakerToRemove.Priority {
		case txs.PrimaryNetworkValidatorPendingPriority, txs.SubnetPermissionlessValidatorPendingPriority:
			if err := changes.PutCurrentValidator(&stakerToAdd); err != nil {
				return nil, false, err
			}
			changes.DeletePendingValidator(stakerToRemove)

		case txs.PrimaryNetworkDelegatorApricotPendingPriority, txs.PrimaryNetworkDelegatorBanffPendingPriority, txs.SubnetPermissionlessDelegatorPendingPriority:
			changes.PutCurrentDelegator(&stakerToAdd)
			changes.DeletePendingDelegator(stakerToRemove)

		default:
			return nil, false, fmt.Errorf("expected staker priority got %d", stakerToRemove.Priority)
		}

		changed = true
	}

	// Remove any current stakers whose [EndTime] <= [newChainTime].
	//
	// Invariant: It is not safe to modify the state while iterating over it,
	// so we use the parentState's iterator rather than the changes iterator.
	// ParentState must not be modified before this iterator is released.
	currentStakerIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return nil, false, err
	}
	defer currentStakerIterator.Release()

	for currentStakerIterator.Next() {
		stakerToRemove := currentStakerIterator.Value()
		if stakerToRemove.EndTime.After(newChainTime) {
			break
		}

		// Invariant: Permissioned stakers are encountered first for a given
		//            timestamp because their priority is the smallest.
		if stakerToRemove.Priority != txs.SubnetPermissionedValidatorCurrentPriority {
			// Permissionless stakers are removed by the RewardValidatorTx, not
			// an AdvanceTimeTx.
			break
		}

		changes.DeleteCurrentValidator(stakerToRemove)
		changed = true
	}

	if !backend.Config.UpgradeConfig.IsEtnaActivated(newChainTime) {
		changes.SetTimestamp(newChainTime)
		return changes, changed, nil
	}

	newChainTimeUnix := uint64(newChainTime.Unix())
	if err := removeStaleExpiries(parentState, changes, newChainTimeUnix); err != nil {
		return nil, false, fmt.Errorf("failed to remove stale expiries: %w", err)
	}

	// Calculate number of seconds the time is advancing
	previousChainTime := changes.GetTimestamp()
	seconds := uint64(newChainTime.Sub(previousChainTime) / time.Second)

	advanceDynamicFeeState(backend.Config.DynamicFeeConfig, changes, seconds)
	l1ValidatorsChanged, err := advanceValidatorFeeState(
		backend.Config.ValidatorFeeConfig,
		parentState,
		changes,
		seconds,
	)
	if err != nil {
		return nil, false, fmt.Errorf("failed to advance validator fee state: %w", err)
	}
	changed = changed || l1ValidatorsChanged

	changes.SetTimestamp(newChainTime)
	return changes, changed, nil
}

// Remove all expiries whose timestamp now implies they can never be re-issued.
//
// The expiry timestamp is the time at which it is no longer valid, so any
// expiry with a timestamp less than or equal to the new chain time can be
// removed.
//
// Ref: https://github.com/avalanche-foundation/ACPs/tree/e333b335c34c8692d84259d21bd07b2bb849dc2c/ACPs/77-reinventing-subnets#registerl1validatortx
func removeStaleExpiries(
	parentState state.Chain,
	changes state.Diff,
	newChainTimeUnix uint64,
) error {
	// Invariant: It is not safe to modify the state while iterating over it, so
	// we use the parentState's iterator rather than the changes iterator.
	// ParentState must not be modified before this iterator is released.
	expiryIterator, err := parentState.GetExpiryIterator()
	if err != nil {
		return fmt.Errorf("could not iterate over expiries: %w", err)
	}
	defer expiryIterator.Release()

	for expiryIterator.Next() {
		expiry := expiryIterator.Value()
		// The expiry iterator is sorted in order of increasing timestamp. Once
		// we find a non-expired expiry, we can break.
		if expiry.Timestamp > newChainTimeUnix {
			break
		}

		changes.DeleteExpiry(expiry)
	}
	return nil
}

func advanceDynamicFeeState(
	config gas.Config,
	changes state.Diff,
	seconds uint64,
) {
	dynamicFeeState := changes.GetFeeState()
	dynamicFeeState = dynamicFeeState.AdvanceTime(
		config.MaxCapacity,
		config.MaxPerSecond,
		config.TargetPerSecond,
		seconds,
	)
	changes.SetFeeState(dynamicFeeState)
}

// advanceValidatorFeeState advances the validator fee state by [seconds]. L1
// validators are read from [parentState] and written to [changes] to avoid
// modifying state while an iterator is held.
func advanceValidatorFeeState(
	config fee.Config,
	parentState state.Chain,
	changes state.Diff,
	seconds uint64,
) (bool, error) {
	validatorFeeState := fee.State{
		Current: gas.Gas(changes.NumActiveL1Validators()),
		Excess:  changes.GetL1ValidatorExcess(),
	}
	validatorCost := validatorFeeState.CostOf(config, seconds)

	accruedFees := changes.GetAccruedFees()
	accruedFees, err := math.Add(accruedFees, validatorCost)
	if err != nil {
		return false, fmt.Errorf("could not calculate accrued fees: %w", err)
	}

	// Invariant: It is not safe to modify the state while iterating over it,
	// so we use the parentState's iterator rather than the changes iterator.
	// ParentState must not be modified before this iterator is released.
	l1ValidatorIterator, err := parentState.GetActiveL1ValidatorsIterator()
	if err != nil {
		return false, fmt.Errorf("could not iterate over active L1 validators: %w", err)
	}
	defer l1ValidatorIterator.Release()

	var changed bool
	for l1ValidatorIterator.Next() {
		l1Validator := l1ValidatorIterator.Value()
		// GetActiveL1ValidatorsIterator iterates in order of increasing
		// EndAccumulatedFee, so we can break early.
		if l1Validator.EndAccumulatedFee > accruedFees {
			break
		}

		l1Validator.EndAccumulatedFee = 0 // Deactivate the validator
		if err := changes.PutL1Validator(l1Validator); err != nil {
			return false, fmt.Errorf("could not deactivate L1 validator %s: %w", l1Validator.ValidationID, err)
		}
		changed = true
	}

	validatorFeeState = validatorFeeState.AdvanceTime(config.Target, seconds)
	changes.SetL1ValidatorExcess(validatorFeeState.Excess)
	changes.SetAccruedFees(accruedFees)
	return changed, nil
}

func GetRewardsCalculator(
	backend *Backend,
	parentState state.Chain,
	subnetID ids.ID,
) (reward.Calculator, error) {
	if subnetID == constants.PrimaryNetworkID {
		return backend.Rewards, nil
	}

	transformSubnet, err := GetTransformSubnetTx(parentState, subnetID)
	if err != nil {
		return nil, err
	}

	return reward.NewCalculator(reward.Config{
		MaxConsumptionRate: transformSubnet.MaxConsumptionRate,
		MinConsumptionRate: transformSubnet.MinConsumptionRate,
		MintingPeriod:      backend.Config.RewardConfig.MintingPeriod,
		SupplyCap:          transformSubnet.MaximumSupply,
	}), nil
}
