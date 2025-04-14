// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
)

func TestUpgrade(t *testing.T) {
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	avalancheGoExecPath            string
	avalancheGoExecPathToUpgradeTo string
	collectorVars                  *flags.CollectorVars
	checkMonitoring                bool
)

func init() {
	flag.StringVar(
		&avalancheGoExecPath,
		"avalanchego-path",
		"",
		"avalanchego executable path",
	)
	flag.StringVar(
		&avalancheGoExecPathToUpgradeTo,
		"avalanchego-path-to-upgrade-to",
		"",
		"avalanchego executable path to upgrade to",
	)
	e2e.SetMonitoringFlags(
		&collectorVars,
		&checkMonitoring,
	)
}

var _ = ginkgo.Describe("[Upgrade]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("can upgrade versions", func() {
		network := tmpnet.NewDefaultNetwork("avalanchego-upgrade")

		network.DefaultRuntimeConfig = tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				AvalancheGoPath: avalancheGoExecPath,
			},
		}

		// Get the default genesis so we can modify it
		genesis, err := network.DefaultGenesis()
		require.NoError(err)
		network.Genesis = genesis

		shutdownDelay := 0 * time.Second
		if collectorVars.StartCollectors {
			require.NoError(tmpnet.StartCollectors(tc.DefaultContext(), tc.Log()))
			shutdownDelay = tmpnet.NetworkShutdownDelay // Ensure a final metrics scrape
		}
		if checkMonitoring {
			// Since cleanups are run in LIFO order, adding this cleanup before
			// StartNetwork is called ensures network shutdown will be called first.
			tc.DeferCleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
				defer cancel()
				require.NoError(tmpnet.CheckMonitoring(ctx, tc.Log(), network.UUID))
			})
		}

		e2e.StartNetwork(
			tc,
			network,
			"", /* rootNetworkDir */
			shutdownDelay,
			e2e.EmptyNetworkCmd,
		)

		tc.By(fmt.Sprintf("restarting all nodes with %q binary", avalancheGoExecPathToUpgradeTo))
		for _, node := range network.Nodes {
			tc.By(fmt.Sprintf("restarting node %q with %q binary", node.NodeID, avalancheGoExecPathToUpgradeTo))
			require.NoError(node.Stop(tc.DefaultContext()))

			node.RuntimeConfig = &tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					AvalancheGoPath: avalancheGoExecPathToUpgradeTo,
				},
			}

			require.NoError(network.StartNode(tc.DefaultContext(), node))

			tc.By(fmt.Sprintf("waiting for node %q to report healthy after restart", node.NodeID))
			e2e.WaitForHealthy(tc, node)
		}

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
