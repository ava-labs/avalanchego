// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

import { expect } from "chai"
import { ethers } from "hardhat"
import { test } from "./utils"
import { Contract, Signer } from "ethers"
import { IFeeManager } from "typechain-types"

const ADMIN_ADDRESS: string = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const FEE_MANAGER = "0x0200000000000000000000000000000000000003"

const GENESIS_CONFIG = require('../../tests/precompile/genesis/fee_manager.json')

describe("ExampleFeeManager", function () {
  beforeEach("setup DS-Test contract", async function () {
    const signer = await ethers.getSigner(ADMIN_ADDRESS)
    const feeManagerPromise = ethers.getContractAt("IFeeManager", FEE_MANAGER, signer)

    return ethers.getContractFactory("ExampleFeeManagerTest", { signer })
      .then(factory => factory.deploy())
      .then(contract => {
        this.testContract = contract
        return contract.waitForDeployment().then(() => contract)
      })
      .then(contract => contract.setUp())
      .then(tx => Promise.all([feeManagerPromise, tx.wait()]))
      .then(([feeManager]) => feeManager.setAdmin(this.testContract.target))
      .then(tx => tx.wait())
  })

  test("should add contract deployer as owner", "step_addContractDeployerAsOwner")

  test("contract should not be able to change fee without enabled", "step_enableWAGMIFeesFailure")

  test("contract should be added to manager list", "step_addContractToManagerList")

  test("admin should be able to enable change fees", "step_changeFees")

  test("should confirm min-fee transaction", "step_minFeeTransaction", {
    maxFeePerGas: GENESIS_CONFIG.config.feeConfig.minBaseFee,
    maxPriorityFeePerGas: 0,
  })

  test("should reject a transaction below the minimum", [
    "step_raiseMinFeeByOne",
    {
      method: "step_minFeeTransaction",
      shouldFail: true,
      overrides: {
        maxFeePerGas: GENESIS_CONFIG.config.feeConfig.minBaseFee,
        maxPriorityFeePerGas: 0,
      },
    },
    "step_lowerMinFeeByOne",
  ])
})

const C_FEES = {
  gasLimit: 8_000_000, // gasLimit
  targetBlockRate: 2, // targetBlockRate
  minBaseFee: 25_000_000_000, // minBaseFee
  targetGas: 15_000_000, // targetGas
  baseFeeChangeDenominator: 36, // baseFeeChangeDenominator
  minBlockGasCost: 0, // minBlockGasCost
  maxBlockGasCost: 1_000_000, // maxBlockGasCost
  blockGasCostStep: 100_000 // blockGasCostStep
}

const WAGMI_FEES = {
  gasLimit: 20_000_000, // gasLimit
  targetBlockRate: 2, // targetBlockRate
  minBaseFee: 1_000_000_000, // minBaseFee
  targetGas: 100_000_000, // targetGas
  baseFeeChangeDenominator: 48, // baseFeeChangeDenominator
  minBlockGasCost: 0, // minBlockGasCost
  maxBlockGasCost: 10_000_000, // maxBlockGasCost
  blockGasCostStep: 100_000 // blockGasCostStep
}

describe("IFeeManager", function () {
  let owner: Signer
  let ownerAddress: string
  let contract: IFeeManager
  before(async function () {
    owner = await ethers.getSigner(ADMIN_ADDRESS);
    ownerAddress = await owner.getAddress()
    contract = await ethers.getContractAt("IFeeManager", FEE_MANAGER, owner)
    // reset to C fees
    let tx = await contract.setFeeConfig(
      C_FEES.gasLimit,
      C_FEES.targetBlockRate,
      C_FEES.minBaseFee,
      C_FEES.targetGas,
      C_FEES.baseFeeChangeDenominator,
      C_FEES.minBlockGasCost,
      C_FEES.maxBlockGasCost,
      C_FEES.blockGasCostStep)
    await tx.wait()
  });

  it("should emit fee config changed event", async function () {
    let tx = await (contract.setFeeConfig(
      WAGMI_FEES.gasLimit,
       WAGMI_FEES.targetBlockRate,
       WAGMI_FEES.minBaseFee,
       WAGMI_FEES.targetGas,
       WAGMI_FEES.baseFeeChangeDenominator,
       WAGMI_FEES.minBlockGasCost,
       WAGMI_FEES.maxBlockGasCost,
       WAGMI_FEES.blockGasCostStep)
      )
    let receipt = await tx.wait()
    await expect(receipt)
      .to.emit(contract, 'FeeConfigChanged')
      .withArgs(
        ownerAddress,
        // old config
        [C_FEES.gasLimit, C_FEES.targetBlockRate, C_FEES.minBaseFee, C_FEES.targetGas, C_FEES.baseFeeChangeDenominator, C_FEES.minBlockGasCost, C_FEES.maxBlockGasCost, C_FEES.blockGasCostStep],
        // new config
        [WAGMI_FEES.gasLimit, WAGMI_FEES.targetBlockRate, WAGMI_FEES.minBaseFee, WAGMI_FEES.targetGas, WAGMI_FEES.baseFeeChangeDenominator, WAGMI_FEES.minBlockGasCost, WAGMI_FEES.maxBlockGasCost, WAGMI_FEES.blockGasCostStep]
       );
  })
})

