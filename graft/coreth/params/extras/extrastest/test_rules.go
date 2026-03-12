// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extrastest

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"

	evmextras "github.com/ava-labs/avalanchego/graft/evm/params/extras"
)

func ForkToRules(fork upgradetest.Fork) *evmextras.Rules {
	chainConfig, ok := paramstest.ForkToChainConfig[fork]
	if !ok {
		panic(fmt.Sprintf("unknown fork: %s", fork))
	}
	return params.GetRulesExtra(chainConfig.Rules(common.Big0, params.IsMergeTODO, 0))
}

func ForkToAvalancheRules(fork upgradetest.Fork) evmextras.AvalancheRules {
	networkUpgrades := extras.GetNetworkUpgrades(upgradetest.GetConfig(fork))
	return networkUpgrades.GetAvalancheRules(uint64(upgrade.InitiallyActiveTime.Unix()))
}
