// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements solidity tests.
package solidity

import (
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
	_ = ginkgo.Describe("[Asynchronized Precompile Tests]", func() {
		// Each ginkgo It node specifies the name of the genesis file (in ./tests/precompile/genesis/)
		// to use to launch the subnet and the name of the TS test file to run on the subnet (in ./contracts/tests/)

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
