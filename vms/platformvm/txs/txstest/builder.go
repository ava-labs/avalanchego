// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/p/signer"
)

func NewBuilder(
	ctx *snow.Context,
	cfg *config.Config,
	state state.State,
) *Builder {
	return &Builder{
		ctx:   ctx,
		cfg:   cfg,
		state: state,
	}
}

type Builder struct {
	ctx   *snow.Context
	cfg   *config.Config
	state state.State
}

func (b *Builder) Builders(keys ...*secp256k1.PrivateKey) (builder.Builder, signer.Signer) {
	var (
		kc      = secp256k1fx.NewKeychain(keys...)
		addrs   = kc.Addresses()
		backend = newBackend(addrs, b.state, b.ctx.SharedMemory)

		timestamp = b.state.GetTimestamp()
		context   = &builder.Context{
			NetworkID:                     b.ctx.NetworkID,
			AVAXAssetID:                   b.ctx.AVAXAssetID,
			BaseTxFee:                     b.cfg.TxFee,
			CreateSubnetTxFee:             b.cfg.GetCreateSubnetTxFee(timestamp),
			TransformSubnetTxFee:          b.cfg.TransformSubnetTxFee,
			CreateBlockchainTxFee:         b.cfg.GetCreateBlockchainTxFee(timestamp),
			AddPrimaryNetworkValidatorFee: b.cfg.AddPrimaryNetworkValidatorFee,
			AddPrimaryNetworkDelegatorFee: b.cfg.AddPrimaryNetworkDelegatorFee,
			AddSubnetValidatorFee:         b.cfg.AddSubnetValidatorFee,
			AddSubnetDelegatorFee:         b.cfg.AddSubnetDelegatorFee,
		}
	)

	return builder.New(addrs, context, backend), signer.New(kc, backend)
}
