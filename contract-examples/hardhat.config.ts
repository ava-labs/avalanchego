import { task } from "hardhat/config"
import { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers"
import { BigNumber } from "ethers"
import "@nomiclabs/hardhat-waffle"

const MINT_ADDRESS = "0x0200000000000000000000000000000000000001";
const ALLOWLIST_ADDRESS = "0x0200000000000000000000000000000000000000";
const BLACKHOLE_ADDRESS = "0x0100000000000000000000000000000000000000";

const ROLES = {
  0: "None",
  1: "Enabled",
  2: "Admin",
};

const getRole = async (allowList, address) => {
  const role = await allowList.readAllowList(address);
  console.log(`${address} has role: ${ROLES[role.toNumber()]}`);
};

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
    );
    console.log(`${account.address} has balance ${balance.toString()}`);
  }
})


task("balance", "a task to get the balance")
  .addParam("address", "the address you want to know balance of")
  .setAction(async (args, hre) => {
    const balance = await hre.ethers.provider.getBalance(args.address)
    const balanceInEth = hre.ethers.utils.formatEther(balance)
    console.log(`balance: ${balanceInEth} ETH`)
  });

// npx hardhat allowList:readRole --network local --address [address]
task("allowList:readRole", "a task to get the network deployer allow list")
  .addParam("address", "the address you want to know the allowlist role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", ALLOWLIST_ADDRESS);
    await getRole(allowList, args.address);
  });

// npx hardhat allowList:addDeployer --network local --address [address]
task("allowList:addDeployer", "a task to add the deployer on the allow list")
  .addParam("address", "the address you want to add as a deployer")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", ALLOWLIST_ADDRESS);
    // ADD CODE BELOW
    await allowList.setEnabled(args.address);
    await getRole(allowList, args.address);
  });

// npx hardhat allowList:addAdmin --network local --address [address]
task("allowList:addAdmin", "a task to add a admin on the allowlist")
  .addParam("address", "the address you want to add as a admin")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", ALLOWLIST_ADDRESS);
    await allowList.setAdmin(args.address);
    await getRole(allowList, args.address);
  });

// npx hardhat allowList:revoke --network local --address [address]
task("allowList:revoke", "remove the address from the list")
  .addParam("address", "the address you want to revoke all permission")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("IAllowList", ALLOWLIST_ADDRESS);
    await allowList.setNone(args.address);
    await getRole(allowList, args.address);
  });

// npx hardhat minter:readRole --network local --address [address]
task("minter:readRole", "a task to get the network deployer minter list")
  .addParam("address", "the address you want to know the minter role for")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS);
    await getRole(allowList, args.address);
  });


// npx hardhat minter:addMinter --network local --address [address]
task("minter:addMinter", "a task to add the address on the minter list")
  .addParam("address", "the address you want to add as a minter")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS);
    await allowList.setEnabled(args.address);
    await getRole(allowList, args.address);
  });

// npx hardhat minter:addAdmin --network local --address [address]
task("minter:addAdmin", "a task to add a admin on the minter list")
  .addParam("address", "the address you want to add as a admin")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS);
    await allowList.setAdmin(args.address);
    await getRole(allowList, args.address);
  });

// npx hardhat minter:revoke --network local --address [address]
task("minter:revoke", "remove the address from the list")
  .addParam("address", "the address you want to revoke all permission")
  .setAction(async (args, hre) => {
    const allowList = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS);
    await allowList.setNone(args.address);
    await getRole(allowList, args.address);
  });

// npx hardhat minter:mint --network local --address [address]
task("minter:mint", "mint native token")
  .addParam("address", "the address you want to mint for")
  .addParam("amount", "the amount you want to mint")
  .setAction(async (args, hre) => {
    const minter = await hre.ethers.getContractAt("INativeMinter", MINT_ADDRESS);
    await minter.mintNativeToken(args.address, args.amount);
  });

// npx hardhat minter:burn --network local --address [address]
task("minter:burn", "burn")
  .addParam("amount", "the amount you want to burn (in ETH unit)")
  .setAction(async (args, hre) => {
    const [owner] = await hre.ethers.getSigners();
    const transactionHash = await owner.sendTransaction({
      to: BLACKHOLE_ADDRESS,
      value: hre.ethers.utils.parseEther(args.amount),
    });
    console.log(transactionHash)
  });

export default {
  solidity: {
    compilers: [
      {
        version: "0.5.16"
      },
      {
        version: "0.6.2"
      },
      {
        version: "0.6.4"
      },
      {
        version: "0.7.0"
      },
      {
        version: "0.8.0"
      }
    ]
  },
  networks: {
    local: {
      //"http://{ip}:{port}/ext/bc/{chainID}/rpc
      url: "http://127.0.0.1:9650/ext/bc/dRTfPJh4jEaRZoGkPc7xreeYbDGBrGWRV48WAYVyUgApsmzGo/rpc",
      chainId: 43214,
      accounts: [
        "0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
        "0x7b4198529994b0dc604278c99d153cfd069d594753d471171a1d102a10438e07",
        "0x15614556be13730e9e8d6eacc1603143e7b96987429df8726384c2ec4502ef6e",
        "0x31b571bf6894a248831ff937bb49f7754509fe93bbd2517c9c73c4144c0e97dc",
        "0x6934bef917e01692b789da754a0eae31a8536eb465e7bff752ea291dad88c675",
        "0xe700bdbdbc279b808b1ec45f8c2370e4616d3a02c336e68d85d4668e08f53cff",
        "0xbbc2865b76ba28016bc2255c7504d000e046ae01934b04c694592a6276988630",
        "0xcdbfd34f687ced8c6968854f8a99ae47712c4f4183b78dcc4a903d1bfe8cbf60",
        "0x86f78c5416151fe3546dece84fda4b4b1e36089f2dbc48496faf3a950f16157c",
        "0x750839e9dbbd2a0910efe40f50b2f3b2f2f59f5580bb4b83bd8c1201cf9a010a"
      ]
    },
  }
}
