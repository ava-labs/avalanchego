import {
  Contract,
  ContractFactory
} from "ethers"
import { ethers } from "hardhat"

const main = async (): Promise<any> => {
  const contractFactory: ContractFactory = await ethers.getContractFactory("ExampleDeployerList")
  const contract: Contract = await contractFactory.deploy()

  await contract.deployed()
  console.log(`Contract deployed to: ${contract.address}`)
}

main()
  .then(() => process.exit(0))
  .catch(error => {
    console.error(error)
    process.exit(1)
  })
