// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	// ensure test packages are scanned by ginkgo
	_ "github.com/ava-labs/avalanchego/tests/e2e/banff"
	_ "github.com/ava-labs/avalanchego/tests/e2e/c"
	_ "github.com/ava-labs/avalanchego/tests/e2e/faultinjection"
	_ "github.com/ava-labs/avalanchego/tests/e2e/p"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x/transfer"

	"github.com/ava-labs/avalanchego/tests/e2e/vms"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm"
)

func TestE2E(t *testing.T) {
	libevm.RegisterExtras()
	ginkgo.RunSpecs(t, "e2e test suites")
}

const (
	suiteFailureMarkerFileName  = ".e2e-spec-failure"
	suiteEligibleMarkerFileName = ".e2e-shared-network-bootstrap-eligible"
)

var (
	flagVars                *e2e.FlagVars
	bootstrapChecksEnabled  bool
	runSuiteBootstrapCheck  bool
	suiteFailureMarkerPath  string
	suiteEligibleMarkerPath string
)

func eligibleForSharedNetworkBootstrapCheck(report ginkgo.SpecReport) bool {
	return report.State == types.SpecStatePassed &&
		!slices.Contains(report.Labels(), e2e.SkipSharedNetworkBootstrapCheckLabel)
}

func init() {
	flagVars = e2e.RegisterFlags(e2e.WithDefaultOwner("avalanchego-e2e"))
}

// Start the shared network once and give every process its path. Parallel runs
// also get a directory for coordination, which suite cleanup removes.
var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	tc := e2e.NewEventHandlerTestContext()

	nodeCount, err := flagVars.NodeCount()
	require.NoError(tc, err)
	nodes := tmpnet.NewNodesOrPanic(nodeCount)
	subnets := vms.XSVMSubnetsOrPanic(nodes...)

	upgrades := tmpnet.UpgradeConfig(flagVars.ActivateLatestAfter())
	tc.Log().Info(
		"setting upgrades",
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
	suiteConfig, _ := ginkgo.GinkgoConfiguration()
	if suiteConfig.ParallelTotal > 1 && os.Getenv(e2e.SkipBootstrapChecksEnvName) == "" {
		env.SuiteStateDir, err = os.MkdirTemp("", "avalanchego-e2e-")
		require.NoError(tc, err)
		tc.DeferCleanup(func() {
			require.NoError(tc, os.RemoveAll(env.SuiteStateDir))
		})
	}
	return env.Marshal()
}, func(envBytes []byte) {
	// Initialize each Ginkgo process from the shared setup data.
	tc := e2e.NewTestContext()
	e2e.InitSharedTestEnvironment(tc, envBytes)

	bootstrapChecksEnabled = os.Getenv(e2e.SkipBootstrapChecksEnvName) == ""
	suiteConfig, _ := ginkgo.GinkgoConfiguration()
	runSuiteBootstrapCheck = bootstrapChecksEnabled && suiteConfig.ParallelTotal > 1
	if runSuiteBootstrapCheck {
		suiteDir := e2e.GetEnv(tc).SuiteStateDir
		suiteFailureMarkerPath = filepath.Join(suiteDir, suiteFailureMarkerFileName)
		suiteEligibleMarkerPath = filepath.Join(suiteDir, suiteEligibleMarkerFileName)
	}
})

// In serial runs, check the shared network after each successful eligible test.
// Parallel tests can change that network during the check, so they wait for the suite check.
var _ = ginkgo.AfterEach(func() {
	if !bootstrapChecksEnabled {
		return
	}
	suiteConfig, _ := ginkgo.GinkgoConfiguration()
	if suiteConfig.ParallelTotal > 1 || !eligibleForSharedNetworkBootstrapCheck(ginkgo.CurrentSpecReport()) {
		return
	}

	tc := e2e.NewTestContext()
	e2e.CheckBootstrapIsPossible(tc, e2e.GetEnv(tc).GetNetwork())
})

// Record failed and eligible tests across parallel processes. The suite check
// needs both results, but Ginkgo does not provide them to SynchronizedAfterSuite.
var _ = ginkgo.ReportAfterEach(func(report ginkgo.SpecReport) {
	if !runSuiteBootstrapCheck {
		return
	}

	tc := e2e.NewEventHandlerTestContext()
	if report.Failed() {
		// The final check must not add a failure to an already failing suite.
		require.NoError(tc, perms.WriteFile(suiteFailureMarkerPath, nil, perms.ReadWrite))
	} else if eligibleForSharedNetworkBootstrapCheck(report) {
		require.NoError(tc, perms.WriteFile(suiteEligibleMarkerPath, nil, perms.ReadWrite))
	}
})

// After every process finishes, process 1 checks the shared network once if an
// eligible test passed and no test failed. The network is still running here.
var _ = ginkgo.SynchronizedAfterSuite(func() {}, func() {
	if !runSuiteBootstrapCheck {
		return
	}

	tc := e2e.NewEventHandlerTestContext()
	_, err := os.Stat(suiteFailureMarkerPath)
	if err == nil {
		tc.Log().Info("skipping suite bootstrap check because one or more tests failed")
		return
	}
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(tc, err)
	}

	_, err = os.Stat(suiteEligibleMarkerPath)
	if errors.Is(err, os.ErrNotExist) {
		tc.Log().Info("skipping suite bootstrap check because no eligible test ran")
		return
	}
	require.NoError(tc, err)
	e2e.CheckBootstrapIsPossible(tc, e2e.GetEnv(tc).GetNetwork())
})
