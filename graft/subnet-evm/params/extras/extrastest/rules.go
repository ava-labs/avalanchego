// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extrastest

import (
	"github.com/MetalBlockchain/metalgo/graft/subnet-evm/params/extras"
	"github.com/MetalBlockchain/metalgo/upgrade"
	"github.com/MetalBlockchain/metalgo/upgrade/upgradetest"
)

func ForkToAvalancheRules(fork upgradetest.Fork) extras.AvalancheRules {
	networkUpgrades := extras.GetNetworkUpgrades(upgradetest.GetConfig(fork))
	return networkUpgrades.GetAvalancheRules(uint64(upgrade.InitiallyActiveTime.Unix()))
}
