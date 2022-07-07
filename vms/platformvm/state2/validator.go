// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
)

type Validator interface {
	// CurrentStaker returns the current staker associated with this validator.
	// May return nil
	CurrentStaker() *Staker

	// PendingStaker returns the pending staker associated with this validator.
	// May return nil
	PendingStaker() *Staker

	// CurrentDelegatorWeight returns the total weight of the current
	// delegations to this validator. It doesn't include this validator's own
	// weight.
	//
	// This could be calculated by summing all the weights in the
	// NewDelegatorIterator, but it is cheap to maintain this value as the
	// delegator set changes.
	CurrentDelegatorWeight() uint64

	// NewCurrentDelegatorIterator returns the current delegators on this
	// validator sorted in order of their removal from the validator set.
	NewCurrentDelegatorIterator() StakerIterator

	// NewPendingDelegatorIterator returns the pending delegators on this
	// validator sorted in order of their addition to the validator set.
	NewPendingDelegatorIterator() StakerIterator
}

type Validators interface {
	// GetValidator returns the staker state associated with this validator.
	GetValidator(subnetID ids.ID, nodeID ids.NodeID) Validator

	// GetNextRewardedStaker returns the next staker that has a non-zero
	// PotentialReward.
	GetNextRewardedStaker() *Staker

	// NewCurrentStakerIterator returns the current stakers in the validator set
	// sorted in order of their future removal.
	NewCurrentStakerIterator() StakerIterator

	// NewPendingStakerIterator returns the pending stakers in the validator set
	// sorted in order of their future addition.
	NewPendingStakerIterator() StakerIterator

	Update(
		currentStakersToAdd []*Staker,
		currentStakersToRemove []*Staker,
		pendingStakersToAdd []*Staker,
		pendingStakersToRemove []*Staker,
	)
}
