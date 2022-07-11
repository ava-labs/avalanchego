// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"time"

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

	currentValidatorDiffs diffValidators
	pendingValidatorDiffs diffValidators

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

func (d *diff) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	newValidator, ok := d.currentValidatorDiffs.GetValidator(subnetID, nodeID)
	if ok {
		if newValidator == nil {
			return nil, database.ErrNotFound
		}
		return newValidator, nil
	}
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}
	return parentState.GetCurrentValidator(subnetID, nodeID)
}

func (d *diff) PutCurrentValidator(staker *Staker) {
	d.currentValidatorDiffs.PutValidator(staker)
}

func (d *diff) DeleteCurrentValidator(staker *Staker) {
	d.currentValidatorDiffs.PutValidator(staker)
}

func (d *diff) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}

	parentIterator, err := parentState.GetCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return d.currentValidatorDiffs.GetDelegatorIterator(parentIterator, subnetID, nodeID), nil
}

func (d *diff) PutCurrentDelegator(staker *Staker) {
	d.currentValidatorDiffs.PutDelegator(staker)
}

func (d *diff) DeleteCurrentDelegator(staker *Staker) {
	d.currentValidatorDiffs.DeleteDelegator(staker)
}

func (d *diff) GetCurrentStakerIterator() (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}

	parentIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.currentValidatorDiffs.GetStakerIterator(parentIterator), nil
}

func (d *diff) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	newValidator, ok := d.pendingValidatorDiffs.GetValidator(subnetID, nodeID)
	if ok {
		if newValidator == nil {
			return nil, database.ErrNotFound
		}
		return newValidator, nil
	}
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}
	return parentState.GetPendingValidator(subnetID, nodeID)
}

func (d *diff) PutPendingValidator(staker *Staker) {
	d.pendingValidatorDiffs.PutValidator(staker)
}

func (d *diff) DeletePendingValidator(staker *Staker) {
	d.pendingValidatorDiffs.PutValidator(staker)
}

func (d *diff) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}

	parentIterator, err := parentState.GetPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return d.pendingValidatorDiffs.GetDelegatorIterator(parentIterator, subnetID, nodeID), nil
}

func (d *diff) PutPendingDelegator(staker *Staker) {
	d.pendingValidatorDiffs.PutDelegator(staker)
}

func (d *diff) DeletePendingDelegator(staker *Staker) {
	d.pendingValidatorDiffs.DeleteDelegator(staker)
}

func (d *diff) GetPendingStakerIterator() (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, errMissingParentState
	}

	parentIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.pendingValidatorDiffs.GetStakerIterator(parentIterator), nil
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
	for _, subnetValidatorDiffs := range d.currentValidatorDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			if validatorDiff.validatorModified {
				if validatorDiff.validatorDeleted {
					baseState.DeleteCurrentValidator(validatorDiff.validator)
				} else {
					baseState.PutCurrentValidator(validatorDiff.validator)
				}
			}

			addedDelegatorIterator := NewTreeIterator(validatorDiff.addedDelegators)
			for addedDelegatorIterator.Next() {
				baseState.PutCurrentDelegator(addedDelegatorIterator.Value())
			}
			addedDelegatorIterator.Release()

			for _, delegator := range validatorDiff.deletedDelegators {
				baseState.DeleteCurrentDelegator(delegator)
			}
		}
	}
	for _, subnetValidatorDiffs := range d.pendingValidatorDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			if validatorDiff.validatorModified {
				if validatorDiff.validatorDeleted {
					baseState.DeletePendingValidator(validatorDiff.validator)
				} else {
					baseState.PutPendingValidator(validatorDiff.validator)
				}
			}

			addedDelegatorIterator := NewTreeIterator(validatorDiff.addedDelegators)
			for addedDelegatorIterator.Next() {
				baseState.PutPendingDelegator(addedDelegatorIterator.Value())
			}
			addedDelegatorIterator.Release()

			for _, delegator := range validatorDiff.deletedDelegators {
				baseState.DeletePendingDelegator(delegator)
			}
		}
	}
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
