// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package paramstest

import (
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

// ForkToChainConfig maps a fork to a chain config
var ForkToChainConfig = map[upgradetest.Fork]*params.ChainConfig{
	upgradetest.ApricotPhase5: params.TestPreSubnetEVMChainConfig,
	upgradetest.ApricotPhase6: params.TestSubnetEVMChainConfig,
	upgradetest.Durango:       params.TestDurangoChainConfig,
	upgradetest.Etna:          params.TestEtnaChainConfig,
	upgradetest.Fortuna:       params.TestFortunaChainConfig,
	upgradetest.Granite:       params.TestGraniteChainConfig,
	upgradetest.Helicon:       params.TestHeliconChainConfig,
}
