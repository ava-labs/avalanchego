// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchaintest

import (
	"time"

	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/libevmextras"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
)

// ChainConfig returns a new C-Chain config with all network upgrades active.
func ChainConfig() *params.ChainConfig {
	c := *saetest.ChainConfig()
	u := upgradetest.GetConfigWithUpgradeTime(upgradetest.Latest, time.Unix(0, 0))
	return libevmextras.WithChainConfig(&c, libevmextras.FromUpgrades(u))
}
