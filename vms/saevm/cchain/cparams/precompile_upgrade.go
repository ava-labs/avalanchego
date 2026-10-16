// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"slices"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
)

// GetActivePrecompileConfig returns the most recent precompile config for
// address that is active at timestamp, or nil if there is none.
//
// TODO(alarso16): Delete with precompile handling refactor.
func (c *ChainConfig) GetActivePrecompileConfig(address common.Address, timestamp uint64) precompileconfig.Config {
	module, ok := modules.GetPrecompileModuleByAddress(address)
	if !ok {
		return nil
	}
	for _, upgrade := range slices.Backward(c.PrecompileConfigs) {
		if upgrade.Key() == module.ConfigKey && isTimestampForked(upgrade.Timestamp(), timestamp) {
			return upgrade
		}
	}
	return nil
}
