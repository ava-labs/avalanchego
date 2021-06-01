# Coreth and the C-Chain

[Avalanche](https://docs.avax.network/learn/platform-overview) is a network composed of multiple blockchains.
Each blockchain is an instance of a Virtual Machine (VM), much like an object in an object-oriented language is an instance of a class.
That is, the VM defines the behavior of the blockchain.
Coreth (from core Ethereum) is the [Virtual Machine (VM)](https://docs.avax.network/learn/platform-overview#virtual-machines) that defines the Contract Chain (C-Chain).
This chain implements the Ethereum Virtual Machine and supports Solidity smart contracts as well as most other Ethereum client functionality.

## Building

The C-Chain runs in a separate process from the main AvalancheGo process and communicates with it over a local gRPC connection.
AvalancheGo's build script downloads Coreth, compiles it, and places the binary into the `avalanchego/build/plugins` directory.

## API

The C-Chain supports the following API namespaces:

- `eth`
- `personal`
- `txpool`
- `debug`

Only the `eth` namespace is enabled by default. 
To enable the other namespaces see the instructions for passing in the `coreth-config` parameter to AvalancheGo: https://docs.avax.network/build/references/command-line-interface#plugins.
Full documentation for the C-Chain's API can be found [here.](https://docs.avax.network/build/avalanchego-apis/contract-chain-c-chain-api)

## Compatibility

The C-Chain is compatible with almost all Ethereum tooling, including [Remix,](https://docs.avax.network/build/tutorials/smart-contracts/deploy-a-smart-contract-on-avalanche-using-remix-and-metamask) [Metamask](https://docs.avax.network/build/tutorials/smart-contracts/deploy-a-smart-contract-on-avalanche-using-remix-and-metamask) and [Truffle.](https://docs.avax.network/build/tutorials/smart-contracts/using-truffle-with-the-avalanche-c-chain)

## Differences Between Avalanche C-Chain and Ethereum

### Atomic Transactions

As a network composed of multiple blockchains, Avalanche uses *atomic transactions* to move assets between chains. Coreth modifies the Ethereum block format by adding an *ExtraData* field, which contains the atomic transactions.

### Avalanche Native Tokens (ANTs)

The C-Chain supports Avalanche Native Tokens, which are created on the X-Chain using precompiled contracts. These precompiled contracts *nativeAssetCall* and *nativeAssetBalance* support the same interface for ANTs as *CALL* and *BALANCE* do for AVAX with the added parameter of *assetID* to specify the asset.

For the full documentation of precompiles for interacting with ANTs and using them in ARC-20s, see [here](https://docs.avax.network/build/references/coreth-arc20s).

### Block Timing

Blocks are produced asynchronously in Snowman Consensus, so the timing assumptions that apply to Ethereum do not apply to Coreth. To support block production in an async environment, a block is permitted to have the same timestamp as its parent. Since there is no general assumption that a block will be produced every 10 seconds, smart contracts built on Avalanche should use the block timestamp instead of the block number for their timing assumptions.

A block with a timestamp more than 10 seconds in the future will not be considered valid. However, a block with a timestamp more than 10 seconds in the past will still be considered valid as long as its timestamp is greater than or equal to the timestamp of its parent block.
