// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Diff = (*diff)(nil)

	ErrMissingParentState = errors.New("missing parent state")
)

type Diff interface {
	Chain

	Apply(State)
}

type diff struct {
	parentID      ids.ID
	stateVersions Versions

	timestamp time.Time

	// Subnet ID --> supply of native asset of the subnet
	currentSupply map[ids.ID]uint64

	currentStakerDiffs diffStakers
	pendingStakerDiffs diffStakers

	addedSubnets []*txs.Tx
	// Subnet ID --> Tx that transforms the subnet
	transformedSubnets map[ids.ID]*txs.Tx
	cachedSubnets      []*txs.Tx

	addedChains  map[ids.ID][]*txs.Tx
	cachedChains map[ids.ID][]*txs.Tx

	addedRewardUTXOs map[ids.ID][]*avax.UTXO

	addedTxs map[ids.ID]*txAndStatus

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*avax.UTXO
}

func NewDiff(
	parentID ids.ID,
	stateVersions Versions,
) (Diff, error) {
	parentState, ok := stateVersions.GetState(parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, parentID)
	}
	return &diff{
		parentID:      parentID,
		stateVersions: stateVersions,
		timestamp:     parentState.GetTimestamp(),
	}, nil
}

func (d *diff) GetTimestamp() time.Time {
	return d.timestamp
}

func (d *diff) SetTimestamp(timestamp time.Time) {
	d.timestamp = timestamp
}

func (d *diff) GetCurrentSupply(subnetID ids.ID) (uint64, error) {
	supply, ok := d.currentSupply[subnetID]
	if ok {
		return supply, nil
	}

	// If the subnet supply wasn't modified in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetCurrentSupply(subnetID)
}

func (d *diff) SetCurrentSupply(subnetID ids.ID, currentSupply uint64) {
	if d.currentSupply == nil {
		d.currentSupply = map[ids.ID]uint64{
			subnetID: currentSupply,
		}
	} else {
		d.currentSupply[subnetID] = currentSupply
	}
}

func (d *diff) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	// If the validator was modified in this diff, return the modified
	// validator.
	newValidator, status := d.currentStakerDiffs.GetValidator(subnetID, nodeID)
	switch status {
	case added:
		return newValidator, nil
	case deleted:
		return nil, database.ErrNotFound
	default:
		// If the validator wasn't modified in this diff, ask the parent state.
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetCurrentValidator(subnetID, nodeID)
	}
}

func (d *diff) PutCurrentValidator(staker *Staker) {
	d.currentStakerDiffs.PutValidator(staker)
}

func (d *diff) DeleteCurrentValidator(staker *Staker) {
	d.currentStakerDiffs.DeleteValidator(staker)
}

func (d *diff) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return d.currentStakerDiffs.GetDelegatorIterator(parentIterator, subnetID, nodeID), nil
}

func (d *diff) PutCurrentDelegator(staker *Staker) {
	d.currentStakerDiffs.PutDelegator(staker)
}

func (d *diff) DeleteCurrentDelegator(staker *Staker) {
	d.currentStakerDiffs.DeleteDelegator(staker)
}

func (d *diff) GetCurrentStakerIterator() (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.currentStakerDiffs.GetStakerIterator(parentIterator), nil
}

func (d *diff) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	// If the validator was modified in this diff, return the modified
	// validator.
	newValidator, status := d.pendingStakerDiffs.GetValidator(subnetID, nodeID)
	switch status {
	case added:
		return newValidator, nil
	case deleted:
		return nil, database.ErrNotFound
	default:
		// If the validator wasn't modified in this diff, ask the parent state.
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetPendingValidator(subnetID, nodeID)
	}
}

func (d *diff) PutPendingValidator(staker *Staker) {
	d.pendingStakerDiffs.PutValidator(staker)
}

func (d *diff) DeletePendingValidator(staker *Staker) {
	d.pendingStakerDiffs.DeleteValidator(staker)
}

func (d *diff) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return d.pendingStakerDiffs.GetDelegatorIterator(parentIterator, subnetID, nodeID), nil
}

func (d *diff) PutPendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.PutDelegator(staker)
}

func (d *diff) DeletePendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.DeleteDelegator(staker)
}

func (d *diff) GetPendingStakerIterator() (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.pendingStakerDiffs.GetStakerIterator(parentIterator), nil
}

func (d *diff) GetSubnets() ([]*txs.Tx, error) {
	if len(d.addedSubnets) == 0 {
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetSubnets()
	}

	if len(d.cachedSubnets) != 0 {
		return d.cachedSubnets, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
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

func (d *diff) GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error) {
	tx, exists := d.transformedSubnets[subnetID]
	if exists {
		return tx, nil
	}

	// If the subnet wasn't transformed in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, ErrMissingParentState
	}
	return parentState.GetSubnetTransformation(subnetID)
}

func (d *diff) AddSubnetTransformation(transformSubnetTxIntf *txs.Tx) {
	transformSubnetTx := transformSubnetTxIntf.Unsigned.(*txs.TransformSubnetTx)
	if d.transformedSubnets == nil {
		d.transformedSubnets = map[ids.ID]*txs.Tx{
			transformSubnetTx.Subnet: transformSubnetTxIntf,
		}
	} else {
		d.transformedSubnets[transformSubnetTx.Subnet] = transformSubnetTxIntf
	}
}

func (d *diff) GetChains(subnetID ids.ID) ([]*txs.Tx, error) {
	addedChains := d.addedChains[subnetID]
	if len(addedChains) == 0 {
		// No chains have been added to this subnet
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
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
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
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
		return nil, status.Unknown, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
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
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
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
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetUTXO(utxoID)
	}
	if utxo == nil {
		return nil, database.ErrNotFound
	}
	return utxo, nil
}

func (d *diff) AddUTXO(utxo *avax.UTXO) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*avax.UTXO{
			utxo.InputID(): utxo,
		}
	} else {
		d.modifiedUTXOs[utxo.InputID()] = utxo
	}
}

func (d *diff) DeleteUTXO(utxoID ids.ID) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*avax.UTXO{
			utxoID: nil,
		}
	} else {
		d.modifiedUTXOs[utxoID] = nil
	}
}

func (d *diff) Apply(baseState State) {
	baseState.SetTimestamp(d.timestamp)
	for subnetID, supply := range d.currentSupply {
		baseState.SetCurrentSupply(subnetID, supply)
	}
	for _, subnetValidatorDiffs := range d.currentStakerDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			switch validatorDiff.validatorStatus {
			case added:
				baseState.PutCurrentValidator(validatorDiff.validator)
			case deleted:
				baseState.DeleteCurrentValidator(validatorDiff.validator)
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
	for _, subnetValidatorDiffs := range d.pendingStakerDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			switch validatorDiff.validatorStatus {
			case added:
				baseState.PutPendingValidator(validatorDiff.validator)
			case deleted:
				baseState.DeletePendingValidator(validatorDiff.validator)
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
	for _, tx := range d.transformedSubnets {
		baseState.AddSubnetTransformation(tx)
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
	for utxoID, utxo := range d.modifiedUTXOs {
		if utxo != nil {
			baseState.AddUTXO(utxo)
		} else {
			baseState.DeleteUTXO(utxoID)
		}
	}
}
