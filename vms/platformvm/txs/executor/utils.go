// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

var (
	ErrChildBlockEarlierThanParent     = errors.New("proposed timestamp not after current chain time")
	ErrChildBlockAfterStakerChangeTime = errors.New("proposed timestamp later than next staker change time")
	ErrChildBlockBeyondSyncBound       = errors.New("proposed timestamp is too far in the future relative to local time")
)

// proposedChainTime returns nil if the [proposedChainTime]
// is a valid chain time given the [currentChainTime], wall clock
// time [now] and when the staking set changes next ([nextStakerChangeTime]).
// The [proposedChainTime] must be >= currentChainTime and <= to
// [nextStakerChangeTime], so that no staking set changes are skipped.
// If [mustIncTimestamp], [proposedChainTime] must > [currentChainTime].
// The [proposedChainTime] must be within syncBound with respect
// to local clock, to make sure chain time approximates "real" time.
// In the example below, proposedChainTime is valid because
// it's after [currentChainTime] and before [nextStakerChangeTime]
// and [now] + [syncBound].
//
//	----|--------|---------X------------|----------------------|
//
//	    ^        ^         ^            ^                      ^
//	    |        |         |            |                      |
//	now |        |         |   now + syncBound                 |
//	      currentChainTime |                          nextStakerChangeTime
//	                proposedChainTime
func ValidateProposedChainTime(
	proposedChainTime,
	currentChainTime,
	nextStakerChangeTime,
	now time.Time,
	mustIncTimestamp bool,
) error {
	if mustIncTimestamp {
		if !proposedChainTime.After(currentChainTime) {
			return fmt.Errorf(
				"%w, proposed timestamp (%s), chain time (%s)",
				ErrChildBlockEarlierThanParent,
				proposedChainTime,
				currentChainTime,
			)
		}
	} else {
		if proposedChainTime.Before(currentChainTime) {
			return fmt.Errorf(
				"%w, proposed timestamp (%s), chain time (%s)",
				ErrChildBlockEarlierThanParent,
				proposedChainTime,
				currentChainTime,
			)
		}
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	if proposedChainTime.After(nextStakerChangeTime) {
		return fmt.Errorf(
			"%w, proposed timestamp (%s), next staker change time (%s)",
			ErrChildBlockAfterStakerChangeTime,
			proposedChainTime,
			nextStakerChangeTime,
		)
	}

	// Note: this means we can only have sprees of <SyncBound> blocks
	// because we increment 1 sec each block and I cannot violate SyncBound
	upperBound := now.Add(SyncBound)
	if upperBound.Before(proposedChainTime) {
		return fmt.Errorf(
			"%w, proposed time (%s), local time (%s)",
			ErrChildBlockBeyondSyncBound,
			proposedChainTime,
			now,
		)
	}
	return nil
}

type StateChanges struct {
	Supply                    uint64
	CurrentValidatorsToAdd    []*state.Staker
	CurrentDelegatorsToAdd    []*state.Staker
	PendingValidatorsToRemove []*state.Staker
	PendingDelegatorsToRemove []*state.Staker
	CurrentValidatorsToRemove []*state.Staker
}

// UpdateStakerSet does not modify [parentState].
// Instead it returns an UpdatedStateData struct with all values modified by
// the advancing of chain time.
func UpdateStakerSet(parentState state.Chain, proposedChainTime time.Time, rewards reward.Calculator) (*StateChanges, error) {
	pendingStakerIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}

	changes := &StateChanges{
		Supply: parentState.GetCurrentSupply(),
	}

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp
	for pendingStakerIterator.Next() {
		stakerToRemove := pendingStakerIterator.Value()
		if stakerToRemove.StartTime.After(proposedChainTime) {
			break
		}

		stakerToAdd := *stakerToRemove
		stakerToAdd.NextTime = stakerToRemove.EndTime
		stakerToAdd.Priority = state.PendingToCurrentPriorities[stakerToRemove.Priority]

		switch stakerToRemove.Priority {
		case state.PrimaryNetworkDelegatorPendingPriority:
			potentialReward := rewards.Calculate(
				stakerToRemove.EndTime.Sub(stakerToRemove.StartTime),
				stakerToRemove.Weight,
				changes.Supply,
			)
			changes.Supply, err = math.Add64(changes.Supply, potentialReward)
			if err != nil {
				pendingStakerIterator.Release()
				return nil, err
			}

			stakerToAdd.PotentialReward = potentialReward

			changes.CurrentDelegatorsToAdd = append(changes.CurrentDelegatorsToAdd, &stakerToAdd)
			changes.PendingDelegatorsToRemove = append(changes.PendingDelegatorsToRemove, stakerToRemove)
		case state.PrimaryNetworkValidatorPendingPriority:
			potentialReward := rewards.Calculate(
				stakerToRemove.EndTime.Sub(stakerToRemove.StartTime),
				stakerToRemove.Weight,
				changes.Supply,
			)
			changes.Supply, err = math.Add64(changes.Supply, potentialReward)
			if err != nil {
				pendingStakerIterator.Release()
				return nil, err
			}

			stakerToAdd.PotentialReward = potentialReward

			changes.CurrentValidatorsToAdd = append(changes.CurrentValidatorsToAdd, &stakerToAdd)
			changes.PendingValidatorsToRemove = append(changes.PendingValidatorsToRemove, stakerToRemove)
		case state.SubnetValidatorPendingPriority:
			// We require that the [txTimestamp] <= [nextStakerChangeTime].
			// Additionally, the minimum stake duration is > 0. This means we
			// know that the staker we are adding here should never be attempted
			// to be removed in the following loop.

			changes.CurrentValidatorsToAdd = append(changes.CurrentValidatorsToAdd, &stakerToAdd)
			changes.PendingValidatorsToRemove = append(changes.PendingValidatorsToRemove, stakerToRemove)
		default:
			pendingStakerIterator.Release()
			return nil, fmt.Errorf("expected staker priority got %d", stakerToRemove.Priority)
		}
	}
	pendingStakerIterator.Release()

	currentStakerIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}

	for currentStakerIterator.Next() {
		stakerToRemove := currentStakerIterator.Value()
		if stakerToRemove.EndTime.After(proposedChainTime) {
			break
		}

		priority := stakerToRemove.Priority
		if priority == state.PrimaryNetworkDelegatorCurrentPriority ||
			priority == state.PrimaryNetworkValidatorCurrentPriority {
			// Primary network stakers are removed by the RewardValidatorTx, not
			// an AdvanceTimeTx.
			break
		}

		changes.CurrentValidatorsToRemove = append(changes.CurrentValidatorsToRemove, stakerToRemove)
	}
	currentStakerIterator.Release()
	return changes, nil
}
