// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

var (
	_ Stakers        = (*stakersStorageModel)(nil)
	_ StakerIterator = (*stakersStorageIteratorModel)(nil)
)

// stakersStorageModel is the executable reference model of how we expect
// P-chain state and diffs to behave with respect to stakers.
// stakersStorageModel abstracts away the complexity related to
// P-chain state persistence and to the Diff flushing mechanisms.
// stakersStorageModel represents how we expect Diff and State to behave
// in a single threaded environment when stakers are written to or read from them.
// The utility of stakersStorageModel as an executable reference model is that
// we can write automatic tests asserting that Diff and State conform
// to stakersStorageModel.

type subnetNodeKey struct {
	subnetID ids.ID
	nodeID   ids.NodeID
}

type stakersStorageModel struct {
	currentValidators map[subnetNodeKey]*Staker
	currentDelegators map[subnetNodeKey](map[ids.ID]*Staker) // <subnetID, nodeID> -> (txID -> Staker)

	pendingValidators map[subnetNodeKey]*Staker
	pendingDelegators map[subnetNodeKey](map[ids.ID]*Staker) // <subnetID, nodeID> -> (txID -> Staker)
}

func newStakersStorageModel() *stakersStorageModel {
	return &stakersStorageModel{
		currentValidators: make(map[subnetNodeKey]*Staker),
		currentDelegators: make(map[subnetNodeKey]map[ids.ID]*Staker),
		pendingValidators: make(map[subnetNodeKey]*Staker),
		pendingDelegators: make(map[subnetNodeKey]map[ids.ID]*Staker),
	}
}

func (m *stakersStorageModel) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return getValidator(subnetID, nodeID, m.currentValidators)
}

func (m *stakersStorageModel) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return getValidator(subnetID, nodeID, m.pendingValidators)
}

func getValidator(subnetID ids.ID, nodeID ids.NodeID, domain map[subnetNodeKey]*Staker) (*Staker, error) {
	key := subnetNodeKey{
		subnetID: subnetID,
		nodeID:   nodeID,
	}
	res, found := domain[key]
	if !found {
		return nil, database.ErrNotFound
	}
	return res, nil
}

func (m *stakersStorageModel) PutCurrentValidator(staker *Staker) {
	putValidator(staker, m.currentValidators)
}

func (m *stakersStorageModel) PutPendingValidator(staker *Staker) {
	putValidator(staker, m.pendingValidators)
}

func putValidator(staker *Staker, domain map[subnetNodeKey]*Staker) {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}

	// overwrite validator even if already exist. In prod code,
	// it's up to block verification to check that we do not overwrite
	// a validator existing on state or lower diffs.
	domain[key] = staker
}

func (m *stakersStorageModel) DeleteCurrentValidator(staker *Staker) {
	deleteValidator(staker, m.currentValidators)
}

func (m *stakersStorageModel) DeletePendingValidator(staker *Staker) {
	deleteValidator(staker, m.pendingValidators)
}

func deleteValidator(staker *Staker, domain map[subnetNodeKey]*Staker) {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}
	delete(domain, key)
}

func (m *stakersStorageModel) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	return getDelegatorIterator(subnetID, nodeID, m.currentDelegators), nil
}

func (m *stakersStorageModel) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	return getDelegatorIterator(subnetID, nodeID, m.pendingDelegators), nil
}

func getDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID, domain map[subnetNodeKey](map[ids.ID]*Staker)) StakerIterator {
	key := subnetNodeKey{
		subnetID: subnetID,
		nodeID:   nodeID,
	}
	dels, found := domain[key]
	if !found {
		return EmptyIterator
	}

	sortedDels := maps.Values(dels)
	utils.Sort(sortedDels)
	return &stakersStorageIteratorModel{
		current:       nil,
		sortedStakers: sortedDels,
	}
}

func (m *stakersStorageModel) PutCurrentDelegator(staker *Staker) {
	putDelegator(staker, m.currentDelegators)
}

func (m *stakersStorageModel) PutPendingDelegator(staker *Staker) {
	putDelegator(staker, m.pendingDelegators)
}

func putDelegator(staker *Staker, domain map[subnetNodeKey]map[ids.ID]*Staker) {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}

	dels, found := domain[key]
	if !found {
		dels = make(map[ids.ID]*Staker)
		domain[key] = dels
	}
	dels[staker.TxID] = staker
}

func (m *stakersStorageModel) DeleteCurrentDelegator(staker *Staker) {
	deleteDelegator(staker, m.currentDelegators)
}

func (m *stakersStorageModel) DeletePendingDelegator(staker *Staker) {
	deleteDelegator(staker, m.pendingDelegators)
}

func deleteDelegator(staker *Staker, domain map[subnetNodeKey]map[ids.ID]*Staker) {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}

	dels, found := domain[key]
	if !found {
		return
	}
	delete(dels, staker.TxID)

	// prune
	if len(dels) == 0 {
		delete(domain, key)
	}
}

func (m *stakersStorageModel) GetCurrentStakerIterator() (StakerIterator, error) {
	return getCurrentStakerIterator(m.currentValidators, m.currentDelegators), nil
}

func (m *stakersStorageModel) GetPendingStakerIterator() (StakerIterator, error) {
	return getCurrentStakerIterator(m.pendingValidators, m.pendingDelegators), nil
}

func getCurrentStakerIterator(
	validators map[subnetNodeKey]*Staker,
	delegators map[subnetNodeKey](map[ids.ID]*Staker),
) StakerIterator {
	allStakers := maps.Values(validators)
	for _, dels := range delegators {
		allStakers = append(allStakers, maps.Values(dels)...)
	}
	utils.Sort(allStakers)
	return &stakersStorageIteratorModel{
		current:       nil,
		sortedStakers: allStakers,
	}
}

func (*stakersStorageModel) SetDelegateeReward(
	ids.ID,
	ids.NodeID,
	uint64,
) error {
	return errors.New("method non implemented in model")
}

func (*stakersStorageModel) GetDelegateeReward(
	ids.ID,
	ids.NodeID,
) (uint64, error) {
	return 0, errors.New("method non implemented in model")
}

type stakersStorageIteratorModel struct {
	current *Staker

	// sortedStakers contains the sorted list of stakers
	// as it should be returned by iteration.
	// sortedStakers must be sorted upon stakersStorageIteratorModel creation.
	// Stakers are evicted from sortedStakers as Next() is called.
	sortedStakers []*Staker
}

func (i *stakersStorageIteratorModel) Next() bool {
	if len(i.sortedStakers) == 0 {
		return false
	}

	i.current = i.sortedStakers[0]
	i.sortedStakers = i.sortedStakers[1:]
	return true
}

func (i *stakersStorageIteratorModel) Value() *Staker {
	return i.current
}

func (i *stakersStorageIteratorModel) Release() {
	i.current = nil
	i.sortedStakers = nil
}
