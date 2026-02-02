// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/math"
)

var (
	ErrAddingStakerAfterDeletion = errors.New("attempted to add a staker after deleting it")
	ErrInvalidStakerMutation     = errors.New("invalid staker mutation")
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

	// UpdateCurrentValidator updates the [staker] describing a validator to the
	// staker set. Only specific mutable fields can be updated.
	UpdateCurrentValidator(staker *Staker) error

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
	validatorDiff.validatorStatus = added
	validatorDiff.validator = staker

	v.stakers.ReplaceOrInsert(staker)
}

func (v *baseStakers) DeleteValidator(staker *Staker) {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	validator.validator = nil
	v.pruneValidator(staker.SubnetID, staker.NodeID)

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.validatorStatus == modified {
		validatorDiff.oldWeight = 0
	}
	validatorDiff.validatorStatus = deleted
	validatorDiff.validator = staker

	v.stakers.Delete(staker)
}

// Invariant: [mutatedValidator] is a valid mutation.
func (v *baseStakers) UpdateValidator(mutatedValidator *Staker) {
	validator := v.getOrCreateValidator(mutatedValidator.SubnetID, mutatedValidator.NodeID)

	validatorDiff := v.getOrCreateValidatorDiff(mutatedValidator.SubnetID, mutatedValidator.NodeID)
	if validatorDiff.validatorStatus == unmodified {
		validatorDiff.validatorStatus = modified
		validatorDiff.oldWeight = validator.validator.Weight
	}
	validatorDiff.validator = mutatedValidator

	validator.validator = mutatedValidator

	v.stakers.ReplaceOrInsert(mutatedValidator)
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
		validatorDiff = &diffValidator{
			validatorStatus: unmodified,
		}
		subnetValidatorDiffs[nodeID] = validatorDiff
	}
	return validatorDiff
}

type diffStakers struct {
	// subnetID --> nodeID --> diff for that validator
	validatorDiffs         map[ids.ID]map[ids.NodeID]*diffValidator
	addedOrModifiedStakers *btree.BTreeG[*Staker]
	deletedStakers         map[ids.ID]*Staker
	modifiedStakers        map[ids.ID]*Staker
}

type diffValidator struct {
	// validatorStatus describes whether a validator has been added or removed.
	//
	// validatorStatus is not affected by delegators ops so unmodified does not
	// mean that diffValidator hasn't change, since delegators may have changed.
	validatorStatus diffValidatorStatus
	validator       *Staker
	oldWeight       uint64 // set iff [validatorStatus] is "modified"; used to compute weight diff

	addedDelegators   *btree.BTreeG[*Staker]
	deletedDelegators map[ids.ID]*Staker
}

func (d *diffValidator) WeightDiff() (ValidatorWeightDiff, error) {
	var weightDiff ValidatorWeightDiff
	switch d.validatorStatus {
	case added:
		weightDiff.Amount = d.validator.Weight
	case modified:
		var err error
		weightDiff.Amount, err = math.Sub(d.validator.Weight, d.oldWeight)
		if err != nil {
			return ValidatorWeightDiff{}, fmt.Errorf("modified validators weight can only increase: %w", err)
		}
	case deleted:
		weightDiff.Decrease = true
		weightDiff.Amount = d.validator.Weight
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
// Invariant: Assumes that the validator will never be removed and then added.
func (s *diffStakers) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, diffValidatorStatus) {
	subnetValidatorDiffs, ok := s.validatorDiffs[subnetID]
	if !ok {
		return nil, unmodified
	}

	validatorDiff, ok := subnetValidatorDiffs[nodeID]
	if !ok {
		return nil, unmodified
	}

	switch validatorDiff.validatorStatus {
	case added, modified:
		return validatorDiff.validator, validatorDiff.validatorStatus
	default:
		return nil, validatorDiff.validatorStatus
	}
}

func (s *diffStakers) PutValidator(staker *Staker) error {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.validatorStatus == deleted {
		// Enforce the invariant that a validator cannot be added after being
		// deleted.
		return ErrAddingStakerAfterDeletion
	}

	validatorDiff.validatorStatus = added
	validatorDiff.validator = staker

	if s.addedOrModifiedStakers == nil {
		s.addedOrModifiedStakers = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	s.addedOrModifiedStakers.ReplaceOrInsert(staker)
	return nil
}

func (s *diffStakers) DeleteValidator(staker *Staker) {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	switch validatorDiff.validatorStatus {
	case added:
		validatorDiff.validatorStatus = unmodified
		s.addedOrModifiedStakers.Delete(validatorDiff.validator)
		validatorDiff.validator = nil
	case modified:
		delete(s.modifiedStakers, staker.TxID)
		s.addedOrModifiedStakers.Delete(validatorDiff.validator)
		validatorDiff.validatorStatus = deleted
		validatorDiff.validator = staker
		validatorDiff.oldWeight = 0
		if s.deletedStakers == nil {
			s.deletedStakers = make(map[ids.ID]*Staker)
		}
		s.deletedStakers[staker.TxID] = staker
	default:
		validatorDiff.validatorStatus = deleted
		validatorDiff.validator = staker
		if s.deletedStakers == nil {
			s.deletedStakers = make(map[ids.ID]*Staker)
		}
		s.deletedStakers[staker.TxID] = staker
	}
}

// Invariants:
//   - [oldValidator] must be an existing validator
//   - [mutatedValidator] must be a valid mutation of [oldValidator].
func (s *diffStakers) UpdateValidator(
	oldValidator *Staker,
	mutatedValidator *Staker,
) {
	validatorDiff := s.getOrCreateDiff(mutatedValidator.SubnetID, mutatedValidator.NodeID)

	switch validatorDiff.validatorStatus {
	case added, modified:
		// Keep the same [validatorDiff.validatorStatus].
		validatorDiff.validator = mutatedValidator
		s.addedOrModifiedStakers.ReplaceOrInsert(mutatedValidator)

	case unmodified:
		validatorDiff.validator = mutatedValidator
		validatorDiff.validatorStatus = modified
		validatorDiff.oldWeight = oldValidator.Weight

		if s.addedOrModifiedStakers == nil {
			s.addedOrModifiedStakers = btree.NewG(defaultTreeDegree, (*Staker).Less)
		}
		s.addedOrModifiedStakers.ReplaceOrInsert(mutatedValidator)

		if s.modifiedStakers == nil {
			s.modifiedStakers = make(map[ids.ID]*Staker)
		}
		s.modifiedStakers[mutatedValidator.TxID] = mutatedValidator
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

	if s.addedOrModifiedStakers == nil {
		s.addedOrModifiedStakers = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	s.addedOrModifiedStakers.ReplaceOrInsert(staker)
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
			iterator.Filter(parentIterator, func(staker *Staker) bool {
				_, ok := s.modifiedStakers[staker.TxID]
				return ok
			}),
			iterator.FromTree(s.addedOrModifiedStakers),
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
		validatorDiff = &diffValidator{
			validatorStatus: unmodified,
		}
		subnetValidatorDiffs[nodeID] = validatorDiff
	}
	return validatorDiff
}
