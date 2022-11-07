// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

// For state and diff
type Camino interface {
	LockedUTXOs(ids.Set, ids.ShortSet, locked.State) ([]*avax.UTXO, error)
	CaminoGenesisState() (*genesis.Camino, error)
}

// For state only
type CaminoState interface {
	GenesisState() *genesis.Camino
	SyncGenesis(genesisState *genesis.State) error
}

type caminoState struct {
	verifyNodeSignature bool
	lockModeBondDeposit bool
}

func newCaminoState() *caminoState {
	return &caminoState{}
}

// Return current genesis args
func (cs *caminoState) GenesisState() *genesis.Camino {
	return &genesis.Camino{
		VerifyNodeSignature: cs.verifyNodeSignature,
		LockModeBondDeposit: cs.lockModeBondDeposit,
	}
}

// Extract camino tag from genesis
func (cs *caminoState) SyncGenesis(genesisState *genesis.State) error {
	cs.lockModeBondDeposit = genesisState.Camino.LockModeBondDeposit
	cs.verifyNodeSignature = genesisState.Camino.VerifyNodeSignature

	return nil
}
