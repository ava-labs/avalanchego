// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/set"

	"github.com/ava-labs/subnet-evm/tests"
	"github.com/ava-labs/subnet-evm/tests/utils"
)

const (
	// The load test requires 5 nodes
	nodeCount = 5

	subnetAName = "load-subnet-a"
)

var (
	flagVars     *e2e.FlagVars
	repoRootPath = tests.GetRepoRootPath("tests/load")
)

func init() {
	// Configures flags used to configure tmpnet
	flagVars = e2e.RegisterFlags()
}

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "subnet-evm small load simulator test suite")
}

var _ = ginkgo.Describe("[Load Simulator]", ginkgo.Ordered, func() {
	require := require.New(ginkgo.GinkgoT())

	var env *e2e.TestEnvironment

	ginkgo.BeforeAll(func() {
		genesisPath := filepath.Join(repoRootPath, "tests/load/genesis/genesis.json")

		// The load tests are flaky at high levels of evm logging, so leave it at
		// the default level instead of raising it to debug (as the warp testing does).
		chainConfig := tmpnet.FlagsMap{}

		nodes := utils.NewTmpnetNodes(nodeCount)

		env = e2e.NewTestEnvironment(
			flagVars,
			utils.NewTmpnetNetwork(
				nodes,
				tmpnet.FlagsMap{
					// The default tmpnet log level (debug) induces too much overhead for load testing.
					config.LogLevelKey: "info",
				},
				utils.NewTmpnetSubnet(subnetAName, genesisPath, chainConfig, nodes...),
			),
		)
	})

	ginkgo.It("basic subnet load test", ginkgo.Label("load"), func() {
		network := env.GetNetwork()

		subnet := network.GetSubnet(subnetAName)
		require.NotNil(subnet)
		blockchainID := subnet.Chains[0].ChainID

		nodeURIs := tmpnet.GetNodeURIs(network.Nodes)
		validatorIDs := set.NewSet[ids.NodeID](len(subnet.ValidatorIDs))
		validatorIDs.Add(subnet.ValidatorIDs...)
		rpcEndpoints := make([]string, 0, len(nodeURIs))
		for _, nodeURI := range nodeURIs {
			if !validatorIDs.Contains(nodeURI.NodeID) {
				continue
			}
			rpcEndpoints = append(rpcEndpoints, fmt.Sprintf("%s/ext/bc/%s/rpc", nodeURI.URI, blockchainID))
		}
		commaSeparatedRPCEndpoints := strings.Join(rpcEndpoints, ",")
		err := os.Setenv("RPC_ENDPOINTS", commaSeparatedRPCEndpoints)
		require.NoError(err)

		log.Info("Running load simulator...", "rpcEndpoints", commaSeparatedRPCEndpoints)
		cmd := exec.Command("./scripts/run_simulator.sh")
		cmd.Dir = repoRootPath
		log.Info("Running load simulator script", "cmd", cmd.String())

		out, err := cmd.CombinedOutput()
		fmt.Printf("\nCombined output:\n\n%s\n", string(out))
		require.NoError(err)
	})
})
