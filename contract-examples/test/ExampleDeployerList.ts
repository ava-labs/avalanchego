import { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers";
import { expect } from "chai";
import {
  Contract,
  ContractFactory,
} from "ethers"
import { ethers } from "hardhat"

// make sure this is always an admin for minter precompile
const adminAddress: string = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const ALLOWLIST_ADDRESS = "0x0200000000000000000000000000000000000000";

const ROLES = {
  NONE: 0,
  DEPLOYER: 1,
  ADMIN: 2
};

describe("ExampleDeployerList", function () {
  let owner: SignerWithAddress
  let contract: Contract
  let deployer: SignerWithAddress
  before(async function () {
    owner = await ethers.getSigner(adminAddress);
    const contractF: ContractFactory = await ethers.getContractFactory("ExampleDeployerList", { signer: owner })
    contract = await contractF.deploy()
    await contract.deployed()
    const contractAddress: string = contract.address
    console.log(`Contract deployed to: ${contractAddress}`)

    const signers: SignerWithAddress[] = await ethers.getSigners()
    deployer = signers.slice(-1)[0]
  });

  it("should add contract deployer as owner", async function () {
    const contractOwnerAddr: string = await contract.owner()
    expect(owner.address).to.equal(contractOwnerAddr)
  });

  it("precompile should see owner address has admin role", async function () {
    // test precompile first
    const allowList = await ethers.getContractAt("IAllowList", ALLOWLIST_ADDRESS, owner);
    let adminRole = await allowList.readAllowList(owner.address);
    expect(adminRole).to.be.equal(ROLES.ADMIN)
  });

  it("precompile should see test address has no role", async function () {
    // test precompile first
    const allowList = await ethers.getContractAt("IAllowList", ALLOWLIST_ADDRESS, owner);
    let noRole = await allowList.readAllowList(deployer.address);
    expect(noRole).to.be.equal(ROLES.NONE)
  });

  it("contract should report test address has no admin role", async function () {
    const result = await contract.isAdmin(deployer.address);
    expect(result).to.be.false
  });


  it("contract should report owner address has admin role", async function () {
    const result = await contract.isAdmin(owner.address);
    expect(result).to.be.true
  });

  it("should not let test address to deploy", async function () {
    const Token: ContractFactory = await ethers.getContractFactory("ERC20NativeMinter", { signer: deployer })
    let token: Contract
    try {
      token = await Token.deploy(11111)
    }
    catch (err) {
      expect(err.message).contains("is not authorized to deploy a contract");
      return
    }
    expect.fail("should have errored")
  });

  it("should not allow deployer to enable itself", async function () {
    try {
      await contract.connect(deployer).addDeployer(deployer.address);
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  });

  it("should not allow admin to enable deployer without enabling contract", async function () {
    const allowList = await ethers.getContractAt("IAllowList", ALLOWLIST_ADDRESS, owner);
    let role = await allowList.readAllowList(contract.address);
    expect(role).to.be.equal(ROLES.NONE)
    const result = await contract.isDeployer(contract.address);
    expect(result).to.be.false
    try {
      await contract.addDeployer(deployer.address);
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  });

  it("should allow admin to add contract as admin", async function () {
    const allowList = await ethers.getContractAt("IAllowList", ALLOWLIST_ADDRESS, owner);
    let role = await allowList.readAllowList(contract.address);
    expect(role).to.be.equal(ROLES.NONE)
    let tx = await allowList.setAdmin(contract.address)
    await tx.wait()
    role = await allowList.readAllowList(contract.address);
    expect(role).to.be.equal(ROLES.ADMIN)
    const result = await contract.isAdmin(contract.address);
    expect(result).to.be.true
  });

  it("should allow admin to add deployer address as deployer through contract", async function () {
    let tx = await contract.addAdmin(deployer.address)
    await tx.wait()
    const result = await contract.isDeployer(deployer.address);
    expect(result).to.be.true
  });

  it("should let deployer address to deploy", async function () {
    const Token: ContractFactory = await ethers.getContractFactory("ERC20NativeMinter", { signer: deployer })
    let token: Contract
    token = await Token.deploy(11111)
    await token.deployed()
    expect(token.address).not.null
  });

  it("should not let deployer add another deployer", async function () {
    try {
      const signers: SignerWithAddress[] = await ethers.getSigners()
      const testAddress = signers.slice(-2)[0]
      await contract.connect(deployer).addDeployer(testAddress.address);
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  });

  it("should not let deployer to revoke admin", async function () {
    try {
      await contract.connect(deployer).revoke(owner.address);
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  });


  it("should not let deployer to revoke itself", async function () {
    try {
      await contract.connect(deployer).revoke(deployer.address);
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  });

  it("should let admin to revoke deployer", async function () {
    let tx = await contract.revoke(deployer.address);
    await tx.wait()
    const allowList = await ethers.getContractAt("IAllowList", ALLOWLIST_ADDRESS, owner);
    let noRole = await allowList.readAllowList(deployer.address);
    expect(noRole).to.be.equal(ROLES.NONE)
  });


  it("should not let admin to revoke itself", async function () {
    try {
      await contract.revoke(owner.address);
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  });
})
