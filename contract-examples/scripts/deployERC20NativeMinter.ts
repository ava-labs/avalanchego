import {
  Contract,
  ContractFactory
} from "ethers"
import { ethers } from "hardhat"

const main = async (): Promise<any> => {
  const Token: ContractFactory = await ethers.getContractFactory("ERC20NativeMinter")
  const token: Contract = await Token.deploy()

  await token.deployed()
  console.log(`Token deployed to: ${token.address}`)
}

main()
  .then(() => process.exit(0))
  .catch(error => {
    console.error(error)
    process.exit(1)
  })
