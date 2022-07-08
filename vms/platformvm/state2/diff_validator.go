// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Validators = &diffValidators{}
	_ Validator  = &diffValidator{}
)

type diffValidator struct {
	diff     *diff
	subnetID ids.ID
	nodeID   ids.NodeID

	currentStaker          *Staker
	pendingStaker          *Staker
	currentDelegatorWeight uint64
}

func (v *diffValidator) CurrentStaker() *Staker {
	return v.currentStaker
}

func (v *diffValidator) PendingStaker() *Staker {
	return v.pendingStaker
}

func (v *diffValidator) CurrentDelegatorWeight() uint64 {
	return v.currentDelegatorWeight
}

func (v *diffValidator) NewCurrentDelegatorIterator() StakerIterator {
	return NewTreeIterator(v.currentDelegators)
}

func (v *diffValidator) NewPendingDelegatorIterator() StakerIterator {
	return NewTreeIterator(v.pendingDelegators)
}

type diffValidators struct {
	diff *diff

	// Representation of DB state
	validators         map[ids.ID]map[ids.NodeID]*diffValidator
	nextRewardedStaker *Staker

	// Representation of pending changes
	currentStakersToAdd    []*Staker
	currentStakersToRemove []*Staker
	pendingStakersToAdd    []*Staker
	pendingStakersToRemove []*Staker
}

func (v *diffValidators) GetValidator(subnetID ids.ID, nodeID ids.NodeID) Validator {
	subnetValidators, ok := v.validators[subnetID]
	if ok {
		validator, ok := subnetValidators[nodeID]
		if ok {
			return validator
		}
	}

	parent, ok := v.diff.stateVersions.GetState(v.diff.parentID)
	if !ok {
		panic("TODO handle this error")
	}
	return parent.GetValidator(subnetID, nodeID)
}

func (v *diffValidators) GetNextRewardedStaker() *Staker {
	return v.nextRewardedStaker
}

func (v *diffValidators) NewCurrentStakerIterator() StakerIterator {
	return NewTreeIterator(v.currentStakers)
}

func (v *diffValidators) NewPendingStakerIterator() StakerIterator {
	return NewTreeIterator(v.pendingStakers)
}

func (v *diffValidators) Update(
	currentStakersToAdd []*Staker,
	currentStakersToRemove []*Staker,
	pendingStakersToAdd []*Staker,
	pendingStakersToRemove []*Staker,
) {
	v.currentStakersToAdd = currentStakersToAdd
	v.currentStakersToRemove = currentStakersToRemove
	v.pendingStakersToAdd = pendingStakersToAdd
	v.pendingStakersToRemove = pendingStakersToRemove
}
