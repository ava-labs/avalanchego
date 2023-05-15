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

	// SetDelegateeReward sets the accrued delegation rewards for [nodeID] on
	// [subnetID] to [amount].
	SetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID, amount uint64) error

	// GetDelegateeReward returns the accrued delegation rewards for [nodeID] on
	// [subnetID].
	GetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID) (uint64, error)

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

// baseStakers is the container for current and pending stakers in State (not Diff)
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
	validator *Staker // if deleted is nil

	// sortedDelegators ordered for iterations
	sortedDelegators *btree.BTreeG[*Staker]

	// delegators allows retrieving delegator
	// to be updated by TxID. We cannot query delegators Tree
	// for it since updated Stakers can have different NextTime
	// (and Tree uses Staker.Less to identify a staker instead of Staker.TxID)
	delegators map[ids.ID]*Staker
}

func newBaseStakers() *baseStakers {
	return &baseStakers{
		validators:     make(map[ids.ID]map[ids.NodeID]*baseStaker),
		stakers:        btree.NewG(defaultTreeDegree, (*Staker).Less),
		validatorDiffs: make(map[ids.ID]map[ids.NodeID]*diffValidator),
	}
}

func (v *baseStakers) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	validator, found := v.getValidator(subnetID, nodeID)
	if !found {
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

	v.stakers.ReplaceOrInsert(staker)

	validatorDiff := getOrCreateDiff(v.validatorDiffs, staker.SubnetID, staker.NodeID)
	validatorDiff.validator = stakerAndStatus{
		staker: staker,
		status: added,
	}
}

func (v *baseStakers) UpdateValidator(staker *Staker) error {
	validator, found := v.getValidator(staker.SubnetID, staker.NodeID)
	if !found {
		return fmt.Errorf("%w, subnetID %v, nodeID %v",
			ErrUpdatingDeletedStaker,
			staker.SubnetID,
			staker.NodeID,
		)
	}
	prevStaker := validator.validator
	validator.validator = staker

	// Explicitly remove prevStaker from stakers tree. This is because stakers tree
	// identifies stakers via stakers.Less function, so a staker with updated Start/EndTime
	// would be treated as a different staker and not be replaced by ReplaceOrInsert call.
	v.stakers.Delete(prevStaker)
	v.stakers.ReplaceOrInsert(staker)

	validatorDiff := getOrCreateDiff(v.validatorDiffs, staker.SubnetID, staker.NodeID)
	validatorDiff.validator = stakerAndStatus{
		staker: staker,
		status: updated,
	}
	return nil
}

func (v *baseStakers) DeleteValidator(staker *Staker) {
	var (
		subnetID = staker.SubnetID
		nodeID   = staker.NodeID
	)
	validator, found := v.getValidator(subnetID, nodeID)
	if !found {
		// deleting an non-existing staker. Nothing to do.
		return
	}
	storedStaker := validator.validator
	validator.validator = nil
	v.pruneValidator(subnetID, nodeID)

	// for sake of generality, we assume we could delete an updated version
	// of validator.validator. We explicitly remove the currently stored
	// version of the staker to handle this case.
	v.stakers.Delete(storedStaker)

	validatorDiff := getOrCreateDiff(v.validatorDiffs, subnetID, nodeID)
	validatorDiff.validator = stakerAndStatus{
		staker: storedStaker,
		status: deleted,
	}
}

func (v *baseStakers) GetDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) StakerIterator {
	validator, found := v.getValidator(subnetID, nodeID)
	if !found {
		return EmptyIterator
	}
	return NewTreeIterator(validator.sortedDelegators)
}

func (v *baseStakers) PutDelegator(staker *Staker) {
	// Note: a delegator may be inserted before its validator
	// hence we use v.getOrCreateValidator instead of v.getValidator
	validator := v.getOrCreateValidator(staker.SubnetID, staker.NodeID)
	if validator.sortedDelegators == nil {
		validator.sortedDelegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	validator.sortedDelegators.ReplaceOrInsert(staker)
	if validator.delegators == nil {
		validator.delegators = make(map[ids.ID]*Staker)
	}
	validator.delegators[staker.TxID] = staker

	v.stakers.ReplaceOrInsert(staker)

	validatorDiff := getOrCreateDiff(v.validatorDiffs, staker.SubnetID, staker.NodeID)
	if validatorDiff.addedDelegators == nil {
		validatorDiff.addedDelegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
	}
	validatorDiff.addedDelegators.ReplaceOrInsert(staker)
}

func (v *baseStakers) UpdateDelegator(staker *Staker) error {
	validator, found := v.getValidator(staker.SubnetID, staker.NodeID)
	if !found {
		return fmt.Errorf("%w, subnetID %v, nodeID %v",
			ErrUpdatingUnknownStaker,
			staker.SubnetID,
			staker.NodeID,
		)
	}
	prevDelegator, found := validator.delegators[staker.TxID]
	if !found {
		return fmt.Errorf("%w, subnetID %v, nodeID %v, txID %v",
			ErrUpdatingUnknownStaker,
			staker.SubnetID,
			staker.NodeID,
			staker.TxID,
		)
	}
	validator.sortedDelegators.Delete(prevDelegator)
	validator.sortedDelegators.ReplaceOrInsert(staker)
	validator.delegators[staker.TxID] = staker

	v.stakers.Delete(prevDelegator)
	v.stakers.ReplaceOrInsert(staker)

	validatorDiff := getOrCreateDiff(v.validatorDiffs, staker.SubnetID, staker.NodeID)
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
	validator, found := v.getValidator(staker.SubnetID, staker.NodeID)
	if !found {
		// deleting an non-existing staker. Nothing to do.
		return
	}
	if validator.sortedDelegators != nil {
		validator.sortedDelegators.Delete(staker)
	}
	v.pruneValidator(staker.SubnetID, staker.NodeID)

	validatorDiff := getOrCreateDiff(v.validatorDiffs, staker.SubnetID, staker.NodeID)
	if validatorDiff.deletedDelegators == nil {
		validatorDiff.deletedDelegators = make(map[ids.ID]*Staker)
	}
	validatorDiff.deletedDelegators[staker.TxID] = staker

	v.stakers.Delete(staker)
}

func (v *baseStakers) GetStakerIterator() StakerIterator {
	return NewTreeIterator(v.stakers)
}

func (v *baseStakers) getValidator(subnetID ids.ID, nodeID ids.NodeID) (*baseStaker, bool) {
	subnetValidators, found := v.validators[subnetID]
	if !found {
		return nil, false
	}
	validator, found := subnetValidators[nodeID]
	return validator, found
}

func (v *baseStakers) getOrCreateValidator(subnetID ids.ID, nodeID ids.NodeID) *baseStaker {
	validator, found := v.getValidator(subnetID, nodeID)
	if found {
		return validator
	}
	// not found, create it
	subnetValidators, found := v.validators[subnetID]
	if !found {
		subnetValidators = make(map[ids.NodeID]*baseStaker)
		v.validators[subnetID] = subnetValidators
	}

	validator = &baseStaker{}
	subnetValidators[nodeID] = validator
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
	if validator.sortedDelegators != nil && validator.sortedDelegators.Len() > 0 {
		return
	}
	delete(subnetValidators, nodeID)
	if len(subnetValidators) == 0 {
		delete(v.validators, subnetID)
	}
}

// diffStakers is the container for current and pending stakers in Diff (not State)
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

func newDiffStakers() *diffStakers {
	return &diffStakers{
		validatorDiffs: make(map[ids.ID]map[ids.NodeID]*diffValidator),
		addedStakers:   btree.NewG(defaultTreeDegree, (*Staker).Less),
		updatedStakers: make(map[ids.ID]*Staker),
		deletedStakers: make(map[ids.ID]*Staker),
	}
}

type stakerAndStatus struct {
	staker *Staker
	status diffStakerStatus
}

type diffValidator struct {
	// validatorStatus describes whether a validator has been added or removed.
	// validator stakerAndStatus
	validator stakerAndStatus

	// Invariant: added/updated/deletedStakers should be mutually non-overlapping
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
	validatorDiff := getOrCreateDiff(s.validatorDiffs, staker.SubnetID, staker.NodeID)
	validatorDiff.validator = stakerAndStatus{
		staker: staker,
		status: added,
	}

	s.addedStakers.ReplaceOrInsert(staker)
	delete(s.deletedStakers, staker.TxID)
}

// UpdateValidator assumes that previous version of staker
// has been pulled up into this diff or that it was just inserted
func (s *diffStakers) UpdateValidator(staker *Staker) error {
	validatorDiff := getOrCreateDiff(s.validatorDiffs, staker.SubnetID, staker.NodeID)
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

		s.updatedStakers[staker.TxID] = staker
	}
	return nil
}

func (s *diffStakers) DeleteValidator(staker *Staker) {
	validatorDiff := getOrCreateDiff(s.validatorDiffs, staker.SubnetID, staker.NodeID)
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
	validatorDiff := getOrCreateDiff(s.validatorDiffs, staker.SubnetID, staker.NodeID)
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

	s.addedStakers.ReplaceOrInsert(staker)
	delete(s.deletedStakers, staker.TxID)
}

func (s *diffStakers) UpdateDelegator(staker *Staker) error {
	validatorDiff := getOrCreateDiff(s.validatorDiffs, staker.SubnetID, staker.NodeID)
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

		s.updatedStakers[staker.TxID] = staker
	}
	return nil
}

func (s *diffStakers) DeleteDelegator(staker *Staker) {
	validatorDiff := getOrCreateDiff(s.validatorDiffs, staker.SubnetID, staker.NodeID)
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

func getOrCreateDiff(
	validatorDiffs map[ids.ID]map[ids.NodeID]*diffValidator,
	subnetID ids.ID,
	nodeID ids.NodeID,
) *diffValidator {
	subnetValidatorDiffs, ok := validatorDiffs[subnetID]
	if !ok {
		subnetValidatorDiffs = make(map[ids.NodeID]*diffValidator)
		validatorDiffs[subnetID] = subnetValidatorDiffs
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
