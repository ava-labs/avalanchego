// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/iterator"
)

type Stakers interface {
	CurrentStakers
	PendingStakers
}

type CurrentStakers interface {
	// GetCurrentValidator returns the [staker] describing the validator on
	// [subnetID] with [nodeID]. If the validator does not exist,
	// [database.ErrNotFound] is returned.
	GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error)

	// PutCurrentValidator adds the [staker] describing a validator to the
	// staker set.
	//
	// Invariant: [staker] is not currently a CurrentValidator
	PutCurrentValidator(staker *Staker) error

	// DeleteCurrentValidator removes the [staker] describing a validator from
	// the staker set.
	//
	// Invariant: [staker] is currently a CurrentValidator
	DeleteCurrentValidator(staker *Staker)

	// SetDelegateeReward sets the accrued delegation rewards for [nodeID] on
	// [subnetID] to [amount].
	SetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID, amount uint64) error

	// GetDelegateeReward returns the accrued delegation rewards for [nodeID] on
	// [subnetID].
	GetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID) (uint64, error)

	// GetCurrentDelegatorIterator returns the delegators associated with the
	// validator on [subnetID] with [nodeID]. Delegators are sorted by their
	// removal from current staker set.
	GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error)

	// PutCurrentDelegator adds the [staker] describing a delegator to the
	// staker set.
	//
	// Invariant: [staker] is not currently a CurrentDelegator
	PutCurrentDelegator(staker *Staker)

	// DeleteCurrentDelegator removes the [staker] describing a delegator from
	// the staker set.
	//
	// Invariant: [staker] is currently a CurrentDelegator
	DeleteCurrentDelegator(staker *Staker)

	// GetCurrentStakerIterator returns stakers in order of their removal from
	// the current staker set.
	GetCurrentStakerIterator() (iterator.Iterator[*Staker], error)
}

type PendingStakers interface {
	// GetPendingValidator returns the Staker describing the validator on
	// [subnetID] with [nodeID]. If the validator does not exist,
	// [database.ErrNotFound] is returned.
	GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error)

	// PutPendingValidator adds the [staker] describing a validator to the
	// staker set.
	PutPendingValidator(staker *Staker) error

	// DeletePendingValidator removes the [staker] describing a validator from
	// the staker set.
	DeletePendingValidator(staker *Staker)

	// GetPendingDelegatorIterator returns the delegators associated with the
	// validator on [subnetID] with [nodeID]. Delegators are sorted by their
	// removal from pending staker set.
	GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error)

	// PutPendingDelegator adds the [staker] describing a delegator to the
	// staker set.
	PutPendingDelegator(staker *Staker)

	// DeletePendingDelegator removes the [staker] describing a delegator from
	// the staker set.
	DeletePendingDelegator(staker *Staker)

	// GetPendingStakerIterator returns stakers in order of their removal from
	// the pending staker set.
	GetPendingStakerIterator() (iterator.Iterator[*Staker], error)
}

type baseStakers struct {
	// subnetID --> nodeID --> current state for the validator of the subnet
	validators map[ids.ID]map[ids.NodeID]*baseStaker
	stakers    *btree.BTreeG[*Staker]
	// subnetID --> nodeID --> diff for that validator since the last db write
	validatorDiffs map[ids.ID]map[ids.NodeID]*diffValidator
}

type baseStaker struct {
	validator  *Staker
	delegators *btree.BTreeG[*Staker]
}

func newBaseStakers() *baseStakers {
	return &baseStakers{
		validators:     make(map[ids.ID]map[ids.NodeID]*baseStaker),
		stakers:        btree.NewG(defaultTreeDegree, (*Staker).Less),
		validatorDiffs: make(map[ids.ID]map[ids.NodeID]*diffValidator),
	}
}

func (v *baseStakers) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	subnetValidators, ok := v.validators[subnetID]
	if !ok {
		return nil, database.ErrNotFound
	}
	validator, ok := subnetValidators[nodeID]
	if !ok {
		return nil, database.ErrNotFound
	}
	if validator.validator == nil {
		return nil, database.ErrNotFound
	}
	return validator.validator, nil
}

func (v *baseStakers) PutValidator(staker *Staker) {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	validator.validator = staker

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	validatorDiff.added = staker

	v.stakers.ReplaceOrInsert(staker)
}

func (v *baseStakers) DeleteValidator(staker *Staker) {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	validator.validator = nil
	v.pruneValidator(staker.SubnetID, staker.NodeID)

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	validatorDiff.added = nil
	validatorDiff.removed = staker

	v.stakers.Delete(staker)
}

func (v *baseStakers) GetDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) iterator.Iterator[*Staker] {
	subnetValidators, ok := v.validators[subnetID]
	if !ok {
		return iterator.Empty[*Staker]{}
	}
	validator, ok := subnetValidators[nodeID]
	if !ok {
		return iterator.Empty[*Staker]{}
	}
	return iterator.FromTree(validator.delegators)
}

func (v *baseStakers) PutDelegator(staker *Staker) {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	if validator.delegators == nil {
		validator.delegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	validator.delegators.ReplaceOrInsert(staker)

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.addedDelegators == nil {
		validatorDiff.addedDelegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	validatorDiff.addedDelegators.ReplaceOrInsert(staker)

	v.stakers.ReplaceOrInsert(staker)
}

func (v *baseStakers) DeleteDelegator(staker *Staker) {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	if validator.delegators != nil {
		validator.delegators.Delete(staker)
	}
	v.pruneValidator(staker.SubnetID, staker.NodeID)

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.deletedDelegators == nil {
		validatorDiff.deletedDelegators = make(map[ids.ID]*Staker)
	}
	validatorDiff.deletedDelegators[staker.TxID] = staker

	v.stakers.Delete(staker)
}

func (v *baseStakers) GetStakerIterator() iterator.Iterator[*Staker] {
	return iterator.FromTree(v.stakers)
}

func (v *baseStakers) getOrCreateValidator(subnetID ids.ID, nodeID ids.NodeID) *baseStaker {
	subnetValidators, ok := v.validators[subnetID]
	if !ok {
		subnetValidators = make(map[ids.NodeID]*baseStaker)
		v.validators[subnetID] = subnetValidators
	}
	validator, ok := subnetValidators[nodeID]
	if !ok {
		validator = &baseStaker{}
		subnetValidators[nodeID] = validator
	}
	return validator
}

// pruneValidator assumes that the named validator is currently in the
// [validators] map.
func (v *baseStakers) pruneValidator(subnetID ids.ID, nodeID ids.NodeID) {
	subnetValidators := v.validators[subnetID]
	validator := subnetValidators[nodeID]
	if validator.validator != nil {
		return
	}
	if validator.delegators != nil && validator.delegators.Len() > 0 {
		return
	}
	delete(subnetValidators, nodeID)
	if len(subnetValidators) == 0 {
		delete(v.validators, subnetID)
	}
}

func (v *baseStakers) getOrCreateValidatorDiff(subnetID ids.ID, nodeID ids.NodeID) *diffValidator {
	subnetValidatorDiffs, ok := v.validatorDiffs[subnetID]
	if !ok {
		subnetValidatorDiffs = make(map[ids.NodeID]*diffValidator)
		v.validatorDiffs[subnetID] = subnetValidatorDiffs
	}
	validatorDiff, ok := subnetValidatorDiffs[nodeID]
	if !ok {
		validatorDiff = &diffValidator{}
		subnetValidatorDiffs[nodeID] = validatorDiff
	}
	return validatorDiff
}

type diffStakers struct {
	// subnetID --> nodeID --> diff for that validator
	validatorDiffs map[ids.ID]map[ids.NodeID]*diffValidator
	addedStakers   *btree.BTreeG[*Staker]
	deletedStakers map[ids.ID]*Staker
}

type diffValidator struct {
	// added represents a validator that was added in this diff, or nil if no
	// validator was added. Can be non-nil at the same time as removed to represent a replacement.
	added *Staker
	// removed represents a validator that was removed in this diff, or nil if no
	// validator was removed. Can be non-nil at the same time as added to represent a replacement.
	removed           *Staker
	addedDelegators   *btree.BTreeG[*Staker]
	deletedDelegators map[ids.ID]*Staker
}

// validatorStatus returns the status of the validator in this diff.
//
// validatorStatus is not affected by delegator ops so unmodified does not
// mean that diffValidator hasn't changed, since delegators may have changed.
func (d *diffValidator) validatorStatus() diffValidatorStatus {
	// If both added and removed are non-nil, this represents a replacement,
	// so we just return the added validator's status, we don't need the removed validator's status.
	if d.added != nil {
		return added
	}
	if d.removed != nil {
		return deleted
	}
	return unmodified
}

func (d *diffValidator) WeightDiff() (ValidatorWeightDiff, error) {
	var weightDiff ValidatorWeightDiff
	if d.added != nil {
		if err := weightDiff.Add(d.added.Weight); err != nil {
			return ValidatorWeightDiff{}, fmt.Errorf("failed to increase weight of added validator: %w", err)
		}
	}
	if d.removed != nil {
		if err := weightDiff.Sub(d.removed.Weight); err != nil {
			return ValidatorWeightDiff{}, fmt.Errorf("failed to decrease weight of deleted validator: %w", err)
		}
	}

	for _, staker := range d.deletedDelegators {
		if err := weightDiff.Sub(staker.Weight); err != nil {
			return ValidatorWeightDiff{}, fmt.Errorf("failed to decrease node weight diff: %w", err)
		}
	}

	addedDelegatorIterator := iterator.FromTree(d.addedDelegators)
	defer addedDelegatorIterator.Release()

	for addedDelegatorIterator.Next() {
		staker := addedDelegatorIterator.Value()

		if err := weightDiff.Add(staker.Weight); err != nil {
			return ValidatorWeightDiff{}, fmt.Errorf("failed to increase node weight diff: %w", err)
		}
	}

	return weightDiff, nil
}

// GetValidator attempts to fetch the validator with the given subnetID and
// nodeID.
func (s *diffStakers) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, diffValidatorStatus) {
	subnetValidatorDiffs, ok := s.validatorDiffs[subnetID]
	if !ok {
		return nil, unmodified
	}

	validatorDiff, ok := subnetValidatorDiffs[nodeID]
	if !ok {
		return nil, unmodified
	}

	return validatorDiff.added, validatorDiff.validatorStatus()
}

func (s *diffStakers) PutValidator(staker *Staker) error {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.removed != nil {
		// TLDR: The validator was previously deleted, so we remove it from the
		// deleted stakers set.

		// We set the removed field when we delete the validator that was not added in this diff before.
		// So if we reached here, it means we removed it first and now either re-adding it or updating it.
		// Either way, we should remove it from the deleted stakers set.
		delete(s.deletedStakers, validatorDiff.removed.TxID)
		if len(s.deletedStakers) == 0 {
			s.deletedStakers = nil
		}

		// If we're re-adding the exact same validator that was removed,
		// the two operations cancel out.
		if validatorDiff.removed.Equals(staker) {
			validatorDiff.removed = nil
			return nil
		}

		// Attention: We do not return here, but combine with the rest of the flow.
	}

	validatorDiff.added = staker

	// Ensure the newly added staker is not tracked as deleted.
	delete(s.deletedStakers, staker.TxID)
	if len(s.deletedStakers) == 0 {
		s.deletedStakers = nil
	}

	if s.addedStakers == nil {
		s.addedStakers = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	s.addedStakers.ReplaceOrInsert(staker)
	return nil
}

func (s *diffStakers) DeleteValidator(staker *Staker) {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.added != nil {
		// This validator was added in this diff. Rollback the addition.
		s.addedStakers.Delete(validatorDiff.added)
		validatorDiff.added = nil

		// TLDR: If there was a previously removed validator, re-add it to
		// deletedStakers since the replacement is being undone.

		// We set the removed field when we delete the validator that was not added in this diff before.
		// Since we reached here, we have first deleted it, and then added it.
		// When we deleted it, we set the removed field and add it to the deleted stakers set.
		// When we added it, we removed it from the deleted stakers set.
		// Since we're now deleting it again, we should add it back to the deleted stakers set.
		// Why are we putting back the validator that was originally removed and not the new staker?
		// Because the original staker, validatorDiff.removed was there at the beginning, and the second
		// removal is just rolling back the addition of the new staker. We therefore put back original staker.
		if validatorDiff.removed != nil {
			if s.deletedStakers == nil {
				s.deletedStakers = make(map[ids.ID]*Staker)
			}
			s.deletedStakers[validatorDiff.removed.TxID] = validatorDiff.removed
		}
	} else {
		validatorDiff.removed = staker
		if s.deletedStakers == nil {
			s.deletedStakers = make(map[ids.ID]*Staker)
		}
		s.deletedStakers[staker.TxID] = staker
	}
}

func (s *diffStakers) GetDelegatorIterator(
	parentIterator iterator.Iterator[*Staker],
	subnetID ids.ID,
	nodeID ids.NodeID,
) iterator.Iterator[*Staker] {
	var (
		addedDelegatorIterator iterator.Iterator[*Staker] = iterator.Empty[*Staker]{}
		deletedDelegators      map[ids.ID]*Staker
	)
	if subnetValidatorDiffs, ok := s.validatorDiffs[subnetID]; ok {
		if validatorDiff, ok := subnetValidatorDiffs[nodeID]; ok {
			addedDelegatorIterator = iterator.FromTree(validatorDiff.addedDelegators)
			deletedDelegators = validatorDiff.deletedDelegators
		}
	}

	return iterator.Filter(
		iterator.Merge(
			(*Staker).Less,
			parentIterator,
			addedDelegatorIterator,
		),
		func(staker *Staker) bool {
			_, ok := deletedDelegators[staker.TxID]
			return ok
		},
	)
}

func (s *diffStakers) PutDelegator(staker *Staker) {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.addedDelegators == nil {
		validatorDiff.addedDelegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	validatorDiff.addedDelegators.ReplaceOrInsert(staker)

	if s.addedStakers == nil {
		s.addedStakers = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	s.addedStakers.ReplaceOrInsert(staker)
}

func (s *diffStakers) DeleteDelegator(staker *Staker) {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.deletedDelegators == nil {
		validatorDiff.deletedDelegators = make(map[ids.ID]*Staker)
	}
	validatorDiff.deletedDelegators[staker.TxID] = staker

	if s.deletedStakers == nil {
		s.deletedStakers = make(map[ids.ID]*Staker)
	}
	s.deletedStakers[staker.TxID] = staker
}

func (s *diffStakers) GetStakerIterator(parentIterator iterator.Iterator[*Staker]) iterator.Iterator[*Staker] {
	return iterator.Filter(
		iterator.Merge(
			(*Staker).Less,
			parentIterator,
			iterator.FromTree(s.addedStakers),
		),
		func(staker *Staker) bool {
			_, ok := s.deletedStakers[staker.TxID]
			return ok
		},
	)
}

func (s *diffStakers) getOrCreateDiff(subnetID ids.ID, nodeID ids.NodeID) *diffValidator {
	if s.validatorDiffs == nil {
		s.validatorDiffs = make(map[ids.ID]map[ids.NodeID]*diffValidator)
	}
	subnetValidatorDiffs, ok := s.validatorDiffs[subnetID]
	if !ok {
		subnetValidatorDiffs = make(map[ids.NodeID]*diffValidator)
		s.validatorDiffs[subnetID] = subnetValidatorDiffs
	}
	validatorDiff, ok := subnetValidatorDiffs[nodeID]
	if !ok {
		validatorDiff = &diffValidator{}
		subnetValidatorDiffs[nodeID] = validatorDiff
	}
	return validatorDiff
}
