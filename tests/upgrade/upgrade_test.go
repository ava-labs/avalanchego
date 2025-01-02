// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"flag"
	"fmt"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

func TestUpgrade(t *testing.T) {
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	avalancheGoExecPath            string
	avalancheGoExecPathToUpgradeTo string
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
}

var _ = ginkgo.Describe("[Upgrade]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("can upgrade versions", func() {
		network := tmpnet.NewDefaultNetwork(tc.Log(), "avalanchego-upgrade")

		// Get the default genesis so we can modify it
		genesis, err := network.DefaultGenesis()
		require.NoError(err)
		network.Genesis = genesis

		// Use a logger with full details for network setup since there
		// won't be any ginkgo logs with timestamps to provide context.
		// TODO(marun) Maybe make this internal to StartNetwork?
		log, err := tests.LoggerForFormat("", "auto")
		require.NoError(err)

		e2e.StartNetwork(
			tc,
			log,
			network,
			&tmpnet.NodeRuntimeConfig{
				AvalancheGoPath: avalancheGoExecPath,
			},
			"",    /* pluginDir */
			0,     /* shutdownDelay */
			false, /* skipShutdown */
			false, /* reuseNetwork */
		)

		tc.By(fmt.Sprintf("restarting all nodes with %q binary", avalancheGoExecPathToUpgradeTo))
		for _, node := range network.Nodes {
			tc.By(fmt.Sprintf("restarting node %q with %q binary", node.NodeID, avalancheGoExecPathToUpgradeTo))
			require.NoError(node.Stop(tc.DefaultContext()))

			node.RuntimeConfig.AvalancheGoPath = avalancheGoExecPathToUpgradeTo

			require.NoError(network.StartNode(tc.DefaultContext(), node))

			tc.By(fmt.Sprintf("waiting for node %q to report healthy after restart", node.NodeID))
			e2e.WaitForHealthy(tc, node)
		}

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
