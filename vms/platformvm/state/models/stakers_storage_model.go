// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package models

import (
	"errors"

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

// stakersStorageModel is the executable reference model of how we expect
// P-chain state and diffs to behave with respect to stakers.
// stakersStorageModel abstracts away the complexity related to
// P-chain state persistence and to the state.Diff flushing mechanisms.
// stakersStorageModel represents how we expect state.Diff and state.State to behave
// in a single threaded environment when stakers are written to or read from them.
// The utility of stakersStorageModel as an executable reference model is that
// we can write automatic tests asserting that state.Diff and state.State conform
// to stakersStorageModel.

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

func (m *stakersStorageModel) UpdateCurrentValidator(staker *state.Staker) error {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}
	if _, found := m.currentValidators[key]; !found {
		return state.ErrUpdatingDeletedStaker
	}

	m.currentValidators[key] = staker
	return nil
}

func (m *stakersStorageModel) PutPendingValidator(staker *state.Staker) {
	putValidator(staker, m.pendingValidators)
}

func putValidator(staker *state.Staker, domain map[subnetNodeKey]*state.Staker) {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}

	// overwrite validator even if already exist. In prod code,
	// it's up to block verification to check that we do not overwrite
	// a validator existing on state or lower diffs.
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

func (m *stakersStorageModel) UpdateCurrentDelegator(staker *state.Staker) error {
	key := subnetNodeKey{
		subnetID: staker.SubnetID,
		nodeID:   staker.NodeID,
	}

	ls, found := m.currentDelegators[key]
	if !found {
		return state.ErrUpdatingDeletedStaker
	}
	if _, found := ls[staker.TxID]; !found {
		return state.ErrUpdatingDeletedStaker
	}
	ls[staker.TxID] = staker
	return nil
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
