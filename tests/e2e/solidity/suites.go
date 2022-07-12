// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements ping tests, requires network-runner cluster.
package solidity

import (
	"fmt"
	"os/exec"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ava-labs/subnet-evm/tests/e2e/runner"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"
)

const vmName = "subnetevm"

func runHardhatTests(test string) {
	cmd := exec.Command("npx", "hardhat", "test", test, "--network", "e2e")
	cmd.Dir = "./contract-examples"
	out, err := cmd.Output()
	fmt.Println(string(out))
	gomega.Expect(err).Should(gomega.BeNil())
}

// startSubnet starts a test network and launches a subnetEVM instance with the genesis file at [genesisPath]
func startSubnet(genesisPath string) error {
	_, err := runner.StartNetwork(evm.ID, vmName, genesisPath, utils.GetPluginDir())
	gomega.Expect(err).Should(gomega.BeNil())
	return utils.UpdateHardhatConfig()
}

// stopSubnet stops the test network.
func stopSubnet() {
	err := runner.StopNetwork()
	gomega.Expect(err).Should(gomega.BeNil())
}

var _ = utils.DescribePrecompile(func() {
	ginkgo.It("tx allow list", func() {
		err := startSubnet("./tests/e2e/genesis/tx_allow_list.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ExampleTxAllowList.ts")
		stopSubnet()
		running = runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeFalse())
	})

	ginkgo.It("deployer allow list", func() {
		err := startSubnet("./tests/e2e/genesis/deployer_allow_list.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ExampleDeployerList.ts")
		stopSubnet()
		running = runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeFalse())
	})

	ginkgo.It("contract native minter", func() {
		err := startSubnet("./tests/e2e/genesis/contract_native_minter.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ERC20NativeMinter.ts")
		stopSubnet()
		running = runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeFalse())
	})

	ginkgo.It("fee manager", func() {
		err := startSubnet("./tests/e2e/genesis/fee_manager.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ExampleFeeManager.ts")
		stopSubnet()
		running = runner.IsRunnerUp()
		gomega.Expect(running).Should(gomega.BeFalse())
	})
})
