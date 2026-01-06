// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ava-labs/libevm/log"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/tests"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/tests/utils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/set"

	ginkgo "github.com/onsi/ginkgo/v2"
)

const (
	// The load test requires 5 nodes
	nodeCount = 5

	subnetAName = "load-subnet-a"
)

var (
	flagVars     *e2e.FlagVars
	repoRootPath string
)

func init() {
	// Configures flags used to configure tmpnet
	flagVars = e2e.RegisterFlags()
	flag.StringVar(
		&repoRootPath,
		"repo-root",
		"",
		"absolute path to the repository root (required if scripts cannot be found via relative paths)",
	)
}

func TestE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "subnet-evm small load simulator test suite")
}

var _ = ginkgo.Describe("[Load Simulator]", ginkgo.Ordered, func() {
	require := require.New(ginkgo.GinkgoT())

	var (
		env        *e2e.TestEnvironment
		scriptPath string
	)

	ginkgo.BeforeAll(func() {
		tc := e2e.NewTestContext()

		scriptPath = filepath.Join("..", "..", "scripts", "run_simulator.sh")
		if _, err := os.Stat(scriptPath); err != nil {
			if repoRootPath != "" {
				scriptPath = filepath.Join(repoRootPath, "scripts", "run_simulator.sh")
			}
			require.NoError(err, "failed to locate run_simulator.sh - try specifying -repo-root flag")
		}

		nodes := utils.NewTmpnetNodes(nodeCount)
		env = e2e.NewTestEnvironment(
			tc,
			flagVars,
			utils.NewTmpnetNetwork(
				"subnet-evm-small-load",
				nodes,
				tmpnet.FlagsMap{},
				utils.NewTmpnetSubnet(subnetAName, tests.Genesis, utils.DefaultChainConfig, nodes...),
			),
		)
	})

	ginkgo.It("basic subnet load test", ginkgo.Label("load"), func() {
		network := env.GetNetwork()

		subnet := network.GetSubnet(subnetAName)
		require.NotNil(subnet)
		blockchainID := subnet.Chains[0].ChainID

		nodeURIs := env.GetNetwork().GetNodeURIs()
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
		require.NoError(os.Setenv("RPC_ENDPOINTS", commaSeparatedRPCEndpoints))

		log.Info("Running load simulator...", "rpcEndpoints", commaSeparatedRPCEndpoints)
		cmd := exec.Command(scriptPath)
		log.Info("Running load simulator script", "cmd", cmd.String())

		out, err := cmd.CombinedOutput()
		fmt.Printf("\nCombined output:\n\n%s\n", string(out))
		require.NoError(err)
	})
})
