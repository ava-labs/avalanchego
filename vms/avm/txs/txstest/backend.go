// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/x/signer"
)

const maxPageSize uint64 = 1024

var (
	_ builder.Backend = (*walletBackendAdapter)(nil)
	_ signer.Backend  = (*walletBackendAdapter)(nil)
)

func NewBackend(
	ctx *snow.Context,
	state state.State,
	sharedMemory atomic.SharedMemory,
	codec codec.Manager,
) *Backend {
	return &Backend{
		xchainID:     ctx.XChainID,
		state:        state,
		sharedMemory: sharedMemory,
		codec:        codec,
	}
}

type Backend struct {
	xchainID     ids.ID
	state        state.State
	sharedMemory atomic.SharedMemory
	codec        codec.Manager
}

func (b *Backend) UTXOs(addrs set.Set[ids.ShortID], sourceChainID ids.ID) ([]*avax.UTXO, error) {
	if sourceChainID == b.xchainID {
		return avax.GetAllUTXOs(b.state, addrs)
	}

	atomicUTXOs, _, _, err := avax.GetAtomicUTXOs(
		b.sharedMemory,
		b.codec,
		sourceChainID,
		addrs,
		ids.ShortEmpty,
		ids.Empty,
		int(maxPageSize),
	)
	return atomicUTXOs, err
}

func (b *Backend) GetUTXO(addrs set.Set[ids.ShortID], chainID, utxoID ids.ID) (*avax.UTXO, error) {
	if chainID == b.xchainID {
		return b.state.GetUTXO(utxoID)
	}

	atomicUTXOs, _, _, err := avax.GetAtomicUTXOs(
		b.sharedMemory,
		b.codec,
		chainID,
		addrs,
		ids.ShortEmpty,
		ids.Empty,
		int(maxPageSize),
	)
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

type walletBackendAdapter struct {
	b     *Backend
	addrs set.Set[ids.ShortID]
}

func (wa *walletBackendAdapter) UTXOs(_ context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	return wa.b.UTXOs(wa.addrs, sourceChainID)
}

func (wa *walletBackendAdapter) GetUTXO(_ context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
	return wa.b.GetUTXO(wa.addrs, chainID, utxoID)
}
