// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extrastest

import (
	"fmt"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/params/paramstest"
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
