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
import ts = require("typescript");

// make sure this is always an admin for reward manager precompile
const adminAddress: string = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const REWARD_MANAGER_ADDRESS = "0x0200000000000000000000000000000000000004";
const BLACKHOLE_ADDRESS = "0x0100000000000000000000000000000000000000";

const ROLES = {
  NONE: 0,
  ENABLED: 1,
  ADMIN: 2
};

describe("ExampleRewardManager", function () {
  this.timeout("30s")
  let owner: SignerWithAddress
  let contract: Contract
  let signer1: SignerWithAddress
  let signer2: SignerWithAddress
  let precompile: Contract

  before(async function () {
    owner = await ethers.getSigner(adminAddress);
    signer1 = (await ethers.getSigners())[1]
    signer2 = (await ethers.getSigners())[2]
    const Contract: ContractFactory = await ethers.getContractFactory("ExampleRewardManager", { signer: owner })
    contract = await Contract.deploy()
    await contract.deployed()
    const contractAddress: string = contract.address
    console.log(`Contract deployed to: ${contractAddress}`)

    precompile = await ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS, owner);

    // Send a transaction to mine a new block
    const tx = await owner.sendTransaction({
      to: signer1.address,
      value: ethers.utils.parseEther("10")
    })
    await tx.wait()
  });

  // this contract is not selected as the reward address yet, so should not be able to receive fees
  it("should send fees to blackhole", async function () {
    let rewardAddress = await contract.currentRewardAddress();
    expect(rewardAddress).to.be.equal(BLACKHOLE_ADDRESS)

    let firstBHBalance = await ethers.provider.getBalance(BLACKHOLE_ADDRESS)

    // Send a transaction to mine a new block
    const tx = await owner.sendTransaction({
      to: signer1.address,
      value: ethers.utils.parseEther("0.0001")
    })
    await tx.wait()

    let secondBHBalance = await ethers.provider.getBalance(BLACKHOLE_ADDRESS)
    expect(secondBHBalance.gt(firstBHBalance)).to.be.true
  })

  it("should not appoint reward address before enabled", async function () {
    let contractRole = await precompile.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.NONE)
    try {
      let tx = await contract.setRewardAddress(signer1.address);
      await tx.wait()
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  });


  it("contract should be added to enabled list", async function () {
    let contractRole = await precompile.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.NONE)

    let enableTx = await precompile.setEnabled(contract.address);
    await enableTx.wait()
    contractRole = await precompile.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.ENABLED)
  });


  it("should be appointed as reward address", async function () {
    let tx = await contract.setRewardAddress(contract.address);
    await tx.wait()
    let rewardAddress = await contract.currentRewardAddress();
    expect(rewardAddress).to.be.equal(contract.address)
  });

  it("should be able to receive fees", async function () {
    let previousBalance = await ethers.provider.getBalance(contract.address)

    // Send a transaction to mine a new block
    const tx = await owner.sendTransaction({
      to: signer1.address,
      value: ethers.utils.parseEther("0.0001")
    })
    await tx.wait()

    let balance = await ethers.provider.getBalance(contract.address)
    expect(balance.gt(previousBalance)).to.be.true
  })

  it("should return false for allowFeeRecipients check", async function () {
    let res = await contract.areFeeRecipientsAllowed();
    expect(res).to.be.false
  })

  it("should enable allowFeeRecipients", async function () {
    let tx = await contract.allowFeeRecipients();
    await tx.wait()
    let res = await contract.areFeeRecipientsAllowed();
    expect(res).to.be.true
  })

  it("should disable reward address", async function () {
    let tx = await contract.disableRewards();
    await tx.wait()

    let rewardAddress = await contract.currentRewardAddress();
    expect(rewardAddress).to.be.equal(BLACKHOLE_ADDRESS)

    let res = await contract.areFeeRecipientsAllowed();
    expect(res).to.be.false
  })
});
