// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

func newContext(
	ctx *snow.Context,
	config *config.Internal,
	state state.State,
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
