// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
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
	state state.State,
) backends.BuilderBackend {
	backendCtx := backends.NewContext(
		ctx.NetworkID,
		ctx.AVAXAssetID,
		cfg.TxFee,
		cfg.CreateSubnetTxFee,
		cfg.TransformSubnetTxFee,
		cfg.CreateBlockchainTxFee,
		cfg.AddPrimaryNetworkValidatorFee,
		cfg.AddPrimaryNetworkDelegatorFee,
		cfg.AddSubnetValidatorFee,
		cfg.AddSubnetDelegatorFee,
	)
	return &buiderBackend{
		Context: backendCtx,
		state:   state,
	}
}

type buiderBackend struct {
	backends.Context

	state state.State
}

func (*buiderBackend) UTXOs(_ context.Context /*sourceChainID*/, _ ids.ID) ([]*avax.UTXO, error) {
	return nil, errors.New("not yet implemented")
}

func (b *buiderBackend) GetSubnetOwner(_ context.Context, subnetID ids.ID) (fx.Owner, error) {
	return b.state.GetSubnetOwner(subnetID)
}
