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
	var (
		staticFeesCfg = cfg.StaticConfig
		upgrades      = cfg.Times
	)

	return &builder.Context{
		NetworkID:                     ctx.NetworkID,
		AVAXAssetID:                   ctx.AVAXAssetID,
		BaseTxFee:                     staticFeesCfg.TxFee,
		CreateSubnetTxFee:             staticFeesCfg.GetCreateSubnetTxFee(upgrades, timestamp),
		TransformSubnetTxFee:          staticFeesCfg.TransformSubnetTxFee,
		CreateBlockchainTxFee:         staticFeesCfg.GetCreateBlockchainTxFee(upgrades, timestamp),
		AddPrimaryNetworkValidatorFee: staticFeesCfg.AddPrimaryNetworkValidatorFee,
		AddPrimaryNetworkDelegatorFee: staticFeesCfg.AddPrimaryNetworkDelegatorFee,
		AddSubnetValidatorFee:         staticFeesCfg.AddSubnetValidatorFee,
		AddSubnetDelegatorFee:         staticFeesCfg.AddSubnetDelegatorFee,
	}
}
