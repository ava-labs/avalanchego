// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"testing"

	"github.com/ava-labs/coreth/core"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade"
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
		network := tmpnet.NewDefaultNetwork("avalanchego-upgrade")

		{
			// Get the default genesis so we can modify it
			genesis, err := network.DefaultGenesis()
			require.NoError(err)
			network.Genesis = genesis
			// Etna enables Cancun which modifies the outcome of the C-Chain genesis
			// This is because of new header fields that modify the genesis block hash.
			// This code can be removed once the Etna upgrade is activated.
			cChainGenesis := new(core.Genesis)
			cChainGenesisStr := network.Genesis.CChainGenesis
			require.NoError(json.Unmarshal([]byte(cChainGenesisStr), cChainGenesis))
			unscheduledActivationTime := uint64(upgrade.UnscheduledActivationTime.Unix())
			cChainGenesis.Config.EtnaTimestamp = &unscheduledActivationTime
			cChainGenesisBytes, err := json.Marshal(cChainGenesis)
			require.NoError(err)
			network.Genesis.CChainGenesis = string(cChainGenesisBytes)
		}

		// Previous version does not have unactivated upgrades.
		// The default upgrade config usually sets the latest upgrade to be activated
		// Use unscheduled
		upgradeJSON, err := json.Marshal(upgrade.LatestUnscheduled)
		require.NoError(err)
		upgradeBase64 := base64.StdEncoding.EncodeToString(upgradeJSON)
		network.DefaultFlags[config.UpgradeFileContentKey] = upgradeBase64

		e2e.StartNetwork(tc, network, avalancheGoExecPath, "" /* pluginDir */, 0 /* shutdownDelay */, false /* reuseNetwork */)

		tc.By(fmt.Sprintf("restarting all nodes with %q binary", avalancheGoExecPathToUpgradeTo))
		for _, node := range network.Nodes {
			tc.By(fmt.Sprintf("restarting node %q with %q binary", node.NodeID, avalancheGoExecPathToUpgradeTo))
			require.NoError(node.Stop(tc.DefaultContext()))

			node.RuntimeConfig.AvalancheGoPath = avalancheGoExecPathToUpgradeTo

			require.NoError(network.StartNode(tc.DefaultContext(), tc.GetWriter(), node))

			tc.By(fmt.Sprintf("waiting for node %q to report healthy after restart", node.NodeID))
			e2e.WaitForHealthy(tc, node)
		}

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
