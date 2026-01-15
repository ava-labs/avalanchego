# EVM Package

The EVM package implements the AvalancheGo VM interface.

## VM

The VM creates the Ethereum backend and provides basic block building, parsing, and retrieval logic to the consensus engine.

## APIs

The VM creates APIs for the node through the function `CreateHandlers()`. CreateHandlers returns the `Service` struct to serve Coreth specific APIs. Additionally, the Ethereum backend APIs are also returned at the `/rpc` extension.

## Block Handling

The VM implements `buildBlock`, `parseBlock`, and `getBlock` which are used by the `chain` package from AvalancheGo to construct a metered state. The metered state wraps blocks returned by these functions with an efficient caching layer and maintains the required invariants for blocks that get returned to the consensus engine.

The VM uses the block type from [`libevm/core/types`](https://github.com/ava-labs/libevm/tree/master/core/types) and extends it with Avalanche-specific fields (such as `ExtDataHash`, `BlockGasCost`, and `Version`) using libevm's extensibility mechanism (defined in [`customtypes`](customtypes/)), then wraps it with [`wrappedBlock`](wrapped_block.go) to implement the AvalancheGo Block interface. The core package's BlockChain type in [blockchain.go](../../core/blockchain.go) handles the insertion and storage of blocks into the chain.

## Block

The Block type implements the AvalancheGo ChainVM Block interface. The key functions for this interface are `Verify()`, `Accept()`, `Reject()`, and `Status()`.

The Block type (implemented as [`wrappedBlock`](wrapped_block.go)) wraps the block type from [`libevm/core/types`](https://github.com/ava-labs/libevm/tree/master/core/types) and implements these functions to allow the consensus engine to verify blocks as valid, perform consensus, and mark them as accepted or rejected. Blocks contain standard Ethereum transactions as well as atomic transactions (stored in the block's `ExtData` field) that enable cross-chain asset transfers. Blocks may also include optional block extensions for extensible VM functionality. See the documentation in AvalancheGo for the more detailed VM invariants that are maintained here.

## Atomic Transactions

Atomic transactions utilize Shared Memory (documented in the [atomic chains README](https://github.com/ava-labs/avalanchego/blob/master/chains/atomic/README.md)) to send assets to the P-Chain and X-Chain.

Operations on shared memory cannot be reverted, so atomic transactions must separate their verification and processing into two stages: verifying the transaction as valid to be performed within its block and actually performing the operation. For example, once an export transaction is accepted, there is no way for the C-Chain to take that asset back and it can be imported immediately by the recipient chain.

The C-Chain uses the account model for its own state, but atomic transactions must be compatible with the P-Chain and X-Chain, such that C-Chain atomic transactions must transform between the account model and the UTXO model.
