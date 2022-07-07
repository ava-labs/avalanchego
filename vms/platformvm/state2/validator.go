// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/google/btree"
)

var _ Validator = &baseValidator{}

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

type baseValidator struct {
	currentStaker          *Staker
	pendingStaker          *Staker
	currentDelegatorWeight uint64
	currentDelegators      *btree.BTree
	pendingDelegators      *btree.BTree
}

func (v *baseValidator) CurrentStaker() *Staker         { return v.currentStaker }
func (v *baseValidator) PendingStaker() *Staker         { return v.pendingStaker }
func (v *baseValidator) CurrentDelegatorWeight() uint64 { return v.currentDelegatorWeight }
func (v *baseValidator) NewCurrentDelegatorIterator() StakerIterator {
	return NewTreeIterator(v.currentDelegators)
}
func (v *baseValidator) NewPendingDelegatorIterator() StakerIterator {
	return NewTreeIterator(v.pendingDelegators)
}
