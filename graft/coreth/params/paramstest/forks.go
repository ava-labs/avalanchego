// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package paramstest

import (
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
)

var ForkToChainConfig = map[upgradetest.Fork]*params.ChainConfig{
	upgradetest.NoUpgrades:        params.TestLaunchConfig,
	upgradetest.ApricotPhase1:     params.TestApricotPhase1Config,
	upgradetest.ApricotPhase2:     params.TestApricotPhase2Config,
	upgradetest.ApricotPhase3:     params.TestApricotPhase3Config,
	upgradetest.ApricotPhase4:     params.TestApricotPhase4Config,
	upgradetest.ApricotPhase5:     params.TestApricotPhase5Config,
	upgradetest.ApricotPhasePre6:  params.TestApricotPhasePre6Config,
	upgradetest.ApricotPhase6:     params.TestApricotPhase6Config,
	upgradetest.ApricotPhasePost6: params.TestApricotPhasePost6Config,
	upgradetest.Banff:             params.TestBanffChainConfig,
	upgradetest.Cortina:           params.TestCortinaChainConfig,
	upgradetest.Durango:           params.TestDurangoChainConfig,
	upgradetest.Etna:              params.TestEtnaChainConfig,
	upgradetest.Fortuna:           params.TestFortunaChainConfig,
	upgradetest.Granite:           params.TestGraniteChainConfig,
	upgradetest.Helicon:           params.TestHeliconChainConfig,
}
