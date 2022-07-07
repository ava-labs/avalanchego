// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/google/btree"
)

var _ Validator = &baseValidator{}

type Validator interface {
	// Staker returns the staker summary associated with this validator
	Staker() *Staker

	// DelegatorWeight could be calculated by summing all the weights in the
	// NewDelegatorIterator, but it is cheap to maintain this value as the
	// delegator set changes.
	DelegatorWeight() uint64

	// NewDelegatorIterator returns the current delegators on this validator
	// sorted in order of their future removal from the validator set.
	NewDelegatorIterator() StakerIterator
}

type baseValidator struct {
	staker *Staker

	// Weight of delegations to this validator. Doesn't include this validator's
	// own weight.
	delegatorWeight uint64

	// Delegators is the collection of delegators to this validator sorted by
	// either StartTime or EndTime.
	delegators *btree.BTree
}

func (v *baseValidator) Staker() *Staker {
	return v.staker
}

func (v *baseValidator) DelegatorWeight() uint64 {
	return v.delegatorWeight
}

func (v *baseValidator) NewDelegatorIterator() StakerIterator {
	return NewTreeIterator(v.delegators)
}

type diffValidator struct {
	parent Validator

	stakerModified bool
	staker         *Staker

	// Weight of delegations to this validator. Doesn't include this validator's
	// own weight.
	delegatorWeight uint64

	// Delegators is the collection of delegators to this validator sorted by
	// either StartTime or EndTime.
	delegators *btree.BTree
}

func (v *diffValidator) Staker() *Staker {
	if v.stakerModified {
		return v.staker
	}
	return v
}

func (v *diffValidator) DelegatorWeight() uint64 {
	return v.delegatorWeight
}

func (v *diffValidator) NewDelegatorIterator() StakerIterator {
	return NewTreeIterator(v.delegators)
}
