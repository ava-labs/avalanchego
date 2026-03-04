# Miner

The miner is a package inherited from go-ethereum with a large amount of functionality stripped out since it is not needed in coreth.

In go-ethereum, the miner needs to perform PoW in order to try and produce the next block. Since Avalanche does not rely on PoW in any way, the miner within Coreth is only used to produce blocks on demand.

All of the async functionality has been stripped out in favor of a much lighter weight miner implementation which takes a backend that supplies the blockchain and transaction pool and exposes the functionality to produce a new block with the contents of the transaction pool.

## FinalizeAndAssemble

One nuance of the miner, is that it makes use of the call `FinalizeAndAssemble` from the coreth [consensus engine](../consensus/dummy/README.md). This callback, as hinted at in the name, performs the same work as `Finalize` in addition to assembling the block.

This means that whenever a verification or processing operation is added in `Finalize` it must be added in `FinalizeAndAssemble` as well to ensure that a block produced by the `miner` is processed in the same way by a node receiving that block, which did not produce it.

To illustrate, if nodeA produces a block and sends it to the network. When nodeB receives that block and processes it, it needs to process it and see the exact same result as nodeA. Otherwise, there could be a situation where two nodes either disagree on the validity of a block or process it differently and perform a different state transition as a result.
