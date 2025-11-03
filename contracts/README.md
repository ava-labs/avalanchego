# Subnet EVM Contracts

CONTRACTS HERE ARE [ALPHA SOFTWARE](https://en.wikipedia.org/wiki/Software_release_life_cycle#Alpha) AND ARE NOT AUDITED. USE AT YOUR OWN RISK!

## Introduction

Avalanche is an open-source platform for launching decentralized applications and enterprise blockchain deployments in one interoperable, highly scalable ecosystem. Avalanche gives you complete control on both the network and application layers&mdash;helping you build anything you can imagine.

The Avalanche Network is composed of many subnets and chains. Chains in subnets run with customizable virtual machines. One of these virtual machines is Subnet EVM. The Subnet EVM's API is almost identical to an Ethereum node's API. Subnet EVM brings its own features like minting native tokens via contracts, restrincting contract deployer etc. These features are presented with `Stateful Precompile Contracts`. These contracts are precompiled and deployed when they're activated.

The goal of this guide is to lay out best practices regarding writing, testing and deployment of smart contracts to Avalanche's Subnet EVM. We'll be building smart contracts with development environment [Hardhat](https://hardhat.org).

## Prerequisites

### Go

This project requires Go 1.21 or later. Install from [golang.org](https://golang.org/dl/).

### Solidity Compiler (solc)

The Solidity compiler version 0.8.30 is required to compile contracts. In CI, this is installed automatically via the [setup-solc](https://github.com/ARR4N/setup-solc) GitHub Action.

For local development, install solc 0.8.30:
- **macOS**: `brew install solidity` 
- **Linux**: Follow instructions at [solidity docs](https://docs.soliditylang.org/en/latest/installing-solidity.html)
- **CI**: Automatically installed via GitHub Actions

After installation, create a version-specific alias or symlink:
```bash
# Option 1: Symlink (works in all contexts including go generate)
sudo ln -sf $(which solc) /usr/local/bin/solc-v0.8.30  # Linux
sudo ln -sf $(which solc) /opt/homebrew/bin/solc-v0.8.30  # macOS (Homebrew)

# Option 2: Shell alias (interactive shells only)
echo "alias solc-v0.8.30='solc'" >> ~/.bashrc  # or ~/.zshrc
```

### Solidity and Avalanche

It is also helpful to have a basic understanding of [Solidity](https://docs.soliditylang.org) and [Avalanche](https://docs.avax.network).

## Dependencies

Clone the repo and install dependencies:

```bash
git clone https://github.com/ava-labs/subnet-evm.git
cd subnet-evm/contracts
npm ci  # Installs OpenZeppelin and other Node.js dependencies
```

## Compiling Contracts

Contracts are compiled using `solc` directly, and Go bindings are generated using `abigen` from [libevm](https://github.com/ava-labs/libevm).

OpenZeppelin contracts are included as a git submodule at `contracts/lib/openzeppelin-contracts/` (pinned to v5.4.0).

From the repository root, run:

```bash
./scripts/run_task.sh setup-contracts
```

This will:
1. Compile all Solidity contracts in `contracts/contracts/` to ABIs and bytecode
2. Generate Go bindings in `contracts/bindings/`

The compilation artifacts (`.abi` and `.bin` files) are stored in `contracts/artifacts/` (gitignored).
The generated Go bindings in `contracts/bindings/` are committed to the repository.

### Manual Compilation

To manually compile contracts and generate bindings:

```bash
cd contracts
npm ci  # Install dependencies if not already done
go generate ./...  # Compile contracts and generate bindings
```

All compilation and code generation is configured in `contracts/contracts/compile.go` using `go:generate` directives. The directives execute in order:
1. First, `solc` compiles `.sol` files to `.abi` and `.bin` files in `artifacts/`
2. Then, `abigen` generates Go bindings from the artifacts to `bindings/*.go`

## Write Contracts

`AllowList.sol` is the base contract which provided AllowList precompile capabilities to inheriting contracts.

`ERC20NativeMinter.sol` is based on [Open Zeppelin](https://openzeppelin.com) [ERC20](https://eips.ethereum.org/EIPS/eip-20) contract powered by native minting capabilities of Subnet EVM. ERC20 is a popular smart contract interface. It uses `INativeMinter` interface to interact with `NativeMinter` precompile.

`ExampleDeployerList` shows how `ContractDeployerAllowList` precompile can be used in a smart contract. It uses `IAllowList` to interact with `ContractDeployerAllowList` precompile. When the precompile is activated only those allowed can deploy contracts.

`ExampleFeeManager` shows how a contract can change fee configuration with the `FeeManager` precompile.

All of these `NativeMinter`, `FeeManager` and `AllowList` contracts should be enabled by a chain config in genesis or as an upgrade. See the example genesis under [Tests](#tests) section.

For more information about precompiles see [subnet-evm precompiles](https://github.com/ava-labs/subnet-evm#precompiles).

## Hardhat Config

Hardhat uses `hardhat.config.js` as the configuration file. You can define tasks, networks, compilers and more in that file. For more information see [here](https://hardhat.org/config/).

In Subnet-EVM, we provide a pre-configured file [hardhat.config.ts](https://github.com/ava-labs/subnet-evm/blob/master/contracts/hardhat.config.ts).

The HardHat config file includes a single network configuration: `local`. `local` defaults to using the following values for the RPC URL and the Chain ID:

```js
var local_rpc_uri = process.env.RPC_URI || "http://127.0.0.1:9650/ext/bc/C/rpc";
var local_chain_id = process.env.CHAIN_ID || 99999;
```

You can use this network configuration by providing the environment variables and specifying the `--network` flag, as Subnet-EVM does in its testing suite:

```bash
RPC_URI=http://127.0.0.1:9650/ext/bc/28N1Tv5CZziQ3FKCaXmo8xtxoFtuoVA6NvZykAT5MtGjF4JkGs/rpc CHAIN_ID=77777 npx hardhat test --network local
```

Alternatively, you can copy and paste the `local` network configuration to create a new network configuration for your own local testing. For example, you can copy and paste the `local` network configuration to create your own network and fill in the required details:

```json
{
  "networks": {
    "mynetwork": {
      "url": "http://127.0.0.1:9650/ext/bc/28N1Tv5CZziQ3FKCaXmo8xtxoFtuoVA6NvZykAT5MtGjF4JkGs/rpc",
      "chainId": 33333,
      "accounts": [
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
      ],
      "pollingInterval": "1s"
    }
  }
}
```

By creating your own network configuration in the HardHat config, you can run HardHat commands directly on your subnet:

```bash
npx hardhat accounts --network mynetwork
```

## Hardhat Tasks

You can define custom hardhat tasks in [tasks.ts](https://github.com/ava-labs/avalanche-smart-contract-quickstart/blob/main/tasks.ts). Tasks contain helpers for precompiles `allowList` and `minter`. Precompiles have their own contract already-deployed when they're activated. So these can be called without deploying any intermediate contract. See `npx hardhat --help` for more information about available tasks.

## Tests

Tests are written for a local network which runs a Subnet-EVM Blockchain.

E.g `RPC_URI=http://127.0.0.1:9650/ext/bc/28N1Tv5CZziQ3FKCaXmo8xtxoFtuoVA6NvZykAT5MtGjF4JkGs/rpc CHAIN_ID=77777 npx hardhat test --network local`.

Subnet-EVM must activate any precompiles used in the test in the genesis.
