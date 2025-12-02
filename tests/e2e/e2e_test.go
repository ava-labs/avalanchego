// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	// ensure test packages are scanned by ginkgo
	_ "github.com/ava-labs/avalanchego/tests/e2e/banff"
	_ "github.com/ava-labs/avalanchego/tests/e2e/c"
	_ "github.com/ava-labs/avalanchego/tests/e2e/faultinjection"
	_ "github.com/ava-labs/avalanchego/tests/e2e/p"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x/transfer"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/e2e/s"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/tests/e2e/vms"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

func TestE2E(t *testing.T) {
	evm.RegisterAllLibEVMExtras()
	ginkgo.RunSpecs(t, "e2e test suites")
}

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlags(e2e.WithDefaultOwner("avalanchego-e2e"))
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	tc := e2e.NewEventHandlerTestContext()

	nodeCount, err := flagVars.NodeCount()
	require.NoError(tc, err)
	nodes := tmpnet.NewNodesOrPanic(nodeCount)
	subnets := vms.XSVMSubnetsOrPanic(nodes...)
	simplexSubnets := s.SimplexSubnetsOrPanic(nodes...)
	upgradeToActivate := upgradetest.Latest
	if !flagVars.ActivateLatest() {
		upgradeToActivate--
	}
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
			Owner:        flagVars.NetworkOwner(),
			DefaultFlags: defaultFlags,
			Nodes:        nodes,
			Subnets:      subnets,
		},
	).Marshal()
}, func(envBytes []byte) {
	// Run in every ginkgo process

	// Initialize the local test environment from the global state
	e2e.InitSharedTestEnvironment(e2e.NewTestContext(), envBytes)
})
