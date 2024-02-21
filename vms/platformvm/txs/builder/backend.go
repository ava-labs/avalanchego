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

var _ backends.Backend = (*backend)(nil)

func NewBackend(
	ctx *snow.Context,
	cfg *config.Config,
	addrs set.Set[ids.ShortID],
	state state.State,
	atomicUTXOsMan avax.AtomicUTXOManager,
) backends.Backend {
	backendCtx := backends.NewContext(
		ctx.NetworkID,
		ctx.AVAXAssetID,
		cfg.TxFee,
		cfg.GetCreateSubnetTxFee(state.GetTimestamp()),
		cfg.TransformSubnetTxFee,
		cfg.CreateBlockchainTxFee,
		cfg.AddPrimaryNetworkValidatorFee,
		cfg.AddPrimaryNetworkDelegatorFee,
		cfg.AddSubnetValidatorFee,
		cfg.AddSubnetDelegatorFee,
	)
	return &backend{
		Context:        backendCtx,
		addrs:          addrs,
		state:          state,
		atomicUTXOsMan: atomicUTXOsMan,
	}
}

type backend struct {
	backends.Context

	addrs          set.Set[ids.ShortID]
	state          state.State
	atomicUTXOsMan avax.AtomicUTXOManager
}

func (b *backend) UTXOs(_ context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	if sourceChainID == constants.PlatformChainID {
		return avax.GetAllUTXOs(b.state, b.addrs) // The UTXOs controlled by [keys]
	}

	atomicUTXOs, _, _, err := b.atomicUTXOsMan.GetAtomicUTXOs(sourceChainID, b.addrs, ids.ShortEmpty, ids.Empty, MaxPageSize)
	return atomicUTXOs, err
}

func (b *backend) GetUTXO(_ context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
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

func (b *backend) GetSubnetOwner(_ context.Context, subnetID ids.ID) (fx.Owner, error) {
	return b.state.GetSubnetOwner(subnetID)
}
