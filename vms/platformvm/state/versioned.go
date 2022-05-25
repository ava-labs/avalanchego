// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ Versioned = &versioned{}

type Versioned interface {
	Mutable

	SetBase(Mutable)
	Apply(Content)
}

type versioned struct {
	parentState Mutable

	transactions.ValidatorState

	timestamp time.Time

	currentSupply uint64

	addedSubnets  []*signed.Tx
	cachedSubnets []*signed.Tx

	addedChains  map[ids.ID][]*signed.Tx
	cachedChains map[ids.ID][]*signed.Tx

	// map of txID -> []*UTXO
	addedRewardUTXOs map[ids.ID][]*avax.UTXO

	// map of txID -> {*transaction.Tx, Status}
	addedTxs map[ids.ID]*transactions.TxAndStatus

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*utxoImpl
}

type utxoImpl struct {
	utxoID ids.ID
	utxo   *avax.UTXO
}

func NewVersioned(
	ps Mutable,
	current transactions.CurrentStaker,
	pending transactions.PendingStaker,
) Versioned {
	return &versioned{
		parentState:    ps,
		ValidatorState: transactions.NewValidatorState(current, pending),
		timestamp:      ps.GetTimestamp(),
		currentSupply:  ps.GetCurrentSupply(),
	}
}

func (vs *versioned) GetTimestamp() time.Time {
	return vs.timestamp
}

func (vs *versioned) SetTimestamp(timestamp time.Time) {
	vs.timestamp = timestamp
}

func (vs *versioned) GetCurrentSupply() uint64 {
	return vs.currentSupply
}

func (vs *versioned) SetCurrentSupply(currentSupply uint64) {
	vs.currentSupply = currentSupply
}

func (vs *versioned) GetSubnets() ([]*signed.Tx, error) {
	if len(vs.addedSubnets) == 0 {
		return vs.parentState.GetSubnets()
	}
	if len(vs.cachedSubnets) != 0 {
		return vs.cachedSubnets, nil
	}
	subnets, err := vs.parentState.GetSubnets()
	if err != nil {
		return nil, err
	}
	newSubnets := make([]*signed.Tx, len(subnets)+len(vs.addedSubnets))
	copy(newSubnets, subnets)
	for i, subnet := range vs.addedSubnets {
		newSubnets[i+len(subnets)] = subnet
	}
	vs.cachedSubnets = newSubnets
	return newSubnets, nil
}

func (vs *versioned) AddSubnet(createSubnetTx *signed.Tx) {
	vs.addedSubnets = append(vs.addedSubnets, createSubnetTx)
	if vs.cachedSubnets != nil {
		vs.cachedSubnets = append(vs.cachedSubnets, createSubnetTx)
	}
}

func (vs *versioned) GetChains(subnetID ids.ID) ([]*signed.Tx, error) {
	if len(vs.addedChains) == 0 {
		// No chains have been added
		return vs.parentState.GetChains(subnetID)
	}
	addedChains := vs.addedChains[subnetID]
	if len(addedChains) == 0 {
		// No chains have been added to this subnet
		return vs.parentState.GetChains(subnetID)
	}

	// There have been chains added to the requested subnet

	if vs.cachedChains == nil {
		// This is the first time we are going to be caching the subnet chains
		vs.cachedChains = make(map[ids.ID][]*signed.Tx)
	}

	cachedChains, cached := vs.cachedChains[subnetID]
	if cached {
		return cachedChains, nil
	}

	// This chain wasn't cached yet
	chains, err := vs.parentState.GetChains(subnetID)
	if err != nil {
		return nil, err
	}

	newChains := make([]*signed.Tx, len(chains)+len(addedChains))
	copy(newChains, chains)
	for i, chain := range addedChains {
		newChains[i+len(chains)] = chain
	}
	vs.cachedChains[subnetID] = newChains
	return newChains, nil
}

func (vs *versioned) AddChain(createChainTx *signed.Tx) {
	tx := createChainTx.Unsigned.(*unsigned.CreateChainTx)
	if vs.addedChains == nil {
		vs.addedChains = map[ids.ID][]*signed.Tx{
			tx.SubnetID: {createChainTx},
		}
	} else {
		vs.addedChains[tx.SubnetID] = append(vs.addedChains[tx.SubnetID], createChainTx)
	}

	cachedChains, cached := vs.cachedChains[tx.SubnetID]
	if !cached {
		return
	}
	vs.cachedChains[tx.SubnetID] = append(cachedChains, createChainTx)
}

func (vs *versioned) GetTx(txID ids.ID) (*signed.Tx, status.Status, error) {
	tx, exists := vs.addedTxs[txID]
	if !exists {
		return vs.parentState.GetTx(txID)
	}
	return tx.Tx, tx.Status, nil
}

func (vs *versioned) AddTx(tx *signed.Tx, status status.Status) {
	txID := tx.ID()
	txStatus := &transactions.TxAndStatus{
		Tx:     tx,
		Status: status,
	}
	if vs.addedTxs == nil {
		vs.addedTxs = map[ids.ID]*transactions.TxAndStatus{
			txID: txStatus,
		}
	} else {
		vs.addedTxs[txID] = txStatus
	}
}

func (vs *versioned) GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error) {
	if utxos, exists := vs.addedRewardUTXOs[txID]; exists {
		return utxos, nil
	}
	return vs.parentState.GetRewardUTXOs(txID)
}

func (vs *versioned) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	if vs.addedRewardUTXOs == nil {
		vs.addedRewardUTXOs = make(map[ids.ID][]*avax.UTXO)
	}
	vs.addedRewardUTXOs[txID] = append(vs.addedRewardUTXOs[txID], utxo)
}

func (vs *versioned) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	utxo, modified := vs.modifiedUTXOs[utxoID]
	if !modified {
		return vs.parentState.GetUTXO(utxoID)
	}
	if utxo.utxo == nil {
		return nil, database.ErrNotFound
	}
	return utxo.utxo, nil
}

func (vs *versioned) AddUTXO(utxo *avax.UTXO) {
	newUTXO := &utxoImpl{
		utxoID: utxo.InputID(),
		utxo:   utxo,
	}
	if vs.modifiedUTXOs == nil {
		vs.modifiedUTXOs = map[ids.ID]*utxoImpl{
			utxo.InputID(): newUTXO,
		}
	} else {
		vs.modifiedUTXOs[utxo.InputID()] = newUTXO
	}
}

func (vs *versioned) DeleteUTXO(utxoID ids.ID) {
	newUTXO := &utxoImpl{
		utxoID: utxoID,
	}
	if vs.modifiedUTXOs == nil {
		vs.modifiedUTXOs = map[ids.ID]*utxoImpl{
			utxoID: newUTXO,
		}
	} else {
		vs.modifiedUTXOs[utxoID] = newUTXO
	}
}

func (vs *versioned) SetBase(parentState Mutable) {
	vs.parentState = parentState
}

func (vs *versioned) Apply(bs Content) {
	bs.SetTimestamp(vs.timestamp)
	bs.SetCurrentSupply(vs.currentSupply)
	for _, subnet := range vs.addedSubnets {
		bs.AddSubnet(subnet)
	}
	for _, chains := range vs.addedChains {
		for _, chain := range chains {
			bs.AddChain(chain)
		}
	}
	for _, tx := range vs.addedTxs {
		bs.AddTx(tx.Tx, tx.Status)
	}
	for txID, utxos := range vs.addedRewardUTXOs {
		for _, utxo := range utxos {
			bs.AddRewardUTXO(txID, utxo)
		}
	}
	for _, utxo := range vs.modifiedUTXOs {
		if utxo.utxo != nil {
			bs.AddUTXO(utxo.utxo)
		} else {
			bs.DeleteUTXO(utxo.utxoID)
		}
	}
	vs.CurrentStakerChainState().Apply(bs)
	vs.PendingStakerChainState().Apply(bs)
}
