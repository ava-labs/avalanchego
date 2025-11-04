# Core Package

The core package maintains the backend for the blockchain, transaction pool, and maintains the required indexes for blocks, transactions, logs, and transaction receipts.

## Blockchain

The [BlockChain](./blockchain.go) struct handles the insertion of blocks into the maintained chain. It maintains a "canonical chain", which is essentially the preferred chain (the chain that ends with the block preferred by the AvalancheGo consensus engine).

When the consensus engine verifies blocks as they are ready to be issued into consensus, it calls `Verify()` on the ChainVM Block interface implemented [here](../plugin/evm/block.go). This calls `InsertBlockManual` on the BlockChain struct implemented in this package, which is the first entrypoint of a block into the blockchain.

InsertBlockManual verifies the block, inserts it into the state manager to track the merkle trie for the block, and adds it to the canonical chain if it extends the currently preferred chain.

Coreth adds functions for Accept and Reject, which take care of marking a block as finalized and performing garbage collection where possible.

The consensus engine can also call `SetPreference` on a VM to tell the VM that a specific block is preferred by the consensus engine to be accepted. This triggers a call to `reorg` the blockchain and set the newly preferred block as the preferred chain.

## Transaction Pool

The transaction pool maintains the set of transactions that need to be issued into a new block. The VM exposes APIs that allow clients to issue transactions into the transaction pool and also performs gossip across the network in order to send and receive pending transactions that need to be issued into a new block. The transaction pool asynchronously follows the preferred block of the `BlockChain` struct by subscribing to new head events and updating its state accordingly. When the transaction pool updates, it ensures that any transactions it contains are still valid to be issued on top of the new preferred block.

## State Manager

The State Manager manages the [TrieDB](../trie/database.go). The TrieDB tracks a merkle forest of all of the merkle tries for the last accepted block and processing blocks. When a block is processed, the state transition results in a new merkle trie added to the merkle forest. The State Manager can operate in either archival or pruning mode.

### Archival Mode

In archival mode, every merkle trie is written to disk so that the node maintains a complete history of all the blocks that it has processed.

### Pruning Mode

In pruning mode, the State Manager keeps a reference to merkle tries of processing blocks. When a block gets accepted, it stays in memory. When a block gets rejected, the state manager can dereference and clean up the no longer needed merkle trie. The State Manager does not immediately write the merkle trie to disk of a block when it gets accepted. Instead, at a regular interval (~4096 blocks) it writes the merkle trie to disk, so that it does not add the overhead of storing every accepted block's merkle trie to disk.
