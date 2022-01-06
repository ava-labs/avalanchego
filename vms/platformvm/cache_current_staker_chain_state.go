// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	errNotEnoughValidators = errors.New("not enough validators")

	_ currentStakerChainState = &currentStakerChainStateImpl{}
)

type currentStakerChainState interface {
	// The NextStaker value returns the next staker that is going to be removed
	// using a RewardValidatorTx. Therefore, only AddValidatorTxs and
	// AddDelegatorTxs will be returned. AddSubnetValidatorTxs are removed using
	// AdvanceTimestampTxs.
	GetNextStaker() (addStakerTx *Tx, potentialReward uint64, err error)
	GetStaker(txID ids.ID) (tx *Tx, potentialReward uint64, err error)
	GetValidator(nodeID ids.ShortID) (currentValidator, error)

	UpdateStakers(
		addValidators []*validatorReward,
		addDelegators []*validatorReward,
		addSubnetValidators []*Tx,
		numTxsToRemove int,
	) (currentStakerChainState, error)
	DeleteNextStaker() (currentStakerChainState, error)

	// Stakers returns the current stakers on the network sorted in order of the
	// order of their future removal from the validator set.
	Stakers() []*Tx

	Apply(InternalState)

	// Return the current validator set of [subnetID].
	ValidatorSet(subnetID ids.ID) (validators.Set, error)
}

// currentStakerChainStateImpl is a copy on write implementation for versioning
// the validator set. None of the slices, maps, or pointers should be modified
// after initialization.
type currentStakerChainStateImpl struct {
	nextStaker *validatorReward

	// nodeID -> validator
	validatorsByNodeID map[ids.ShortID]*currentValidatorImpl

	// txID -> tx
	validatorsByTxID map[ids.ID]*validatorReward

	// list of current validators in order of their removal from the validator
	// set
	validators []*Tx

	addedStakers   []*validatorReward
	deletedStakers []*Tx
}

type validatorReward struct {
	addStakerTx     *Tx
	potentialReward uint64
}

func (cs *currentStakerChainStateImpl) GetNextStaker() (addStakerTx *Tx, potentialReward uint64, err error) {
	if cs.nextStaker == nil {
		return nil, 0, database.ErrNotFound
	}
	return cs.nextStaker.addStakerTx, cs.nextStaker.potentialReward, nil
}

func (cs *currentStakerChainStateImpl) GetValidator(nodeID ids.ShortID) (currentValidator, error) {
	vdr, exists := cs.validatorsByNodeID[nodeID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return vdr, nil
}

func (cs *currentStakerChainStateImpl) UpdateStakers(
	addValidatorTxs []*validatorReward,
	addDelegatorTxs []*validatorReward,
	addSubnetValidatorTxs []*Tx,
	numTxsToRemove int,
) (currentStakerChainState, error) {
	if numTxsToRemove > len(cs.validators) {
		return nil, errNotEnoughValidators
	}
	newCS := &currentStakerChainStateImpl{
		validatorsByNodeID: make(map[ids.ShortID]*currentValidatorImpl, len(cs.validatorsByNodeID)+len(addValidatorTxs)),
		validatorsByTxID:   make(map[ids.ID]*validatorReward, len(cs.validatorsByTxID)+len(addValidatorTxs)+len(addDelegatorTxs)+len(addSubnetValidatorTxs)),
		validators:         cs.validators[numTxsToRemove:], // sorted in order of removal

		addedStakers:   append(addValidatorTxs, addDelegatorTxs...),
		deletedStakers: cs.validators[:numTxsToRemove],
	}

	for nodeID, vdr := range cs.validatorsByNodeID {
		newCS.validatorsByNodeID[nodeID] = vdr
	}

	for txID, vdr := range cs.validatorsByTxID {
		newCS.validatorsByTxID[txID] = vdr
	}

	if numAdded := len(addValidatorTxs) + len(addDelegatorTxs) + len(addSubnetValidatorTxs); numAdded != 0 {
		numCurrent := len(newCS.validators)
		newSize := numCurrent + numAdded
		newValidators := make([]*Tx, newSize)
		copy(newValidators, newCS.validators)
		copy(newValidators[numCurrent:], addSubnetValidatorTxs)

		numStart := numCurrent + len(addSubnetValidatorTxs)
		for i, tx := range addValidatorTxs {
			newValidators[numStart+i] = tx.addStakerTx
		}

		numStart = numCurrent + len(addSubnetValidatorTxs) + len(addValidatorTxs)
		for i, tx := range addDelegatorTxs {
			newValidators[numStart+i] = tx.addStakerTx
		}

		sortValidatorsByRemoval(newValidators)
		newCS.validators = newValidators

		for _, vdr := range addValidatorTxs {
			switch tx := vdr.addStakerTx.UnsignedTx.(type) {
			case *UnsignedAddValidatorTx:
				newCS.validatorsByNodeID[tx.Validator.NodeID] = &currentValidatorImpl{
					addValidatorTx:  tx,
					potentialReward: vdr.potentialReward,
				}
				newCS.validatorsByTxID[vdr.addStakerTx.ID()] = vdr
			default:
				return nil, errWrongTxType
			}
		}

		for _, vdr := range addDelegatorTxs {
			switch tx := vdr.addStakerTx.UnsignedTx.(type) {
			case *UnsignedAddDelegatorTx:
				oldVdr := newCS.validatorsByNodeID[tx.Validator.NodeID]
				newVdr := *oldVdr
				newVdr.delegators = make([]*UnsignedAddDelegatorTx, len(oldVdr.delegators)+1)
				copy(newVdr.delegators, oldVdr.delegators)
				newVdr.delegators[len(oldVdr.delegators)] = tx
				sortDelegatorsByRemoval(newVdr.delegators)
				newVdr.delegatorWeight += tx.Validator.Wght
				newCS.validatorsByNodeID[tx.Validator.NodeID] = &newVdr
				newCS.validatorsByTxID[vdr.addStakerTx.ID()] = vdr
			default:
				return nil, errWrongTxType
			}
		}

		for _, vdr := range addSubnetValidatorTxs {
			switch tx := vdr.UnsignedTx.(type) {
			case *UnsignedAddSubnetValidatorTx:
				oldVdr := newCS.validatorsByNodeID[tx.Validator.NodeID]
				newVdr := *oldVdr
				newVdr.subnets = make(map[ids.ID]*UnsignedAddSubnetValidatorTx, len(oldVdr.subnets)+1)
				for subnetID, addTx := range oldVdr.subnets {
					newVdr.subnets[subnetID] = addTx
				}
				newVdr.subnets[tx.Validator.Subnet] = tx
				newCS.validatorsByNodeID[tx.Validator.NodeID] = &newVdr
			default:
				return nil, errWrongTxType
			}

			wrappedTx := &validatorReward{
				addStakerTx: vdr,
			}
			newCS.validatorsByTxID[vdr.ID()] = wrappedTx
			newCS.addedStakers = append(newCS.addedStakers, wrappedTx)
		}
	}

	for i := 0; i < numTxsToRemove; i++ {
		removed := cs.validators[i]
		removedID := removed.ID()
		delete(newCS.validatorsByTxID, removedID)

		switch tx := removed.UnsignedTx.(type) {
		case *UnsignedAddSubnetValidatorTx:
			oldVdr := newCS.validatorsByNodeID[tx.Validator.NodeID]
			newVdr := *oldVdr
			newVdr.subnets = make(map[ids.ID]*UnsignedAddSubnetValidatorTx, len(oldVdr.subnets)-1)
			for subnetID, addTx := range oldVdr.subnets {
				if removedID != addTx.ID() {
					newVdr.subnets[subnetID] = addTx
				}
			}
			newCS.validatorsByNodeID[tx.Validator.NodeID] = &newVdr
		default:
			return nil, errWrongTxType
		}
	}

	newCS.setNextStaker()
	return newCS, nil
}

func (cs *currentStakerChainStateImpl) DeleteNextStaker() (currentStakerChainState, error) {
	removedTx, _, err := cs.GetNextStaker()
	if err != nil {
		return nil, err
	}
	removedTxID := removedTx.ID()

	newCS := &currentStakerChainStateImpl{
		validatorsByNodeID: make(map[ids.ShortID]*currentValidatorImpl, len(cs.validatorsByNodeID)),
		validatorsByTxID:   make(map[ids.ID]*validatorReward, len(cs.validatorsByTxID)-1),
		validators:         cs.validators[1:], // sorted in order of removal

		deletedStakers: []*Tx{removedTx},
	}

	switch tx := removedTx.UnsignedTx.(type) {
	case *UnsignedAddValidatorTx:
		for nodeID, vdr := range cs.validatorsByNodeID {
			if nodeID != tx.Validator.NodeID {
				newCS.validatorsByNodeID[nodeID] = vdr
			}
		}
	case *UnsignedAddDelegatorTx:
		for nodeID, vdr := range cs.validatorsByNodeID {
			if nodeID != tx.Validator.NodeID {
				newCS.validatorsByNodeID[nodeID] = vdr
			} else {
				newCS.validatorsByNodeID[nodeID] = &currentValidatorImpl{
					validatorImpl: validatorImpl{
						delegators: vdr.delegators[1:], // sorted in order of removal
						subnets:    vdr.subnets,
					},
					addValidatorTx:  vdr.addValidatorTx,
					delegatorWeight: vdr.delegatorWeight - tx.Validator.Wght,
					potentialReward: vdr.potentialReward,
				}
			}
		}
	default:
		return nil, errWrongTxType
	}

	for txID, vdr := range cs.validatorsByTxID {
		if txID != removedTxID {
			newCS.validatorsByTxID[txID] = vdr
		}
	}

	newCS.setNextStaker()
	return newCS, nil
}

func (cs *currentStakerChainStateImpl) Stakers() []*Tx {
	return cs.validators
}

func (cs *currentStakerChainStateImpl) Apply(is InternalState) {
	for _, added := range cs.addedStakers {
		is.AddCurrentStaker(added.addStakerTx, added.potentialReward)
	}
	for _, deleted := range cs.deletedStakers {
		is.DeleteCurrentStaker(deleted)
	}
	is.SetCurrentStakerChainState(cs)

	// Validator changes should only be applied once.
	cs.addedStakers = nil
	cs.deletedStakers = nil
}

func (cs *currentStakerChainStateImpl) ValidatorSet(subnetID ids.ID) (validators.Set, error) {
	if subnetID == constants.PrimaryNetworkID {
		return cs.primaryValidatorSet()
	}
	return cs.subnetValidatorSet(subnetID)
}

func (cs *currentStakerChainStateImpl) primaryValidatorSet() (validators.Set, error) {
	vdrs := validators.NewSet()

	var err error
	for nodeID, vdr := range cs.validatorsByNodeID {
		vdrWeight := vdr.addValidatorTx.Validator.Wght
		vdrWeight, err = safemath.Add64(vdrWeight, vdr.delegatorWeight)
		if err != nil {
			return nil, err
		}
		if err := vdrs.AddWeight(nodeID, vdrWeight); err != nil {
			return nil, err
		}
	}

	return vdrs, nil
}

func (cs *currentStakerChainStateImpl) subnetValidatorSet(subnetID ids.ID) (validators.Set, error) {
	vdrs := validators.NewSet()

	for nodeID, vdr := range cs.validatorsByNodeID {
		subnetVDR, exists := vdr.subnets[subnetID]
		if !exists {
			continue
		}
		if err := vdrs.AddWeight(nodeID, subnetVDR.Validator.Wght); err != nil {
			return nil, err
		}
	}

	return vdrs, nil
}

func (cs *currentStakerChainStateImpl) GetStaker(txID ids.ID) (tx *Tx, reward uint64, err error) {
	staker, exists := cs.validatorsByTxID[txID]
	if !exists {
		return nil, 0, database.ErrNotFound
	}
	return staker.addStakerTx, staker.potentialReward, nil
}

// setNextStaker to the next staker that will be removed using a
// RewardValidatorTx.
func (cs *currentStakerChainStateImpl) setNextStaker() {
	for _, tx := range cs.validators {
		switch tx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx, *UnsignedAddDelegatorTx:
			cs.nextStaker = cs.validatorsByTxID[tx.ID()]
			return
		}
	}
}

type innerSortValidatorsByRemoval []*Tx

func (s innerSortValidatorsByRemoval) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	var (
		iEndTime  time.Time
		iPriority byte
	)
	switch tx := iDel.UnsignedTx.(type) {
	case *UnsignedAddValidatorTx:
		iEndTime = tx.EndTime()
		iPriority = lowPriority
	case *UnsignedAddDelegatorTx:
		iEndTime = tx.EndTime()
		iPriority = mediumPriority
	case *UnsignedAddSubnetValidatorTx:
		iEndTime = tx.EndTime()
		iPriority = topPriority
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", iDel.UnsignedTx))
	}

	var (
		jEndTime  time.Time
		jPriority byte
	)
	switch tx := jDel.UnsignedTx.(type) {
	case *UnsignedAddValidatorTx:
		jEndTime = tx.EndTime()
		jPriority = lowPriority
	case *UnsignedAddDelegatorTx:
		jEndTime = tx.EndTime()
		jPriority = mediumPriority
	case *UnsignedAddSubnetValidatorTx:
		jEndTime = tx.EndTime()
		jPriority = topPriority
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", jDel.UnsignedTx))
	}

	if iEndTime.Before(jEndTime) {
		return true
	}
	if jEndTime.Before(iEndTime) {
		return false
	}

	// If the end times are the same, then we sort by the tx type. First we
	// remove UnsignedAddSubnetValidatorTxs, then UnsignedAddDelegatorTx, then
	// UnsignedAddValidatorTx.
	if iPriority > jPriority {
		return true
	}
	if iPriority < jPriority {
		return false
	}

	// If the end times are the same, and the tx types are the same, then we
	// sort by the txID.
	iTxID := iDel.ID()
	jTxID := jDel.ID()
	return bytes.Compare(iTxID[:], jTxID[:]) == -1
}

func (s innerSortValidatorsByRemoval) Len() int {
	return len(s)
}

func (s innerSortValidatorsByRemoval) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func sortValidatorsByRemoval(s []*Tx) {
	sort.Sort(innerSortValidatorsByRemoval(s))
}

type innerSortDelegatorsByRemoval []*UnsignedAddDelegatorTx

func (s innerSortDelegatorsByRemoval) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	iEndTime := iDel.EndTime()
	jEndTime := jDel.EndTime()
	if iEndTime.Before(jEndTime) {
		return true
	}
	if jEndTime.Before(iEndTime) {
		return false
	}

	// If the end times are the same, then we sort by the txID
	iTxID := iDel.ID()
	jTxID := jDel.ID()
	return bytes.Compare(iTxID[:], jTxID[:]) == -1
}

func (s innerSortDelegatorsByRemoval) Len() int {
	return len(s)
}

func (s innerSortDelegatorsByRemoval) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func sortDelegatorsByRemoval(s []*UnsignedAddDelegatorTx) {
	sort.Sort(innerSortDelegatorsByRemoval(s))
}
