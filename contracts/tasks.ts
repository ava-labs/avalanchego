import { task } from "hardhat/config"

const BLACKHOLE_ADDRESS = "0x0100000000000000000000000000000000000000"
const CONTRACT_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000000"
const MINT_ADDRESS = "0x0200000000000000000000000000000000000001"
const TX_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000002"
const FEE_MANAGER_ADDRESS = "0x0200000000000000000000000000000000000003"
const REWARD_MANAGER_ADDRESS = "0x0200000000000000000000000000000000000004"


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
  const accounts = await hre.ethers.getSigners()
  accounts.forEach((account): void => {
    console.log(account.address)
  })
})

task("balances", "Prints the list of account balances", async (args, hre): Promise<void> => {
  const accounts = await hre.ethers.getSigners()
  for (const account of accounts) {
    const balance = await hre.ethers.provider.getBalance(
      account.address
    )
    console.log(`${account.address} has balance ${balance.toString()}`)
  }
})


task("balance", "get the balance")
  .addParam("address", "the address you want to know balance of")
  .setAction(async (args, hre) => {
    const balance = await hre.ethers.provider.getBalance(args.address)
    const balanceInCoin = hre.ethers.formatEther(balance)
    console.log(`balance: ${balanceInCoin} Coin`)
  })

// npx hardhat allowList:readRole --network local --address [address]
task("txAllowList:readRole", "Gets the network transaction allow list")
  .addParam("address", "the address you want to know the allowlist role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:addDeployer --network local --address [address]
task("txAllowList:addDeployer", "Adds an address to the transaction allow list")
  .addParam("address", "the address you want to add as a deployer")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS)
    // ADD CODE BELOW
    await allowList.setEnabled(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:addAdmin --network local --address [address]
task("txAllowList:addAdmin", "Adds an admin on the transaction allow list")
  .addParam("address", "the address you want to add as a admin")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS)
    await allowList.setAdmin(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:revoke --network local --address [address]
task("txAllowList:revoke", "Removes the address from the transaction allow list")
  .addParam("address", "the address you want to revoke all permission")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", TX_ALLOW_LIST_ADDRESS)
    await allowList.setNone(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat rewardManager:currentRewardAddress --network local
task("rewardManager:currentRewardAddress", "Gets the current configured rewarding address")
  .setAction(async (_, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS)
    const areFeeRecipientsAllowed = await rewardManager.areFeeRecipientsAllowed()
    const result = await rewardManager.currentRewardAddress()
    if (areFeeRecipientsAllowed) {
      console.log("Custom Fee Recipients are allowed. (%s)", result)
    } else {
      console.log(`Current reward address is ${result}`)
    }
  })

// npx hardhat rewardManager:areFeeRecipientsAllowed --network local
task("rewardManager:areFeeRecipientsAllowed", "Gets whether the fee recipients are allowed to receive rewards")
  .setAction(async (_, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS)
    const result = await rewardManager.areFeeRecipientsAllowed()
    console.log(result)
  })

// npx hardhat rewardManager:setRewardAddress --network local --address [address]
task("rewardManager:setRewardAddress", "Sets a new reward address")
  .addParam("address", "the address that will receive rewards")
  .setAction(async (args, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS)
    const result = await rewardManager.setRewardAddress(args.address)
    console.log(result)
  })

// npx hardhat rewardManager:allowFeeRecipients --network local
task("rewardManager:allowFeeRecipients", "Allows custom fee recipients to receive rewards")
  .setAction(async (_, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS)
    const result = await rewardManager.allowFeeRecipients()
    console.log(result)
  })

// npx hardhat rewardManager:disableRewards --network local
task("rewardManager:disableRewards", "Disables all rewards, and starts burning fees.")
  .setAction(async (_, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDRESS)
    const result = await rewardManager.disableRewards()
    console.log(result)
  })