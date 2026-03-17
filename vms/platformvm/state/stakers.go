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

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	ErrAddingStakerAfterDeletion = errors.New("attempted to add a staker after deleting it")
	errUnexpectedStaker          = errors.New("unexpected staker")
)

// StakerAdditionAfterDeletionLegality specifies whether a staker can be added after being deleted in the same diff.
// Pre Helicon it is forbidden, and post Helicon it is allowed.
type StakerAdditionAfterDeletionLegality bool

const (
	StakerAdditionAfterDeletionAllowed   StakerAdditionAfterDeletionLegality = true
	StakerAdditionAfterDeletionForbidden StakerAdditionAfterDeletionLegality = false
)

type Stakers interface {
	CurrentStakers
	PendingStakers
}

type CurrentStakers interface {
	// GetCurrentValidator returns the Staker describing the validator on subnetID with nodeID.
	// [database.ErrNotFound] is returned if the validator is not in the validator set.
	GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error)

	// PutCurrentValidator adds the Staker to the validator set.
	//
	// This returns an error if staker is already in the validator set.
	PutCurrentValidator(staker *Staker) error

	// DeleteCurrentValidator removes the Staker from the validator set.
	//
	// This returns an error if staker is not already in the validator set or if there are delegators
	// for staker still present.
	DeleteCurrentValidator(staker *Staker) error

	// SetStakingInfo updates the mutable staking info for nodeID on subnetID.
	//
	// This returns an error if the validator is not in the validator set.
	SetStakingInfo(subnetID ids.ID, nodeID ids.NodeID, stakingInfo StakingInfo) error

	// GetStakingInfo returns the mutable staking info for nodeID on subnetID.
	//
	// This returns an error if the validator is not in the validator set.
	GetStakingInfo(subnetID ids.ID, nodeID ids.NodeID) (StakingInfo, error)

	// GetCurrentDelegatorIterator returns the delegators associated with the
	// validator on subnetID with nodeID. Delegators are sorted by their
	// removal from current staker set (i.e. Staker.NextTime).
	//
	// This returns an empty iterator if the validator is not in the validator set.
	GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error)

	// PutCurrentDelegator adds the staker describing a delegator to the
	// staker set.
	//
	// This returns an error if the validator is not in the validator set.
	//
	// Invariant: staker is not currently a CurrentDelegator
	// TODO error if the delegator is already present
	PutCurrentDelegator(staker *Staker) error

	// DeleteCurrentDelegator removes the staker describing a delegator from
	// the staker set.
	//
	// This returns an error if the validator is not in the validator set.
	//
	// Invariant: staker is currently a CurrentDelegator
	// TODO error if the delegator was not present
	DeleteCurrentDelegator(staker *Staker) error

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
	// isAdditionAfterDeletionAllowed specifies whether a staker can be added after being deleted in the same diff.
	// This is done to preserve the pre-Helicon invariant that a staker cannot be added after being deleted,
	// while allowing post-Helicon diffs to do that.
	isAdditionAfterDeletionAllowed StakerAdditionAfterDeletionLegality
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
	stakingInfo       *StakingInfo
	addedDelegators   *btree.BTreeG[*Staker]
	deletedDelegators map[ids.ID]*Staker
}

// weightChanges returns the total weight added to and removed from this
// validator by this diff. The added weight includes the added validator and all
// added delegators. The removed weight includes the removed validator and all
// deleted delegators.
func (d *diffValidator) weightChanges() (addedWeight uint64, removedWeight uint64, err error) {
	if d.added != nil {
		addedWeight = d.added.Weight
	}

	addedDelegatorIterator := iterator.FromTree(d.addedDelegators)
	defer addedDelegatorIterator.Release()

	for addedDelegatorIterator.Next() {
		addedWeight, err = safemath.Add(addedWeight, addedDelegatorIterator.Value().Weight)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to calculate added weight: %w", err)
		}
	}

	if d.removed != nil {
		removedWeight = d.removed.Weight
	}
	for _, staker := range d.deletedDelegators {
		removedWeight, err = safemath.Add(removedWeight, staker.Weight)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to calculate removed weight: %w", err)
		}
	}

	return addedWeight, removedWeight, nil
}

func (d *diffValidator) WeightDiff() (ValidatorWeightDiff, error) {
	addedWeight, removedWeight, err := d.weightChanges()
	if err != nil {
		return ValidatorWeightDiff{}, err
	}

	var weightDiff ValidatorWeightDiff
	if err := weightDiff.Add(addedWeight); err != nil {
		return ValidatorWeightDiff{}, fmt.Errorf("failed to increase node weight diff: %w", err)
	}
	if err := weightDiff.Sub(removedWeight); err != nil {
		return ValidatorWeightDiff{}, fmt.Errorf("failed to decrease node weight diff: %w", err)
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

	switch {
	case validatorDiff.added != nil:
		return validatorDiff.added, added
	case validatorDiff.removed != nil:
		return nil, deleted
	default:
		return nil, unmodified
	}
}

func (s *diffStakers) PutValidator(staker *Staker) error {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)

	if validatorDiff.removed != nil && !s.isAdditionAfterDeletionAllowed {
		// Enforce the invariant that a validator cannot be added after being
		// deleted.
		return ErrAddingStakerAfterDeletion
	}

	if validatorDiff.removed != nil && validatorDiff.removed.Equals(staker) {
		// We set the removed field when we delete the validator that was not added in this diff before.
		// So if we reached here, it means we removed it first and now either re-adding it.
		// If we're re-adding the exact same validator, we should remove it from the deleted stakers set since it's no longer deleted.

		delete(s.deletedStakers, validatorDiff.removed.TxID)
		if len(s.deletedStakers) == 0 {
			s.deletedStakers = nil
		}

		// If we're re-adding the exact same validator that was removed,
		// the two operations cancel out.
		validatorDiff.removed = nil
		return nil
	}

	validatorDiff.added = staker

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
			deletedStaker, ok := s.deletedStakers[staker.TxID]
			if !ok {
				return false
			}
			return deletedStaker.Equals(staker)
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
