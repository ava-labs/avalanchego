// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

import { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers";
import { expect } from "chai";
import {
  BigNumber,
  Contract,
  ContractFactory,
} from "ethers"
import { ethers } from "hardhat"

// make sure this is always an admin for the precompile
const adminAddress: string = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const FEE_MANAGER = "0x0200000000000000000000000000000000000003";

const ROLES = {
  NONE: 0,
  ENABLED: 1,
  ADMIN: 2
};

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

// TODO: These tests keep state to the next state. It means that some tests cases assumes some preconditions
// set by previous test cases. We should make these tests stateless.
describe("ExampleFeeManager", function () {
  this.timeout("30s")

  let owner: SignerWithAddress
  let contract: Contract
  let manager: SignerWithAddress
  let nonEnabled: SignerWithAddress
  let managerPrecompile: Contract
  let ownerPrecompile: Contract
  before(async function () {
    owner = await ethers.getSigner(adminAddress);
    const contractF: ContractFactory = await ethers.getContractFactory("ExampleFeeManager", { signer: owner })
    contract = await contractF.deploy()
    await contract.deployed()
    const contractAddress: string = contract.address
    console.log(`Contract deployed to: ${contractAddress}`)

    managerPrecompile = await ethers.getContractAt("IFeeManager", FEE_MANAGER, manager);
    ownerPrecompile = await ethers.getContractAt("IFeeManager", FEE_MANAGER, owner);

    const signers: SignerWithAddress[] = await ethers.getSigners()
    manager = signers.slice(-1)[0]
    nonEnabled = signers.slice(-2)[0]

    let tx = await ownerPrecompile.setEnabled(manager.address);
    await tx.wait()

    tx = await owner.sendTransaction({
      to: manager.address,
      value: ethers.utils.parseEther("1")
    })
    await tx.wait()

    tx = await owner.sendTransaction({
      to: nonEnabled.address,
      value: ethers.utils.parseEther("1")
    })
    await tx.wait()
  });

  it("should add contract deployer as owner", async function () {
    const contractOwnerAddr: string = await contract.owner()
    expect(owner.address).to.be.equal(contractOwnerAddr)
  });

  it("contract should not be able to change fee without enabled", async function () {
    let contractRole = await managerPrecompile.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.NONE)
    try {
      let tx = await contract.enableWAGMIFees()
      await tx.wait()
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })

  it("contract should be added to manager list", async function () {
    let adminRole = await ownerPrecompile.readAllowList(adminAddress);
    expect(adminRole).to.be.equal(ROLES.ADMIN)
    let contractRole = await ownerPrecompile.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.NONE)

    let enableTx = await ownerPrecompile.setEnabled(contract.address);
    await enableTx.wait()
    contractRole = await ownerPrecompile.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.ENABLED)
  });

  it("admin should be able to change fees through contract", async function () {
    const testFees = {
      gasLimit: 12_345_678, // gasLimit
      targetBlockRate: 2, // targetBlockRate
      minBaseFee: 1_234_567, // minBaseFee
      targetGas: 100_000_000, // targetGas
      baseFeeChangeDenominator: 48, // baseFeeChangeDenominator
      minBlockGasCost: 0, // minBlockGasCost
      maxBlockGasCost: 10_000_000, // maxBlockGasCost
      blockGasCostStep: 100_000 // blockGasCostStep
    }
    var res = await contract.getCurrentFeeConfig()
    expect(res.gasLimit).to.be.not.equal(testFees.gasLimit)
    expect(res.minBaseFee).to.be.not.equal(testFees.minBaseFee)

    let enableTx = await contract.enableCustomFees(testFees)
    let txRes = await enableTx.wait()

    var res = await contract.getCurrentFeeConfig()
    expect(res.gasLimit).to.be.equal(testFees.gasLimit)
    expect(res.minBaseFee).to.be.equal(testFees.minBaseFee)

    var res = await contract.getFeeConfigLastChangedAt()
    expect(res).to.be.equal(txRes.blockNumber)
  })

  it("admin should be able to enable wagmi fees through contract", async function () {
    var res = await contract.getCurrentFeeConfig()
    expect(res.gasLimit).to.be.not.equal(WAGMI_FEES.gasLimit)
    expect(res.minBaseFee).to.be.not.equal(WAGMI_FEES.minBaseFee)
    // set wagmi fees now
    let enableTx = await contract.enableWAGMIFees({
      maxFeePerGas: WAGMI_FEES.minBaseFee * 2,
      maxPriorityFeePerGas: WAGMI_FEES.minBaseFee
    })
    let txRes = await enableTx.wait()

    res = await contract.getCurrentFeeConfig()
    expect(res.gasLimit).to.be.equal(WAGMI_FEES.gasLimit)
    expect(res.minBaseFee).to.be.equal(WAGMI_FEES.minBaseFee)

    var res = await contract.getFeeConfigLastChangedAt()
    expect(res).to.be.equal(txRes.blockNumber)
  })

  it("should let low fee tx to be in mempool", async function () {
    var res = await contract.getCurrentFeeConfig()
    expect(res.minBaseFee).to.be.equal(WAGMI_FEES.minBaseFee)

    var testMaxFeePerGas = WAGMI_FEES.minBaseFee + 10000

    let tx = await owner.sendTransaction({
      to: manager.address,
      value: ethers.utils.parseEther("0.1"),
      maxFeePerGas: testMaxFeePerGas,
      maxPriorityFeePerGas: 0
    });
    let confirmedTx = await tx.wait()
    expect(confirmedTx.confirmations).to.be.greaterThanOrEqual(1)
  })

  it("should not let low fee tx to be in mempool", async function () {

    let enableTx = await contract.enableCustomFees(C_FEES)
    await enableTx.wait()
    let getRes = await contract.getCurrentFeeConfig()
    expect(getRes.minBaseFee).to.be.equal(C_FEES.minBaseFee)

    var testMaxFeePerGas = C_FEES.minBaseFee - 100000

    // send tx with lower han C_FEES minBaseFee
    try {
      let tx = await owner.sendTransaction({
        to: manager.address,
        value: ethers.utils.parseEther("0.1"),
        maxFeePerGas: testMaxFeePerGas,
        maxPriorityFeePerGas: 10000
      });
      await tx.wait()
    }
    catch (err) {
      expect(err.toString()).to.include("transaction underpriced")
      return
    }
    expect.fail("should have errored")
  })

  it("should be able to get current fee config", async function () {
    let enableTx = await contract.enableCustomFees(C_FEES,
      {
        maxFeePerGas: C_FEES.minBaseFee * 2,
        maxPriorityFeePerGas: 0
      })
    await enableTx.wait()

    var res = await contract.getCurrentFeeConfig()
    expect(res.gasLimit).to.be.equal(C_FEES.gasLimit)

    var res = await contract.connect(manager).getCurrentFeeConfig()
    expect(res.gasLimit).to.be.equal(C_FEES.gasLimit)

    var res = await contract.connect(nonEnabled).getCurrentFeeConfig()
    expect(res.gasLimit).to.be.equal(C_FEES.gasLimit)
  });

  it("nonEnabled should not be able to set fee config", async function () {
    let nonEnabledRole = await ownerPrecompile.readAllowList(nonEnabled.address);

    expect(nonEnabledRole).to.be.equal(ROLES.NONE)
    try {
      await contract.connect(nonEnabled).enableWAGMIFees({
        maxFeePerGas: C_FEES.minBaseFee * 2,
        maxPriorityFeePerGas: 0
      })
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })

  it("manager should be able to change fees through contract", async function () {
    let enableTx = await contract.connect(manager).enableCustomFees(WAGMI_FEES,
      {
        maxFeePerGas: C_FEES.minBaseFee * 2,
        maxPriorityFeePerGas: 0
      })
    await enableTx.wait()

    var res = await contract.connect(manager).getCurrentFeeConfig()
    expect(res.minBaseFee).to.be.equal(WAGMI_FEES.minBaseFee)
  })


  it("non-enabled should not be able to change fees through contract", async function () {
    try {
      let enableTx = await contract.connect(nonEnabled).enableCustomFees(WAGMI_FEES,
        {
          maxFeePerGas: WAGMI_FEES.minBaseFee * 2,
          maxPriorityFeePerGas: 0
        })
      await enableTx.wait()
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })
})
