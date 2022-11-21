// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements solidity tests.
package solidity

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ava-labs/subnet-evm/tests/e2e/runner"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const vmName = "subnetevm"

// network-runner-grpc-endpoint from run script
const grpcEp = "0.0.0.0:12342"

func runHardhatTests(test string) {
	cmd := exec.Command("npx", "hardhat", "test", test, "--network", "e2e")
	cmd.Dir = "./contract-examples"
	out, err := cmd.Output()
	if err != nil {
		fmt.Println(string(out))
		fmt.Println(err)
	}
	gomega.Expect(err).Should(gomega.BeNil())
}

// startSubnet starts a test network and launches a subnetEVM instance with the genesis file at [genesisPath]
func startSubnet(genesisPath string) error {
	fmt.Println("AVALANCHEGO_PATH:", os.Getenv("AVALANCHEGO_PATH"))
	_, err := runner.StartNetwork(grpcEp, os.Getenv("AVALANCHEGO_PATH"), evm.ID, vmName, genesisPath, utils.GetPluginDir())
	gomega.Expect(err).Should(gomega.BeNil())
	return utils.UpdateHardhatConfig()
}

// stopSubnet stops the test network.
func stopSubnet() {
	err := runner.StopNetwork(grpcEp)
	gomega.Expect(err).Should(gomega.BeNil())
}

var _ = utils.DescribePrecompile(func() {
	ginkgo.It("tx allow list", ginkgo.Label("solidity-with-npx"), func() {
		err := startSubnet("./tests/e2e/genesis/tx_allow_list.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ExampleTxAllowList.ts")
		stopSubnet()
		running = runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeFalse())
	})

	ginkgo.It("deployer allow list", ginkgo.Label("solidity-with-npx"), func() {
		err := startSubnet("./tests/e2e/genesis/deployer_allow_list.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ExampleDeployerList.ts")
		stopSubnet()
		running = runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeFalse())
	})

	ginkgo.It("contract native minter", ginkgo.Label("solidity-with-npx"), func() {
		err := startSubnet("./tests/e2e/genesis/contract_native_minter.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ERC20NativeMinter.ts")
		stopSubnet()
		running = runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeFalse())
	})

	ginkgo.It("fee manager", ginkgo.Label("solidity-with-npx"), func() {
		err := startSubnet("./tests/e2e/genesis/fee_manager.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ExampleFeeManager.ts")
		stopSubnet()
		running = runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeFalse())
	})

	ginkgo.It("reward manager", ginkgo.Label("solidity-with-npx"), func() {
		err := startSubnet("./tests/e2e/genesis/reward_manager.json")
		gomega.Expect(err).Should(gomega.BeNil())
		running := runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeTrue())
		runHardhatTests("./test/ExampleRewardManager.ts")
		stopSubnet()
		running = runner.IsRunnerUp(grpcEp)
		gomega.Expect(running).Should(gomega.BeFalse())
	})

	// ADD YOUR PRECOMPILE HERE
	/*
			ginkgo.It("your precompile", ginkgo.Label("solidity-with-npx"), func() {
			err := startSubnet("./tests/e2e/genesis/{your_precompile}.json")
			gomega.Expect(err).Should(gomega.BeNil())
			running := runner.IsRunnerUp(grpcEp)
			gomega.Expect(running).Should(gomega.BeTrue())
			runHardhatTests("./test/{YourPrecompileTest}.ts")
			stopSubnet()
			running = runner.IsRunnerUp(grpcEp)
			gomega.Expect(running).Should(gomega.BeFalse())
		})
	*/
})
