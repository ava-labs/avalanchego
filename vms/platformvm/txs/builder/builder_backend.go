// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/backends"
)

var _ backends.BuilderBackend = (*buiderBackend)(nil)

func NewBuilderBackend(
	ctx *snow.Context,
	cfg *config.Config,
	addrs set.Set[ids.ShortID],
	state state.State,
	atomicUTXOsMan avax.AtomicUTXOManager,
) backends.BuilderBackend {
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
	return &buiderBackend{
		Context:        backendCtx,
		addrs:          addrs,
		state:          state,
		atomicUTXOsMan: atomicUTXOsMan,
	}
}

type buiderBackend struct {
	backends.Context

	addrs          set.Set[ids.ShortID]
	state          state.State
	atomicUTXOsMan avax.AtomicUTXOManager
}

func (b *buiderBackend) UTXOs(_ context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	if sourceChainID == constants.PlatformChainID {
		return avax.GetAllUTXOs(b.state, b.addrs) // The UTXOs controlled by [keys]
	}

	atomicUTXOs, _, _, err := b.atomicUTXOsMan.GetAtomicUTXOs(sourceChainID, b.addrs, ids.ShortEmpty, ids.Empty, MaxPageSize)
	return atomicUTXOs, err
}

func (b *buiderBackend) GetSubnetOwner(_ context.Context, subnetID ids.ID) (fx.Owner, error) {
	return b.state.GetSubnetOwner(subnetID)
}
