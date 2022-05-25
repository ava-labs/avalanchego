// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ PendingStakerState = &pendingStakerState{}

// PendingStakerState manages the set of stakers (both validators and
// delegators) that are slated to start staking in the future.
type PendingStakerState interface {
	GetValidatorTx(nodeID ids.NodeID) (addStakerTx *unsigned.AddValidatorTx, txID ids.ID, err error)
	GetValidator(nodeID ids.NodeID) validator

	AddStaker(addStakerTx *signed.Tx) PendingStakerState
	DeleteStakers(numToRemove int) PendingStakerState

	// Stakers returns the list of pending validators in order of their removal
	// from the pending staker set
	Stakers() []*signed.Tx

	Apply(Content)
}

// pendingStakerState is a copy on write implementation for versioning
// the validator set. None of the slices, maps, or pointers should be modified
// after initialization.
type pendingStakerState struct {
	// nodeID -> validator
	validatorsByNodeID      map[ids.NodeID]signed.ValidatorAndID
	validatorExtrasByNodeID map[ids.NodeID]*validatorImpl

	// list of pending validators in order of their removal from the pending
	// staker set
	validators []*signed.Tx

	addedStakers   []*signed.Tx
	deletedStakers []*signed.Tx
}

func (ps *pendingStakerState) GetValidatorTx(nodeID ids.NodeID) (
	addStakerTx *unsigned.AddValidatorTx,
	txID ids.ID,
	err error,
) {
	vdr, exists := ps.validatorsByNodeID[nodeID]
	if !exists {
		return nil, ids.Empty, database.ErrNotFound
	}
	return vdr.UnsignedAddValidatorTx, vdr.TxID, nil
}

func (ps *pendingStakerState) GetValidator(nodeID ids.NodeID) validator {
	if vdr, exists := ps.validatorExtrasByNodeID[nodeID]; exists {
		return vdr
	}
	return &validatorImpl{}
}

func (ps *pendingStakerState) AddStaker(addStakerTx *signed.Tx) PendingStakerState {
	newPS := &pendingStakerState{
		validators:   make([]*signed.Tx, len(ps.validators)+1),
		addedStakers: []*signed.Tx{addStakerTx},
	}
	copy(newPS.validators, ps.validators)
	newPS.validators[len(ps.validators)] = addStakerTx
	sortValidatorsByAddition(newPS.validators)

	txID := addStakerTx.ID()
	switch tx := addStakerTx.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		newPS.validatorExtrasByNodeID = ps.validatorExtrasByNodeID

		newPS.validatorsByNodeID = make(map[ids.NodeID]signed.ValidatorAndID, len(ps.validatorsByNodeID)+1)
		for nodeID, vdr := range ps.validatorsByNodeID {
			newPS.validatorsByNodeID[nodeID] = vdr
		}
		newPS.validatorsByNodeID[tx.Validator.NodeID] = signed.ValidatorAndID{
			UnsignedAddValidatorTx: tx,
			TxID:                   txID,
		}
	case *unsigned.AddDelegatorTx:
		newPS.validatorsByNodeID = ps.validatorsByNodeID

		newPS.validatorExtrasByNodeID = make(map[ids.NodeID]*validatorImpl, len(ps.validatorExtrasByNodeID)+1)
		for nodeID, vdr := range ps.validatorExtrasByNodeID {
			if nodeID != tx.Validator.NodeID {
				newPS.validatorExtrasByNodeID[nodeID] = vdr
			}
		}
		if vdr, exists := ps.validatorExtrasByNodeID[tx.Validator.NodeID]; exists {
			newDelegators := make([]signed.DelegatorAndID, len(vdr.delegators)+1)
			copy(newDelegators, vdr.delegators)
			newDelegators[len(vdr.delegators)] = signed.DelegatorAndID{
				UnsignedAddDelegatorTx: tx,
				TxID:                   txID,
			}
			sortDelegatorsByAddition(newDelegators)

			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: newDelegators,
				subnets:    vdr.subnets,
			}
		} else {
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: []signed.DelegatorAndID{
					{
						UnsignedAddDelegatorTx: tx,
						TxID:                   txID,
					},
				},
			}
		}
	case *unsigned.AddSubnetValidatorTx:
		newPS.validatorsByNodeID = ps.validatorsByNodeID

		newPS.validatorExtrasByNodeID = make(map[ids.NodeID]*validatorImpl, len(ps.validatorExtrasByNodeID)+1)
		for nodeID, vdr := range ps.validatorExtrasByNodeID {
			if nodeID != tx.Validator.NodeID {
				newPS.validatorExtrasByNodeID[nodeID] = vdr
			}
		}
		if vdr, exists := ps.validatorExtrasByNodeID[tx.Validator.NodeID]; exists {
			newSubnets := make(map[ids.ID]signed.SubnetValidatorAndID, len(vdr.subnets)+1)
			for subnet, subnetTx := range vdr.subnets {
				newSubnets[subnet] = subnetTx
			}
			newSubnets[tx.Validator.Subnet] = signed.SubnetValidatorAndID{
				UnsignedAddSubnetValidator: tx,
				TxID:                       txID,
			}
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: vdr.delegators,
				subnets:    newSubnets,
			}
		} else {
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				subnets: map[ids.ID]signed.SubnetValidatorAndID{
					tx.Validator.Subnet: {
						UnsignedAddSubnetValidator: tx,
						TxID:                       txID,
					},
				},
			}
		}
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", addStakerTx.Unsigned))
	}

	return newPS
}

func (ps *pendingStakerState) DeleteStakers(numToRemove int) PendingStakerState {
	newPS := &pendingStakerState{
		validatorsByNodeID:      make(map[ids.NodeID]signed.ValidatorAndID, len(ps.validatorsByNodeID)),
		validatorExtrasByNodeID: make(map[ids.NodeID]*validatorImpl, len(ps.validatorExtrasByNodeID)),
		validators:              ps.validators[numToRemove:],

		deletedStakers: ps.validators[:numToRemove],
	}

	for nodeID, vdr := range ps.validatorsByNodeID {
		newPS.validatorsByNodeID[nodeID] = vdr
	}
	for nodeID, vdr := range ps.validatorExtrasByNodeID {
		newPS.validatorExtrasByNodeID[nodeID] = vdr
	}

	for _, removedTx := range ps.validators[:numToRemove] {
		switch tx := removedTx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			delete(newPS.validatorsByNodeID, tx.Validator.NodeID)
		case *unsigned.AddDelegatorTx:
			vdr := newPS.validatorExtrasByNodeID[tx.Validator.NodeID]
			if len(vdr.delegators) == 1 && len(vdr.subnets) == 0 {
				delete(newPS.validatorExtrasByNodeID, tx.Validator.NodeID)
				break
			}
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: vdr.delegators[1:], // sorted in order of removal
				subnets:    vdr.subnets,
			}
		case *unsigned.AddSubnetValidatorTx:
			vdr := newPS.validatorExtrasByNodeID[tx.Validator.NodeID]
			if len(vdr.delegators) == 0 && len(vdr.subnets) == 1 {
				delete(newPS.validatorExtrasByNodeID, tx.Validator.NodeID)
				break
			}
			newSubnets := make(map[ids.ID]signed.SubnetValidatorAndID, len(vdr.subnets)-1)
			for subnetID, subnetTx := range vdr.subnets {
				if subnetID != tx.Validator.Subnet {
					newSubnets[subnetID] = subnetTx
				}
			}
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: vdr.delegators,
				subnets:    newSubnets,
			}
		default:
			panic(fmt.Errorf("expected staker tx type but got %T", removedTx.Unsigned))
		}
	}

	return newPS
}

func (ps *pendingStakerState) Stakers() []*signed.Tx {
	return ps.validators
}

func (ps *pendingStakerState) Apply(bs Content) {
	for _, added := range ps.addedStakers {
		bs.AddPendingStaker(added)
	}
	for _, deleted := range ps.deletedStakers {
		bs.DeletePendingStaker(deleted)
	}
	bs.SetPendingStakerChainState(ps)

	// Validator changes should only be applied once.
	ps.addedStakers = nil
	ps.deletedStakers = nil
}

type innerSortValidatorsByAddition []*signed.Tx

func (s innerSortValidatorsByAddition) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	var (
		iStartTime time.Time
		iPriority  byte
	)
	switch tx := iDel.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		iStartTime = tx.StartTime()
		iPriority = mediumPriority
	case *unsigned.AddDelegatorTx:
		iStartTime = tx.StartTime()
		iPriority = topPriority
	case *unsigned.AddSubnetValidatorTx:
		iStartTime = tx.StartTime()
		iPriority = lowPriority
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", iDel.Unsigned))
	}

	var (
		jStartTime time.Time
		jPriority  byte
	)
	switch tx := jDel.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		jStartTime = tx.StartTime()
		jPriority = mediumPriority
	case *unsigned.AddDelegatorTx:
		jStartTime = tx.StartTime()
		jPriority = topPriority
	case *unsigned.AddSubnetValidatorTx:
		jStartTime = tx.StartTime()
		jPriority = lowPriority
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", jDel.Unsigned))
	}

	if iStartTime.Before(jStartTime) {
		return true
	}
	if jStartTime.Before(iStartTime) {
		return false
	}

	// If the end times are the same, then we sort by the tx type. First we
	// add unsigned.AddValidatorTx, then unsigned.AddDelegatorTx, then
	// unsigned.AddSubnetValidatorTxs.
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

func (s innerSortValidatorsByAddition) Len() int {
	return len(s)
}

func (s innerSortValidatorsByAddition) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func sortValidatorsByAddition(s []*signed.Tx) {
	sort.Sort(innerSortValidatorsByAddition(s))
}

type innerSortDelegatorsByAddition []signed.DelegatorAndID

func (s innerSortDelegatorsByAddition) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	iStartTime := iDel.UnsignedAddDelegatorTx.StartTime()
	jStartTime := jDel.UnsignedAddDelegatorTx.StartTime()
	if iStartTime.Before(jStartTime) {
		return true
	}
	if jStartTime.Before(iStartTime) {
		return false
	}

	// If the end times are the same, then we sort by the txID
	iTxID := iDel.TxID
	jTxID := jDel.TxID
	return bytes.Compare(iTxID[:], jTxID[:]) == -1
}

func (s innerSortDelegatorsByAddition) Len() int {
	return len(s)
}

func (s innerSortDelegatorsByAddition) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func sortDelegatorsByAddition(s []signed.DelegatorAndID) {
	sort.Sort(innerSortDelegatorsByAddition(s))
}
