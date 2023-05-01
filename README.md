# Subnet EVM

[![Build + Test + Release](https://github.com/ava-labs/subnet-evm/actions/workflows/lint-tests-release.yml/badge.svg)](https://github.com/ava-labs/subnet-evm/actions/workflows/lint-tests-release.yml)
[![CodeQL](https://github.com/ava-labs/subnet-evm/actions/workflows/codeql-analysis.yml/badge.svg)](https://github.com/ava-labs/subnet-evm/actions/workflows/codeql-analysis.yml)

[Avalanche](https://docs.avax.network/overview/getting-started/avalanche-platform) is a network composed of multiple blockchains.
Each blockchain is an instance of a Virtual Machine (VM), much like an object in an object-oriented language is an instance of a class.
That is, the VM defines the behavior of the blockchain.

Subnet EVM is the [Virtual Machine (VM)](https://docs.avax.network/overview/getting-started/avalanche-platform/#virtual-machines) that defines the Subnet Contract Chains. Subnet EVM is a simplified version of [Coreth VM (C-Chain)](https://github.com/ava-labs/coreth).

This chain implements the Ethereum Virtual Machine and supports Solidity smart contracts as well as most other Ethereum client functionality.

## Building

The Subnet EVM runs in a separate process from the main AvalancheGo process and communicates with it over a local gRPC connection.

### AvalancheGo Compatibility

```text
[v0.1.0] AvalancheGo@v1.7.0-v1.7.4 (Protocol Version: 9)
[v0.1.1-v0.1.2] AvalancheGo@v1.7.5-v1.7.6 (Protocol Version: 10)
[v0.2.0] AvalancheGo@v1.7.7-v1.7.9 (Protocol Version: 11)
[v0.2.1] AvalancheGo@v1.7.10 (Protocol Version: 12)
[v0.2.2] AvalancheGo@v1.7.11-v1.7.12 (Protocol Version: 14)
[v0.2.3] AvalancheGo@v1.7.13-v1.7.16 (Protocol Version: 15)
[v0.2.4] AvalancheGo@v1.7.13-v1.7.16 (Protocol Version: 15)
[v0.2.5] AvalancheGo@v1.7.13-v1.7.16 (Protocol Version: 15)
[v0.2.6] AvalancheGo@v1.7.13-v1.7.16 (Protocol Version: 15)
[v0.2.7] AvalancheGo@v1.7.13-v1.7.16 (Protocol Version: 15)
[v0.2.8] AvalancheGo@v1.7.13-v1.7.18 (Protocol Version: 15)
[v0.2.9] AvalancheGo@v1.7.13-v1.7.18 (Protocol Version: 15)
[v0.3.0] AvalancheGo@v1.8.0-v1.8.6 (Protocol Version: 16)
[v0.4.0] AvalancheGo@v1.9.0 (Protocol Version: 17)
[v0.4.1] AvalancheGo@v1.9.1 (Protocol Version: 18)
[v0.4.2] AvalancheGo@v1.9.1 (Protocol Version: 18)
[v0.4.3] AvalancheGo@v1.9.2-v1.9.3 (Protocol Version: 19)
[v0.4.4] AvalancheGo@v1.9.2-v1.9.3 (Protocol Version: 19)
[v0.4.5] AvalancheGo@v1.9.4 (Protocol Version: 20)
[v0.4.6] AvalancheGo@v1.9.4 (Protocol Version: 20)
[v0.4.7] AvalancheGo@v1.9.5 (Protocol Version: 21)
[v0.4.8] AvalancheGo@v1.9.6-v1.9.8 (Protocol Version: 22)
[v0.4.9] AvalancheGo@v1.9.9 (Protocol Version: 23)
[v0.4.10] AvalancheGo@v1.9.9 (Protocol Version: 23)
[v0.4.11] AvalancheGo@v1.9.10-v1.9.16 (Protocol Version: 24)
[v0.4.12] AvalancheGo@v1.9.10-v1.9.16 (Protocol Version: 24)
[v0.5.0] AvalancheGo@v1.10.0 (Protocol Version: 25)
[v0.5.1] AvalancheGo@v1.10.1 (Protocol Version: 26)
```

## API

The Subnet EVM supports the following API namespaces:

- `eth`
- `personal`
- `txpool`
- `debug`

Only the `eth` namespace is enabled by default.
Full documentation for the C-Chain's API can be found [here.](https://docs.avax.network/apis/avalanchego/apis/c-chain)

## Compatibility

The Subnet EVM is compatible with almost all Ethereum tooling, including [Remix](https://docs.avax.network/dapps/smart-contracts/deploy-a-smart-contract-on-avalanche-using-remix-and-metamask/), [Metamask](https://docs.avax.network/dapps/smart-contracts/deploy-a-smart-contract-on-avalanche-using-remix-and-metamask/) and [Truffle](https://docs.avax.network/dapps/smart-contracts/using-truffle-with-the-avalanche-c-chain/).

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

First install Go 1.19.6 or later. Follow the instructions [here](https://golang.org/doc/install). You can verify by running `go version`.

Set `$GOPATH` environment variable properly for Go to look for Go Workspaces. Please read [this](https://go.dev/doc/gopath_code) for details. You can verify by running `echo $GOPATH`.

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

To run a local network, it is recommended to use the [avalanche-cli](https://github.com/ava-labs/avalanche-cli#avalanche-cli) to set up an instance of Subnet-EVM on an local Avalanche Network.

There are two options when using the Avalanche-CLI:

1. Use an official Subnet-EVM release: https://docs.avax.network/subnets/build-first-subnet
2. Build and deploy a locally built (and optionally modified) version of Subnet-EVM: https://docs.avax.network/subnets/create-custom-subnet
