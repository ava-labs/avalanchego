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
	ErrChildBlockAfterStakerChangeTime = errors.New("proposed timestamp later than next staker change time")
	ErrChildBlockBeyondSyncBound       = errors.New("proposed timestamp is too far in the future relative to local time")
)

// VerifyNewChainTime returns nil if the [newChainTime] is a valid chain time
// given the wall clock time ([now]) and when the next staking set change occurs
// ([nextStakerChangeTime]).
// Requires:
//   - [newChainTime] <= [nextStakerChangeTime]: so that no staking set changes
//     are skipped.
//   - [newChainTime] <= [now] + [SyncBound]: to ensure chain time approximates
//     "real" time.
func VerifyNewChainTime(
	newChainTime,
	nextStakerChangeTime,
	now time.Time,
) error {
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

	// Only allow timestamp to reasonably far forward
	maxNewChainTime := now.Add(SyncBound)
	if newChainTime.After(maxNewChainTime) {
		return fmt.Errorf(
			"%w, proposed time (%s), local time (%s)",
			ErrChildBlockBeyondSyncBound,
			newChainTime,
			now,
		)
	}
	return nil
}

// AdvanceTimeTo applies all state changes to [parentState] resulting from
// advancing the chain time to [newChainTime].
// Returns true iff the validator set changed.
func AdvanceTimeTo(
	backend *Backend,
	parentState state.Chain,
	newChainTime time.Time,
) (bool, error) {
	// We promote pending stakers to current stakers first and remove
	// completed stakers from the current staker set. We assume that any
	// promoted staker will not immediately be removed from the current staker
	// set. This is guaranteed by the following invariants.
	//
	// Invariant: MinStakeDuration > 0 => guarantees [StartTime] != [EndTime]
	// Invariant: [newChainTime] <= nextStakerChangeTime.

	changes, err := state.NewDiffOn(parentState)
	if err != nil {
		return false, err
	}

	pendingStakerIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return false, err
	}
	defer pendingStakerIterator.Release()

	var changed bool
	// Promote any pending stakers to current if [StartTime] <= [newChainTime].
	for pendingStakerIterator.Next() {
		stakerToRemove := pendingStakerIterator.Value()
		if stakerToRemove.StartTime.After(newChainTime) {
			break
		}

		stakerToAdd := *stakerToRemove
		stakerToAdd.NextTime = stakerToRemove.EndTime
		stakerToAdd.Priority = txs.PendingToCurrentPriorities[stakerToRemove.Priority]

		if stakerToRemove.Priority == txs.SubnetPermissionedValidatorPendingPriority {
			changes.PutCurrentValidator(&stakerToAdd)
			changes.DeletePendingValidator(stakerToRemove)
			changed = true
			continue
		}

		supply, err := changes.GetCurrentSupply(stakerToRemove.SubnetID)
		if err != nil {
			return false, err
		}

		rewards, err := GetRewardsCalculator(backend, parentState, stakerToRemove.SubnetID)
		if err != nil {
			return false, err
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
			changes.PutCurrentValidator(&stakerToAdd)
			changes.DeletePendingValidator(stakerToRemove)

		case txs.PrimaryNetworkDelegatorApricotPendingPriority, txs.PrimaryNetworkDelegatorBanffPendingPriority, txs.SubnetPermissionlessDelegatorPendingPriority:
			changes.PutCurrentDelegator(&stakerToAdd)
			changes.DeletePendingDelegator(stakerToRemove)

		default:
			return false, fmt.Errorf("expected staker priority got %d", stakerToRemove.Priority)
		}

		changed = true
	}

	// Remove any current stakers whose [EndTime] <= [newChainTime].
	currentStakerIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return false, err
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

	if backend.Config.UpgradeConfig.IsEActivated(newChainTime) {
		previousChainTime := changes.GetTimestamp()
		duration := uint64(newChainTime.Sub(previousChainTime) / time.Second)

		feeComplexity := changes.GetFeeComplexity()
		feeComplexity = feeComplexity.AdvanceTime(
			backend.Config.DynamicFeeConfig.MaxGasCapacity,
			backend.Config.DynamicFeeConfig.MaxGasPerSecond,
			backend.Config.DynamicFeeConfig.TargetGasPerSecond,
			duration,
		)
		changes.SetFeeComplexity(feeComplexity)
	}

	changes.SetTimestamp(newChainTime)
	return changed, changes.Apply(parentState)
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
