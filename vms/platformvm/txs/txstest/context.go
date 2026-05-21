// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/vms/components/gas"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/config"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/state"
	"github.com/MetalBlockchain/metalgo/wallet/chain/p/builder"
)

func newContext(
	ctx *snow.Context,
	config *config.Internal,
	state *state.State,
) *builder.Context {
	builderContext := &builder.Context{
		NetworkID:   ctx.NetworkID,
		AVAXAssetID: ctx.AVAXAssetID,
	}

	builderContext.ComplexityWeights = config.DynamicFeeConfig.Weights
	builderContext.GasPrice = gas.CalculatePrice(
		config.DynamicFeeConfig.MinPrice,
		state.GetFeeState().Excess,
		config.DynamicFeeConfig.ExcessConversionConstant,
	)

	return builderContext
}
