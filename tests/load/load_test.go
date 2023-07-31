// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ava-labs/subnet-evm/tests/utils/runner"
	"github.com/ethereum/go-ethereum/log"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var getSubnet func() *runner.Subnet

func init() {
	getSubnet = runner.RegisterFiveNodeSubnetRun()
}

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "subnet-evm small load simulator test suite")
}

var _ = ginkgo.Describe("[Load Simulator]", ginkgo.Ordered, func() {
	ginkgo.It("basic subnet load test", ginkgo.Label("load"), func() {
		subnetDetails := getSubnet()
		blockchainID := subnetDetails.BlockchainID

		nodeURIs := subnetDetails.ValidatorURIs
		rpcEndpoints := make([]string, 0, len(nodeURIs))
		for _, uri := range nodeURIs {
			rpcEndpoints = append(rpcEndpoints, fmt.Sprintf("%s/ext/bc/%s/rpc", uri, blockchainID))
		}
		commaSeparatedRPCEndpoints := strings.Join(rpcEndpoints, ",")
		err := os.Setenv("RPC_ENDPOINTS", commaSeparatedRPCEndpoints)
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Running load simulator...", "rpcEndpoints", commaSeparatedRPCEndpoints)
		cmd := exec.Command("./scripts/run_simulator.sh")
		log.Info("Running load simulator script", "cmd", cmd.String())

		out, err := cmd.CombinedOutput()
		fmt.Printf("\nCombined output:\n\n%s\n", string(out))
		gomega.Expect(err).Should(gomega.BeNil())
	})
})
