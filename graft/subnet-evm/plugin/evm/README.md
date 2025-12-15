# EVM Package

The EVM package implements the AvalancheGo VM interface.

## VM

The VM creates the Ethereum backend and provides basic block building, parsing, and retrieval logic to the consensus engine.

## APIs

The VM creates APIs for the node through the function `CreateHandlers()`. CreateHandlers returns the `Service` struct to serve subnet-evm specific APIs. Additionally, the Ethereum backend APIs are also returned at the `/rpc` extension.

## Block Handling

The VM implements `buildBlock`, `parseBlock`, and `getBlock` which are used by the `chain` package from AvalancheGo to construct a metered state. The metered state wraps blocks returned by these functions with an efficient caching layer and maintains the required invariants for blocks that get returned to the consensus engine.

The VM uses the block type from [`libevm/core/types`](https://github.com/ava-labs/libevm/tree/master/core/types) and extends it with Avalanche-specific fields (such as `ExtDataHash`, `BlockGasCost`, and `Version`) using libevm's extensibility mechanism (defined in [`customtypes`](customtypes/)), then wraps it with [`wrappedBlock`](wrapped_block.go) to implement the AvalancheGo Block interface. The core package's BlockChain type in [blockchain.go](../../core/blockchain.go) handles the insertion and storage of blocks into the chain.

## Block

The Block type implements the AvalancheGo ChainVM Block interface. The key functions for this interface are `Verify()`, `Accept()`, `Reject()`, and `Status()`.

The Block type (implemented as [`wrappedBlock`](wrapped_block.go)) wraps the block type from [`libevm/core/types`](https://github.com/ava-labs/libevm/tree/master/core/types) and implements these functions to allow the consensus engine to verify blocks as valid, perform consensus, and mark them as accepted or rejected. Blocks contain standard Ethereum transactions that enable cross-chain asset transfers. Blocks may also include optional block extensions for extensible VM functionality. See the documentation in AvalancheGo for the more detailed VM invariants that are maintained here.
