// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

import { expect } from "chai";
import { ethers } from "hardhat";
import { Contract, Signer } from "ethers";
import { IWarpMessenger } from "typechain-types";

const WARP_ADDRESS = "0x0200000000000000000000000000000000000005";
let senderAddress = process.env["SENDER_ADDRESS"];
// Expected to be a hex string
let payload = process.env["PAYLOAD"];
let expectedUnsignedMessage = process.env["EXPECTED_UNSIGNED_MESSAGE"];
let sourceID = process.env["SOURCE_CHAIN_ID"];

describe("IWarpMessenger", function () {
  let owner: Signer;
  let contract: IWarpMessenger;
  before(async function () {
    owner = await ethers.getSigner(senderAddress);
    contract = await ethers.getContractAt("IWarpMessenger", WARP_ADDRESS, owner)
  });

  it("contract should be to send warp message", async function () {
    console.log(`Sending warp message with payload ${payload}, expected unsigned message ${expectedUnsignedMessage}`);

    // Get ID of payload by taking sha256 of unsigned message
    let messageID = ethers.sha256(expectedUnsignedMessage);
    let tx = await contract.sendWarpMessage(payload)
    let receipt = await tx.wait()
    await expect(receipt)
      .to.emit(contract, 'SendWarpMessage')
      .withArgs(senderAddress, messageID, expectedUnsignedMessage);
  })

  it("should be able to fetch correct blockchain ID", async function () {
    let blockchainID = await contract.getBlockchainID();
    expect(blockchainID).to.be.equal(sourceID);
  })
})
