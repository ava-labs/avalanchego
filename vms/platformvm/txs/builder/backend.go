// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/wallet/chain/p/backends"
)

var _ backends.Backend = (*Backend)(nil)

func NewBackend(
	ctx *snow.Context,
	cfg *config.Config,
	state state.State,
	atomicUTXOsMan avax.AtomicUTXOManager,
) *Backend {
	backendCtx := backends.NewContext(
		ctx.NetworkID,
		ctx.AVAXAssetID,
		cfg.TxFee,
		cfg.GetCreateSubnetTxFee(state.GetTimestamp()),
		cfg.TransformSubnetTxFee,
		cfg.GetCreateBlockchainTxFee(state.GetTimestamp()),
		cfg.AddPrimaryNetworkValidatorFee,
		cfg.AddPrimaryNetworkDelegatorFee,
		cfg.AddSubnetValidatorFee,
		cfg.AddSubnetDelegatorFee,
	)
	return &Backend{
		Context:        backendCtx,
		cfg:            cfg,
		state:          state,
		atomicUTXOsMan: atomicUTXOsMan,
	}
}

type Backend struct {
	backends.Context

	cfg            *config.Config
	addrs          set.Set[ids.ShortID]
	state          state.State
	atomicUTXOsMan avax.AtomicUTXOManager
}

// Override [backend.Context.CreateSubnetTxFee] to refresh fee
// relevant in unit tests only
func (b *Backend) CreateSubnetTxFee() uint64 {
	return b.cfg.GetCreateSubnetTxFee(b.state.GetTimestamp())
}

// Override [backend.Context.GetCreateBlockchainTxFee] to refresh fee
// relevant in unit tests only
func (b *Backend) GetCreateBlockchainTxFee() uint64 {
	return b.cfg.GetCreateBlockchainTxFee(b.state.GetTimestamp())
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
