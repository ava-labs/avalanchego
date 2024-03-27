// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"

	walletbuilder "github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

func NewContext(
	ctx *snow.Context,
	cfg *config.Config,
	timestamp time.Time,
) *walletbuilder.Context {
	return &walletbuilder.Context{
		NetworkID:                     ctx.NetworkID,
		AVAXAssetID:                   ctx.AVAXAssetID,
		BaseTxFee:                     cfg.TxFee,
		CreateSubnetTxFee:             cfg.GetCreateSubnetTxFee(timestamp),
		TransformSubnetTxFee:          cfg.TransformSubnetTxFee,
		CreateBlockchainTxFee:         cfg.GetCreateBlockchainTxFee(timestamp),
		AddPrimaryNetworkValidatorFee: cfg.AddPrimaryNetworkValidatorFee,
		AddPrimaryNetworkDelegatorFee: cfg.AddPrimaryNetworkDelegatorFee,
		AddSubnetValidatorFee:         cfg.AddSubnetValidatorFee,
		AddSubnetDelegatorFee:         cfg.AddSubnetDelegatorFee,
	}
}
