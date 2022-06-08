// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements ping tests, requires network-runner cluster.
package solidity

import (
	"fmt"
	"os/exec"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/subnet-evm/tests/e2e"
	"github.com/ava-labs/subnet-evm/tests/e2e/runner"
)

var _ = e2e.DescribePrecompile("[TX Allow List]", func() {
	ginkgo.BeforeAll(func() {
		const vmName = "subnetevm"
		b := make([]byte, 32)
		copy(b, []byte(vmName))
		var err error
		vmID, err := ids.ToID(b)
		if err != nil {
			panic(err)
		}
		runner.StartNetwork(vmID, vmName, "/tmp/genesis.json", "/tmp/avalanchego-v1.7.11/plugins")
	})

	ginkgo.AfterAll(func() {
		// if e2e.GetRunnerGRPCEndpoint() != "" {
		// 	runnerCli := e2e.GetRunnerClient()
		// 	gomega.Expect(runnerCli).ShouldNot(gomega.BeNil())

		// 	tests.Outf("{{red}}shutting down network-runner cluster{{/}}\n")
		// 	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		// 	_, err := runnerCli.Stop(ctx)
		// 	cancel()
		// 	gomega.Expect(err).Should(gomega.BeNil())

		// 	tests.Outf("{{red}}shutting down network-runner client{{/}}\n")
		// 	err = e2e.CloseRunnerClient()
		// 	gomega.Expect(err).Should(gomega.BeNil())
		// }
	})

	ginkgo.It("hardhat tests", func() {
		tests.Outf("{{green}}run hardhat{{/}}\n")
		cmd := exec.Command("npx hardhat run scripts/testAllowList.ts --network subnet")
		cmd.Dir = "./contract-examples"
		out, err := cmd.Output()
		fmt.Println("About to print output")
		fmt.Println(string(out))
		fmt.Println("Printed output")
		gomega.Expect(err).Should(gomega.BeNil())
	})
})
