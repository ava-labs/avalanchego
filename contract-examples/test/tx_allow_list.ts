// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

import { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers"
import { expect } from "chai"
import {
  Contract,
  ContractFactory,
} from "ethers"
import { ethers } from "hardhat"

// make sure this is always an admin for minter precompile
const adminAddress: string = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const TX_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000002"

const ROLES = {
  NONE: 0,
  ALLOWED: 1,
  ADMIN: 2
}

describe("ExampleTxAllowList", function () {
  let admin: SignerWithAddress
  let contract: Contract
  let allowed: SignerWithAddress
  let noRole: SignerWithAddress
  before(async function () {
    admin = await ethers.getSigner(adminAddress)
    const contractF: ContractFactory = await ethers.getContractFactory("ExampleTxAllowList", { signer: admin })
    contract = await contractF.deploy()
    await contract.deployed()
    const contractAddress: string = contract.address
    console.log(`Contract deployed to: ${contractAddress}`)

      ;[, allowed, noRole] = await ethers.getSigners()

    // Fund allowed address
    await admin.sendTransaction({
      to: allowed.address,
      value: ethers.utils.parseEther("10")
    })

    // Fund no role address
    let tx = await admin.sendTransaction({
      to: noRole.address,
      value: ethers.utils.parseEther("10")
    })
    await tx.wait()
  })

  it("should add contract deployer as admin", async function () {
    const contractOwnerAdmin: string = await contract.isAdmin(contract.owner())
    expect(contractOwnerAdmin).to.be.true
  })

  it("precompile should see admin address has admin role", async function () {
    // test precompile first
    const allowList = await ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS, admin)
    let adminRole = await allowList.readAllowList(admin.address)
    expect(adminRole).to.be.equal(ROLES.ADMIN)
  })

  it("precompile should see test address has no role", async function () {
    // test precompile first
    const allowList = await ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS, admin)
    let role = await allowList.readAllowList(noRole.address)
    expect(role).to.be.equal(ROLES.NONE)
  })

  it("contract should report test address has no admin role", async function () {
    const result = await contract.isAdmin(noRole.address)
    expect(result).to.be.false
  })


  it("contract should report admin address has admin role", async function () {
    const result = await contract.isAdmin(admin.address)
    expect(result).to.be.true
  })

  it("should not let test address submit txs", async function () {
    const Token: ContractFactory = await ethers.getContractFactory("ERC20NativeMinter", { signer: noRole })
    let token: Contract
    try {
      token = await Token.deploy(11111)
    }
    catch (err) {
      expect(err.message).contains("cannot issue transaction from non-allow listed address")
      return
    }
    expect.fail("should have errored")
  })

  it("should not allow noRole to enable itself", async function () {
    try {
      await contract.connect(noRole).addDeployer(noRole.address)
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })

  it("should not allow admin to enable noRole without enabling contract", async function () {
    const allowList = await ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS, admin)
    let role = await allowList.readAllowList(contract.address)
    expect(role).to.be.equal(ROLES.NONE)
    const result = await contract.isEnabled(contract.address)
    expect(result).to.be.false
    try {
      await contract.setEnabled(noRole.address)
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })

  it("should allow admin to add contract as admin", async function () {
    const allowList = await ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS, admin)
    let role = await allowList.readAllowList(contract.address)
    expect(role).to.be.equal(ROLES.NONE)
    let tx = await allowList.setAdmin(contract.address)
    await tx.wait()
    role = await allowList.readAllowList(contract.address)
    expect(role).to.be.equal(ROLES.ADMIN)
    const result = await contract.isAdmin(contract.address)
    expect(result).to.be.true
  })

  it("should allow admin to add allowed address as allowed through contract", async function () {
    let result = await contract.isEnabled(allowed.address)
    expect(result).to.be.false
    let tx = await contract.setEnabled(allowed.address)
    await tx.wait()
    result = await contract.isEnabled(allowed.address)
    expect(result).to.be.true
  })

  it("should let allowed address deploy", async function () {
    const Token: ContractFactory = await ethers.getContractFactory("ERC20NativeMinter", { signer: allowed })
    let token: Contract
    token = await Token.deploy(11111)
    await token.deployed()
    expect(token.address).not.null
  })

  it("should not let allowed add another allowed", async function () {
    try {
      const signers: SignerWithAddress[] = await ethers.getSigners()
      const testAddress = signers.slice(-2)[0]
      await contract.connect(allowed).setEnabled(noRole)
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })

  it("should not let allowed to revoke admin", async function () {
    try {
      await contract.connect(allowed).revoke(admin.address)
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })


  it("should not let allowed to revoke itself", async function () {
    try {
      await contract.connect(allowed).revoke(allowed.address)
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })

  it("should let admin to revoke allowed", async function () {
    let tx = await contract.revoke(allowed.address)
    await tx.wait()
    const allowList = await ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS, admin)
    let noRole = await allowList.readAllowList(allowed.address)
    expect(noRole).to.be.equal(ROLES.NONE)
  })


  it("should not let admin to revoke itself", async function () {
    try {
      await contract.revoke(admin.address)
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })
})
