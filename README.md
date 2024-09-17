# Subnet EVM

[![Build + Test + Release](https://github.com/ava-labs/subnet-evm/actions/workflows/lint-tests-release.yml/badge.svg)](https://github.com/ava-labs/subnet-evm/actions/workflows/lint-tests-release.yml)
[![CodeQL](https://github.com/ava-labs/subnet-evm/actions/workflows/codeql-analysis.yml/badge.svg)](https://github.com/ava-labs/subnet-evm/actions/workflows/codeql-analysis.yml)

[Avalanche](https://docs.avax.network/overview/getting-started/avalanche-platform) is a network composed of multiple blockchains.
Each blockchain is an instance of a Virtual Machine (VM), much like an object in an object-oriented language is an instance of a class.
That is, the VM defines the behavior of the blockchain.

Subnet EVM is the [Virtual Machine (VM)](https://docs.avax.network/learn/avalanche/virtual-machines) that defines the Subnet Contract Chains. Subnet EVM is a simplified version of [Coreth VM (C-Chain)](https://github.com/ava-labs/coreth).

This chain implements the Ethereum Virtual Machine and supports Solidity smart contracts as well as most other Ethereum client functionality.

## Building

The Subnet EVM runs in a separate process from the main AvalancheGo process and communicates with it over a local gRPC connection.

### AvalancheGo Compatibility

```text
[v0.6.0] AvalancheGo@v1.11.0-v1.11.1 (Protocol Version: 33)
[v0.6.1] AvalancheGo@v1.11.0-v1.11.1 (Protocol Version: 33)
[v0.6.2] AvalancheGo@v1.11.2 (Protocol Version: 34)
[v0.6.3] AvalancheGo@v1.11.3-v1.11.9 (Protocol Version: 35)
[v0.6.4] AvalancheGo@v1.11.3-v1.11.9 (Protocol Version: 35)
[v0.6.5] AvalancheGo@v1.11.3-v1.11.9 (Protocol Version: 35)
[v0.6.6] AvalancheGo@v1.11.3-v1.11.9 (Protocol Version: 35)
[v0.6.7] AvalancheGo@v1.11.3-v1.11.9 (Protocol Version: 35)
[v0.6.8] AvalancheGo@v1.11.10 (Protocol Version: 36)
[v0.6.9] AvalancheGo@v1.11.11 (Protocol Version: 37)
[v0.6.10] AvalancheGo@v1.11.11 (Protocol Version: 37)
```

## API

The Subnet EVM supports the following API namespaces:

- `eth`
- `personal`
- `txpool`
- `debug`

Only the `eth` namespace is enabled by default.
Subnet EVM is a simplified version of [Coreth VM (C-Chain)](https://github.com/ava-labs/coreth).
Full documentation for the C-Chain's API can be found [here](https://docs.avax.network/apis/avalanchego/apis/c-chain).

## Compatibility

The Subnet EVM is compatible with almost all Ethereum tooling, including [Remix](https://docs.avax.network/build/dapp/smart-contracts/remix-deploy), [Metamask](https://docs.avax.network/build/dapp/chain-settings), and [Foundry](https://docs.avax.network/build/dapp/smart-contracts/toolchains/foundry).

## Differences Between Subnet EVM and Coreth

- Added configurable fees and gas limits in genesis
- Merged Avalanche hardforks into the single "Subnet EVM" hardfork
- Removed Atomic Txs and Shared Memory
- Removed Multicoin Contract and State

## Block Format

To support these changes, there have been a number of changes to the SubnetEVM block format compared to what exists on the C-Chain and Ethereum. Here we list the changes to the block format as compared to Ethereum.

### Block Header

- `BaseFee`: Added by EIP-1559 to represent the base fee of the block (present in Ethereum as of EIP-1559)
- `BlockGasCost`: surcharge for producing a block faster than the target rate

## Create an EVM Subnet on a Local Network

### Clone Subnet-evm

First install Go 1.21.12 or later. Follow the instructions [here](https://go.dev/doc/install). You can verify by running `go version`.

Set `$GOPATH` environment variable properly for Go to look for Go Workspaces. Please read [this](https://go.dev/doc/code) for details. You can verify by running `echo $GOPATH`.

As a few software will be installed into `$GOPATH/bin`, please make sure that `$GOPATH/bin` is in your `$PATH`, otherwise, you may get error running the commands below.

Download the `subnet-evm` repository into your `$GOPATH`:

```sh
cd $GOPATH
mkdir -p src/github.com/ava-labs
cd src/github.com/ava-labs
git clone git@github.com:ava-labs/subnet-evm.git
cd subnet-evm
```

This will clone and checkout to `master` branch.

### Run Local Network

To run a local network, it is recommended to use the [avalanche-cli](https://github.com/ava-labs/avalanche-cli#avalanche-cli) to set up an instance of Subnet-EVM on a local Avalanche Network.

There are two options when using the Avalanche-CLI:

1. Use an official Subnet-EVM release: https://docs.avax.network/subnets/build-first-subnet
2. Build and deploy a locally built (and optionally modified) version of Subnet-EVM: https://docs.avax.network/subnets/create-custom-subnet
