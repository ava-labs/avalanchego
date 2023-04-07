// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	ErrUpdatingDeletedStaker = errors.New("trying to update deleted staker")
	ErrUpdatingUnknownStaker = errors.New("trying to update unknown staker")
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
	PutCurrentValidator(staker *Staker)

	// UpdateCurrentValidator updates the [staker] describing a validator to the
	// staker set.
	//
	// Invariant: [staker] is a non deleted CurrentValidator
	UpdateCurrentValidator(staker *Staker) error

	// DeleteCurrentValidator removes the [staker] describing a validator from
	// the staker set.
	//
	// Invariant: [staker] is currently a CurrentValidator
	DeleteCurrentValidator(staker *Staker)

	// GetCurrentDelegatorIterator returns the delegators associated with the
	// validator on [subnetID] with [nodeID]. Delegators are sorted by their
	// removal from current staker set.
	GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error)

	// PutCurrentDelegator adds the [staker] describing a delegator to the
	// staker set.
	//
	// Invariant: [staker] is not currently a CurrentDelegator
	PutCurrentDelegator(staker *Staker)

	// UpdateCurrentDelegator updates the [staker] describing a delegator to the
	// staker set.
	//
	// Invariant: [staker] is a non deleted CurrentDelegator
	UpdateCurrentDelegator(staker *Staker) error

	// DeleteCurrentDelegator removes the [staker] describing a delegator from
	// the staker set.
	//
	// Invariant: [staker] is currently a CurrentDelegator
	DeleteCurrentDelegator(staker *Staker)

	// GetCurrentStakerIterator returns stakers in order of their removal from
	// the current staker set.
	GetCurrentStakerIterator() (StakerIterator, error)
}

type PendingStakers interface {
	// GetPendingValidator returns the Staker describing the validator on
	// [subnetID] with [nodeID]. If the validator does not exist,
	// [database.ErrNotFound] is returned.
	GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error)

	// PutPendingValidator adds the [staker] describing a validator to the
	// staker set.
	PutPendingValidator(staker *Staker)

	// DeletePendingValidator removes the [staker] describing a validator from
	// the staker set.
	DeletePendingValidator(staker *Staker)

	// GetPendingDelegatorIterator returns the delegators associated with the
	// validator on [subnetID] with [nodeID]. Delegators are sorted by their
	// removal from pending staker set.
	GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error)

	// PutPendingDelegator adds the [staker] describing a delegator to the
	// staker set.
	PutPendingDelegator(staker *Staker)

	// DeletePendingDelegator removes the [staker] describing a delegator from
	// the staker set.
	DeletePendingDelegator(staker *Staker)

	// GetPendingStakerIterator returns stakers in order of their removal from
	// the pending staker set.
	GetPendingStakerIterator() (StakerIterator, error)
}

type baseStakers struct {
	// subnetID --> nodeID --> current state for the validator of the subnet
	// Note: validators supports iteration of stakers over a specific subnetID/nodeID pair
	validators map[ids.ID]map[ids.NodeID]*baseStaker

	// stakers supports iteration of all stakers across any subnetID/nodeID pair
	stakers *btree.BTreeG[*Staker]

	// subnetID --> nodeID --> diff for that validator since the last db write
	// validatorDiffs helps tracking diffs to be flushed to disk upon commits
	validatorDiffs map[ids.ID]map[ids.NodeID]*diffValidator
}

type baseStaker struct {
	validator *Staker

	// delegators ordered for iterations
	delegators *btree.BTreeG[*Staker]

	// delegatorsByTxID allows retrieving delegator
	// to be updated by TxID. We cannot query delegators
	// for it since updated Stakers can have different NextTime
	// (Tree uses Staker.Less to identify a staker instead of Staker.TxID)
	delegatorsByTxID map[ids.ID]*Staker
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
	validatorDiff.validator = stakerAndStatus{
		staker: staker,
		status: added,
	}

	v.stakers.ReplaceOrInsert(staker)
}

func (v *baseStakers) UpdateValidator(staker *Staker) error {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	if validator.validator == nil {
		return fmt.Errorf("%w, subnetID %v, nodeID %v",
			ErrUpdatingDeletedStaker,
			staker.SubnetID,
			staker.NodeID,
		)
	}
	prevStaker := validator.validator

	// Explicitly remove prevStaker from stakers tree. This is because stakers tree
	// identify stakers via stakers.Less function, so stakers with updated start or end time
	// would be treated as a different staker and not replaced by ReplaceOrInsert call.
	v.stakers.Delete(prevStaker)
	v.stakers.ReplaceOrInsert(staker)

	validator.validator = staker

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	validatorDiff.validator = stakerAndStatus{
		staker: staker,
		status: updated,
	}
	return nil
}

func (v *baseStakers) DeleteValidator(staker *Staker) {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)

	// for sake of generality, we assume staker may be an updated version
	// of validator.validator. We explicitly remove the previous version of
	// staker to handle this case.
	prevStaker := validator.validator
	v.stakers.Delete(prevStaker)

	validator.validator = nil
	v.pruneValidator(staker.SubnetID, staker.NodeID)

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	validatorDiff.validator = stakerAndStatus{
		staker: staker,
		status: deleted,
	}
}

func (v *baseStakers) GetDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) StakerIterator {
	subnetValidators, ok := v.validators[subnetID]
	if !ok {
		return EmptyIterator
	}
	validator, ok := subnetValidators[nodeID]
	if !ok {
		return EmptyIterator
	}
	return NewTreeIterator(validator.delegators)
}

func (v *baseStakers) PutDelegator(staker *Staker) {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	if validator.delegators == nil {
		validator.delegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	validator.delegators.ReplaceOrInsert(staker)
	if validator.delegatorsByTxID == nil {
		validator.delegatorsByTxID = make(map[ids.ID]*Staker)
	}
	validator.delegatorsByTxID[staker.TxID] = staker

	v.stakers.ReplaceOrInsert(staker)

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.addedDelegators == nil {
		validatorDiff.addedDelegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	validatorDiff.addedDelegators.ReplaceOrInsert(staker)
}

func (v *baseStakers) UpdateDelegator(staker *Staker) error {
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	if validator.delegatorsByTxID == nil {
		return fmt.Errorf("%w, subnetID %v, nodeID %v",
			ErrUpdatingUnknownStaker,
			staker.SubnetID,
			staker.NodeID,
		)
	}
	prevDelegator, found := validator.delegatorsByTxID[staker.TxID]
	if !found {
		return fmt.Errorf("%w, subnetID %v, nodeID %v",
			ErrUpdatingUnknownStaker,
			staker.SubnetID,
			staker.NodeID,
		)
	}
	validator.delegators.Delete(prevDelegator)
	validator.delegators.ReplaceOrInsert(staker)
	validator.delegatorsByTxID[staker.TxID] = staker

	v.stakers.Delete(prevDelegator)
	v.stakers.ReplaceOrInsert(staker)

	validatorDiff := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.addedDelegators != nil && validatorDiff.addedDelegators.Has(prevDelegator) {
		// updating a staker just added. Keep it as added
		validatorDiff.addedDelegators.Delete(prevDelegator)
		validatorDiff.addedDelegators.ReplaceOrInsert(staker)
	} else {
		if validatorDiff.updatedDelegators == nil {
			validatorDiff.updatedDelegators = make(map[ids.ID]*Staker)
		}
		validatorDiff.updatedDelegators[staker.TxID] = staker
	}
	return nil
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

func (v *baseStakers) GetStakerIterator() StakerIterator {
	return NewTreeIterator(v.stakers)
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
			validator: stakerAndStatus{
				status: unmodified,
			},
		}
		subnetValidatorDiffs[nodeID] = validatorDiff
	}
	return validatorDiff
}

type diffStakers struct {
	// subnetID --> nodeID --> diff for that validator
	// validatorDiffs helps tracking diffs to be pushed to lower level diff/state upon Apply
	// moveover it supported stakers iteration over a specific subnetID/nodeID pair
	validatorDiffs map[ids.ID]map[ids.NodeID]*diffValidator

	// added/deleted/updatedStakers must be mutually non-overlapping
	// added/deleted/updatedStakers support iteration of all stakers across any subnetID/nodeID pair
	addedStakers   *btree.BTreeG[*Staker]
	updatedStakers map[ids.ID]*Staker
	deletedStakers map[ids.ID]*Staker
}

type stakerAndStatus struct {
	staker *Staker
	status diffStakerStatus
}

type diffValidator struct {
	// validatorStatus describes whether a validator has been added or removed.
	// validator stakerAndStatus
	validator stakerAndStatus

	// added/updated/deletedStakers should be mutually non-overlapping
	// TODO ABENEGIA: Verify whether they are, with ad-hoc UTs in state.Commit
	// TODO ABENEGIA: consider enforcing state exclusivity via construction rather than verification
	addedDelegators   *btree.BTreeG[*Staker]
	updatedDelegators map[ids.ID]*Staker
	deletedDelegators map[ids.ID]*Staker

	delegators map[ids.ID]stakerAndStatus // by TxID
}

// GetValidator attempts to fetch the validator with the given subnetID and
// nodeID.
// Invariant: Assumes that the validator will never be removed and then added.
func (s *diffStakers) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, diffStakerStatus) {
	subnetValidatorDiffs, ok := s.validatorDiffs[subnetID]
	if !ok {
		return nil, unmodified
	}

	validatorDiff, ok := subnetValidatorDiffs[nodeID]
	if !ok {
		return nil, unmodified
	}

	switch status := validatorDiff.validator.status; status {
	case added, updated:
		return validatorDiff.validator.staker, status
	default:
		return nil, status
	}
}

func (s *diffStakers) PutValidator(staker *Staker) {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	validatorDiff.validator = stakerAndStatus{
		staker: staker,
		status: added,
	}

	if s.addedStakers == nil {
		s.addedStakers = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	s.addedStakers.ReplaceOrInsert(staker)
	delete(s.deletedStakers, staker.TxID)
	delete(s.updatedStakers, staker.TxID)
}

// UpdateValidator assumes that previous version of staker
// has been pulled up into this diff or that it was just inserted
func (s *diffStakers) UpdateValidator(staker *Staker) error {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	switch validatorDiff.validator.status {
	case added:
		// validator was added and is being immediately updated.
		// We mark it as added
		prevStaker := validatorDiff.validator.staker
		s.addedStakers.Delete(prevStaker)

		validatorDiff.validator = stakerAndStatus{
			staker: staker,
			status: added,
		}
		s.addedStakers.ReplaceOrInsert(staker)

	case deleted:
		return fmt.Errorf("%w, subnetID %v, nodeID %v",
			ErrUpdatingDeletedStaker,
			staker.SubnetID,
			staker.NodeID,
		)

	default: // already updated or unmodified
		validatorDiff.validator = stakerAndStatus{
			staker: staker,
			status: updated,
		}

		if s.updatedStakers == nil {
			s.updatedStakers = make(map[ids.ID]*Staker)
		}
		s.updatedStakers[staker.TxID] = staker
	}
	return nil
}

func (s *diffStakers) DeleteValidator(staker *Staker) {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.validator.status == added {
		// This validator was added and immediately removed in this diff. We
		// treat it as if it was never added.
		s.addedStakers.Delete(validatorDiff.validator.staker)
		validatorDiff.validator = stakerAndStatus{
			staker: nil,
			status: unmodified,
		}
	} else {
		validatorDiff.validator = stakerAndStatus{
			staker: staker,
			status: deleted,
		}
		if s.deletedStakers == nil {
			s.deletedStakers = make(map[ids.ID]*Staker)
		}
		s.deletedStakers[staker.TxID] = staker
		delete(s.updatedStakers, staker.TxID) // deleted and updated Stakers must not overlap
	}
}

func (s *diffStakers) GetDelegatorIterator(
	parentIterator StakerIterator,
	subnetID ids.ID,
	nodeID ids.NodeID,
) StakerIterator {
	var (
		addedDelegatorIterator = EmptyIterator
		deletedDelegators      map[ids.ID]*Staker
		updatedDelegators      map[ids.ID]*Staker
	)
	if subnetValidatorDiffs, ok := s.validatorDiffs[subnetID]; ok {
		if validatorDiff, ok := subnetValidatorDiffs[nodeID]; ok {
			addedDelegatorIterator = NewTreeIterator(validatorDiff.addedDelegators)
			deletedDelegators = validatorDiff.deletedDelegators
			updatedDelegators = validatorDiff.updatedDelegators
		}
	}

	return NewMaskedIterator(
		NewMergedIterator(
			parentIterator,
			addedDelegatorIterator,
		),
		deletedDelegators,
		updatedDelegators,
	)
}

func (s *diffStakers) PutDelegator(staker *Staker) {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.addedDelegators == nil {
		validatorDiff.addedDelegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	validatorDiff.addedDelegators.ReplaceOrInsert(staker)
	delete(validatorDiff.deletedDelegators, staker.TxID)

	if validatorDiff.delegators == nil {
		validatorDiff.delegators = make(map[ids.ID]stakerAndStatus)
	}
	validatorDiff.delegators[staker.TxID] = stakerAndStatus{
		staker: staker,
		status: added,
	}

	if s.addedStakers == nil {
		s.addedStakers = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	s.addedStakers.ReplaceOrInsert(staker)
	delete(s.deletedStakers, staker.TxID)
}

func (s *diffStakers) UpdateDelegator(staker *Staker) error {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	prevStaker, found := validatorDiff.delegators[staker.TxID]
	if !found {
		prevStaker = stakerAndStatus{
			staker: nil,
			status: unmodified,
		}
	}
	switch prevStaker.status {
	case added:
		// delegator was added and is being immediately updated.
		// We mark it as added
		validatorDiff.addedDelegators.Delete(prevStaker.staker)
		validatorDiff.addedDelegators.ReplaceOrInsert(staker)

		s.addedStakers.Delete(prevStaker.staker)
		s.addedStakers.ReplaceOrInsert(staker)

		validatorDiff.delegators[staker.TxID] = stakerAndStatus{
			staker: staker,
			status: added,
		}

	case deleted:
		return fmt.Errorf("%w, subnetID %v, nodeID %v",
			ErrUpdatingDeletedStaker,
			staker.SubnetID,
			staker.NodeID,
		)

	default: // already updated or unmodified
		if validatorDiff.delegators == nil {
			validatorDiff.delegators = make(map[ids.ID]stakerAndStatus)
		}
		validatorDiff.delegators[staker.TxID] = stakerAndStatus{
			staker: staker,
			status: updated,
		}

		if validatorDiff.updatedDelegators == nil {
			validatorDiff.updatedDelegators = make(map[ids.ID]*Staker)
		}
		validatorDiff.updatedDelegators[staker.TxID] = staker

		if s.updatedStakers == nil {
			s.updatedStakers = make(map[ids.ID]*Staker)
		}
		s.updatedStakers[staker.TxID] = staker
	}
	return nil
}

func (s *diffStakers) DeleteDelegator(staker *Staker) {
	validatorDiff := s.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.deletedDelegators == nil {
		validatorDiff.deletedDelegators = make(map[ids.ID]*Staker)
	}
	validatorDiff.deletedDelegators[staker.TxID] = staker
	if validatorDiff.delegators == nil {
		validatorDiff.delegators = make(map[ids.ID]stakerAndStatus)
	}
	validatorDiff.delegators[staker.TxID] = stakerAndStatus{
		staker: staker,
		status: deleted,
	}

	if s.deletedStakers == nil {
		s.deletedStakers = make(map[ids.ID]*Staker)
	}
	s.deletedStakers[staker.TxID] = staker
	delete(s.updatedStakers, staker.TxID)
}

func (s *diffStakers) GetStakerIterator(parentIterator StakerIterator) StakerIterator {
	return NewMaskedIterator(
		NewMergedIterator(
			parentIterator,
			NewTreeIterator(s.addedStakers),
		),
		s.deletedStakers,
		s.updatedStakers,
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
			validator: stakerAndStatus{
				status: unmodified,
			},
		}
		subnetValidatorDiffs[nodeID] = validatorDiff
	}
	return validatorDiff
}
