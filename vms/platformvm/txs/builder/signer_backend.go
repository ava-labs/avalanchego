// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/backends"
)

var _ backends.SignerBackend = (*signerBackend)(nil)

func NewSignerBackend(state state.State, atomicUTXOManager avax.AtomicUTXOManager, addrs set.Set[ids.ShortID]) backends.SignerBackend {
	return &signerBackend{
		state:             state,
		atomicUTXOManager: atomicUTXOManager,
		addrs:             addrs,
	}
}

type signerBackend struct {
	state             state.State
	atomicUTXOManager avax.AtomicUTXOManager
	addrs             set.Set[ids.ShortID]
}

func (s *signerBackend) GetUTXO(_ context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
	if chainID == constants.PlatformChainID {
		return s.state.GetUTXO(utxoID)
	}

	atomicUTXOs, _, _, err := s.atomicUTXOManager.GetAtomicUTXOs(chainID, s.addrs, ids.ShortEmpty, ids.Empty, MaxPageSize)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}
	for _, utxo := range atomicUTXOs {
		if utxo.InputID() == utxoID {
			return utxo, nil
		}
	}
	return nil, database.ErrNotFound
}

func (s *signerBackend) GetSubnetOwner(_ context.Context, subnetID ids.ID) (fx.Owner, error) {
	return s.state.GetSubnetOwner(subnetID)
}
