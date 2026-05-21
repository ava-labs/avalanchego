# Snowman VMs

## Recap of Avalanche Subnets

The Avalanche Network is composed of multiple validator sets and blockchains. A validator set defines a group of validators and their specified weights in the consensus process. A subnet is a validator set working together to achieve consensus on a set of blockchains. Every blockchain is validated by one subnet, and one subnet can validate many blockchains.

There is a special subnet inherent to the Avalanche Network called the Primary Network. The Primary Network validates three blockchains that are inherent to the Avalanche Network: the P-Chain, C-Chain, and X-Chain.

For each blockchain, consensus is driven by the consensus engine. For each subnet, the P-Chain, or Platform Chain, defines the validator set and the set of blockchains that are validated by the subnet.

A blockchain consists of two components: a consensus engine and a Virtual Machine (VM). The consensus engine samples validators, handles the responses, and pushes the results of the completed polls into the consensus [code](../snow/consensus/) to decide which containers to Accept/Reject. The VM encodes the application logic for the blockchain. The VM defines the contents of a block, the rules for determining whether a block is valid, the APIs exposed to users, the state transition that occurs if a given block is accepted, and so on.

The consensus engine is general and agnostic to the application semantics of the blockchain. There are two consensus engine implementations in AvalancheGo: Snowman and Avalanche. Snowman provides a consensus engine for linear chains and Avalanche provides a consensus engine for DAGs. These consensus engine implementations can be re-used for multiple different blockchains in the Avalanche ecosystem, and each blockchain actually runs its own independent instance of consensus.

To launch a blockchain on Avalanche, you just need to write a VM that defines your application; the consensus part is handled by the existing consensus engine implementations.

This document will go into the details of implementing a ChainVM to run on the Snowman consensus engine. To implement a VM for snowman, we just need to implement the `ChainVM` interface defined [here.](../snow/engine/snowman/block/vm.go)

VMs are reusable. Arbitrarily many blockchains can run the same VM. Each blockchain has its own state. In this way, a VM is to a blockchain what a class is to an instance of a class in an object-oriented programming language.

## Snowman VM From the Perspective of the Consensus Engine

To the consensus engine, the Snowman VM is a black box that handles all block building, parsing, and storage and provides a simple block interface for the consensus engine to call as it decides blocks.

### Snowman VM Block Handling

The Snowman VM needs to implement the following functions used by the consensus engine during the consensus process.

#### Build Block

Build block allows the VM to propose a new block to be added to consensus.

The VM gets notified of when to build a block after `vm.WaitForEvent()` returns with a `PendingTxs` message.

The major caveat to this is the Snowman VMs are wrapped with [Snowman++](./proposervm/README.md). Snowman++ provides congestion control by using a soft leader, where a leader is designated as the proposer that should create a block at a given time. Snowman++ gracefully falls back to increase the number of validators that are allowed to propose a block to handle the case that the leader does not propose a block in a timely manner.

Since a VM may be ready to build a block before its turn to propose a block according to Snowman++, the proposer VM will buffer PendingTxs messages until the ProposerVM agrees that it is time to build a block as well. This means that the consensus engine is not guaranteed to receive the `PendingTxs` message and call `BuildBlock()` in a timely manner.

When the consensus engine does call `BuildBlock`, the VM should build a block on top of the currently [preferred block](#set-preference). This increases the likelihood that the block will be accepted since if the VM builds on top of a block that is not preferred, then the consensus engine is already leaning towards accepting something else, such that the newly created block will be more likely to get rejected.

#### Parse Block

Parse block provides the consensus engine the ability to parse a byte array into the block interface.

`ParseBlock(bytes []byte)` attempts to parse a byte array into a block, so that it can return the block interface to the consensus engine. ParseBlock can perform syntactic verification to ensure that a block is well formed. For example, if a certain field of a block is invalid such that the block can be immediately determined to not be valid, ParseBlock can immediately return an error so that the consensus engine does not need to do the extra work of verifying it.

It is required for all historical blocks to be parsable.

#### GetBlock

GetBlock fetches blocks that are already known to the VM. If the block has been verified, the VM is required to return that uniquified block when requested until Accept or Reject are called. After a block has been decided, it is no longer necessary to return a uniquified block, and if the block has been rejected the VM is no longer required to store that block or return it at all.

#### Set Preference

The VM implements the function `SetPreference(blkID ids.ID)` to allow the consensus engine to notify the VM which block is currently preferred to be accepted. The VM should use this information to set the head of its blockchain. Most importantly, when the consensus engine calls BuildBlock, the VM should be sure to build on top of the block that is the most recently set preference.

Note: SetPreference will always be called with a block that has no verified children.

### Implementing the Snowman VM Block

From the perspective of the consensus engine, the state of the VM can be defined as a linear chain starting from the
genesis block through to the last accepted block.

Following the last accepted block, the consensus engine may have any number of different blocks that are
processing. The configuration of the processing set can be defined as a tree with the last accepted
block as the root.

In practice, this looks like the following:

```
    G
    |
    .
    .
    .
    |
    L
    |
    A
  /   \
 B     C
```

In this example, G -> ... -> L is the linear chain of blocks that have already been accepted by the consensus
engine.

A, with parent block L, has been issued into consensus and is currently in processing.
Blocks B and C, both with parent block A, have also been issued into consensus and are currently in processing
as well.

We will call this state a possible configuration of the ChainVM from the view of the consensus engine, and
we will try to define clearly the set of possible steps from this configuration to the next, which the ChainVM
must implement correctly.

Given a configuration of the consensus engine, there are three possible actions the consensus engine may take:

1. The consensus engine will attempt to verify a block whose parent is either the last accepted block or is currently in consensus.
2. The consensus engine may change its preference (update the block that it currently prefers to accept).
3. The consensus engine may arrive at a decision and call Accept/Reject on a series of blocks.

If the consensus engine arrives at a decision, then it may have decided one or more blocks and will perform
the following steps:

1. Call Accept on a block
2. Call Reject on all transitive conflicts
3. Repeat steps 1-2 until there are no more blocks to accept

Therefore, if the tree of blocks in consensus (with root L, the last accepted block), looks like the following:

```
    L
  /   \
 A     B
 |    / \
 C   D   G
    / \
   E   F
```

If the consensus engine decides A and C simultaneously, the consensus engine would perform the following operations:

1. Accept(A)
2. Reject(B), Reject(D), Reject(E), Reject(F), and Reject(G)
3. Accept(C)

To see the actual code where Accept/Reject are performed, look [here.](../snow/consensus/snowman/topological.go)

### Block Statuses

A block's status must be one of `Accepted`, `Rejected`, `Processing`.

A block that is `Accepted` or `Rejected` is considered to be `Decided`.

#### Processing Blocks

If a block has status `Processing` we have the byte representation of the block and have parsed it but have not decided it.

Additionally, blocks with status `Processing` may or may not have had `Verify()` called on them. Therefore, a block with status `Processing` hasn't necessarily been added to consensus. That is, the consensus engine may not be trying to decide the block. Before adding a block to consensus, the consensus engine verifies the block to ensure it's valid. The VM defines what constitutes a valid block for that blockchain.

For example, when a node receives a block from the network it will call `ParseBlock(blockBytes)` and return a block to the consensus engine. This block has not yet been fully verified and is not in consensus at this point. When the consensus engine has fetched its full ancestry and is ready to issue it to consensus, it will verify all of the block's ancestors, call `Verify()` on this block as well, and if it returns a nil error, then the block will be added to consensus.

#### Accepted Blocks

After `Accept` has been called on a block, it must report status `Accepted`. Accepted blocks must still be retrievable by VM method `GetBlock`.

#### Rejected Blocks

After `Reject` has been called on a block, it should report its status as rejected. This is not required to be maintained forever. After Reject has been called, it may be convenient to keep it in a caching layer so that its status is easily accessible. For performance reasons, it may be optimal to report the status of a block as rejected until the last accepted block height is >= the rejected block height, since at that point the consensus engine can see its height and immediately see that the block does not need to be processed again.

### Uniquifying Blocks

The consensus engine requires that the VM return a unique reference to blocks when they are in consensus.

To repeat, from the perspective of the VM, a block is in consensus if `Verify()` has been called on it and it returned no error, but the block has not yet been decided. In other words, `Verify()` has successfully returned, but not yet been followed by `Accept()` or `Reject()`.

When a block is processing, the VM needs to ensure that any function on the VM that gets called and returns a block, returns a reference to that same block that the consensus engine has a reference to.

After a block has been decided, the consensus engine will not call `Verify()`, `Accept()`, or `Reject()` on the same block again. Since the block will not go through consensus again, it's safe to return a non-unique block if that block has already been decided.

If a block is processing, but hasn't been verified (i.e. it hasn't been added to consensus) then it is also safe to return a non-unique block.

Once `Verify()` has been called and returns a non-nil error, the VM must subsequently return a reference to the same block.

This means that the VM needs to handle uniquification and leads to very specific requirements for how blocks are cached. This is why the [chain](./components/chain/) package was implemented to create a simple helper that helps a VM implement an efficient caching layer while correctly uniquifying blocks. For an example of how it's used, you can look at the [rpcchainvm](./rpcchainvm/).

## Snowman VM APIs

The VM must also implement `CreateHandlers()` which can return a map of extensions mapped to HTTP handlers that will be added to the node's API server. This allows the VM to expose APIs for querying and interacting with the blockchain implemented by the API.
