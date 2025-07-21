// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

import { ethers } from "hardhat"
import { test } from "./utils"
import { expect } from "chai";
import { Contract, Signer } from "ethers"
import { IRewardManager } from "typechain-types";

// make sure this is always an admin for reward manager precompile
const ADMIN_ADDRESS = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const REWARD_MANAGER_ADDRESS = "0x0200000000000000000000000000000000000004"
const BLACKHOLE_ADDRESS = "0x0100000000000000000000000000000000000000"

describe("ExampleRewardManager", function () {
  beforeEach('Setup DS-Test contract', async function () {
    const signer = await ethers.getSigner(ADMIN_ADDRESS)
    const rewardManagerPromise = ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS, signer)

    return ethers.getContractFactory("ExampleRewardManagerTest", signer)
      .then(factory => factory.deploy())
      .then(contract => {
        this.testContract = contract
        return contract.waitForDeployment().then(() => contract)
      })
      .then(contract => contract.setUp())
      .then(tx => Promise.all([rewardManagerPromise, tx.wait()]))
      .then(([rewardManager]) => rewardManager.setAdmin(this.testContract.target))
      .then(tx => tx.wait())
  })

  test("should send fees to blackhole", ["step_captureBlackholeBalance", "step_checkSendFeesToBlackhole"])

  test("should not appoint reward address before enabled", "step_doesNotSetRewardAddressBeforeEnabled")

  test("contract should be added to enabled list", "step_setEnabled")

  test("should be appointed as reward address", "step_setRewardAddress")

  // we need to change the fee receiver, send a transaction for the new receiver to receive fees, then check the balance change.
  // the new fee receiver won't receive fees in the same block where it was set.
  test("should be able to receive fees", ["step_setupReceiveFees", "step_receiveFees", "step_checkReceiveFees"])

  test("should return false for allowFeeRecipients check", "step_areFeeRecipientsAllowed")

  test("should enable allowFeeRecipients", "step_allowFeeRecipients")

  test("should disable reward address", "step_disableRewardAddress")
});

describe("IRewardManager", function () {
  let owner: Signer
  let ownerAddress: string
  let contract: IRewardManager
  before(async function () {
    owner = await ethers.getSigner(ADMIN_ADDRESS);
    ownerAddress = await owner.getAddress()
    contract = await ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS, owner)
  });

  it("should emit reward address changed event ", async function () {
    let testAddress = "0x0444400000000000000000000000000000000004"
    let tx = await contract.setRewardAddress(testAddress)
    let receipt = await tx.wait()
    await expect(receipt)
      .to.be.emit(contract, 'RewardAddressChanged')
      .withArgs(ownerAddress, BLACKHOLE_ADDRESS, testAddress)
  })

  it("should emit fee recipients allowed event ", async function () {
    let tx = await contract.allowFeeRecipients()
    let receipt = await tx.wait()
    await expect(receipt)
      .to.be.emit(contract, 'FeeRecipientsAllowed')
      .withArgs(ownerAddress)
  })

  it("should emit rewards disabled event ", async function () {
    let tx = await contract.disableRewards()
    let receipt = await tx.wait()
    await expect(receipt)
      .to.be.emit(contract, 'RewardsDisabled')
      .withArgs(ownerAddress)
  })
})
