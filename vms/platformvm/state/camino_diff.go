// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

func (d *diff) LockedUTXOs(txIDs ids.Set, addresses ids.ShortSet, lockState locked.State) ([]*avax.UTXO, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	retUtxos, err := parentState.LockedUTXOs(txIDs, addresses, lockState)
	if err != nil {
		return nil, err
	}

	// Apply modifiedUTXO's
	// Step 1: remove / update existing UTXOs
	remaining := ids.NewSet(len(d.modifiedUTXOs))
	for k := range d.modifiedUTXOs {
		remaining.Add(k)
	}
	for i := len(retUtxos) - 1; i >= 0; i-- {
		if utxo, exists := d.modifiedUTXOs[retUtxos[i].InputID()]; exists {
			if utxo.utxo == nil {
				retUtxos = append(retUtxos[:i], retUtxos[i+1:]...)
			} else {
				retUtxos[i] = utxo.utxo
			}
			delete(remaining, utxo.utxoID)
		}
	}

	// Step 2: Append new UTXOs
	for utxoID := range remaining {
		utxo := d.modifiedUTXOs[utxoID].utxo
		if utxo != nil {
			if lockedOut, ok := utxo.Out.(*locked.Out); ok && lockedOut.IDs.Match(lockState, txIDs) {
				retUtxos = append(retUtxos, utxo)
			}
		}
	}

	return nil, nil
}

func (d *diff) CaminoGenesisState() (*genesis.Camino, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.CaminoGenesisState()
}
