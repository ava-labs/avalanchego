// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

func newContext(
	ctx *snow.Context,
	cfg *config.Config,
	timestamp time.Time,
) *builder.Context {
	return &builder.Context{
		NetworkID:                     ctx.NetworkID,
		AVAXAssetID:                   ctx.AVAXAssetID,
		BaseTxFee:                     cfg.StaticFeeConfig.TxFee,
		CreateSubnetTxFee:             cfg.StaticFeeConfig.GetCreateSubnetTxFee(cfg.UpgradeConfig, timestamp),
		TransformSubnetTxFee:          cfg.StaticFeeConfig.TransformSubnetTxFee,
		CreateBlockchainTxFee:         cfg.StaticFeeConfig.GetCreateBlockchainTxFee(cfg.UpgradeConfig, timestamp),
		AddPrimaryNetworkValidatorFee: cfg.StaticFeeConfig.AddPrimaryNetworkValidatorFee,
		AddPrimaryNetworkDelegatorFee: cfg.StaticFeeConfig.AddPrimaryNetworkDelegatorFee,
		AddSubnetValidatorFee:         cfg.StaticFeeConfig.AddSubnetValidatorFee,
		AddSubnetDelegatorFee:         cfg.StaticFeeConfig.AddSubnetDelegatorFee,
	}
}
