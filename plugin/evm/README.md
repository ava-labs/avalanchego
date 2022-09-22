# EVM Package

The EVM package implements the AvalancheGo VM interface.

## VM

The VM creates the Ethereum backend and provides basic block building, parsing, and retrieval logic to the consensus engine.

## APIs

The VM creates APIs for the node through the function `CreateHandlers()`. CreateHandlers returns the `Service` struct to serve Subnet-EVM specific APIs. Additionally, the Ethereum backend APIs are also returned at the `/rpc` extension.

## Block Handling

The VM implements `buildBlock`, `parseBlock`, and `getBlock` and uses the `chain` package from AvalancheGo to construct a metered state, which uses these functions to implement an efficient caching layer and maintain the required invariants for blocks that get returned to the consensus engine.

To do this, the VM uses a modified version of the Ethereum RLP block type [here](../../core/types/block.go) and uses the core package's BlockChain type [here](../../core/blockchain.go) to handle the insertion and storage of blocks into the chain.

## Block

The Block type implements the AvalancheGo ChainVM Block interface. The key functions for this interface are `Verify()`, `Accept()`, `Reject()`, and `Status()`.

The Block type wraps the stateless block type [here](../../core/types/block.go) and implements these functions to allow the consensus engine to verify blocks as valid, perform consensus, and mark them as accepted or rejected. See the documentation in AvalancheGo for the more detailed VM invariants that are maintained here.
