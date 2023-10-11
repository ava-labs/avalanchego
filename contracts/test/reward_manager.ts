// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

import { ethers } from "hardhat"
import { test } from "./utils"

// make sure this is always an admin for reward manager precompile
const ADMIN_ADDRESS = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const REWARD_MANAGER_ADDRESS = "0x0200000000000000000000000000000000000004"

describe("ExampleRewardManager", function () {
  beforeEach('Setup DS-Test contract', async function () {
    const signer = await ethers.getSigner(ADMIN_ADDRESS)
    const rewardManagerPromise = ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS, signer)

    return ethers.getContractFactory("ExampleRewardManagerTest", { signer })
      .then(factory => factory.deploy())
      .then(contract => {
        this.testContract = contract
        return contract.deployed().then(() => contract)
      })
      .then(contract => contract.setUp())
      .then(tx => Promise.all([rewardManagerPromise, tx.wait()]))
      .then(([rewardManager]) => rewardManager.setAdmin(this.testContract.address))
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
