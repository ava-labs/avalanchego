// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package models

import (
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

var (
	_ state.Stakers        = (*stakersStorageModel)(nil)
	_ state.StakerIterator = (*stakersStorageIteratorModel)(nil)
)

type subnetNodeKey struct {
	subnetID ids.ID
	nodeID   ids.NodeID
}

type stakersStorageModel struct {
	currentValidators map[subnetNodeKey]*state.Staker
	currentDelegators map[subnetNodeKey](map[ids.ID]*state.Staker) // <subnetID, nodeID> -> (txID -> Staker)

	pendingValidators map[subnetNodeKey]*state.Staker
	pendingDelegators map[subnetNodeKey](map[ids.ID]*state.Staker) // <subnetID, nodeID> -> (txID -> Staker)
}

func newStakersStorageModel() *stakersStorageModel {
	return &stakersStorageModel{
		currentValidators: make(map[subnetNodeKey]*state.Staker),
		currentDelegators: make(map[subnetNodeKey]map[ids.ID]*state.Staker),
		pendingValidators: make(map[subnetNodeKey]*state.Staker),
		pendingDelegators: make(map[subnetNodeKey]map[ids.ID]*state.Staker),
	}
}

func (m *stakersStorageModel) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*state.Staker, error) {
	return getValidator(subnetID, nodeID, m.currentValidators)
}

func (m *stakersStorageModel) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*state.Staker, error) {
	return getValidator(subnetID, nodeID, m.pendingValidators)
}

func getValidator(subnetID ids.ID, nodeID ids.NodeID, domain map[subnetNodeKey]*state.Staker) (*state.Staker, error) {
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

func (m *stakersStorageModel) PutCurrentValidator(staker *state.Staker) {
	putValidator(staker, m.currentValidators)
}

func (m *stakersStorageModel) PutPendingValidator(staker *state.Staker) {
	putValidator(staker, m.pendingValidators)
}

func putValidator(staker *state.Staker, domain map[subnetNodeKey]*state.Staker) {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}
	domain[key] = staker
}

func (m *stakersStorageModel) DeleteCurrentValidator(staker *state.Staker) {
	deleteValidator(staker, m.currentValidators)
}

func (m *stakersStorageModel) DeletePendingValidator(staker *state.Staker) {
	deleteValidator(staker, m.pendingValidators)
}

func deleteValidator(staker *state.Staker, domain map[subnetNodeKey]*state.Staker) {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}
	delete(domain, key)
}

func (m *stakersStorageModel) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (state.StakerIterator, error) {
	return getDelegatorIterator(subnetID, nodeID, m.currentDelegators), nil
}

func (m *stakersStorageModel) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (state.StakerIterator, error) {
	return getDelegatorIterator(subnetID, nodeID, m.pendingDelegators), nil
}

func getDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID, domain map[subnetNodeKey](map[ids.ID]*state.Staker)) state.StakerIterator {
	key := subnetNodeKey{
		subnetID: subnetID,
		nodeID:   nodeID,
	}
	dels, found := domain[key]
	if !found {
		return state.EmptyIterator
	}

	sortedDels := maps.Values(dels)
	utils.Sort(sortedDels)
	return &stakersStorageIteratorModel{
		current:       nil,
		sortedStakers: sortedDels,
	}
}

func (m *stakersStorageModel) PutCurrentDelegator(staker *state.Staker) {
	putDelegator(staker, m.currentDelegators)
}

func (m *stakersStorageModel) PutPendingDelegator(staker *state.Staker) {
	putDelegator(staker, m.pendingDelegators)
}

func putDelegator(staker *state.Staker, domain map[subnetNodeKey]map[ids.ID]*state.Staker) {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}

	ls, found := domain[key]
	if !found {
		ls = make(map[ids.ID]*state.Staker)
		domain[key] = ls
	}
	ls[staker.TxID] = staker
}

func (m *stakersStorageModel) DeleteCurrentDelegator(staker *state.Staker) {
	deleteDelegator(staker, m.currentDelegators)
}

func (m *stakersStorageModel) DeletePendingDelegator(staker *state.Staker) {
	deleteDelegator(staker, m.pendingDelegators)
}

func deleteDelegator(staker *state.Staker, domain map[subnetNodeKey]map[ids.ID]*state.Staker) {
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

func (m *stakersStorageModel) GetCurrentStakerIterator() (state.StakerIterator, error) {
	return getCurrentStakerIterator(m.currentValidators, m.currentDelegators), nil
}

func (m *stakersStorageModel) GetPendingStakerIterator() (state.StakerIterator, error) {
	return getCurrentStakerIterator(m.pendingValidators, m.pendingDelegators), nil
}

func getCurrentStakerIterator(
	validators map[subnetNodeKey]*state.Staker,
	delegators map[subnetNodeKey](map[ids.ID]*state.Staker),
) state.StakerIterator {
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

type stakersStorageIteratorModel struct {
	current *state.Staker

	// sortedStakers contains the sorted list of stakers
	// as it should be returned by iteration.
	// sortedStakers must be sorted upon stakersStorageIteratorModel creation.
	// Stakers are evicted from sortedStakers as Value() is called.
	sortedStakers []*state.Staker
}

func (i *stakersStorageIteratorModel) Next() bool {
	if len(i.sortedStakers) == 0 {
		return false
	}

	i.current = i.sortedStakers[0]
	i.sortedStakers = i.sortedStakers[1:]
	return true
}

func (i *stakersStorageIteratorModel) Value() *state.Staker {
	return i.current
}

func (i *stakersStorageIteratorModel) Release() {
	i.current = nil
	i.sortedStakers = nil
}
