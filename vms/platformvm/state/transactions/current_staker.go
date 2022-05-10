// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

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
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ CurrentStaker = &currentStaker{}

	ErrNotEnoughValidators = errors.New("not enough validators")
)

type CurrentStaker interface {
	// The NextStaker value returns the next staker that is going to be removed
	// using a RewardValidatorTx. Therefore, only AddValidatorTxs and
	// AddDelegatorTxs will be returned. AddSubnetValidatorTxs are removed using
	// AdvanceTimestampTxs.
	GetNextStaker() (addStakerTx *signed.Tx, potentialReward uint64, err error)
	GetStaker(txID ids.ID) (tx *signed.Tx, potentialReward uint64, err error)
	GetValidator(nodeID ids.NodeID) (currentValidator, error)

	UpdateStakers(
		addValidators []*ValidatorReward,
		addDelegators []*ValidatorReward,
		addSubnetValidators []*signed.Tx,
		numTxsToRemove int,
	) (CurrentStaker, error)
	DeleteNextStaker() (CurrentStaker, error)

	// Stakers returns the current stakers on the network sorted in order of the
	// order of their future removal from the validator set.
	Stakers() []*signed.Tx

	Apply(Content)

	// Return the current validator set of [subnetID].
	ValidatorSet(subnetID ids.ID) (validators.Set, error)
}

// currentStaker is a copy on write implementation for versioning
// the validator set. None of the slices, maps, or pointers should be modified
// after initialization.
type currentStaker struct {
	// nodeID -> validator
	ValidatorsByNodeID map[ids.NodeID]*currentValidatorImpl

	// txID -> tx
	ValidatorsByTxID map[ids.ID]*ValidatorReward

	// list of current Validators in order of their removal from the validator
	// set
	Validators []*signed.Tx

	nextStaker     *ValidatorReward
	addedStakers   []*ValidatorReward
	deletedStakers []*signed.Tx
}

type ValidatorReward struct {
	AddStakerTx     *signed.Tx
	PotentialReward uint64
}

func (cs *currentStaker) GetNextStaker() (addStakerTx *signed.Tx, potentialReward uint64, err error) {
	if cs.nextStaker == nil {
		return nil, 0, database.ErrNotFound
	}
	return cs.nextStaker.AddStakerTx, cs.nextStaker.PotentialReward, nil
}

func (cs *currentStaker) GetValidator(nodeID ids.NodeID) (currentValidator, error) {
	vdr, exists := cs.ValidatorsByNodeID[nodeID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return vdr, nil
}

func (cs *currentStaker) UpdateStakers(
	addValidatorTxs []*ValidatorReward,
	addDelegatorTxs []*ValidatorReward,
	addSubnetValidatorTxs []*signed.Tx,
	numTxsToRemove int,
) (CurrentStaker, error) {
	if numTxsToRemove > len(cs.Validators) {
		return nil, ErrNotEnoughValidators
	}
	newCS := &currentStaker{
		ValidatorsByNodeID: make(map[ids.NodeID]*currentValidatorImpl, len(cs.ValidatorsByNodeID)+len(addValidatorTxs)),
		ValidatorsByTxID:   make(map[ids.ID]*ValidatorReward, len(cs.ValidatorsByTxID)+len(addValidatorTxs)+len(addDelegatorTxs)+len(addSubnetValidatorTxs)),
		Validators:         cs.Validators[numTxsToRemove:], // sorted in order of removal

		addedStakers:   append(addValidatorTxs, addDelegatorTxs...),
		deletedStakers: cs.Validators[:numTxsToRemove],
	}

	for nodeID, vdr := range cs.ValidatorsByNodeID {
		newCS.ValidatorsByNodeID[nodeID] = vdr
	}

	for txID, vdr := range cs.ValidatorsByTxID {
		newCS.ValidatorsByTxID[txID] = vdr
	}

	if numAdded := len(addValidatorTxs) + len(addDelegatorTxs) + len(addSubnetValidatorTxs); numAdded != 0 {
		numCurrent := len(newCS.Validators)
		newSize := numCurrent + numAdded
		newValidators := make([]*signed.Tx, newSize)
		copy(newValidators, newCS.Validators)
		copy(newValidators[numCurrent:], addSubnetValidatorTxs)

		numStart := numCurrent + len(addSubnetValidatorTxs)
		for i, tx := range addValidatorTxs {
			newValidators[numStart+i] = tx.AddStakerTx
		}

		numStart = numCurrent + len(addSubnetValidatorTxs) + len(addValidatorTxs)
		for i, tx := range addDelegatorTxs {
			newValidators[numStart+i] = tx.AddStakerTx
		}

		SortValidatorsByRemoval(newValidators)
		newCS.Validators = newValidators

		for _, vdr := range addValidatorTxs {
			switch tx := vdr.AddStakerTx.Unsigned.(type) {
			case *unsigned.AddValidatorTx:
				newCS.ValidatorsByNodeID[tx.Validator.NodeID] = &currentValidatorImpl{
					addValidatorTx:  tx,
					potentialReward: vdr.PotentialReward,
				}
				newCS.ValidatorsByTxID[vdr.AddStakerTx.Unsigned.ID()] = vdr
			default:
				return nil, unsigned.ErrWrongTxType
			}
		}

		for _, vdr := range addDelegatorTxs {
			switch tx := vdr.AddStakerTx.Unsigned.(type) {
			case *unsigned.AddDelegatorTx:
				oldVdr := newCS.ValidatorsByNodeID[tx.Validator.NodeID]
				newVdr := *oldVdr
				newVdr.delegators = make([]*unsigned.AddDelegatorTx, len(oldVdr.delegators)+1)
				copy(newVdr.delegators, oldVdr.delegators)
				newVdr.delegators[len(oldVdr.delegators)] = tx
				SortDelegatorsByRemoval(newVdr.delegators)
				newVdr.delegatorWeight += tx.Validator.Wght
				newCS.ValidatorsByNodeID[tx.Validator.NodeID] = &newVdr
				newCS.ValidatorsByTxID[vdr.AddStakerTx.Unsigned.ID()] = vdr
			default:
				return nil, unsigned.ErrWrongTxType
			}
		}

		for _, vdr := range addSubnetValidatorTxs {
			switch tx := vdr.Unsigned.(type) {
			case *unsigned.AddSubnetValidatorTx:
				oldVdr := newCS.ValidatorsByNodeID[tx.Validator.NodeID]
				newVdr := *oldVdr
				newVdr.subnets = make(map[ids.ID]*unsigned.AddSubnetValidatorTx, len(oldVdr.subnets)+1)
				for subnetID, addTx := range oldVdr.subnets {
					newVdr.subnets[subnetID] = addTx
				}
				newVdr.subnets[tx.Validator.Subnet] = tx
				newCS.ValidatorsByNodeID[tx.Validator.NodeID] = &newVdr
			default:
				return nil, unsigned.ErrWrongTxType
			}

			wrappedTx := &ValidatorReward{
				AddStakerTx: vdr,
			}
			newCS.ValidatorsByTxID[vdr.Unsigned.ID()] = wrappedTx
			newCS.addedStakers = append(newCS.addedStakers, wrappedTx)
		}
	}

	for i := 0; i < numTxsToRemove; i++ {
		removed := cs.Validators[i]
		removedID := removed.Unsigned.ID()
		delete(newCS.ValidatorsByTxID, removedID)

		switch tx := removed.Unsigned.(type) {
		case *unsigned.AddSubnetValidatorTx:
			oldVdr := newCS.ValidatorsByNodeID[tx.Validator.NodeID]
			newVdr := *oldVdr
			newVdr.subnets = make(map[ids.ID]*unsigned.AddSubnetValidatorTx, len(oldVdr.subnets)-1)
			for subnetID, addTx := range oldVdr.subnets {
				if removedID != addTx.ID() {
					newVdr.subnets[subnetID] = addTx
				}
			}
			newCS.ValidatorsByNodeID[tx.Validator.NodeID] = &newVdr
		default:
			return nil, unsigned.ErrWrongTxType
		}
	}

	newCS.SetNextStaker()
	return newCS, nil
}

func (cs *currentStaker) DeleteNextStaker() (CurrentStaker, error) {
	removedTx, _, err := cs.GetNextStaker()
	if err != nil {
		return nil, err
	}
	removedTxID := removedTx.Unsigned.ID()

	newCS := &currentStaker{
		ValidatorsByNodeID: make(map[ids.NodeID]*currentValidatorImpl, len(cs.ValidatorsByNodeID)),
		ValidatorsByTxID:   make(map[ids.ID]*ValidatorReward, len(cs.ValidatorsByTxID)-1),
		Validators:         cs.Validators[1:], // sorted in order of removal

		deletedStakers: []*signed.Tx{removedTx},
	}

	switch tx := removedTx.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		for nodeID, vdr := range cs.ValidatorsByNodeID {
			if nodeID != tx.Validator.NodeID {
				newCS.ValidatorsByNodeID[nodeID] = vdr
			}
		}
	case *unsigned.AddDelegatorTx:
		for nodeID, vdr := range cs.ValidatorsByNodeID {
			if nodeID != tx.Validator.NodeID {
				newCS.ValidatorsByNodeID[nodeID] = vdr
			} else {
				newCS.ValidatorsByNodeID[nodeID] = &currentValidatorImpl{
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
		return nil, unsigned.ErrWrongTxType
	}

	for txID, vdr := range cs.ValidatorsByTxID {
		if txID != removedTxID {
			newCS.ValidatorsByTxID[txID] = vdr
		}
	}

	newCS.SetNextStaker()
	return newCS, nil
}

func (cs *currentStaker) Stakers() []*signed.Tx {
	return cs.Validators
}

func (cs *currentStaker) Apply(bs Content) {
	for _, added := range cs.addedStakers {
		bs.AddCurrentStaker(added.AddStakerTx, added.PotentialReward)
	}
	for _, deleted := range cs.deletedStakers {
		bs.DeleteCurrentStaker(deleted)
	}
	bs.SetCurrentStakerChainState(cs)

	// Validator changes should only be applied once.
	cs.addedStakers = nil
	cs.deletedStakers = nil
}

func (cs *currentStaker) ValidatorSet(subnetID ids.ID) (validators.Set, error) {
	if subnetID == constants.PrimaryNetworkID {
		return cs.primaryValidatorSet()
	}
	return cs.subnetValidatorSet(subnetID)
}

func (cs *currentStaker) primaryValidatorSet() (validators.Set, error) {
	vdrs := validators.NewSet()

	var err error
	for nodeID, vdr := range cs.ValidatorsByNodeID {
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

func (cs *currentStaker) subnetValidatorSet(subnetID ids.ID) (validators.Set, error) {
	vdrs := validators.NewSet()

	for nodeID, vdr := range cs.ValidatorsByNodeID {
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

func (cs *currentStaker) GetStaker(txID ids.ID) (tx *signed.Tx, reward uint64, err error) {
	staker, exists := cs.ValidatorsByTxID[txID]
	if !exists {
		return nil, 0, database.ErrNotFound
	}
	return staker.AddStakerTx, staker.PotentialReward, nil
}

// SetNextStaker to the next staker that will be removed using a
// RewardValidatorTx.
func (cs *currentStaker) SetNextStaker() {
	for _, tx := range cs.Validators {
		switch tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx, *unsigned.AddDelegatorTx:
			cs.nextStaker = cs.ValidatorsByTxID[tx.Unsigned.ID()]
			return
		}
	}
}

type innerSortValidatorsByRemoval []*signed.Tx

func (s innerSortValidatorsByRemoval) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	var (
		iEndTime  time.Time
		iPriority byte
	)
	switch tx := iDel.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		iEndTime = tx.EndTime()
		iPriority = lowPriority
	case *unsigned.AddDelegatorTx:
		iEndTime = tx.EndTime()
		iPriority = mediumPriority
	case *unsigned.AddSubnetValidatorTx:
		iEndTime = tx.EndTime()
		iPriority = topPriority
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", iDel.Unsigned))
	}

	var (
		jEndTime  time.Time
		jPriority byte
	)
	switch tx := jDel.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		jEndTime = tx.EndTime()
		jPriority = lowPriority
	case *unsigned.AddDelegatorTx:
		jEndTime = tx.EndTime()
		jPriority = mediumPriority
	case *unsigned.AddSubnetValidatorTx:
		jEndTime = tx.EndTime()
		jPriority = topPriority
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", jDel.Unsigned))
	}

	if iEndTime.Before(jEndTime) {
		return true
	}
	if jEndTime.Before(iEndTime) {
		return false
	}

	// If the end times are the same, then we sort by the tx type. First we
	// remove UnsignedAddSubnetValidatorTxs, then UnsignedAddDelegatorTx, then
	// unsigned.AddValidatorTx.
	if iPriority > jPriority {
		return true
	}
	if iPriority < jPriority {
		return false
	}

	// If the end times are the same, and the tx types are the same, then we
	// sort by the txID.
	iTxID := iDel.Unsigned.ID()
	jTxID := jDel.Unsigned.ID()
	return bytes.Compare(iTxID[:], jTxID[:]) == -1
}

func (s innerSortValidatorsByRemoval) Len() int {
	return len(s)
}

func (s innerSortValidatorsByRemoval) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func SortValidatorsByRemoval(s []*signed.Tx) {
	sort.Sort(innerSortValidatorsByRemoval(s))
}

type innerSortDelegatorsByRemoval []*unsigned.AddDelegatorTx

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

func SortDelegatorsByRemoval(s []*unsigned.AddDelegatorTx) {
	sort.Sort(innerSortDelegatorsByRemoval(s))
}
