// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

func newContext(
	ctx *snow.Context,
	cfg *config.Config,
	timestamp time.Time,
) (*builder.Context, error) {
	var (
		staticFeesCfg  = cfg.StaticConfig
		upgrades       = cfg.Times
		staticFeeCalc  = fee.NewStaticCalculator(staticFeesCfg, upgrades, timestamp)
		createSubnetTx = &txs.CreateSubnetTx{}
		createChainTx  = &txs.CreateChainTx{}
	)
	if err := createSubnetTx.Visit(staticFeeCalc); err != nil {
		return nil, err
	}
	createSubnetFee := staticFeeCalc.Fee

	if err := createChainTx.Visit(staticFeeCalc); err != nil {
		return nil, err
	}
	createChainFee := staticFeeCalc.Fee

	return &builder.Context{
		NetworkID:                     ctx.NetworkID,
		AVAXAssetID:                   ctx.AVAXAssetID,
		BaseTxFee:                     staticFeesCfg.TxFee,
		CreateSubnetTxFee:             createSubnetFee,
		TransformSubnetTxFee:          staticFeesCfg.TransformSubnetTxFee,
		CreateBlockchainTxFee:         createChainFee,
		AddPrimaryNetworkValidatorFee: staticFeesCfg.AddPrimaryNetworkValidatorFee,
		AddPrimaryNetworkDelegatorFee: staticFeesCfg.AddPrimaryNetworkDelegatorFee,
		AddSubnetValidatorFee:         staticFeesCfg.AddSubnetValidatorFee,
		AddSubnetDelegatorFee:         staticFeesCfg.AddSubnetDelegatorFee,
	}, nil
}
