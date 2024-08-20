// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

func newContext(
	ctx *snow.Context,
	config *config.Config,
	state state.State,
) *builder.Context {
	var (
		timestamp      = state.GetTimestamp()
		builderContext = &builder.Context{
			NetworkID:   ctx.NetworkID,
			AVAXAssetID: ctx.AVAXAssetID,
		}
	)
	switch {
	case config.UpgradeConfig.IsEtnaActivated(timestamp):
		builderContext.ComplexityWeights = config.DynamicFeeConfig.Weights
		builderContext.GasPrice = 10 * fee.CalculateGasPrice(
			config.DynamicFeeConfig.MinGasPrice,
			state.GetFeeState().Excess,
			config.DynamicFeeConfig.ExcessConversionConstant,
		)
	case config.UpgradeConfig.IsApricotPhase3Activated(timestamp):
		builderContext.StaticFeeConfig = config.StaticFeeConfig
	default:
		builderContext.StaticFeeConfig = config.StaticFeeConfig
		builderContext.StaticFeeConfig.CreateSubnetTxFee = config.CreateAssetTxFee
		builderContext.StaticFeeConfig.CreateBlockchainTxFee = config.CreateAssetTxFee
	}
	return builderContext
}
