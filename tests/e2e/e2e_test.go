// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/tests/e2e/vms"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

func TestE2E(t *testing.T) {
	evm.RegisterAllLibEVMExtras()
	ginkgo.RunSpecs(t, "e2e test suites")
}

const suiteFailureMarkerFileName = ".e2e-spec-failure"

var (
	flagVars               *e2e.FlagVars
	runSuiteBootstrapCheck bool
	suiteFailureMarkerPath string
)

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

	upgrades := tmpnet.UpgradeConfig(flagVars.ActivateLatestAfter())
	tc.Log().Info("setting upgrades",
		zap.Reflect("upgrades", upgrades),
	)

	defaultFlags, err := tmpnet.UpgradeFlags(upgrades)
	require.NoError(tc, err)
	defaultFlags.SetDefaults(tmpnet.DefaultE2EFlags())

	env := e2e.NewTestEnvironment(
		tc,
		flagVars,
		&tmpnet.Network{
			Owner:        flagVars.NetworkOwner(),
			DefaultFlags: defaultFlags,
			Nodes:        nodes,
			Subnets:      subnets,
		},
	)
	env.SuiteStateDir, err = os.MkdirTemp("", "avalanchego-e2e-")
	require.NoError(tc, err)
	tc.DeferCleanup(func() {
		require.NoError(tc, os.RemoveAll(env.SuiteStateDir))
	})
	return env.Marshal()
}, func(envBytes []byte) {
	// Run in every ginkgo process

	// Initialize the local test environment from the global state
	tc := e2e.NewTestContext()
	e2e.InitSharedTestEnvironment(tc, envBytes)

	suiteConfig, _ := ginkgo.GinkgoConfiguration()
	if suiteConfig.ParallelTotal <= 1 || os.Getenv(e2e.SkipBootstrapChecksEnvName) != "" {
		return
	}

	// Parallel tests can change the shared network at the same time. The suite
	// checks the shared network after all tests pass.
	runSuiteBootstrapCheck = true
	suiteFailureMarkerPath = filepath.Join(e2e.GetEnv(tc).SuiteStateDir, suiteFailureMarkerFileName)
})

var _ = ginkgo.ReportAfterEach(func(report ginkgo.SpecReport) {
	if !report.Failed() || !runSuiteBootstrapCheck {
		return
	}

	// The primary process uses this marker to avoid running a bootstrap check when
	// the suite has any failed specs since a failed spec may result in inconsistent
	// network state.
	tc := e2e.NewEventHandlerTestContext()
	require.NoError(tc, os.WriteFile(suiteFailureMarkerPath, nil, 0o600))
})

var _ = ginkgo.SynchronizedAfterSuite(func() {}, func() {
	if !runSuiteBootstrapCheck {
		return
	}

	tc := e2e.NewEventHandlerTestContext()
	_, err := os.Stat(suiteFailureMarkerPath)
	if err == nil {
		tc.Log().Info("skipping suite bootstrap check because one or more specs failed")
		return
	}
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(tc, err)
	}

	e2e.CheckBootstrapIsPossibleAfterParallelRun(tc, e2e.GetEnv(tc).GetNetwork())
})
