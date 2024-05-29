import { ethers } from "hardhat"
import { ExampleTxAllowList } from "typechain-types"

const main = async (): Promise<any> => {
  const contract: ExampleTxAllowList = await ethers.deployContract("ExampleTxAllowList")

  await contract.waitForDeployment()
  console.log(`Contract deployed to: ${contract.target}`)
}

main()
  .then(() => process.exit(0))
  .catch(error => {
    console.error(error)
    process.exit(1)
  })
