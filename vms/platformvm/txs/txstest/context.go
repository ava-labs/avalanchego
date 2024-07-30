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
	feeConfig := cfg.StaticFeeConfig
	if !cfg.UpgradeConfig.IsApricotPhase3Activated(timestamp) {
		feeConfig.CreateSubnetTxFee = cfg.CreateAssetTxFee
		feeConfig.CreateBlockchainTxFee = cfg.CreateAssetTxFee
	}

	return &builder.Context{
		NetworkID:       ctx.NetworkID,
		AVAXAssetID:     ctx.AVAXAssetID,
		StaticFeeConfig: feeConfig,
	}
}
