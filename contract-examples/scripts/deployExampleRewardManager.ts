import {
  Contract,
  ContractFactory
} from "ethers"
import { ethers } from "hardhat"


const main = async (): Promise<any> => {
  const Contract: ContractFactory = await ethers.getContractFactory("ExampleRewardManager")
  const contract: Contract = await Contract.deploy()

  await contract.deployed()
  console.log(`Contract deployed to: ${contract.address}`)
}

main()
  .then(() => process.exit(0))
  .catch(error => {
    console.error(error)
    process.exit(1)
  })
