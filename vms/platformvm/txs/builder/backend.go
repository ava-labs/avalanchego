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
	"github.com/ava-labs/avalanchego/wallet/chain/p/signer"

	walletbuilder "github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

var (
	_ walletbuilder.Backend = (*Backend)(nil)
	_ signer.Backend        = (*Backend)(nil)
)

func NewBackend(
	state state.State,
	atomicUTXOsMan avax.AtomicUTXOManager,
) *Backend {
	return &Backend{
		state:          state,
		atomicUTXOsMan: atomicUTXOsMan,
	}
}

type Backend struct {
	addrs          set.Set[ids.ShortID]
	state          state.State
	atomicUTXOsMan avax.AtomicUTXOManager
}

func (b *Backend) ResetAddresses(addrs set.Set[ids.ShortID]) {
	b.addrs = addrs
}

func (b *Backend) UTXOs(_ context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	if sourceChainID == constants.PlatformChainID {
		return avax.GetAllUTXOs(b.state, b.addrs)
	}

	atomicUTXOs, _, _, err := b.atomicUTXOsMan.GetAtomicUTXOs(sourceChainID, b.addrs, ids.ShortEmpty, ids.Empty, MaxPageSize)
	return atomicUTXOs, err
}

func (b *Backend) GetUTXO(_ context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
	if chainID == constants.PlatformChainID {
		return b.state.GetUTXO(utxoID)
	}

	atomicUTXOs, _, _, err := b.atomicUTXOsMan.GetAtomicUTXOs(chainID, b.addrs, ids.ShortEmpty, ids.Empty, MaxPageSize)
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

func (b *Backend) GetSubnetOwner(_ context.Context, subnetID ids.ID) (fx.Owner, error) {
	return b.state.GetSubnetOwner(subnetID)
}
