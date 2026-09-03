// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

func TestE2E(t *testing.T) {
	evm.RegisterAllLibEVMExtras()
	ginkgo.RunSpecs(t, "warp e2e test suites")
}

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlags(e2e.WithDefaultOwner("avalanchego-e2e-warp"))
}

// We cannot use the tests/e2e_test.go file and the shared test environment because the warp test suite
// has validator set assumptions which are not guaranteed by other tests (p-chain) in tests/ package.
var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	tc := e2e.NewEventHandlerTestContext()

	nodeCount, err := flagVars.NodeCount()
	require.NoError(tc, err)
	nodes := tmpnet.NewNodesOrPanic(nodeCount)
	keys, err := tmpnet.NewPrivateKeys(tmpnet.DefaultPreFundedKeyCount)
	require.NoError(tc, err)
	subnets := SubnetEVMSubnetsOrPanic(keys, nodes...)

	upgradeToActivate := upgradetest.Helicon
	upgrades := upgradetest.GetConfig(upgradeToActivate)
	upgrades.GraniteEpochDuration = 4 * time.Second
	tc.Log().Info("setting upgrades",
		zap.Reflect("upgrades", upgrades),
	)

	upgradeJSON, err := json.Marshal(upgrades)
	require.NoError(tc, err)

	upgradeBase64 := base64.StdEncoding.EncodeToString(upgradeJSON)

	defaultFlags := tmpnet.FlagsMap{
		config.UpgradeFileContentKey: upgradeBase64,
		// Ensure a min stake duration compatible with testing staking logic
		config.MinStakeDurationKey: "1s",
	}
	defaultFlags.SetDefaults(tmpnet.DefaultE2EFlags())

	return e2e.NewTestEnvironment(
		tc,
		flagVars,
		&tmpnet.Network{
			Owner:         flagVars.NetworkOwner(),
			DefaultFlags:  defaultFlags,
			Nodes:         nodes,
			Subnets:       subnets,
			PreFundedKeys: keys,
		},
	).Marshal()
}, func(envBytes []byte) {
	// Run in every ginkgo process

	// Initialize the local test environment from the global state
	e2e.InitSharedTestEnvironment(e2e.NewTestContext(), envBytes)
})
