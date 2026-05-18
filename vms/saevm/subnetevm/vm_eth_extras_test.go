// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

func TestEthGetActiveRulesAtWarpEnabledAtGenesis(t *testing.T) {
	sut := newSUT(t, withFork(upgradetest.Latest), withWarpEnabled())

	warpActivation := uint64(upgrade.InitiallyActiveTime.Unix())

	res, err := sut.client.GetActiveRulesAt(sut.ctx, &warpActivation)
	require.NoError(t, err)
	require.Contains(t, res.ActivePrecompiles, "warpConfig",
		"warpConfig should be present at the activation timestamp")
	require.Equal(t, warpActivation, res.ActivePrecompiles["warpConfig"].Timestamp,
		"warpConfig activation timestamp should round-trip")

	beforeWarp := warpActivation - 1
	res, err = sut.client.GetActiveRulesAt(sut.ctx, &beforeWarp)
	require.NoError(t, err)
	require.NotContains(t, res.ActivePrecompiles, "warpConfig",
		"warpConfig should NOT be present strictly before the activation timestamp")
}

func TestEthGetActiveRulesAtNilDefaultsToCurrentHeader(t *testing.T) {
	sut := newSUT(t, withFork(upgradetest.Latest), withWarpEnabled())

	currentHeaderTime := sut.vm.GethRPCBackends().CurrentHeader().Time

	resNil, err := sut.client.GetActiveRulesAt(sut.ctx, nil)
	require.NoError(t, err)
	resExplicit, err := sut.client.GetActiveRulesAt(sut.ctx, &currentHeaderTime)
	require.NoError(t, err)

	require.Equal(t, resExplicit.EthRules, resNil.EthRules)
	require.Equal(t, resExplicit.AvalancheRules, resNil.AvalancheRules)
	require.Equal(t, resExplicit.ActivePrecompiles, resNil.ActivePrecompiles)
}
