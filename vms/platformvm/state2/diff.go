// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Diff = &diff{}

	errMissingParentState = errors.New("missing parent state")
)

type Diff interface {
	Chain

	Apply(State)
}

type diff struct {
	parentID      ids.ID
	stateVersions Versions

	timestamp time.Time

	currentSupply uint64

	validatorDiffs map[ids.ID]map[ids.NodeID]*diffValidator
	addedStakers   *btree.BTree
	deletedStakers map[ids.ID]*Staker

	addedSubnets  []*txs.Tx
	cachedSubnets []*txs.Tx

	addedChains  map[ids.ID][]*txs.Tx
	cachedChains map[ids.ID][]*txs.Tx

	// map of txID -> []*UTXO
	addedRewardUTXOs map[ids.ID][]*avax.UTXO

	// map of txID -> {*txs.Tx, Status}
	addedTxs map[ids.ID]*txAndStatus

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*utxoModification
}

type utxoModification struct {
	utxoID ids.ID
	utxo   *avax.UTXO
}

type diffValidator struct {
	stakerModified bool
	stakerDeleted  bool
	staker         *Staker

	addedDelegators   *btree.BTree
	deletedDelegators map[ids.ID]*Staker
}

func NewDiff(
	parentID ids.ID,
	stateVersions Versions,
) (Diff, error) {
	parentState, ok := stateVersions.GetState(parentID)
	if !ok {
		return nil, errMissingParentState
	}
	return &diff{
		parentID:      parentID,
		stateVersions: stateVersions,
		timestamp:     parentState.GetTimestamp(),
		currentSupply: parentState.GetCurrentSupply(),
	}, nil
}

func (d *diff) GetTimestamp() time.Time {
	return d.timestamp
}

func (d *diff) SetTimestamp(timestamp time.Time) {
	d.timestamp = timestamp
}

func (d *diff) GetCurrentSupply() uint64 {
	return d.currentSupply
}

func (d *diff) SetCurrentSupply(currentSupply uint64) {
	d.currentSupply = currentSupply
}

func (d *diff) getOrCreateDiff(subnetID ids.ID, nodeID ids.NodeID) *diffValidator {
	subnetValidatorDiffs, ok := d.validatorDiffs[subnetID]
	if !ok {
		subnetValidatorDiffs = make(map[ids.NodeID]*diffValidator)
		d.validatorDiffs[subnetID] = subnetValidatorDiffs
	}
	validatorDiff, ok := subnetValidatorDiffs[nodeID]
	if !ok {
		validatorDiff = &diffValidator{}
		subnetValidatorDiffs[nodeID] = validatorDiff
	}
	return validatorDiff
}

func (d *diff) GetStaker(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	if subnetValidatorDiffs, ok := d.validatorDiffs[subnetID]; ok {
		if validatorDiff, ok := subnetValidatorDiffs[nodeID]; ok {
			if validatorDiff.stakerModified {
				if validatorDiff.stakerDeleted {
					return nil, database.ErrNotFound
				}
				return validatorDiff.staker, nil
			}
		}
	}
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}
	return parentState.GetStaker(subnetID, nodeID)
}

func (d *diff) PutStaker(staker *Staker) {
	validatorDiff := d.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	validatorDiff.stakerModified = true
	validatorDiff.stakerDeleted = false
	validatorDiff.staker = staker

	if d.addedStakers == nil {
		d.addedStakers = btree.New(defaultTreeDegree)
	}
	d.addedStakers.ReplaceOrInsert(staker)
}

func (d *diff) DeleteStaker(staker *Staker) {
	validatorDiff := d.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	validatorDiff.stakerModified = true
	validatorDiff.stakerDeleted = true
	validatorDiff.staker = staker

	if d.deletedStakers == nil {
		d.deletedStakers = make(map[ids.ID]*Staker)
	}
	d.deletedStakers[staker.TxID] = staker
}

func (d *diff) GetDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) StakerIterator {
	return nil

	// parentState, ok := d.stateVersions.GetState(d.parentID)
	// if !ok {
	// 	// TODO: handle this
	// 	return EmptyIterator
	// }

	// if subnetValidatorDiffs, ok := d.validatorDiffs[subnetID]; ok {
	// 	if validatorDiff, ok := subnetValidatorDiffs[nodeID]; ok {
	// 		if validatorDiff.stakerModified {
	// 			if validatorDiff.stakerDeleted {
	// 				return nil, database.ErrNotFound
	// 			}
	// 			return validatorDiff.staker, nil
	// 		}
	// 	}
	// }

	// subnetValidators, ok := v.validators[subnetID]
	// if !ok {
	// 	return EmptyIterator
	// }
	// validator, ok := subnetValidators[nodeID]
	// if !ok {
	// 	return EmptyIterator
	// }
	// return NewTreeIterator(validator.delegators)
}

func (d *diff) PutDelegator(staker *Staker) {
	validatorDiff := d.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.addedDelegators == nil {
		validatorDiff.addedDelegators = btree.New(defaultTreeDegree)
	}
	validatorDiff.addedDelegators.ReplaceOrInsert(staker)

	if d.addedStakers == nil {
		d.addedStakers = btree.New(defaultTreeDegree)
	}
	d.addedStakers.ReplaceOrInsert(staker)
}

func (d *diff) DeleteDelegator(staker *Staker) {
	validatorDiff := d.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	if validatorDiff.deletedDelegators == nil {
		validatorDiff.deletedDelegators = make(map[ids.ID]*Staker)
	}
	validatorDiff.deletedDelegators[staker.TxID] = staker

	if d.deletedStakers == nil {
		d.deletedStakers = make(map[ids.ID]*Staker)
	}
	d.deletedStakers[staker.TxID] = staker
}

func (d *diff) GetStakerIterator() StakerIterator {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		// TODO: handle this
		return EmptyIterator
	}
	parentStakerIterator := parentState.GetStakerIterator()
	addedStakerIterator := NewTreeIterator(d.addedStakers)
	return NewTreeIterator(v.stakers)
}

func (d *diff) GetSubnets() ([]*txs.Tx, error) {
	if len(d.addedSubnets) == 0 {
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, errMissingParentState
		}
		return parentState.GetSubnets()
	}

	if len(d.cachedSubnets) != 0 {
		return d.cachedSubnets, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}
	subnets, err := parentState.GetSubnets()
	if err != nil {
		return nil, err
	}
	newSubnets := make([]*txs.Tx, len(subnets)+len(d.addedSubnets))
	copy(newSubnets, subnets)
	for i, subnet := range d.addedSubnets {
		newSubnets[i+len(subnets)] = subnet
	}
	d.cachedSubnets = newSubnets
	return newSubnets, nil
}

func (d *diff) AddSubnet(createSubnetTx *txs.Tx) {
	d.addedSubnets = append(d.addedSubnets, createSubnetTx)
	if d.cachedSubnets != nil {
		d.cachedSubnets = append(d.cachedSubnets, createSubnetTx)
	}
}

func (d *diff) GetChains(subnetID ids.ID) ([]*txs.Tx, error) {
	addedChains := d.addedChains[subnetID]
	if len(addedChains) == 0 {
		// No chains have been added to this subnet
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, errMissingParentState
		}
		return parentState.GetChains(subnetID)
	}

	// There have been chains added to the requested subnet

	if d.cachedChains == nil {
		// This is the first time we are going to be caching the subnet chains
		d.cachedChains = make(map[ids.ID][]*txs.Tx)
	}

	cachedChains, cached := d.cachedChains[subnetID]
	if cached {
		return cachedChains, nil
	}

	// This chain wasn't cached yet
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}
	chains, err := parentState.GetChains(subnetID)
	if err != nil {
		return nil, err
	}

	newChains := make([]*txs.Tx, len(chains)+len(addedChains))
	copy(newChains, chains)
	for i, chain := range addedChains {
		newChains[i+len(chains)] = chain
	}
	d.cachedChains[subnetID] = newChains
	return newChains, nil
}

func (d *diff) AddChain(createChainTx *txs.Tx) {
	tx := createChainTx.Unsigned.(*txs.CreateChainTx)
	if d.addedChains == nil {
		d.addedChains = map[ids.ID][]*txs.Tx{
			tx.SubnetID: {createChainTx},
		}
	} else {
		d.addedChains[tx.SubnetID] = append(d.addedChains[tx.SubnetID], createChainTx)
	}

	cachedChains, cached := d.cachedChains[tx.SubnetID]
	if !cached {
		return
	}
	d.cachedChains[tx.SubnetID] = append(cachedChains, createChainTx)
}

func (d *diff) GetTx(txID ids.ID) (*txs.Tx, status.Status, error) {
	if tx, exists := d.addedTxs[txID]; exists {
		return tx.tx, tx.status, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, status.Unknown, errMissingParentState
	}
	return parentState.GetTx(txID)
}

func (d *diff) AddTx(tx *txs.Tx, status status.Status) {
	txID := tx.ID()
	txStatus := &txAndStatus{
		tx:     tx,
		status: status,
	}
	if d.addedTxs == nil {
		d.addedTxs = map[ids.ID]*txAndStatus{
			txID: txStatus,
		}
	} else {
		d.addedTxs[txID] = txStatus
	}
}

func (d *diff) GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error) {
	if utxos, exists := d.addedRewardUTXOs[txID]; exists {
		return utxos, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}
	return parentState.GetRewardUTXOs(txID)
}

func (d *diff) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	if d.addedRewardUTXOs == nil {
		d.addedRewardUTXOs = make(map[ids.ID][]*avax.UTXO)
	}
	d.addedRewardUTXOs[txID] = append(d.addedRewardUTXOs[txID], utxo)
}

func (d *diff) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	utxo, modified := d.modifiedUTXOs[utxoID]
	if !modified {
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, errMissingParentState
		}
		return parentState.GetUTXO(utxoID)
	}
	if utxo.utxo == nil {
		return nil, database.ErrNotFound
	}
	return utxo.utxo, nil
}

func (d *diff) AddUTXO(utxo *avax.UTXO) {
	newUTXO := &utxoModification{
		utxoID: utxo.InputID(),
		utxo:   utxo,
	}
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*utxoModification{
			utxo.InputID(): newUTXO,
		}
	} else {
		d.modifiedUTXOs[utxo.InputID()] = newUTXO
	}
}

func (d *diff) DeleteUTXO(utxoID ids.ID) {
	newUTXO := &utxoModification{
		utxoID: utxoID,
	}
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*utxoModification{
			utxoID: newUTXO,
		}
	} else {
		d.modifiedUTXOs[utxoID] = newUTXO
	}
}

func (d *diff) Apply(baseState State) {
	baseState.SetTimestamp(d.timestamp)
	baseState.SetCurrentSupply(d.currentSupply)
	for _, subnet := range d.addedSubnets {
		baseState.AddSubnet(subnet)
	}
	for _, chains := range d.addedChains {
		for _, chain := range chains {
			baseState.AddChain(chain)
		}
	}
	for _, tx := range d.addedTxs {
		baseState.AddTx(tx.tx, tx.status)
	}
	for txID, utxos := range d.addedRewardUTXOs {
		for _, utxo := range utxos {
			baseState.AddRewardUTXO(txID, utxo)
		}
	}
	for _, utxo := range d.modifiedUTXOs {
		if utxo.utxo != nil {
			baseState.AddUTXO(utxo.utxo)
		} else {
			baseState.DeleteUTXO(utxo.utxoID)
		}
	}
}
