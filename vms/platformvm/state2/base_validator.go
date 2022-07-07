// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Validator  = &baseValidator{}
	_ Validators = &baseValidators{}
)

type baseValidator struct {
	// Representation of DB state
	currentStaker          *Staker
	pendingStaker          *Staker
	currentDelegatorWeight uint64
	currentDelegators      *btree.BTree
	pendingDelegators      *btree.BTree

	// Representation of pending changes
	currentDelegatorsToAdd    []*Staker
	currentDelegatorsToRemove []*Staker
	pendingDelegatorsToAdd    []*Staker
	pendingDelegatorsToRemove []*Staker
}

func (v *baseValidator) CurrentStaker() *Staker {
	return v.currentStaker
}

func (v *baseValidator) PendingStaker() *Staker {
	return v.pendingStaker
}

func (v *baseValidator) CurrentDelegatorWeight() uint64 {
	return v.currentDelegatorWeight
}

func (v *baseValidator) NewCurrentDelegatorIterator() StakerIterator {
	var treeIterator StakerIterator
	if numRemoved := len(v.currentDelegatorsToRemove); numRemoved == 0 {
		treeIterator = NewTreeIterator(v.currentDelegators)
	} else {
		treeIterator = NewTreeIteratorAfter(v.currentDelegators, v.currentDelegatorsToRemove[numRemoved-1])
	}
	return NewMultiIterator(
		treeIterator,
		NewSliceIterator(v.currentDelegatorsToAdd),
	)
}

func (v *baseValidator) NewPendingDelegatorIterator() StakerIterator {
	var treeIterator StakerIterator
	if numRemoved := len(v.pendingDelegatorsToRemove); numRemoved == 0 {
		treeIterator = NewTreeIterator(v.pendingDelegators)
	} else {
		treeIterator = NewTreeIteratorAfter(v.pendingDelegators, v.pendingDelegatorsToRemove[numRemoved-1])
	}
	return NewMultiIterator(
		treeIterator,
		NewSliceIterator(v.pendingDelegatorsToAdd),
	)
}

type baseValidators struct {
	// Representation of DB state
	validators         map[ids.ID]map[ids.NodeID]*baseValidator
	nextRewardedStaker *Staker
	currentStakers     *btree.BTree
	pendingStakers     *btree.BTree

	// Representation of pending changes
	currentStakersToAdd    []*Staker
	currentStakersToRemove []*Staker
	pendingStakersToAdd    []*Staker
	pendingStakersToRemove []*Staker
}

func (v *baseValidators) GetValidator(subnetID ids.ID, nodeID ids.NodeID) Validator {
	subnetValidators, ok := v.validators[subnetID]
	if !ok {
		return &baseValidator{}
	}
	validator, ok := subnetValidators[nodeID]
	if !ok {
		return &baseValidator{}
	}
	return validator
}

func (v *baseValidators) GetNextRewardedStaker() *Staker {
	return v.nextRewardedStaker
}

func (v *baseValidators) NewCurrentStakerIterator() StakerIterator {
	var treeIterator StakerIterator
	if numRemoved := len(v.currentStakersToRemove); numRemoved == 0 {
		treeIterator = NewTreeIterator(v.currentStakers)
	} else {
		treeIterator = NewTreeIteratorAfter(v.currentStakers, v.currentStakersToRemove[numRemoved-1])
	}
	return NewMultiIterator(
		treeIterator,
		NewSliceIterator(v.currentStakersToAdd),
	)
}

func (v *baseValidators) NewPendingStakerIterator() StakerIterator {
	var treeIterator StakerIterator
	if numRemoved := len(v.pendingStakersToRemove); numRemoved == 0 {
		treeIterator = NewTreeIterator(v.pendingStakers)
	} else {
		treeIterator = NewTreeIteratorAfter(v.pendingStakers, v.pendingStakersToRemove[numRemoved-1])
	}
	return NewMultiIterator(
		treeIterator,
		NewSliceIterator(v.pendingStakersToAdd),
	)
}
