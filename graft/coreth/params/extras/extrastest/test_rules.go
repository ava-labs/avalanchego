// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extrastest

import (
	"fmt"

	"github.com/MetalBlockchain/libevm/common"

	"github.com/MetalBlockchain/metalgo/graft/coreth/params"
	"github.com/MetalBlockchain/metalgo/graft/coreth/params/extras"
	"github.com/MetalBlockchain/metalgo/graft/coreth/params/paramstest"
	"github.com/MetalBlockchain/metalgo/upgrade"
	"github.com/MetalBlockchain/metalgo/upgrade/upgradetest"
)

func ForkToRules(fork upgradetest.Fork) *extras.Rules {
	chainConfig, ok := paramstest.ForkToChainConfig[fork]
	if !ok {
		panic(fmt.Sprintf("unknown fork: %s", fork))
	}
	return params.GetRulesExtra(chainConfig.Rules(common.Big0, params.IsMergeTODO, 0))
}

func ForkToAvalancheRules(fork upgradetest.Fork) extras.AvalancheRules {
	networkUpgrades := extras.GetNetworkUpgrades(upgradetest.GetConfig(fork))
	return networkUpgrades.GetAvalancheRules(uint64(upgrade.InitiallyActiveTime.Unix()))
}
