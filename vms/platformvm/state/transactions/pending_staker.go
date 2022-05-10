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

var _ PendingStaker = &pendingStaker{}

// PendingStaker manages the set of stakers (both validators and
// delegators) that are slated to start staking in the future.
type PendingStaker interface {
	GetValidatorTx(nodeID ids.NodeID) (addStakerTx *unsigned.AddValidatorTx, err error)
	GetValidator(nodeID ids.NodeID) validatorCache

	AddStaker(addStakerTx *signed.Tx) PendingStaker
	DeleteStakers(numToRemove int) PendingStaker

	// Stakers returns the list of pending validators in order of their removal
	// from the pending staker set
	Stakers() []*signed.Tx

	Apply(Content)
}

// pendingStaker is a copy on write implementation for versioning
// the validator set. None of the slices, maps, or pointers should be modified
// after initialization.
type pendingStaker struct {
	// nodeID -> validator
	validatorsByNodeID      map[ids.NodeID]*unsigned.AddValidatorTx
	validatorExtrasByNodeID map[ids.NodeID]*validatorImpl

	// list of pending validators in order of their removal from the pending
	// staker set
	validators []*signed.Tx

	addedStakers   []*signed.Tx
	deletedStakers []*signed.Tx
}

func (ps *pendingStaker) GetValidatorTx(nodeID ids.NodeID) (addStakerTx *unsigned.AddValidatorTx, err error) {
	vdr, exists := ps.validatorsByNodeID[nodeID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return vdr, nil
}

func (ps *pendingStaker) GetValidator(nodeID ids.NodeID) validatorCache {
	if vdr, exists := ps.validatorExtrasByNodeID[nodeID]; exists {
		return vdr
	}
	return &validatorImpl{}
}

func (ps *pendingStaker) AddStaker(addStakerTx *signed.Tx) PendingStaker {
	newPS := &pendingStaker{
		validators:   make([]*signed.Tx, len(ps.validators)+1),
		addedStakers: []*signed.Tx{addStakerTx},
	}
	copy(newPS.validators, ps.validators)
	newPS.validators[len(ps.validators)] = addStakerTx
	sortValidatorsByAddition(newPS.validators)

	switch tx := addStakerTx.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		newPS.validatorExtrasByNodeID = ps.validatorExtrasByNodeID

		newPS.validatorsByNodeID = make(map[ids.NodeID]*unsigned.AddValidatorTx, len(ps.validatorsByNodeID)+1)
		for nodeID, vdr := range ps.validatorsByNodeID {
			newPS.validatorsByNodeID[nodeID] = vdr
		}
		newPS.validatorsByNodeID[tx.Validator.NodeID] = tx
	case *unsigned.AddDelegatorTx:
		newPS.validatorsByNodeID = ps.validatorsByNodeID

		newPS.validatorExtrasByNodeID = make(map[ids.NodeID]*validatorImpl, len(ps.validatorExtrasByNodeID)+1)
		for nodeID, vdr := range ps.validatorExtrasByNodeID {
			if nodeID != tx.Validator.NodeID {
				newPS.validatorExtrasByNodeID[nodeID] = vdr
			}
		}
		if vdr, exists := ps.validatorExtrasByNodeID[tx.Validator.NodeID]; exists {
			newDelegators := make([]*unsigned.AddDelegatorTx, len(vdr.delegators)+1)
			copy(newDelegators, vdr.delegators)
			newDelegators[len(vdr.delegators)] = tx
			sortDelegatorsByAddition(newDelegators)

			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: newDelegators,
				subnets:    vdr.subnets,
			}
		} else {
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: []*unsigned.AddDelegatorTx{
					tx,
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
			newSubnets := make(map[ids.ID]*unsigned.AddSubnetValidatorTx, len(vdr.subnets)+1)
			for subnet, subnetTx := range vdr.subnets {
				newSubnets[subnet] = subnetTx
			}
			newSubnets[tx.Validator.Subnet] = tx

			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: vdr.delegators,
				subnets:    newSubnets,
			}
		} else {
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				subnets: map[ids.ID]*unsigned.AddSubnetValidatorTx{
					tx.Validator.Subnet: tx,
				},
			}
		}
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", addStakerTx.Unsigned))
	}

	return newPS
}

func (ps *pendingStaker) DeleteStakers(numToRemove int) PendingStaker {
	newPS := &pendingStaker{
		validatorsByNodeID:      make(map[ids.NodeID]*unsigned.AddValidatorTx, len(ps.validatorsByNodeID)),
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
			newSubnets := make(map[ids.ID]*unsigned.AddSubnetValidatorTx, len(vdr.subnets)-1)
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

func (ps *pendingStaker) Stakers() []*signed.Tx {
	return ps.validators
}

func (ps *pendingStaker) Apply(bs Content) {
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
	// add unsigned.AddValidatorTx, then UnsignedAddDelegatorTx, then
	// UnsignedAddSubnetValidatorTxs.
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

func (s innerSortValidatorsByAddition) Len() int {
	return len(s)
}

func (s innerSortValidatorsByAddition) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func sortValidatorsByAddition(s []*signed.Tx) {
	sort.Sort(innerSortValidatorsByAddition(s))
}

type innerSortDelegatorsByAddition []*unsigned.AddDelegatorTx

func (s innerSortDelegatorsByAddition) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	iStartTime := iDel.StartTime()
	jStartTime := jDel.StartTime()
	if iStartTime.Before(jStartTime) {
		return true
	}
	if jStartTime.Before(iStartTime) {
		return false
	}

	// If the end times are the same, then we sort by the txID
	iTxID := iDel.ID()
	jTxID := jDel.ID()
	return bytes.Compare(iTxID[:], jTxID[:]) == -1
}

func (s innerSortDelegatorsByAddition) Len() int {
	return len(s)
}

func (s innerSortDelegatorsByAddition) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func sortDelegatorsByAddition(s []*unsigned.AddDelegatorTx) {
	sort.Sort(innerSortDelegatorsByAddition(s))
}
