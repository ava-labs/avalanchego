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
	"github.com/ava-labs/subnet-evm/tests/e2e/runner"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"
)

var vmId ids.ID

const vmName = "subnetevm"

func runHardhatTests(test string) {
	cmd := exec.Command("npx", "hardhat", "test", test, "--network", "subnet")
	cmd.Dir = "./contract-examples"
	out, err := cmd.Output()
	fmt.Println(string(out))
	gomega.Expect(err).Should(gomega.BeNil())
}

func startSubnet(genesisPath string) error {
	runner.StartNetwork(vmId, vmName, genesisPath, utils.GetPluginDir())
	return utils.UpdateHardhatConfig()
}

var _ = utils.DescribePrecompile("[TX Allow List]", func() {
	ginkgo.BeforeAll(func() {
		b := make([]byte, 32)
		copy(b, []byte(vmName))
		var err error
		vmId, err = ids.ToID(b)
		if err != nil {
			panic(err)
		}
	})

	ginkgo.It("tx allow list", func() {
		err := startSubnet("./tests/e2e/genesis/tx_allow_list_genesis.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ExampleTxAllowList.ts")
	})
})
