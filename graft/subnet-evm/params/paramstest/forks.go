// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package paramstest

import (
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/upgrade"
)

// ForkToChainConfig maps a fork to a chain config
var ForkToChainConfig = map[upgrade.Upgrade]*params.ChainConfig{
	upgrade.ApricotPhase5: params.TestPreSubnetEVMChainConfig,
	upgrade.ApricotPhase6: params.TestSubnetEVMChainConfig,
	upgrade.Durango:       params.TestDurangoChainConfig,
	upgrade.Etna:          params.TestEtnaChainConfig,
	upgrade.Fortuna:       params.TestFortunaChainConfig,
	upgrade.Granite:       params.TestGraniteChainConfig,
	upgrade.Helicon:       params.TestHeliconChainConfig,
	upgrade.Igloo:         params.TestIglooChainConfig,
}
