# Consensus

Disclaimer: the consensus package in Subnet-EVM is a complete misnomer.

The consensus package in go-ethereum handles block validation and specifically handles validating the PoW portion of consensus - thus the name.

Since AvalancheGo handles consensus for Subnet-EVM, Subnet-EVM is just the VM, but we keep the consensus package in place to handle part of the block verification process.

## Block Verification

The dummy consensus engine is responsible for performing verification on the header of a block. The engine verifies that all of the fields of the header are correct.

## Dynamic Fees

Subnet-EVM includes a dynamic fee algorithm based off of [EIP-1559](https://eips.ethereum.org/EIPS/eip-1559). This introduces a field to the block type called `BaseFee`. The Base Fee sets a minimum gas price for any transaction to be included in the block. For example, a transaction with a gas price of 49 gwei, will be invalid to include in a block with a base fee of 50 gwei.

The dynamic fee algorithm aims to adjust the base fee to handle network congestion. Subnet-EVM sets a target utilization on the network, and the dynamic fee algorithm adjusts the base fee accordingly. If the network operates above the target utilization, the dynamic fee algorithm will increase the base fee to make utilizing the network more expensive and bring overall utilization down. If the network operates below the target utilization, the dynamic fee algorithm will decrease the base fee to make it cheaper to use the network.

- EIP-1559 is intended for Ethereum where a block is produced roughly every 10s
- The dynamic fee algorithm needs to handle the case that the network quiesces and there are no blocks for a long period of time
- Since Subnet-EVM produces blocks at a different cadence, it adapts EIP-1559 to sum the amount of gas consumed within a 10-second interval instead of using only the amount of gas consumed in the parent block

## Consensus Engine Callbacks

The consensus engine is called while blocks are being both built and processed and Subnet-EVM adds callback functions into the dummy consensus engine to insert its own logic into these stages.

### FinalizeAndAssemble

The FinalizeAndAssemble callback is used as the final step in building a block within the miner package.

### Finalize

Finalize is called as the final step in processing a block in [state_processor.go](../../core/state_processor.go). Since either Finalize or FinalizeAndAssemble are called, but not both, when building or verifying/processing a block they need to perform the exact same processing/verification step to ensure that a block produced by the miner where FinalizeAndAssemble is called will be processed and verified in the same way when Finalize gets called.
