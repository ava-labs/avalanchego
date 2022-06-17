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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ CurrentStakerState = &currentStakerState{}

	ErrNotEnoughValidators = errors.New("not enough validators")
)

type CurrentStakerState interface {
	// The NextStaker value returns the next staker that is going to be removed
	// using a RewardValidatorTx. Therefore, only AddValidatorTxs and
	// AddDelegatorTxs will be returned. AddSubnetValidatorTxs are removed using
	// AdvanceTimestampTxs.
	GetNextStaker() (addStakerTx *txs.Tx, potentialReward uint64, err error)
	GetStaker(txID ids.ID) (tx *txs.Tx, potentialReward uint64, err error)
	GetValidator(nodeID ids.NodeID) (currentValidator, error)

	UpdateStakers(
		addValidators []*ValidatorReward,
		addDelegators []*ValidatorReward,
		addSubnetValidators []*txs.Tx,
		numTxsToRemove int,
	) (CurrentStakerState, error)
	DeleteNextStaker() (CurrentStakerState, error)

	// Stakers returns the current stakers on the network sorted in order of the
	// order of their future removal from the validator set.
	Stakers() []*txs.Tx

	Apply(Content)

	// Return the current validator set of [subnetID].
	ValidatorSet(subnetID ids.ID) (validators.Set, error)
}

// currentStakerState is a copy on write implementation for versioning
// the validator set. None of the slices, maps, or pointers should be modified
// after initialization.
type currentStakerState struct {
	// nodeID -> validator
	validatorsByNodeID map[ids.NodeID]*currentValidatorImpl

	// txID -> tx
	validatorsByTxID map[ids.ID]*ValidatorReward

	// list of current validators in order of their removal from the validator
	// set
	validators []*txs.Tx

	nextStaker     *ValidatorReward
	addedStakers   []*ValidatorReward
	deletedStakers []*txs.Tx
}

type ValidatorReward struct {
	AddStakerTx     *txs.Tx
	PotentialReward uint64
}

func (cs *currentStakerState) GetNextStaker() (addStakerTx *txs.Tx, potentialReward uint64, err error) {
	if cs.nextStaker == nil {
		return nil, 0, database.ErrNotFound
	}
	return cs.nextStaker.AddStakerTx, cs.nextStaker.PotentialReward, nil
}

func (cs *currentStakerState) GetValidator(nodeID ids.NodeID) (currentValidator, error) {
	vdr, exists := cs.validatorsByNodeID[nodeID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return vdr, nil
}

func (cs *currentStakerState) UpdateStakers(
	addValidatorTxs []*ValidatorReward,
	addDelegatorTxs []*ValidatorReward,
	addSubnetValidatorTxs []*txs.Tx,
	numTxsToRemove int,
) (CurrentStakerState, error) {
	if numTxsToRemove > len(cs.validators) {
		return nil, ErrNotEnoughValidators
	}
	newCS := &currentStakerState{
		validatorsByNodeID: make(map[ids.NodeID]*currentValidatorImpl, len(cs.validatorsByNodeID)+len(addValidatorTxs)),
		validatorsByTxID:   make(map[ids.ID]*ValidatorReward, len(cs.validatorsByTxID)+len(addValidatorTxs)+len(addDelegatorTxs)+len(addSubnetValidatorTxs)),
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
		newValidators := make([]*txs.Tx, newSize)
		copy(newValidators, newCS.validators)
		copy(newValidators[numCurrent:], addSubnetValidatorTxs)

		numStart := numCurrent + len(addSubnetValidatorTxs)
		for i, tx := range addValidatorTxs {
			newValidators[numStart+i] = tx.AddStakerTx
		}

		numStart = numCurrent + len(addSubnetValidatorTxs) + len(addValidatorTxs)
		for i, tx := range addDelegatorTxs {
			newValidators[numStart+i] = tx.AddStakerTx
		}

		sortValidatorsByRemoval(newValidators)
		newCS.validators = newValidators

		for _, vdr := range addValidatorTxs {
			switch tx := vdr.AddStakerTx.Unsigned.(type) {
			case *txs.AddValidatorTx:
				txID := vdr.AddStakerTx.ID()
				newCS.validatorsByNodeID[tx.Validator.NodeID] = &currentValidatorImpl{
					addValidator: ValidatorAndID{
						Tx:   tx,
						TxID: txID,
					},
					potentialReward: vdr.PotentialReward,
				}
				newCS.validatorsByTxID[vdr.AddStakerTx.ID()] = vdr
			default:
				return nil, fmt.Errorf("expected tx type *unsigned.AddValidatorTx but got %T", tx)
			}
		}

		for _, vdr := range addDelegatorTxs {
			switch tx := vdr.AddStakerTx.Unsigned.(type) {
			case *txs.AddDelegatorTx:
				txID := vdr.AddStakerTx.ID()
				oldVdr := newCS.validatorsByNodeID[tx.Validator.NodeID]
				newVdr := *oldVdr
				newVdr.delegators = make([]DelegatorAndID, len(oldVdr.delegators)+1)
				copy(newVdr.delegators, oldVdr.delegators)
				newVdr.delegators[len(oldVdr.delegators)] = DelegatorAndID{
					Tx:   tx,
					TxID: txID,
				}
				sortDelegatorsByRemoval(newVdr.delegators)
				newVdr.delegatorWeight += tx.Validator.Wght
				newCS.validatorsByNodeID[tx.Validator.NodeID] = &newVdr
				newCS.validatorsByTxID[txID] = vdr
			default:
				return nil, fmt.Errorf("expected tx type *unsigned.AddDelegatorTx but got %T", tx)
			}
		}

		for _, vdr := range addSubnetValidatorTxs {
			switch tx := vdr.Unsigned.(type) {
			case *txs.AddSubnetValidatorTx:
				txID := vdr.ID()
				oldVdr := newCS.validatorsByNodeID[tx.Validator.NodeID]
				newVdr := *oldVdr
				newVdr.subnets = make(map[ids.ID]SubnetValidatorAndID, len(oldVdr.subnets)+1)
				for subnetID, addTx := range oldVdr.subnets {
					newVdr.subnets[subnetID] = addTx
				}
				newVdr.subnets[tx.Validator.Subnet] = SubnetValidatorAndID{
					Tx:   tx,
					TxID: txID,
				}
				newCS.validatorsByNodeID[tx.Validator.NodeID] = &newVdr
			default:
				return nil, fmt.Errorf("expected tx type *unsigned.AddSubnetValidatorTx but got %T", tx)
			}

			wrappedTx := &ValidatorReward{
				AddStakerTx: vdr,
			}
			newCS.validatorsByTxID[vdr.ID()] = wrappedTx
			newCS.addedStakers = append(newCS.addedStakers, wrappedTx)
		}
	}

	for i := 0; i < numTxsToRemove; i++ {
		removed := cs.validators[i]
		removedID := removed.ID()
		delete(newCS.validatorsByTxID, removedID)

		switch tx := removed.Unsigned.(type) {
		case *txs.AddSubnetValidatorTx:
			oldVdr := newCS.validatorsByNodeID[tx.Validator.NodeID]
			newVdr := *oldVdr
			newVdr.subnets = make(map[ids.ID]SubnetValidatorAndID, len(oldVdr.subnets)-1)
			for subnetID, addTx := range oldVdr.subnets {
				if removedID != addTx.TxID {
					newVdr.subnets[subnetID] = addTx
				}
			}
			newCS.validatorsByNodeID[tx.Validator.NodeID] = &newVdr
		default:
			return nil, fmt.Errorf("expected tx type *unsigned.AddSubnetValidatorTx but got %T", tx)
		}
	}

	newCS.SetNextStaker()
	return newCS, nil
}

func (cs *currentStakerState) DeleteNextStaker() (CurrentStakerState, error) {
	removedTx, _, err := cs.GetNextStaker()
	if err != nil {
		return nil, err
	}
	removedTxID := removedTx.ID()

	newCS := &currentStakerState{
		validatorsByNodeID: make(map[ids.NodeID]*currentValidatorImpl, len(cs.validatorsByNodeID)),
		validatorsByTxID:   make(map[ids.ID]*ValidatorReward, len(cs.validatorsByTxID)-1),
		validators:         cs.validators[1:], // sorted in order of removal

		deletedStakers: []*txs.Tx{removedTx},
	}

	switch tx := removedTx.Unsigned.(type) {
	case *txs.AddValidatorTx:
		for nodeID, vdr := range cs.validatorsByNodeID {
			if nodeID != tx.Validator.NodeID {
				newCS.validatorsByNodeID[nodeID] = vdr
			}
		}
	case *txs.AddDelegatorTx:
		for nodeID, vdr := range cs.validatorsByNodeID {
			if nodeID != tx.Validator.NodeID {
				newCS.validatorsByNodeID[nodeID] = vdr
			} else {
				newCS.validatorsByNodeID[nodeID] = &currentValidatorImpl{
					validatorImpl: validatorImpl{
						delegators: vdr.delegators[1:], // sorted in order of removal
						subnets:    vdr.subnets,
					},
					addValidator: ValidatorAndID{
						Tx:   vdr.addValidator.Tx,
						TxID: vdr.addValidator.TxID,
					},
					delegatorWeight: vdr.delegatorWeight - tx.Validator.Wght,
					potentialReward: vdr.potentialReward,
				}
			}
		}
	default:
		return nil, fmt.Errorf("expected tx type *unsigned.AddValidatorTx or *unsigned.AddDelegatorTx but got %T", removedTx.Unsigned)
	}

	for txID, vdr := range cs.validatorsByTxID {
		if txID != removedTxID {
			newCS.validatorsByTxID[txID] = vdr
		}
	}

	newCS.SetNextStaker()
	return newCS, nil
}

func (cs *currentStakerState) Stakers() []*txs.Tx {
	return cs.validators
}

func (cs *currentStakerState) Apply(c Content) {
	for _, added := range cs.addedStakers {
		c.AddCurrentStaker(added.AddStakerTx, added.PotentialReward)
	}
	for _, deleted := range cs.deletedStakers {
		c.DeleteCurrentStaker(deleted)
	}
	c.SetCurrentStakerChainState(cs)

	// Validator changes should only be applied once.
	cs.addedStakers = nil
	cs.deletedStakers = nil
}

func (cs *currentStakerState) ValidatorSet(subnetID ids.ID) (validators.Set, error) {
	if subnetID == constants.PrimaryNetworkID {
		return cs.primaryValidatorSet()
	}
	return cs.subnetValidatorSet(subnetID)
}

func (cs *currentStakerState) primaryValidatorSet() (validators.Set, error) {
	vdrs := validators.NewSet()

	var err error
	for nodeID, vdr := range cs.validatorsByNodeID {
		vdrWeight := vdr.addValidator.Tx.Validator.Wght
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

func (cs *currentStakerState) subnetValidatorSet(subnetID ids.ID) (validators.Set, error) {
	vdrs := validators.NewSet()

	for nodeID, vdr := range cs.validatorsByNodeID {
		subnetVDR, exists := vdr.subnets[subnetID]
		if !exists {
			continue
		}
		if err := vdrs.AddWeight(nodeID, subnetVDR.Tx.Validator.Wght); err != nil {
			return nil, err
		}
	}

	return vdrs, nil
}

func (cs *currentStakerState) GetStaker(txID ids.ID) (tx *txs.Tx, reward uint64, err error) {
	staker, exists := cs.validatorsByTxID[txID]
	if !exists {
		return nil, 0, database.ErrNotFound
	}
	return staker.AddStakerTx, staker.PotentialReward, nil
}

// SetNextStaker to the next staker that will be removed using a
// RewardValidatorTx.
func (cs *currentStakerState) SetNextStaker() {
	for _, tx := range cs.validators {
		switch tx.Unsigned.(type) {
		case *txs.AddValidatorTx, *txs.AddDelegatorTx:
			cs.nextStaker = cs.validatorsByTxID[tx.ID()]
			return
		}
	}
}

type innerSortValidatorsByRemoval []*txs.Tx

func (s innerSortValidatorsByRemoval) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	var (
		iEndTime  time.Time
		iPriority byte
	)
	switch tx := iDel.Unsigned.(type) {
	case *txs.AddValidatorTx:
		iEndTime = tx.EndTime()
		iPriority = lowPriority
	case *txs.AddDelegatorTx:
		iEndTime = tx.EndTime()
		iPriority = mediumPriority
	case *txs.AddSubnetValidatorTx:
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
	case *txs.AddValidatorTx:
		jEndTime = tx.EndTime()
		jPriority = lowPriority
	case *txs.AddDelegatorTx:
		jEndTime = tx.EndTime()
		jPriority = mediumPriority
	case *txs.AddSubnetValidatorTx:
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
	// remove txs.AddSubnetValidatorTxs, then txs.AddDelegatorTx, then
	// txs.AddValidatorTx.
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

func sortValidatorsByRemoval(s []*txs.Tx) {
	sort.Sort(innerSortValidatorsByRemoval(s))
}

type innerSortDelegatorsByRemoval []DelegatorAndID

func (s innerSortDelegatorsByRemoval) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	iEndTime := iDel.Tx.EndTime()
	jEndTime := jDel.Tx.EndTime()
	if iEndTime.Before(jEndTime) {
		return true
	}
	if jEndTime.Before(iEndTime) {
		return false
	}

	// If the end times are the same, then we sort by the txID
	iTxID := iDel.TxID
	jTxID := jDel.TxID
	return bytes.Compare(iTxID[:], jTxID[:]) == -1
}

func (s innerSortDelegatorsByRemoval) Len() int {
	return len(s)
}

func (s innerSortDelegatorsByRemoval) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func sortDelegatorsByRemoval(s []DelegatorAndID) {
	sort.Sort(innerSortDelegatorsByRemoval(s))
}
