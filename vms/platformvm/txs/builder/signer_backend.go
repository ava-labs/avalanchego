// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/backends"
)

var _ backends.SignerBackend = (*signerBackend)(nil)

func NewSignerBackend(state state.State, sourceChain ids.ID, atomicUTXOs []*avax.UTXO) backends.SignerBackend {
	importedUTXO := make(map[ids.ID]*avax.UTXO, len(atomicUTXOs))
	for _, utxo := range atomicUTXOs {
		importedUTXO[utxo.InputID()] = utxo
	}

	return &signerBackend{
		state:           state,
		importedChainID: sourceChain,
		importedUTXOs:   importedUTXO,
	}
}

type signerBackend struct {
	state state.State

	importedChainID ids.ID
	importedUTXOs   map[ids.ID]*avax.UTXO // utxoID --> utxo
}

func (s *signerBackend) GetUTXO(_ context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
	switch chainID {
	case constants.PlatformChainID:
		return s.state.GetUTXO(utxoID)
	case s.importedChainID:
		utxo, found := s.importedUTXOs[utxoID]
		if !found {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	default:
		return nil, fmt.Errorf("unexpected chain ID %s", chainID)
	}
}

func (s *signerBackend) GetSubnetOwner(_ context.Context, subnetID ids.ID) (fx.Owner, error) {
	return s.state.GetSubnetOwner(subnetID)
}
