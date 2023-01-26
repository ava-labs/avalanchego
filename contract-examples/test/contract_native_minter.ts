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

// make sure this is always an admin for minter precompile
const adminAddress: string = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const MINT_PRECOMPILE_ADDRESS = "0x0200000000000000000000000000000000000001";
const mintValue = ethers.utils.parseEther("1")
const initialValue = ethers.utils.parseEther("10")

const ROLES = {
  NONE: 0,
  MINTER: 1,
  ADMIN: 2
};

describe("ERC20NativeMinter", function () {
  let owner: SignerWithAddress
  let contract: Contract
  let minter: SignerWithAddress
  before(async function () {
    owner = await ethers.getSigner(adminAddress);
    const Token: ContractFactory = await ethers.getContractFactory("ERC20NativeMinter", { signer: owner })
    contract = await Token.deploy(initialValue)
    await contract.deployed()
    const contractAddress: string = contract.address
    console.log(`Contract deployed to: ${contractAddress}`)

    const name: string = await contract.name()
    console.log(`Name: ${name}`)

    const symbol: string = await contract.symbol()
    console.log(`Symbol: ${symbol}`)

    const decimals: string = await contract.decimals()
    console.log(`Decimals: ${decimals}`)

    const signers: SignerWithAddress[] = await ethers.getSigners()
    minter = signers.slice(-1)[0]

    // Fund minter address
    await owner.sendTransaction({
      to: minter.address,
      value: ethers.utils.parseEther("1")
    })
  });

  it("should add contract deployer as owner", async function () {
    const contractOwnerAddr: string = await contract.owner()
    expect(owner.address).to.equal(contractOwnerAddr)
  });

  // this contract is not given minter permission yet, so should not mintdraw
  it("contract should not be able to mintdraw", async function () {
    const minterList = await ethers.getContractAt("INativeMinter", MINT_PRECOMPILE_ADDRESS, owner);
    let contractRole = await minterList.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.NONE)
    try {
      await contract.mintdraw(mintValue)
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })

  it("should be added to minter list", async function () {
    const minterList = await ethers.getContractAt("INativeMinter", MINT_PRECOMPILE_ADDRESS, owner);
    let adminRole = await minterList.readAllowList(adminAddress);
    expect(adminRole).to.be.equal(ROLES.ADMIN)
    let contractRole = await minterList.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.NONE)

    let mintTx = await minterList.setEnabled(contract.address);
    await mintTx.wait()
    contractRole = await minterList.readAllowList(contract.address);
    expect(contractRole).to.be.equal(ROLES.MINTER)
  });

  // admin should mintdraw since it has ERC20 token initially.
  it("admin should mintdraw", async function () {
    let initBalance: BigNumber = await contract.balanceOf(owner.address)
    let initNativeBalance: BigNumber = await ethers.provider.getBalance(owner.address)
    let tx = await contract.mintdraw(mintValue)
    let txRec = await tx.wait()
    let balance = await contract.balanceOf(owner.address)
    expect(balance).to.be.equal(initBalance.sub(mintValue))

    let nativeBalance = await ethers.provider.getBalance(owner.address)
    let gasUsed: BigNumber = txRec.cumulativeGasUsed
    let gasPrice: BigNumber = txRec.effectiveGasPrice
    let txFee = gasUsed.mul(gasPrice)
    expect(nativeBalance).to.be.equal(initNativeBalance.add(mintValue).sub(txFee))
  })

  // minter should not mintdraw since it has no ERC20 token.
  it("minter should not mintdraw ", async function () {
    try {
      await contract.connect(minter).mintdraw(mintValue)
    }
    catch (err) {
      return
    }
    expect.fail("should have errored")
  })

  // minter should not mintdraw since it has no ERC20 token.
  it("should deposit for minter", async function () {
    let initBalance: BigNumber = await contract.balanceOf(minter.address)
    let initNativeBalance: BigNumber = await ethers.provider.getBalance(minter.address)
    let tx = await contract.connect(minter).deposit({ value: mintValue })
    let txRec = await tx.wait()

    let balance = await contract.balanceOf(minter.address)
    expect(balance).to.be.equal(initBalance.add(mintValue))
    let nativeBalance = await ethers.provider.getBalance(minter.address)
    let gasUsed: BigNumber = txRec.cumulativeGasUsed
    let gasPrice: BigNumber = txRec.effectiveGasPrice
    let txFee = gasUsed.mul(gasPrice)
    expect(nativeBalance).to.be.equal(initNativeBalance.sub(mintValue).sub(txFee))
  })

  // minter should mintdraw now since it has ERC20 token.
  it("minter should mintdraw", async function () {
    let initBalance: BigNumber = await contract.balanceOf(minter.address)
    let initNativeBalance: BigNumber = await ethers.provider.getBalance(minter.address)
    let tx = await contract.connect(minter).mintdraw(mintValue)
    let txRec = await tx.wait()

    let balance = await contract.balanceOf(minter.address)
    expect(balance).to.be.equal(initBalance.sub(mintValue))
    let nativeBalance = await ethers.provider.getBalance(minter.address)
    let gasUsed: BigNumber = txRec.cumulativeGasUsed
    let gasPrice: BigNumber = txRec.effectiveGasPrice
    let txFee = gasUsed.mul(gasPrice)
    expect(nativeBalance).to.be.equal(initNativeBalance.add(mintValue).sub(txFee))
  })
})
