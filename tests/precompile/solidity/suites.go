// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements solidity tests.
package solidity

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/subnet-evm/tests/utils"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// Registers the Asynchronized Precompile Tests
// Before running the tests, this function creates all subnets given in the genesis files
// and then runs the hardhat tests for each one asynchronously if called with `ginkgo run -procs=`.
func RegisterAsyncTests() {
	// Tests here assumes that the genesis files are in ./tests/precompile/genesis/
	// with the name {precompile_name}.json
	genesisFiles, err := utils.GetFilesAndAliases("./tests/precompile/genesis/*.json")
	if err != nil {
		ginkgo.AbortSuite("Failed to get genesis files: " + err.Error())
	}
	if len(genesisFiles) == 0 {
		ginkgo.AbortSuite("No genesis files found")
	}
	subnetsSuite := utils.CreateSubnetsSuite(genesisFiles)

	timeout := 3 * time.Minute
	_ = ginkgo.Describe("[Asynchronized Precompile Tests]", func() {
		// Register the ping test first
		utils.RegisterPingTest()

		// Each ginkgo It node specifies the name of the genesis file (in ./tests/precompile/genesis/)
		// to use to launch the subnet and the name of the TS test file to run on the subnet (in ./contracts/tests/)
		ginkgo.It("contract native minter", ginkgo.Label("Precompile"), ginkgo.Label("ContractNativeMinter"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			blockchainID := subnetsSuite.GetBlockchainID("contract_native_minter")
			runDefaultHardhatTests(ctx, blockchainID, "contract_native_minter")
		})

		ginkgo.It("tx allow list", ginkgo.Label("Precompile"), ginkgo.Label("TxAllowList"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			blockchainID := subnetsSuite.GetBlockchainID("tx_allow_list")
			runDefaultHardhatTests(ctx, blockchainID, "tx_allow_list")
		})

		ginkgo.It("fee manager", ginkgo.Label("Precompile"), ginkgo.Label("FeeManager"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			blockchainID := subnetsSuite.GetBlockchainID("fee_manager")
			runDefaultHardhatTests(ctx, blockchainID, "fee_manager")
		})

		ginkgo.It("reward manager", ginkgo.Label("Precompile"), ginkgo.Label("RewardManager"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			blockchainID := subnetsSuite.GetBlockchainID("reward_manager")
			runDefaultHardhatTests(ctx, blockchainID, "reward_manager")
		})

		// ADD YOUR PRECOMPILE HERE
		/*
			ginkgo.It("your precompile", ginkgo.Label("Precompile"), ginkgo.Label("YourPrecompile"), func() {
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()

				// Specify the name shared by the genesis file in ./tests/precompile/genesis/{your_precompile}.json
				// and the test file in ./contracts/tests/{your_precompile}.ts
				// If you want to use a different test command and genesis path than the defaults, you can
				// use the utils.RunTestCMD. See utils.RunDefaultHardhatTests for an example.
				subnetsSuite.RunHardhatTests(ctx, "your_precompile")
			})
		*/
	})
}

//	Default parameters are:
//
// 1. Hardhat contract environment is located at ./contracts
// 2. Hardhat test file is located at ./contracts/test/<test>.ts
// 3. npx is available in the ./contracts directory
func runDefaultHardhatTests(ctx context.Context, blockchainID, testName string) {
	cmdPath := "./contracts"
	// test path is relative to the cmd path
	testPath := fmt.Sprintf("./test/%s.ts", testName)
	utils.RunHardhatTests(ctx, blockchainID, cmdPath, testPath)
}
