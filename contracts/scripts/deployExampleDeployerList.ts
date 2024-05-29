import { ethers } from "hardhat"
import { ExampleDeployerList } from "typechain-types"

const main = async (): Promise<any> => {
  const contract: ExampleDeployerList  = await ethers.deployContract("ExampleDeployerList")
  await contract.waitForDeployment()
  console.log(`Contract deployed to: ${contract.target}`)
}

main()
  .then(() => process.exit(0))
  .catch(error => {
    console.error(error)
    process.exit(1)
  })
