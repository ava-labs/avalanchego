// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

const (
	// priority values are used as part of the keys in the pending/current
	// validator state to ensure they are sorted in the order that they should
	// be added/removed.
	lowPriority byte = iota
	mediumPriority
	topPriority
)

var _ Stakers = &stakers{}

type Stakers interface {
	SetCurrentStakers(cs CurrentStakers)
	CurrentStakers() CurrentStakers
	SetPendingStakers(ps PendingStakers)
	PendingStakers() PendingStakers

	// GetNextStakerChangeTime returns the next time that a staker set change
	// should occur.
	GetNextStakerChangeTime() (time.Time, error)
}

func NewStakers(current CurrentStakers, pending PendingStakers) Stakers {
	return &stakers{
		current: current,
		pending: pending,
	}
}

type stakers struct {
	current CurrentStakers
	pending PendingStakers
}

func (s *stakers) CurrentStakers() CurrentStakers {
	return s.current
}

func (s *stakers) PendingStakers() PendingStakers {
	return s.pending
}

func (s *stakers) SetCurrentStakers(cs CurrentStakers) {
	s.current = cs
}

func (s *stakers) SetPendingStakers(ps PendingStakers) {
	s.pending = ps
}

func (s *stakers) GetNextStakerChangeTime() (time.Time, error) {
	earliest := mockable.MaxTime
	currentStakers := s.CurrentStakers()
	if currentStakers := currentStakers.Stakers(); len(currentStakers) > 0 {
		nextStakerToRemove := currentStakers[0]
		staker, ok := nextStakerToRemove.Unsigned.(txs.StakerTx)
		if !ok {
			return time.Time{}, fmt.Errorf("expected tx type StakerTx but got %T", nextStakerToRemove.Unsigned)
		}
		if endTime := staker.EndTime(); endTime.Before(earliest) {
			earliest = endTime
		}
	}
	pendingStakers := s.PendingStakers()
	if pendingStakers := pendingStakers.Stakers(); len(pendingStakers) > 0 {
		nextStakerToAdd := pendingStakers[0]
		staker, ok := nextStakerToAdd.Unsigned.(txs.StakerTx)
		if !ok {
			return time.Time{}, fmt.Errorf("expected tx type txs.StakerTx but got %T", nextStakerToAdd.Unsigned)
		}
		if startTime := staker.StartTime(); startTime.Before(earliest) {
			earliest = startTime
		}
	}
	return earliest, nil
}

func (s *stakers) maxSubnetStakeAmount(
	subnetID ids.ID,
	nodeID ids.NodeID,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	var (
		vdrTxAndID SubnetValidatorAndID
		exists     bool
	)
	currentValidator, err := s.current.GetValidator(nodeID)
	pendingValidator := s.pending.GetValidator(nodeID)

	switch err {
	case nil:
		vdrTxAndID, exists = currentValidator.SubnetValidators()[subnetID]
		if !exists {
			vdrTxAndID = pendingValidator.SubnetValidators()[subnetID]
		}
	case database.ErrNotFound:
		vdrTxAndID = pendingValidator.SubnetValidators()[subnetID]
	default:
		return 0, err
	}

	vdrTx := vdrTxAndID.Tx
	if vdrTx == nil {
		return 0, nil
	}
	if vdrTx.StartTime().After(endTime) {
		return 0, nil
	}
	if vdrTx.EndTime().Before(startTime) {
		return 0, nil
	}
	return vdrTx.Weight(), nil
}

func (s *stakers) maxPrimarySubnetStakeAmount(
	nodeID ids.NodeID,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	currentValidator, err := s.current.GetValidator(nodeID)
	pendingValidator := s.pending.GetValidator(nodeID)

	switch err {
	case nil:
		vdrTx, _ := currentValidator.AddValidatorTx()
		if vdrTx.StartTime().After(endTime) {
			return 0, nil
		}
		if vdrTx.EndTime().Before(startTime) {
			return 0, nil
		}

		currentWeight := vdrTx.Weight()
		currentWeight, err = math.Add64(currentWeight, currentValidator.DelegatorWeight())
		if err != nil {
			return 0, err
		}
		return getMaxStakeAmount(
			currentValidator.Delegators(),
			pendingValidator.Delegators(),
			startTime,
			endTime,
			currentWeight,
		)
	case database.ErrNotFound:
		futureValidator, _, err := s.pending.GetValidatorTx(nodeID)
		if err == database.ErrNotFound {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		if futureValidator.StartTime().After(endTime) {
			return 0, nil
		}
		if futureValidator.EndTime().Before(startTime) {
			return 0, nil
		}

		return getMaxStakeAmount(
			nil,
			pendingValidator.Delegators(),
			startTime,
			endTime,
			futureValidator.Weight(),
		)
	default:
		return 0, err
	}
}

// CanDelegate returns if the [new] delegator can be added to a validator who
// has [current] and [pending] delegators. [currentStake] is the current amount
// of stake on the validator, include the [current] delegators. [maximumStake]
// is the maximum amount of stake that can be on the validator at any given
// time. It is assumed that the validator without adding [new] does not violate
// [maximumStake].
func CanDelegate(
	current,
	pending []DelegatorAndID, // sorted by next start time first
	new *txs.AddDelegatorTx,
	currentStake,
	maximumStake uint64,
) (bool, error) {
	maxStake, err := getMaxStakeAmount(current, pending, new.StartTime(), new.EndTime(), currentStake)
	if err != nil {
		return false, err
	}
	newMaxStake, err := math.Add64(maxStake, new.Validator.Wght)
	if err != nil {
		return false, err
	}
	return newMaxStake <= maximumStake, nil
}

// Return the maximum amount of stake on a node (including delegations) at any
// given time between [startTime] and [endTime] given that:
// * The amount of stake on the node right now is [currentStake]
// * The delegations currently on this node are [current]
// * [current] is sorted in order of increasing delegation end time.
// * The stake delegated in [current] are already included in [currentStake]
// * [startTime] is in the future, and [endTime] > [startTime]
// * The delegations that will be on this node in the future are [pending]
// * The start time of all delegations in [pending] are in the future
// * [pending] is sorted in order of increasing delegation start time
func getMaxStakeAmount(
	current,
	pending []DelegatorAndID, // sorted by next start time first
	startTime time.Time,
	endTime time.Time,
	currentStake uint64,
) (uint64, error) {
	// Keep track of which delegators should be removed next so that we can
	// efficiently remove delegators and keep the current stake updated.
	toRemoveHeap := validator.EndTimeHeap{}
	for _, currentDelegator := range current {
		toRemoveHeap.Add(&currentDelegator.Tx.Validator)
	}

	var (
		err error
		// [maxStake] is the max stake at any point between now [starTime] and [endTime]
		maxStake uint64
	)

	// Calculate what the amount staked will be when each pending delegation
	// starts.
	for _, nextPending := range pending { // Iterates in order of increasing start time
		// Calculate what the amount staked will be when this delegation starts.
		nextPendingStartTime := nextPending.Tx.StartTime()

		if nextPendingStartTime.After(endTime) {
			// This delegation starts after [endTime].
			// Since we're calculating the max amount staked in
			// [startTime, endTime], we can stop. (Recall that [pending] is
			// sorted in order of increasing end time.)
			break
		}

		// Subtract from [currentStake] all of the current delegations that will
		// have ended by the time that the delegation [nextPending] starts.
		for toRemoveHeap.Len() > 0 {
			// Get the next current delegation that will end.
			toRemove := toRemoveHeap.Peek()
			toRemoveEndTime := toRemove.EndTime()
			if toRemoveEndTime.After(nextPendingStartTime) {
				break
			}
			// This current delegation [toRemove] ends before [nextPending]
			// starts, so its stake should be subtracted from [currentStake].

			// Changed in AP3:
			// If the new delegator has started, then this current delegator
			// should have an end time that is > [startTime].
			newDelegatorHasStartedBeforeFinish := toRemoveEndTime.After(startTime)
			if newDelegatorHasStartedBeforeFinish && currentStake > maxStake {
				// Only update [maxStake] if it's after [startTime]
				maxStake = currentStake
			}

			currentStake, err = math.Sub64(currentStake, toRemove.Wght)
			if err != nil {
				return 0, err
			}

			// Changed in AP3:
			// Remove the delegator from the heap and update the heap so that
			// the top of the heap is the next delegator to remove.
			toRemoveHeap.Remove()
		}

		// Add to [currentStake] the stake of this pending delegator to
		// calculate what the stake will be when this pending delegation has
		// started.
		currentStake, err = math.Add64(currentStake, nextPending.Tx.Validator.Wght)
		if err != nil {
			return 0, err
		}

		// Changed in AP3:
		// If the new delegator has started, then this pending delegator should
		// have a start time that is >= [startTime]. Otherwise, the delegator
		// hasn't started yet and the [currentStake] shouldn't count towards the
		// [maximumStake] during the delegators delegation period.
		newDelegatorHasStarted := !nextPendingStartTime.Before(startTime)
		if newDelegatorHasStarted && currentStake > maxStake {
			// Only update [maxStake] if it's after [startTime]
			maxStake = currentStake
		}

		// This pending delegator is a current delegator relative
		// when considering later pending delegators that start late
		toRemoveHeap.Add(&nextPending.Tx.Validator)
	}

	// [currentStake] is now the amount staked before the next pending delegator
	// whose start time is after [endTime].

	// If there aren't any delegators that will be added before the end of our
	// delegation period, we should advance through time until our delegation
	// period starts.
	for toRemoveHeap.Len() > 0 {
		toRemove := toRemoveHeap.Peek()
		toRemoveEndTime := toRemove.EndTime()
		if toRemoveEndTime.After(startTime) {
			break
		}

		currentStake, err = math.Sub64(currentStake, toRemove.Wght)
		if err != nil {
			return 0, err
		}

		// Changed in AP3:
		// Remove the delegator from the heap and update the heap so that the
		// top of the heap is the next delegator to remove.
		toRemoveHeap.Remove()
	}

	// We have advanced time to be inside the delegation window.
	// Make sure that the max stake is updated accordingly.
	if currentStake > maxStake {
		maxStake = currentStake
	}
	return maxStake, nil
}
