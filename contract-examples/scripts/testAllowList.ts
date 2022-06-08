import { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers"
import { expect } from "chai"
import {
  Contract,
  ContractFactory,
} from "ethers"
import { ethers } from "hardhat"
import { abi } from "../artifacts/contracts/IAllowList.sol/IAllowList.json"

const adminAddress: string = "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
const TX_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000002"



const main = async (): Promise<any> => {
  let owner = await ethers.getSigner(adminAddress)
  let contract = await ethers.getContractAt(abi, TX_ALLOW_LIST_ADDRESS, owner)
  let role = await contract.readAllowList(owner.address)
  console.log("Role", role)
}

main()
  .then(() => process.exit(0))
  .catch(error => {
    console.error(error)
    process.exit(1)
  })
