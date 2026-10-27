// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchaintest

import (
	"time"

	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
)

// ChainConfig returns a new C-Chain config with all network upgrades active.
func ChainConfig() *params.ChainConfig {
	c := *saetest.ChainConfig()
	u := upgradetest.GetConfigWithUpgradeTime(upgradetest.Latest, time.Unix(0, 0))
	extra := extras.ChainConfig{
		NetworkUpgrades: extras.GetNetworkUpgrades(u),
	}
	return corethparams.WithExtra(&c, &extra)
}
