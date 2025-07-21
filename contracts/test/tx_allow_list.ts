// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

import { ethers } from "hardhat"
import { Roles, test } from "./utils"
import { expect } from "chai";
import { Contract, Signer } from "ethers"
import { IAllowList } from "typechain-types";

// make sure this is always an admin for minter precompile
const ADMIN_ADDRESS = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const OTHER_SIGNER = "0x0Fa8EA536Be85F32724D57A37758761B86416123"
const TX_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000002"

describe("ExampleTxAllowList", function () {
  beforeEach('Setup DS-Test contract', async function () {
    const signer = await ethers.getSigner(ADMIN_ADDRESS)
    const allowListPromise = ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS, signer)

    return ethers.getContractFactory("ExampleTxAllowListTest", { signer })
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

  test("should add contract deployer as admin", "step_contractOwnerIsAdmin")

  test("precompile should see admin address has admin role", "step_precompileHasDeployerAsAdmin")

  test("precompile should see test address has no role", "step_newAddressHasNoRole")

  test("contract should report test address has on admin role", "step_noRoleIsNotAdmin")

  test("contract should report admin address has admin role", "step_exampleAllowListReturnsTestIsAdmin")

  test("should not let test address submit txs", [
    {
      method: "step_fromOther",
      overrides: { from: OTHER_SIGNER },
      shouldFail: true,
    },
    {
      method: "step_enableOther",
      overrides: { from: ADMIN_ADDRESS },
      shouldFail: false,
    },
    {
      method: "step_fromOther",
      overrides: { from: OTHER_SIGNER },
      shouldFail: false,
    },
  ]);

  test("should not allow noRole to enable itself", "step_noRoleCannotEnableItself")

  test("should allow admin to add contract as admin", "step_addContractAsAdmin")

  test("should allow admin to add allowed address as allowed through contract", "step_enableThroughContract")

  test("should let allowed address deploy", "step_canDeploy")

  test("should not let allowed add another allowed", "step_onlyAdminCanEnable")

  test("should not let allowed to revoke admin", "step_onlyAdminCanRevoke")

  test("should let admin to revoke allowed", "step_adminCanRevoke")

  test("should let manager to add allowed", "step_managerCanAllow")

  test("should let manager to revoke allowed", "step_managerCanRevoke")

  test("should not let manager to revoke admin", "step_managerCannotRevokeAdmin")

  test("should not let manager to add admin", "step_managerCannotGrantAdmin")

  test("should not let manager to add manager", "step_managerCannotGrantManager")

  test("should not let manager to revoke manager", "step_managerCannotRevokeManager")

  test("should let manager to deploy", "step_managerCanDeploy")
})

describe("IAllowList", function () {
  let owner: Signer
  let ownerAddress: string
  let contract: IAllowList
  before(async function () {
    owner = await ethers.getSigner(ADMIN_ADDRESS);
    ownerAddress = await owner.getAddress()
    contract = await ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS, owner)
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
