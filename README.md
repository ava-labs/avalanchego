# Subnet EVM

[![Releases](https://img.shields.io/github/v/tag/ava-labs/subnet-evm.svg?sort=semver)](https://github.com/ava-labs/subnet-evm/releases)
[![CI](https://github.com/ava-labs/subnet-evm/actions/workflows/ci.yml/badge.svg)](https://github.com/ava-labs/subnet-evm/actions/workflows/ci.yml)
[![CodeQL](https://github.com/ava-labs/subnet-evm/actions/workflows/codeql-analysis.yml/badge.svg)](https://github.com/ava-labs/subnet-evm/actions/workflows/codeql-analysis.yml)
[![License](https://img.shields.io/github/license/ava-labs/subnet-evm)](https://github.com/ava-labs/subnet-evm/blob/master/LICENSE)

[Avalanche](https://docs.avax.network/avalanche-l1s) is a network composed of multiple blockchains.
Each blockchain is an instance of a Virtual Machine (VM), much like an object in an object-oriented language is an instance of a class.
That is, the VM defines the behavior of the blockchain.

Subnet EVM is the [Virtual Machine (VM)](https://docs.avax.network/learn/virtual-machines) that defines the Subnet Contract Chains. Subnet EVM is a simplified version of [Coreth VM (C-Chain)](https://github.com/ava-labs/coreth).

This chain implements the Ethereum Virtual Machine and supports Solidity smart contracts as well as most other Ethereum client functionality.

## Building

The Subnet EVM runs in a separate process from the main AvalancheGo process and communicates with it over a local gRPC connection.

### AvalancheGo Compatibility

```text
[v0.7.0] AvalancheGo@v1.12.0-v1.12.1 (Protocol Version: 38)
[v0.7.1] AvalancheGo@v1.12.2 (Protocol Version: 39)
[v0.7.2] AvalancheGo@v1.12.2/1.13.0-fuji (Protocol Version: 39)
[v0.7.3] AvalancheGo@v1.12.2/1.13.0 (Protocol Version: 39)
[v0.7.4] AvalancheGo@v1.13.1 (Protocol Version: 40)
[v0.7.5] AvalancheGo@v1.13.2 (Protocol Version: 41)
[v0.7.6] AvalancheGo@v1.13.3-rc.2 (Protocol Version: 42)
[v0.7.7] AvalancheGo@v1.13.3 (Protocol Version: 42)
[v0.7.8] AvalancheGo@v1.13.4 (Protocol Version: 43)
[v0.7.9] AvalancheGo@v1.13.5 (Protocol Version: 43)
```

## API

The Subnet EVM supports the following API namespaces:

- `eth`
- `personal`
- `txpool`
- `debug`

Only the `eth` namespace is enabled by default.
Subnet EVM is a simplified version of [Coreth VM (C-Chain)](https://github.com/ava-labs/coreth).
Full documentation for the C-Chain's API can be found [here](https://build.avax.network/docs/api-reference/c-chain/api).

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

First install Go 1.24.8 or later. Follow the instructions [here](https://go.dev/doc/install). You can verify by running `go version`.

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

1. Use an official Subnet-EVM release: <https://docs.avax.network/subnets/build-first-subnet>
2. Build and deploy a locally built (and optionally modified) version of Subnet-EVM: <https://docs.avax.network/subnets/create-custom-subnet>

## Releasing

You can refer to the [`docs/releasing/README.md`](docs/releasing/README.md) file for the release process.
