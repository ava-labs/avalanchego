// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ava-labs/libevm/log"
	"github.com/stretchr/testify/require"

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
	flagVars *e2e.FlagVars
	testDir  string
)

func init() {
	// Configures flags used to configure tmpnet
	flagVars = e2e.RegisterFlags()
	testDir = func() string {
		_, thisFile, _, _ := runtime.Caller(0)
		return filepath.Dir(thisFile)
	}()
}

func TestE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "subnet-evm small load simulator test suite")
}

var _ = ginkgo.Describe("[Load Simulator]", ginkgo.Ordered, func() {
	require := require.New(ginkgo.GinkgoT())

	var env *e2e.TestEnvironment

	ginkgo.BeforeAll(func() {
		tc := e2e.NewTestContext()
		genesisPath := filepath.Join(testDir, "genesis/genesis.json")

		nodes := utils.NewTmpnetNodes(nodeCount)
		env = e2e.NewTestEnvironment(
			tc,
			flagVars,
			utils.NewTmpnetNetwork(
				"subnet-evm-small-load",
				nodes,
				tmpnet.FlagsMap{},
				utils.NewTmpnetSubnet(subnetAName, genesisPath, utils.DefaultChainConfig, nodes...),
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
		cmd := exec.Command("./scripts/run_simulator.sh")
		cmd.Dir = filepath.Join(testDir, "../..")
		log.Info("Running load simulator script", "cmd", cmd.String())

		out, err := cmd.CombinedOutput()
		fmt.Printf("\nCombined output:\n\n%s\n", string(out))
		require.NoError(err)
	})
})
