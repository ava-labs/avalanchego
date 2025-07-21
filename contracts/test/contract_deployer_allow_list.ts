// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

import { ethers } from "hardhat"

import { Roles, test } from "./utils"
import { expect } from "chai";
import { Contract, Signer } from "ethers"
import { IAllowList } from "typechain-types";

const ADMIN_ADDRESS: string = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const OTHER_SIGNER = "0x0Fa8EA536Be85F32724D57A37758761B86416123"
const DEPLOYER_ALLOWLIST_ADDRESS = "0x0200000000000000000000000000000000000000"

describe("ExampleDeployerList", function () {
  beforeEach('Setup DS-Test contract', async function () {
    const signer = await ethers.getSigner(ADMIN_ADDRESS)
    const allowListPromise = ethers.getContractAt("IAllowList", DEPLOYER_ALLOWLIST_ADDRESS, signer)

    return ethers.getContractFactory("ExampleDeployerListTest", { signer })
      .then(factory => factory.deploy())
      .then(contract => {
        this.testContract = contract
        return Promise.all([
          contract.waitForDeployment().then(() => contract),
          allowListPromise.then(allowList => allowList.setAdmin(contract.target)).then(tx => tx.wait()),
        ])
      })
      .then(([contract]) => contract.setUp())
      .then(tx => tx.wait())
  })

  test("precompile should see owner address has admin role", "step_verifySenderIsAdmin")

  test("precompile should see test address has no role", "step_newAddressHasNoRole")

  test("contract should report test address has no admin role", "step_noRoleIsNotAdmin")

  test("contract should report owner address has admin role", "step_ownerIsAdmin")

  test("should not let test address deploy", {
    method: "step_noRoleCannotDeploy",
    overrides: { from: OTHER_SIGNER },
    shouldFail: false,
  })

  test("should allow admin to add contract as admin", "step_adminAddContractAsAdmin")

  test("should allow admin to add deployer address as deployer through contract", "step_addDeployerThroughContract")

  test("should let deployer address to deploy", "step_deployerCanDeploy")

  test("should let admin revoke deployer", "step_adminCanRevokeDeployer")
})

describe("IAllowList", function () {
  let owner: Signer
  let ownerAddress: string
  let contract: IAllowList
  before(async function () {
    owner = await ethers.getSigner(ADMIN_ADDRESS);
    ownerAddress = await owner.getAddress()
    contract = await ethers.getContractAt("IAllowList", DEPLOYER_ALLOWLIST_ADDRESS, owner)
  });

  it("should emit event after set admin", async function () {
    let testAddress = "0x0111000000000000000000000000000000000001"
    let tx = await contract.setAdmin(testAddress)
    let receipt = await tx.wait()
    await expect(receipt)
      .to.emit(contract, 'RoleSet')
      .withArgs(Roles.Admin, testAddress, ownerAddress, Roles.None)
  })

  it("should emit event after set manager", async function () {
    let testAddress = "0x0222000000000000000000000000000000000002"
    let tx = await contract.setManager(testAddress)
    let receipt = await tx.wait()
    await expect(receipt)
      .to.emit(contract, 'RoleSet')
      .withArgs(Roles.Manager, testAddress, ownerAddress, Roles.None)
  })

  it("should emit event after set enabled", async function () {
    let testAddress = "0x0333000000000000000000000000000000000003"
    let tx = await contract.setEnabled(testAddress)
    let receipt = await tx.wait()
    await expect(receipt)
      .to.emit(contract, 'RoleSet')
      .withArgs(Roles.Enabled, testAddress, ownerAddress, Roles.None)
  })

  it("should emit event after set none", async function () {
    let testAddress = "0x0333000000000000000000000000000000000003"
    let tx = await contract.setNone(testAddress)
    let receipt = await tx.wait()
    await expect(receipt)
      .to.emit(contract, 'RoleSet')
      .withArgs(Roles.None, testAddress, ownerAddress, Roles.Enabled)
  })
})
