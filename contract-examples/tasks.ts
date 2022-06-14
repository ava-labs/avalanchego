import { task } from "hardhat/config"
import { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers"
import { BigNumber } from "ethers"

const BLACKHOLE_ADDRESS = "0x0100000000000000000000000000000000000000"
const CONTRACT_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000000"
const MINT_ADDRESS = "0x0200000000000000000000000000000000000001"
const TX_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000002"


const ROLES = {
  0: "None",
  1: "Enabled",
  2: "Admin",
}

const getRole = async (allowList, address) => {
  const role = await allowList.readAllowList(address)
  console.log(`${address} has role: ${ROLES[role.toNumber()]}`)
}

task("accounts", "Prints the list of accounts", async (args, hre): Promise<void> => {
  const accounts: SignerWithAddress[] = await hre.ethers.getSigners()
  accounts.forEach((account: SignerWithAddress): void => {
    console.log(account.address)
  })
})

task("balances", "Prints the list of account balances", async (args, hre): Promise<void> => {
  const accounts: SignerWithAddress[] = await hre.ethers.getSigners()
  for (const account of accounts) {
    const balance: BigNumber = await hre.ethers.provider.getBalance(
      account.address
    )
    console.log(`${account.address} has balance ${balance.toString()}`)
  }
})


task("balance", "a task to get the balance")
  .addParam("address", "the address you want to know balance of")
  .setAction(async (args, hre) => {
    const balance = await hre.ethers.provider.getBalance(args.address)
    const balanceInEth = hre.ethers.utils.formatEther(balance)
    console.log(`balance: ${balanceInEth} ETH`)
  })

// npx hardhat allowList:readRole --network local --address [address]
task("deployerAllowList:readRole", "a task to get the network deployer allow list")
  .addParam("address", "the address you want to know the allowlist role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", CONTRACT_ALLOW_LIST_ADDRESS)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:addDeployer --network local --address [address]
task("deployerAllowList:addDeployer", "a task to add the deployer on the allow list")
  .addParam("address", "the address you want to add as a deployer")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", CONTRACT_ALLOW_LIST_ADDRESS)
    // ADD CODE BELOW
    await allowList.setEnabled(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:addAdmin --network local --address [address]
task("deployerAllowList:addAdmin", "a task to add a admin on the allowlist")
  .addParam("address", "the address you want to add as a admin")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", CONTRACT_ALLOW_LIST_ADDRESS)
    await allowList.setAdmin(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:revoke --network local --address [address]
task("deployerAllowList:revoke", "remove the address from the list")
  .addParam("address", "the address you want to revoke all permission")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", CONTRACT_ALLOW_LIST_ADDRESS)
    await allowList.setNone(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:readRole --network local --address [address]
task("txAllowList:readRole", "a task to get the network transaction allow list")
  .addParam("address", "the address you want to know the allowlist role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:addDeployer --network local --address [address]
task("txAllowList:addDeployer", "a task to add an address to the transaction allow list")
  .addParam("address", "the address you want to add as a deployer")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS)
    // ADD CODE BELOW
    await allowList.setEnabled(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:addAdmin --network local --address [address]
task("txAllowList:addAdmin", "a task to add a admin on the transaction allow list")
  .addParam("address", "the address you want to add as a admin")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS)
    await allowList.setAdmin(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:revoke --network local --address [address]
task("txAllowList:revoke", "remove the address from the transaction allow list")
  .addParam("address", "the address you want to revoke all permission")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS)
    await allowList.setNone(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat minter:readRole --network local --address [address]
task("minter:readRole", "a task to get the network deployer minter list")
  .addParam("address", "the address you want to know the minter role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await getRole(allowList, args.address)
  })


// npx hardhat minter:addMinter --network local --address [address]
task("minter:addMinter", "a task to add the address on the minter list")
  .addParam("address", "the address you want to add as a minter")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await allowList.setEnabled(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat minter:addAdmin --network local --address [address]
task("minter:addAdmin", "a task to add a admin on the minter list")
  .addParam("address", "the address you want to add as a admin")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await allowList.setAdmin(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat minter:revoke --network local --address [address]
task("minter:revoke", "remove the address from the list")
  .addParam("address", "the address you want to revoke all permission")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await allowList.setNone(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat minter:mint --network local --address [address]
task("minter:mint", "mint native token")
  .addParam("address", "the address you want to mint for")
  .addParam("amount", "the amount you want to mint")
  .setAction(async (args, hre) => {
    const minter = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await minter.mintNativeToken(args.address, args.amount)
  })

// npx hardhat minter:burn --network local --address [address]
task("minter:burn", "burn")
  .addParam("amount", "the amount you want to burn (in ETH unit)")
  .setAction(async (args, hre) => {
    const [owner] = await hre.ethers.getSigners()
    const transactionHash = await owner.sendTransaction({
      to: BLACKHOLE_ADDRESS,
      value: hre.ethers.utils.parseEther(args.amount),
    })
    console.log(transactionHash)
  })
