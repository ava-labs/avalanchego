import { ethers } from "hardhat"
import { ERC20NativeMinter } from "typechain-types"

const main = async (): Promise<any> => {
  const token: ERC20NativeMinter  = await ethers.deployContract("ERC20NativeMinter")
  await token.waitForDeployment()
  console.log(`Token deployed to: ${token.target}`)
}

main()
  .then(() => process.exit(0))
  .catch(error => {
    console.error(error)
    process.exit(1)
  })
