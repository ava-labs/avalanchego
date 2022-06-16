// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ pendingStakerChainState = &pendingStakerChainStateImpl{}

// pendingStakerChainState manages the set of stakers (both validators and
// delegators) that are slated to start staking in the future.
type pendingStakerChainState interface {
	// return txs.AddValidatorTx content along with the ID of its signed.Tx
	GetValidatorTx(nodeID ids.NodeID) (addStakerTx *txs.AddValidatorTx, txID ids.ID, err error)
	GetValidator(nodeID ids.NodeID) validatorIntf

	AddStaker(addStakerTx *txs.Tx) pendingStakerChainState
	DeleteStakers(numToRemove int) pendingStakerChainState

	// Stakers returns the list of pending validators in order of their removal
	// from the pending staker set
	Stakers() []*txs.Tx

	Apply(InternalState)
}

// pendingStakerChainStateImpl is a copy on write implementation for versioning
// the validator set. None of the slices, maps, or pointers should be modified
// after initialization.
type pendingStakerChainStateImpl struct {
	// nodeID -> validator
	validatorsByNodeID      map[ids.NodeID]ValidatorAndID
	validatorExtrasByNodeID map[ids.NodeID]*validatorImpl

	// list of pending validators in order of their removal from the pending
	// staker set
	validators []*txs.Tx

	addedStakers   []*txs.Tx
	deletedStakers []*txs.Tx
}

func (ps *pendingStakerChainStateImpl) GetValidatorTx(nodeID ids.NodeID) (
	addStakerTx *txs.AddValidatorTx,
	txID ids.ID,
	err error,
) {
	vdr, exists := ps.validatorsByNodeID[nodeID]
	if !exists {
		return nil, ids.Empty, database.ErrNotFound
	}
	return vdr.Tx, vdr.TxID, nil
}

func (ps *pendingStakerChainStateImpl) GetValidator(nodeID ids.NodeID) validatorIntf {
	if vdr, exists := ps.validatorExtrasByNodeID[nodeID]; exists {
		return vdr
	}
	return &validatorImpl{}
}

func (ps *pendingStakerChainStateImpl) AddStaker(addStakerTx *txs.Tx) pendingStakerChainState {
	newPS := &pendingStakerChainStateImpl{
		validators:   make([]*txs.Tx, len(ps.validators)+1),
		addedStakers: []*txs.Tx{addStakerTx},
	}
	copy(newPS.validators, ps.validators)
	newPS.validators[len(ps.validators)] = addStakerTx
	sortValidatorsByAddition(newPS.validators)

	txID := addStakerTx.ID()
	switch tx := addStakerTx.Unsigned.(type) {
	case *txs.AddValidatorTx:
		newPS.validatorExtrasByNodeID = ps.validatorExtrasByNodeID

		newPS.validatorsByNodeID = make(map[ids.NodeID]ValidatorAndID, len(ps.validatorsByNodeID)+1)
		for nodeID, vdr := range ps.validatorsByNodeID {
			newPS.validatorsByNodeID[nodeID] = vdr
		}
		newPS.validatorsByNodeID[tx.Validator.NodeID] = ValidatorAndID{
			Tx:   tx,
			TxID: txID,
		}
	case *txs.AddDelegatorTx:
		newPS.validatorsByNodeID = ps.validatorsByNodeID

		newPS.validatorExtrasByNodeID = make(map[ids.NodeID]*validatorImpl, len(ps.validatorExtrasByNodeID)+1)
		for nodeID, vdr := range ps.validatorExtrasByNodeID {
			if nodeID != tx.Validator.NodeID {
				newPS.validatorExtrasByNodeID[nodeID] = vdr
			}
		}
		if vdr, exists := ps.validatorExtrasByNodeID[tx.Validator.NodeID]; exists {
			newDelegators := make([]DelegatorAndID, len(vdr.delegators)+1)
			copy(newDelegators, vdr.delegators)
			newDelegators[len(vdr.delegators)] = DelegatorAndID{
				Tx:   tx,
				TxID: txID,
			}
			sortDelegatorsByAddition(newDelegators)

			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: newDelegators,
				subnets:    vdr.subnets,
			}
		} else {
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: []DelegatorAndID{
					{
						Tx:   tx,
						TxID: txID,
					},
				},
			}
		}
	case *txs.AddSubnetValidatorTx:
		newPS.validatorsByNodeID = ps.validatorsByNodeID

		newPS.validatorExtrasByNodeID = make(map[ids.NodeID]*validatorImpl, len(ps.validatorExtrasByNodeID)+1)
		for nodeID, vdr := range ps.validatorExtrasByNodeID {
			if nodeID != tx.Validator.NodeID {
				newPS.validatorExtrasByNodeID[nodeID] = vdr
			}
		}
		if vdr, exists := ps.validatorExtrasByNodeID[tx.Validator.NodeID]; exists {
			newSubnets := make(map[ids.ID]SubnetValidatorAndID, len(vdr.subnets)+1)
			for subnet, subnetTx := range vdr.subnets {
				newSubnets[subnet] = subnetTx
			}
			newSubnets[tx.Validator.Subnet] = SubnetValidatorAndID{
				Tx:   tx,
				TxID: txID,
			}
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: vdr.delegators,
				subnets:    newSubnets,
			}
		} else {
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				subnets: map[ids.ID]SubnetValidatorAndID{
					tx.Validator.Subnet: {
						Tx:   tx,
						TxID: txID,
					},
				},
			}
		}
	default:
		panic(fmt.Errorf("expected staker tx type but got %T", addStakerTx.Unsigned))
	}

	return newPS
}

func (ps *pendingStakerChainStateImpl) DeleteStakers(numToRemove int) pendingStakerChainState {
	newPS := &pendingStakerChainStateImpl{
		validatorsByNodeID:      make(map[ids.NodeID]ValidatorAndID, len(ps.validatorsByNodeID)),
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
		case *txs.AddValidatorTx:
			delete(newPS.validatorsByNodeID, tx.Validator.NodeID)
		case *txs.AddDelegatorTx:
			vdr := newPS.validatorExtrasByNodeID[tx.Validator.NodeID]
			if len(vdr.delegators) == 1 && len(vdr.subnets) == 0 {
				delete(newPS.validatorExtrasByNodeID, tx.Validator.NodeID)
				break
			}
			newPS.validatorExtrasByNodeID[tx.Validator.NodeID] = &validatorImpl{
				delegators: vdr.delegators[1:], // sorted in order of removal
				subnets:    vdr.subnets,
			}
		case *txs.AddSubnetValidatorTx:
			vdr := newPS.validatorExtrasByNodeID[tx.Validator.NodeID]
			if len(vdr.delegators) == 0 && len(vdr.subnets) == 1 {
				delete(newPS.validatorExtrasByNodeID, tx.Validator.NodeID)
				break
			}
			newSubnets := make(map[ids.ID]SubnetValidatorAndID, len(vdr.subnets)-1)
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

func (ps *pendingStakerChainStateImpl) Stakers() []*txs.Tx {
	return ps.validators
}

func (ps *pendingStakerChainStateImpl) Apply(is InternalState) {
	for _, added := range ps.addedStakers {
		is.AddPendingStaker(added)
	}
	for _, deleted := range ps.deletedStakers {
		is.DeletePendingStaker(deleted)
	}
	is.SetPendingStakerChainState(ps)

	// Validator changes should only be applied once.
	ps.addedStakers = nil
	ps.deletedStakers = nil
}

type innerSortValidatorsByAddition []*txs.Tx

func (s innerSortValidatorsByAddition) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	var (
		iStartTime time.Time
		iPriority  byte
	)
	switch tx := iDel.Unsigned.(type) {
	case *txs.AddValidatorTx:
		iStartTime = tx.StartTime()
		iPriority = mediumPriority
	case *txs.AddDelegatorTx:
		iStartTime = tx.StartTime()
		iPriority = topPriority
	case *txs.AddSubnetValidatorTx:
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
	case *txs.AddValidatorTx:
		jStartTime = tx.StartTime()
		jPriority = mediumPriority
	case *txs.AddDelegatorTx:
		jStartTime = tx.StartTime()
		jPriority = topPriority
	case *txs.AddSubnetValidatorTx:
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
	// add txs.AddValidatorTx, then txs.AddDelegatorTx, then
	// txs.AddSubnetValidatorTxs.
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

func sortValidatorsByAddition(s []*txs.Tx) {
	sort.Sort(innerSortValidatorsByAddition(s))
}

type innerSortDelegatorsByAddition []DelegatorAndID

func (s innerSortDelegatorsByAddition) Less(i, j int) bool {
	iDel := s[i]
	jDel := s[j]

	iStartTime := iDel.Tx.StartTime()
	jStartTime := jDel.Tx.StartTime()
	if iStartTime.Before(jStartTime) {
		return true
	}
	if jStartTime.Before(iStartTime) {
		return false
	}

	// If the end times are the same, then we sort by the txID
	return bytes.Compare(iDel.TxID[:], jDel.TxID[:]) == -1
}

func (s innerSortDelegatorsByAddition) Len() int {
	return len(s)
}

func (s innerSortDelegatorsByAddition) Swap(i, j int) {
	s[j], s[i] = s[i], s[j]
}

func sortDelegatorsByAddition(s []DelegatorAndID) {
	sort.Sort(innerSortDelegatorsByAddition(s))
}
