// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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
	nextStakerChangeTime, err := state.GetNextStakerChangeTime(currentState, newChainTime)
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

	if backend.Config.UpgradeConfig.IsEtnaActivated(newChainTime) {
		previousChainTime := changes.GetTimestamp()
		duration := uint64(newChainTime.Sub(previousChainTime) / time.Second)

		feeState := changes.GetFeeState()
		feeState = feeState.AdvanceTime(
			backend.Config.DynamicFeeConfig.MaxCapacity,
			backend.Config.DynamicFeeConfig.MaxPerSecond,
			backend.Config.DynamicFeeConfig.TargetPerSecond,
			duration,
		)
		changes.SetFeeState(feeState)
	}

	// Remove all expiries whose timestamp now implies they can never be
	// re-issued.
	//
	// The expiry timestamp is the time at which it is no longer valid, so any
	// expiry with a timestamp less than or equal to the new chain time can be
	// removed.
	//
	// Ref: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/77-reinventing-subnets#registersubnetvalidatortx
	//
	// The expiry iterator is sorted in order of increasing timestamp.
	//
	// Invariant: It is not safe to modify the state while iterating over it,
	// so we use the parentState's iterator rather than the changes iterator.
	// ParentState must not be modified before this iterator is released.
	expiryIterator, err := parentState.GetExpiryIterator()
	if err != nil {
		return nil, false, err
	}
	defer expiryIterator.Release()

	newChainTimeUnix := uint64(newChainTime.Unix())
	for expiryIterator.Next() {
		expiry := expiryIterator.Value()
		if expiry.Timestamp > newChainTimeUnix {
			break
		}

		changes.DeleteExpiry(expiry)
	}

	changes.SetTimestamp(newChainTime)
	return changes, changed, nil
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
