// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ currentStakerChainState = &currentStakerChainStateImpl{}
)

type currentStakerChainState interface {
	GetNextStaker() (addStakerTx *Tx, potentialReward uint64, err error)
	GetValidator(nodeID ids.ShortID) (currentValidator, error)

	UpdateStakers(
		addStakerTxsWithRewards []*validatorReward,
		addStakerTxsWithoutRewards []*Tx,
		numTxsToRemove int,
	) (currentStakerChainState, error)
	DeleteNextStaker() (currentStakerChainState, error)

	Stakers() []*Tx // Sorted in removal order

	Apply(internalState) error
}

type currentStakerChainStateImpl struct {
	nextStaker *validatorReward

	// nodeID -> validator
	validatorsByNodeID map[ids.ShortID]*currentValidatorImpl

	// txID -> tx
	validatorsByTxID map[ids.ID]*validatorReward

	// list of current validators in order of their removal
	validators []*Tx
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
	addStakerTxsWithRewards []*validatorReward,
	addStakerTxsWithoutRewards []*Tx,
	numTxsToRemove int,
) (currentStakerChainState, error) {
	newCS := &currentStakerChainStateImpl{
		validatorsByNodeID: make(map[ids.ShortID]*currentValidatorImpl, len(cs.validatorsByNodeID)),
		validatorsByTxID:   make(map[ids.ID]*validatorReward, len(cs.validatorsByTxID)+len(addStakerTxsWithRewards)+len(addStakerTxsWithRewards)),
		validators:         cs.validators[numTxsToRemove:], // sorted in order of removal
	}

	for nodeID, vdr := range cs.validatorsByNodeID {
		newCS.validatorsByNodeID[nodeID] = vdr
	}

	for txID, vdr := range cs.validatorsByTxID {
		newCS.validatorsByTxID[txID] = vdr
	}

	if numAdded := len(addStakerTxsWithRewards) + len(addStakerTxsWithoutRewards); numAdded != 0 {
		numCurrent := len(newCS.validators)
		newSize := numCurrent + numAdded
		newValidators := make([]*Tx, newSize)
		copy(newValidators, newCS.validators)
		copy(newValidators[numCurrent:], addStakerTxsWithoutRewards)

		numStart := numCurrent + len(addStakerTxsWithoutRewards)
		for i := 0; i < len(addStakerTxsWithRewards); i++ {
			newValidators[numStart+i] = addStakerTxsWithRewards[i].addStakerTx
		}
		sortValidatorsByRemoval(newValidators)
		newCS.validators = newValidators

		for _, vdr := range addStakerTxsWithRewards {
			switch tx := vdr.addStakerTx.UnsignedTx.(type) {
			case *UnsignedAddValidatorTx:
				newCS.validatorsByNodeID[tx.Validator.NodeID] = &currentValidatorImpl{
					addValidatorTx:  tx,
					potentialReward: vdr.potentialReward,
				}
			case *UnsignedAddDelegatorTx:
				oldVdr := newCS.validatorsByNodeID[tx.Validator.NodeID]
				newVdr := *oldVdr
				newVdr.delegators = make([]*UnsignedAddDelegatorTx, len(oldVdr.delegators)+1)
				copy(newVdr.delegators, oldVdr.delegators)
				newVdr.delegators[len(oldVdr.delegators)] = tx
				sortDelegatorsByRemoval(newVdr.delegators)
				newCS.validatorsByNodeID[tx.Validator.NodeID] = &newVdr
			default:
				return nil, errWrongTxType
			}
			newCS.validatorsByTxID[vdr.addStakerTx.ID()] = vdr
		}
		for _, vdr := range addStakerTxsWithoutRewards {
			switch tx := vdr.UnsignedTx.(type) {
			case *UnsignedAddSubnetValidatorTx:
				oldVdr := newCS.validatorsByNodeID[tx.Validator.NodeID]
				newVdr := *oldVdr
				newVdr.subnets = make(map[ids.ID]*UnsignedAddSubnetValidatorTx, len(oldVdr.subnets)-1)
				for subnetID, addTx := range oldVdr.subnets {
					newVdr.subnets[subnetID] = addTx
				}
				newVdr.subnets[tx.Validator.Subnet] = tx
			default:
				return nil, errWrongTxType
			}
			newCS.validatorsByTxID[vdr.ID()] = &validatorReward{
				addStakerTx: vdr,
			}
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

	for _, tx := range newCS.validators {
		switch tx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx, *UnsignedAddDelegatorTx:
			newCS.nextStaker = newCS.validatorsByTxID[tx.ID()]
			return newCS, nil
		}
	}
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

	for _, tx := range newCS.validators {
		switch tx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx, *UnsignedAddDelegatorTx:
			newCS.nextStaker = newCS.validatorsByTxID[tx.ID()]
			return newCS, nil
		}
	}
	return newCS, nil
}

func (cs *currentStakerChainStateImpl) Stakers() []*Tx {
	return cs.validators
}

func (cs *currentStakerChainStateImpl) Apply(is internalState) error {
	return cs.validators
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
		panic(errWrongTxType)
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
		panic(errWrongTxType)
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
