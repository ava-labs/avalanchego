// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/modules"
)

// ConfigKey is the key used in json config files to specify this precompile config.
// must be unique across all precompiles.
const ConfigKey = "warpConfig"

// ContractAddress is the address of the warp precompile contract
var ContractAddress = common.HexToAddress("0x0200000000000000000000000000000000000005")

// Module is the precompile module. It is used to register the precompile contract.
var Module = modules.Module{
	ConfigKey: ConfigKey,
	Address:   ContractAddress,
	Contract:  WarpPrecompile,
}

func init() {
	// Register the precompile module.
	// Each precompile contract registers itself through [RegisterModule] function.
	if err := modules.RegisterModule(Module); err != nil {
		panic(err)
	}
}
