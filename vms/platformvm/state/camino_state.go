// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

func (s *state) LockedUTXOs(txIDs ids.Set, addresses ids.ShortSet, lockState locked.State) ([]*avax.UTXO, error) {
	retUtxos := []*avax.UTXO{}
	for address := range addresses {
		utxoIDs, err := s.UTXOIDs(address.Bytes(), ids.ID{}, math.MaxInt)
		if err != nil {
			return nil, err
		}
		for _, utxoID := range utxoIDs {
			utxo, err := s.GetUTXO(utxoID)
			if err != nil {
				return nil, err
			}
			if utxo == nil {
				continue
			}
			if lockedOut, ok := utxo.Out.(*locked.Out); ok && lockedOut.IDs.Match(lockState, txIDs) {
				retUtxos = append(retUtxos, utxo)
			}
		}
	}
	return retUtxos, nil
}

func (s *state) CaminoGenesisState() (*genesis.Camino, error) {
	return s.caminoState.GenesisState(), nil
}
