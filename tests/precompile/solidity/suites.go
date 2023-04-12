// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements solidity tests.
package solidity

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/subnet-evm/tests/utils"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("[Precompiles]", ginkgo.Ordered, func() {
	ginkgo.It("ping the network", ginkgo.Label("setup"), func() {
		client := health.NewClient(utils.DefaultLocalNodeURI)
		healthy, err := client.Readiness(context.Background(), nil)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(healthy.Healthy).Should(gomega.BeTrue())
	})
})

var _ = ginkgo.Describe("[Precompiles]", ginkgo.Ordered, func() {
	// Each ginkgo It node specifies the name of the genesis file (in ./tests/precompile/genesis/)
	// to use to launch the subnet and the name of the TS test file to run on the subnet (in ./contract-examples/tests/)
	ginkgo.It("contract native minter", ginkgo.Label("Precompile"), ginkgo.Label("ContractNativeMinter"), func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		utils.ExecuteHardHatTestOnNewBlockchain(ctx, "contract_native_minter")
	})

	ginkgo.It("tx allow list", ginkgo.Label("Precompile"), ginkgo.Label("TxAllowList"), func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		utils.ExecuteHardHatTestOnNewBlockchain(ctx, "tx_allow_list")
	})

	ginkgo.It("contract deployer allow list", ginkgo.Label("Precompile"), ginkgo.Label("ContractDeployerAllowList"), func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		utils.ExecuteHardHatTestOnNewBlockchain(ctx, "contract_deployer_allow_list")
	})

	ginkgo.It("fee manager", ginkgo.Label("Precompile"), ginkgo.Label("FeeManager"), func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		utils.ExecuteHardHatTestOnNewBlockchain(ctx, "fee_manager")
	})

	ginkgo.It("reward manager", ginkgo.Label("Precompile"), ginkgo.Label("RewardManager"), func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		utils.ExecuteHardHatTestOnNewBlockchain(ctx, "reward_manager")
	})

	// TODO: can we refactor this so that it automagically checks to ensure each hardhat test file matches the name of a hardhat genesis file
	// and then runs the hardhat tests for each one without forcing precompile developers to modify this file.
	// ADD YOUR PRECOMPILE HERE
	/*
		ginkgo.It("your precompile", ginkgo.Label("Precompile"), ginkgo.Label("YourPrecompile"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			// Specify the name shared by the genesis file in ./tests/precompile/genesis/{your_precompile}.json
			// and the test file in ./contract-examples/tests/{your_precompile}.ts
			utils.ExecuteHardHatTestOnNewBlockchain(ctx, "your_precompile")
		})
	*/
})
