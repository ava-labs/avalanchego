# Subnet EVM Contracts

CONTRACTS HERE ARE [ALPHA SOFTWARE](https://en.wikipedia.org/wiki/Software_release_life_cycle#Alpha) AND ARE NOT YET AUDITED. USE AT YOUR OWN RISK!

## Introduction

Avalanche is an open-source platform for launching decentralized applications and enterprise blockchain deployments in one interoperable, highly scalable ecosystem. Avalanche gives you complete control on both the network and application layers&mdash;helping you build anything you can imagine.

The Avalanche Network is composed of many subnets and chains. Chains in subnets run with customizable virtual machines. One of these virtual machines is Subnet EVM. The Subnet EVM's API is almost identical to an Ethereum node's API. Subnet EVM brings its own features like minting native tokens via contracts, restrincting contract deployer etc. These features are presented with `Stateful Precompile Contracts`. These contracts are precompiled and deployed when they're activated.

The goal of this guide is to lay out best practices regarding writing, testing and deployment of smart contracts to Avalanche's Subnet EVM. We'll be building smart contracts with development environment [Hardhat](https://hardhat.org).

## Prerequisites

### NodeJS and Yarn

First, install the LTS (long-term support) version of [nodejs](https://nodejs.org/en). This is `16.2.0` at the time of writing. NodeJS bundles `npm`.

Next, install [yarn](https://yarnpkg.com):

```zsh
npm install -g yarn
```

### Solidity and Avalanche

It is also helpful to have a basic understanding of [Solidity](https://docs.soliditylang.org) and [Avalanche](https://docs.avax.network).

## Dependencies

Clone the repo and install the necessary packages via `yarn`.

```zsh
$ git clone https://github.com/ava-labs/subnet-evm.git
$ cd contract-examples
$ yarn
```

## Write Contracts

`AllowList.sol` is the base contract which provided AllowList precompile capabilities to inheriting contracts.

`ERC20NativeMinter.sol` is based on [Open Zeppelin](https://openzeppelin.com) [ERC20](https://eips.ethereum.org/EIPS/eip-20) contract powered by native minting capabilities of Subnet EVM. ERC20 is a popular smart contract interface. It uses `INativeMinter` interface to interact with `NativeMinter` precompile.

`ExampleDeployerList` shows how `ContractDeployerAllowList` precompile can be used in a smart contract. It uses `IAllowList` to interact with `ContractDeployerAllowList` precompile. When the precompile is activated only those allowed can deploy contracts.

`ExampleFeeManager` shows how a contract can change fee configuration with the `FeeConfigManager` precompile.

All of these `NativeMinter`, `FeeManager` and `AllowList` contracts should be enabled by a chain config in genesis or as an upgrade. See the example genesis under [Tests](#tests) section.

For more information about precompiles see [subnet-evm precompiles](https://github.com/ava-labs/subnet-evm#precompiles).

## Hardhat Config

Hardhat uses `hardhat.config.js` as the configuration file. You can define tasks, networks, compilers and more in that file. For more information see [here](https://hardhat.org/config/).

In our repository we use a pre-configured file [hardhat.config.ts](https://github.com/ava-labs/avalanche-smart-contract-quickstart/blob/main/hardhat.config.ts). This file configures necessary network information to provide smooth interaction with Avalanche. There are two networks in the hardhat config: `e2e` and `local`. `e2e` network is used for e2e tests and should not be changed. `local` network is used by tasks and for local deployments. There are also some pre-defined private keys for these networks. Each chain in subnets has their own RPC URL. Subnet EVM's RPC URL is in form of: `"http://{ip}:{port}/ext/bc/{chainID}/rpc`. When you create your own subnet and Subnet EVM chain `{chainID}` will be different. You can change `local` network RPC url with the `local_rpc.json`. There is an example file named with `local_rpc_example.json`. You can copy & rename this file to customize the url:

```
cp local_rpc.example.json local_rpc.json
```

Do not forget to set correct URL in the `local_rpc.json` file.

## Hardhat Tasks

You can define custom hardhat tasks in [tasks.ts](https://github.com/ava-labs/avalanche-smart-contract-quickstart/blob/main/tasks.ts). Tasks contain helpers for precompiles `allowList` and `minter`. Precompiles have their own contract already-deployed when they're activated. So these can be called without deploying any intermediate contract. See `npx hardhat --help` for more information about available tasks.

## Tests

Tests are written for a local network which runs a Subnet-EVM chain. E.g `npx hardhat test --network local`. Subnet-EVM must activate required precompiles with following genesis:

```json
{
  "config": {
    "chainId": 43214,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip150Hash": "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0",
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "subnetEVMTimestamp": 0,
    "feeConfig": {
      "gasLimit": 8000000,
      "minBaseFee": 25000000000,
      "targetGas": 15000000,
      "baseFeeChangeDenominator": 36,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 1000000,
      "targetBlockRate": 2,
      "blockGasCostStep": 200000
    },
    "contractDeployerAllowListConfig": {
      "blockTimestamp": 0,
      "adminAddresses": ["0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"]
    },
    "contractNativeMinterConfig": {
      "blockTimestamp": 0,
      "adminAddresses": ["0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"]
    },
    "allowFeeRecipients": false
  },
  "alloc": {
    "8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
      "balance": "0x295BE96E64066972000000"
    }
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x00",
  "gasLimit": "0x7A1200",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```
