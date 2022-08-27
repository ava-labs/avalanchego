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
	errChildBlockAfterStakerChangeTime = errors.New("proposed timestamp later than next staker change time")
	errChildBlockBeyondSyncBound       = errors.New("proposed timestamp is too far in the future relative to local time")
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
			errChildBlockAfterStakerChangeTime,
			newChainTime,
			nextStakerChangeTime,
		)
	}

	// Only allow timestamp to reasonably far forward
	maxNewChainTime := now.Add(SyncBound)
	if newChainTime.After(maxNewChainTime) {
		return fmt.Errorf(
			"%w, proposed time (%s), local time (%s)",
			errChildBlockBeyondSyncBound,
			newChainTime,
			now,
		)
	}
	return nil
}

type StateChanges interface {
	Apply(onAccept state.Diff)
	Len() int
}

type stateChanges struct {
	supply                    uint64
	currentValidatorsToAdd    []*state.Staker
	currentDelegatorsToAdd    []*state.Staker
	pendingValidatorsToRemove []*state.Staker
	pendingDelegatorsToRemove []*state.Staker
	currentValidatorsToRemove []*state.Staker
}

func (s *stateChanges) Apply(stateDiff state.Diff) {
	stateDiff.SetCurrentSupply(s.supply)

	for _, currentValidatorToAdd := range s.currentValidatorsToAdd {
		stateDiff.PutCurrentValidator(currentValidatorToAdd)
	}
	for _, pendingValidatorToRemove := range s.pendingValidatorsToRemove {
		stateDiff.DeletePendingValidator(pendingValidatorToRemove)
	}
	for _, currentDelegatorToAdd := range s.currentDelegatorsToAdd {
		stateDiff.PutCurrentDelegator(currentDelegatorToAdd)
	}
	for _, pendingDelegatorToRemove := range s.pendingDelegatorsToRemove {
		stateDiff.DeletePendingDelegator(pendingDelegatorToRemove)
	}
	for _, currentValidatorToRemove := range s.currentValidatorsToRemove {
		stateDiff.DeleteCurrentValidator(currentValidatorToRemove)
	}
}

func (s *stateChanges) Len() int {
	return len(s.currentValidatorsToAdd) + len(s.currentDelegatorsToAdd) +
		len(s.pendingValidatorsToRemove) + len(s.pendingDelegatorsToRemove) +
		len(s.currentValidatorsToRemove)
}

// AdvanceTimeTo does not modify [parentState].
// Instead it returns all the StateChanges caused by advancing the chain time to
// the [newChainTime].
func AdvanceTimeTo(parentState state.Chain, newChainTime time.Time, rewards reward.Calculator) (StateChanges, error) {
	pendingStakerIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}

	changes := &stateChanges{
		supply: parentState.GetCurrentSupply(),
	}

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp
	for pendingStakerIterator.Next() {
		stakerToRemove := pendingStakerIterator.Value()
		if stakerToRemove.StartTime.After(newChainTime) {
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
				changes.supply,
			)
			changes.supply, err = math.Add64(changes.supply, potentialReward)
			if err != nil {
				pendingStakerIterator.Release()
				return nil, err
			}

			stakerToAdd.PotentialReward = potentialReward

			changes.currentDelegatorsToAdd = append(changes.currentDelegatorsToAdd, &stakerToAdd)
			changes.pendingDelegatorsToRemove = append(changes.pendingDelegatorsToRemove, stakerToRemove)
		case state.PrimaryNetworkValidatorPendingPriority:
			potentialReward := rewards.Calculate(
				stakerToRemove.EndTime.Sub(stakerToRemove.StartTime),
				stakerToRemove.Weight,
				changes.supply,
			)
			changes.supply, err = math.Add64(changes.supply, potentialReward)
			if err != nil {
				pendingStakerIterator.Release()
				return nil, err
			}

			stakerToAdd.PotentialReward = potentialReward

			changes.currentValidatorsToAdd = append(changes.currentValidatorsToAdd, &stakerToAdd)
			changes.pendingValidatorsToRemove = append(changes.pendingValidatorsToRemove, stakerToRemove)
		case state.SubnetValidatorPendingPriority:
			// We require that the [txTimestamp] <= [nextStakerChangeTime].
			// Additionally, the minimum stake duration is > 0. This means we
			// know that the staker we are adding here should never be attempted
			// to be removed in the following loop.

			changes.currentValidatorsToAdd = append(changes.currentValidatorsToAdd, &stakerToAdd)
			changes.pendingValidatorsToRemove = append(changes.pendingValidatorsToRemove, stakerToRemove)
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
		if stakerToRemove.EndTime.After(newChainTime) {
			break
		}

		priority := stakerToRemove.Priority
		if priority == state.PrimaryNetworkDelegatorCurrentPriority ||
			priority == state.PrimaryNetworkValidatorCurrentPriority {
			// Primary network stakers are removed by the RewardValidatorTx, not
			// an AdvanceTimeTx.
			break
		}

		changes.currentValidatorsToRemove = append(changes.currentValidatorsToRemove, stakerToRemove)
	}
	currentStakerIterator.Release()
	return changes, nil
}
