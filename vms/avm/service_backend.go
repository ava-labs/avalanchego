// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
)

var _ txBuilderBackend = (*serviceBackend)(nil)

func newServiceBackend(
	feeAssetID ids.ID,
	ctx *snow.Context,
	cfg *config.Config,
	state state.State,
	sharedMemory atomic.SharedMemory,
	codec codec.Manager,
) *serviceBackend {
	backendCtx := &builder.Context{
		NetworkID:        ctx.NetworkID,
		BlockchainID:     ctx.XChainID,
		AVAXAssetID:      feeAssetID,
		BaseTxFee:        cfg.TxFee,
		CreateAssetTxFee: cfg.CreateAssetTxFee,
	}

	return &serviceBackend{
		ctx:          backendCtx,
		xchainID:     ctx.XChainID,
		cfg:          cfg,
		state:        state,
		sharedMemory: sharedMemory,
		codec:        codec,
	}
}

type serviceBackend struct {
	ctx          *builder.Context
	xchainID     ids.ID
	cfg          *config.Config
	addrs        set.Set[ids.ShortID]
	state        state.State
	sharedMemory atomic.SharedMemory
	codec        codec.Manager
}

func (b *serviceBackend) State() state.State {
	return b.state
}

func (b *serviceBackend) Config() *config.Config {
	return b.cfg
}

func (b *serviceBackend) Codec() codec.Manager {
	return b.codec
}

func (b *serviceBackend) Context() *builder.Context {
	return b.ctx
}

func (b *serviceBackend) ResetAddresses(addrs set.Set[ids.ShortID]) {
	b.addrs = addrs
}

func (b *serviceBackend) UTXOs(_ context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	if sourceChainID == b.xchainID {
		return avax.GetAllUTXOs(b.state, b.addrs)
	}

	atomicUTXOs, _, _, err := avax.GetAtomicUTXOs(
		b.sharedMemory,
		b.codec,
		sourceChainID,
		b.addrs,
		ids.ShortEmpty,
		ids.Empty,
		int(maxPageSize),
	)
	return atomicUTXOs, err
}

func (b *serviceBackend) GetUTXO(_ context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
	if chainID == b.xchainID {
		return b.state.GetUTXO(utxoID)
	}

	atomicUTXOs, _, _, err := avax.GetAtomicUTXOs(
		b.sharedMemory,
		b.codec,
		chainID,
		b.addrs,
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
