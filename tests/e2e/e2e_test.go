// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	// ensure test packages are scanned by ginkgo
	_ "github.com/ava-labs/avalanchego/tests/e2e/banff"
	_ "github.com/ava-labs/avalanchego/tests/e2e/c"
	_ "github.com/ava-labs/avalanchego/tests/e2e/etna"
	_ "github.com/ava-labs/avalanchego/tests/e2e/faultinjection"
	_ "github.com/ava-labs/avalanchego/tests/e2e/p"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x/transfer"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/e2e/vms"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade"

	ginkgo "github.com/onsi/ginkgo/v2"
)

func TestE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "e2e test suites")
}

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlags()
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	tc := e2e.NewTestContext()

	nodes := tmpnet.NewNodesOrPanic(flagVars.NodeCount())
	subnets := vms.XSVMSubnetsOrPanic(nodes...)

	upgrades := upgrade.Default
	if flagVars.ActivateEtna() {
		upgrades.EtnaTime = upgrade.InitiallyActiveTime
	} else {
		upgrades.EtnaTime = upgrade.UnscheduledActivationTime
	}

	upgradeJSON, err := json.Marshal(upgrades)
	require.NoError(tc, err)

	upgradeBase64 := base64.StdEncoding.EncodeToString(upgradeJSON)
	return e2e.NewTestEnvironment(
		tc,
		flagVars,
		&tmpnet.Network{
			Owner: "avalanchego-e2e",
			DefaultFlags: tmpnet.FlagsMap{
				config.UpgradeFileContentKey: upgradeBase64,
			},
			Nodes:   nodes,
			Subnets: subnets,
		},
	).Marshal()
}, func(envBytes []byte) {
	// Run in every ginkgo process

	// Initialize the local test environment from the global state
	e2e.InitSharedTestEnvironment(ginkgo.GinkgoT(), envBytes)
})
