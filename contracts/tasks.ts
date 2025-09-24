import { task } from "hardhat/config"

const BLACKHOLE_ADDRESS = "0x0100000000000000000000000000000000000000"
const CONTRACT_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000000"
const MINT_ADDRESS = "0x0200000000000000000000000000000000000001"
const TX_ALLOW_LIST_ADDRESS = "0x0200000000000000000000000000000000000002"
const FEE_MANAGER_ADDRESS = "0x0200000000000000000000000000000000000003"
const REWARD_MANAGER_ADDDRESS = "0x0200000000000000000000000000000000000004"


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
task("deployerAllowList:readRole", "Gets the network deployer allow list")
  .addParam("address", "the address you want to know the allowlist role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", CONTRACT_ALLOW_LIST_ADDRESS)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:addDeployer --network local --address [address]
task("deployerAllowList:addDeployer", "Adds the deployer on the allow list")
  .addParam("address", "the address you want to add as a deployer")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", CONTRACT_ALLOW_LIST_ADDRESS)
    // ADD CODE BELOW
    await allowList.setEnabled(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:addAdmin --network local --address [address]
task("deployerAllowList:addAdmin", "Adds an admin on the allowlist")
  .addParam("address", "the address you want to add as a admin")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", CONTRACT_ALLOW_LIST_ADDRESS)
    await allowList.setAdmin(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat allowList:revoke --network local --address [address]
task("deployerAllowList:revoke", "Removes the address from the list")
  .addParam("address", "the address you want to revoke all permission")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", CONTRACT_ALLOW_LIST_ADDRESS)
    await allowList.setNone(args.address)
    await getRole(allowList, args.address)
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

// npx hardhat minter:readRole --network local --address [address]
task("minter:readRole", "Gets the network deployer minter list")
  .addParam("address", "the address you want to know the minter role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await getRole(allowList, args.address)
  })


// npx hardhat minter:addMinter --network local --address [address]
task("minter:addMinter", "Adds the address on the minter list")
  .addParam("address", "the address you want to add as a minter")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await allowList.setEnabled(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat minter:addAdmin --network local --address [address]
task("minter:addAdmin", "Adds an admin on the minter list")
  .addParam("address", "the address you want to add as a admin")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await allowList.setAdmin(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat minter:revoke --network local --address [address]
task("minter:revoke", "Removes the address from the list")
  .addParam("address", "the address you want to revoke all permission")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await allowList.setNone(args.address)
    await getRole(allowList, args.address)
  })

// npx hardhat minter:mint --network local --address [address]
task("minter:mint", "Mints native tokens")
  .addParam("address", "the address you want to mint for")
  .addParam("amount", "the amount you want to mint")
  .setAction(async (args, hre) => {
    const minter = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS)
    await minter.mintNativeCoin(args.address, args.amount)
  })

// npx hardhat minter:burn --network local --address [address]
task("minter:burn", "Burns native tokens")
  .addParam("amount", "the amount you want to burn (in AVAX unit)")
  .setAction(async (args, hre) => {
    const [owner] = await hre.ethers.getSigners()
    const transactionHash = await owner.sendTransaction({
      to: BLACKHOLE_ADDRESS,
      value: hre.ethers.parseEther(args.amount),
    })
    console.log(transactionHash)
  })

// npx hardhat feeManager:set --network local --address [address]
task("feeManager:set", "Sets the provided fee config")
  .addParam("gaslimit", "", undefined, undefined, false)
  .addParam("targetblockrate", "", undefined, undefined, false)
  .addParam("minbasefee", "", undefined, undefined, false)
  .addParam("targetgas", "", undefined, undefined, false)
  .addParam("basefeechangedenominator", "", undefined, undefined, false)
  .addParam("minblockgascost", "", undefined, undefined, false)
  .addParam("maxblockgascost", "", undefined, undefined, false)
  .addParam("blockgascoststep", "", undefined, undefined, false)

  .setAction(async (args, hre) => {
    const feeManager = await hre.ethers.getContractAt("IFeeManager", FEE_MANAGER_ADDRESS)
    await feeManager.setFeeConfig(
      args.gaslimit,
      args.targetblockrate,
      args.minbasefee,
      args.targetgas,
      args.basefeechangedenominator,
      args.minblockgascost,
      args.maxblockgascost,
      args.blockgascoststep)
  })

task("feeManager:get", "Gets the fee config")
  .setAction(async (_, hre) => {
    const feeManager = await hre.ethers.getContractAt("IFeeManager", FEE_MANAGER_ADDRESS)
    const result = await feeManager.getFeeConfig()
    console.log(`Fee Manager Precompile Config is set to:
  gasLimit: ${result[0]}
  targetBlockRate: ${result[1]}
  minBaseFee: ${result[2]}
  targetGas: ${result[3]}
  baseFeeChangeDenominator: ${result[4]}
  minBlockGasCost: ${result[5]}
  maxBlockGasCost: ${result[6]}
  blockGasCostStep: ${result[7]}`)
  })


// npx hardhat feeManager:readRole --network local --address [address]
task("feeManager:readRole", "Gets the network deployer minter list")
  .addParam("address", "the address you want to know the minter role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IFeeManager", FEE_MANAGER_ADDRESS)
    await getRole(allowList, args.address)
  })


// npx hardhat rewardManager:currentRewardAddress --network local
task("rewardManager:currentRewardAddress", "Gets the current configured rewarding address")
  .setAction(async (_, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDDRESS)
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
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDDRESS)
    const result = await rewardManager.areFeeRecipientsAllowed()
    console.log(result)
  })

// npx hardhat rewardManager:setRewardAddress --network local --address [address]
task("rewardManager:setRewardAddress", "Sets a new reward address")
  .addParam("address", "the address that will receive rewards")
  .setAction(async (args, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDDRESS)
    const result = await rewardManager.setRewardAddress(args.address)
    console.log(result)
  })

// npx hardhat rewardManager:allowFeeRecipients --network local
task("rewardManager:allowFeeRecipients", "Allows custom fee recipients to receive rewards")
  .setAction(async (_, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDDRESS)
    const result = await rewardManager.allowFeeRecipients()
    console.log(result)
  })

// npx hardhat rewardManager:disableRewards --network local
task("rewardManager:disableRewards", "Disables all rewards, and starts burning fees.")
  .setAction(async (_, hre) => {
    const rewardManager = await hre.ethers.getContractAt("IRewardManager", REWARD_MANAGER_ADDDRESS)
    const result = await rewardManager.disableRewards()
    console.log(result)
  })