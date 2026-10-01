// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/params/paramstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

func TestGetNetworkUpgrades(t *testing.T) {
	for fork := upgradetest.NoUpgrades; fork <= upgradetest.Latest; fork++ {
		t.Run(fork.String(), func(t *testing.T) {
			upgrades := getNetworkUpgrades(upgradetest.GetConfig(fork))
			got := upgrades.GetAvalancheRules(uint64(upgrade.InitiallyActiveTime.Unix()))
			require.Equal(t, paramstest.ForkToAvalancheRules(fork), got)
		})
	}
}
