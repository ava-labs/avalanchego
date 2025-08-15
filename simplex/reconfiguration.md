# Reconfiguring a Simplex L1

- [1. The Simplex consensus protocol](#1-the-simplex-consensus-protocol)
  - [Challenges of reconfiguring a (Simplex) consensus system](#challenges-of-reconfiguring-a-simplex-consensus-system)
- [2. Epoch management](#2-epoch-management)
  - [ICM epochs vs Simplex epochs](#icm-epochs-vs-simplex-epochs)
  - [Simplex epoch encoding](#simplex-epoch-encoding)
  - [Simplex epoch update mechanism](#simplex-epoch-update-mechanism)
- [3. Consensus protocol metadata management using a Metadata State-Machine (MSM)](#3-consensus-protocol-metadata-management-using-a-metadata-state-machine-msm)
  - [Motivation and introduction](#motivation-and-introduction)
  - [Epoch change using the MSM](#epoch-change-using-the-msm)
- [4. Onboarding new nodes across epochs](#4-onboarding-new-nodes-across-epochs)
  - [Determining how to validate the finalization certificates of an epoch](#determining-how-to-validate-the-finalization-certificates-of-an-epoch)
  - [Replicating a Simplex chain](#replicating-a-simplex-chain)

In this document, we describe the reconfiguration technique of Simplex.
We start by briefly recalling the Simplex consensus protocol, following by describing the challenges of reconfiguring a consensus system,
after which we describe how epochs address these challenges.
Specifically, we distinguish between ICM epochs and Simplex epochs and then explain how Simplex epochs are implemented.
We then introduce the MSM (Metadata State-Machine) which is a consensus agnostic functionality that is used to manage metadata for consensus systems.
Once we have explained how epochs and metadata are maintained, we describe the Simplex reconfiguration mechanism and how it is accomplished by using the MSM.
Lastly, we explain how a new node that onboards the system can validate all blocks in the chain from genesis despite the fact that the validator set changed over time.


## 1. The Simplex consensus protocol

The Simplex consensus protocol is a single leader / block proposer consensus protocol which assumes a semi-synchronous network.
The protocol guarantees safety and liveness as long as less than a third of the nodes are faulty.

The protocol proceeds in rounds, where in each round a block is built by a different node, which is the leader / block proposer of that round.
When receiving a block proposal from the leader of the round, a node broadcasts a vote for the block, and in order to progress to the next round,
a node either collects a quorum of votes for that block (denoted as a notarization of the block), or it collects a quorum of votes that say that this round will be skipped in order to let the next leader propose a block.
Upon collecting a quorum of votes for a block, a node broadcasts a finalization message to let the rest of the nodes know that it has seen a quorum of votes for the block.
A block is considered as finalized (can be delivered to the application) once a quorum of finalization messages is collected for it, which convinces the node that no other node has skipped the round in which the block was proposed.

What differentiates Simplex from other consensus protocols is that it allows two or more nodes to progress to the next round due to different reasons.
In particular, a node may collect a notarization for a block, while another node may collect a quorum of messages that say that the round is skipped.
In such a case, the block that was notarized might be finalized later on, once a descendant block is finalized,
but it might also be that the block will never be finalized, and a different block with the same height but in a different round will be finalized instead.

### Challenges of reconfiguring a (Simplex) consensus system

Reconfiguring a consensus system is all about making sure that all nodes are consistent with how many nodes should vote on each block.
While it may at first seem like a trivial problem, it isn't always so:

- In some protocols, a block might be voted on while the predecessor block is still being voted on.
  If the predecessor block changes the amount of nodes that need to vote on a block, then this fact needs to be taken
  into account when counting the votes of the descendant block. However, it is often that the consensus instance needs to
  interact with its application in order to understand how the voting rules would change as a result of accepting the predecessor block
  which as mentioned, may not be accepted yet at the time of voting on its descendant.
  A technique known in the scientific literature to solve this problem involves "flushing the pipeline" by adding a series of "no-op" blocks
  after each block that reconfigures the consensus protocol. Flushing the pipeline of blocks makes it that the consensus protocol instances
  can delay the reconfiguration until the reconfiguration block has been accepted and processed by the application.
- In Simplex, a block may be notarized but only finalized at a later round once a descendant block is finalized.
  This phenomenon is possible when in a round, a few nodes collect a notarization on the block, but a quorum or more of the nodes vote that no block will be finalized
  in that round, due to network conditions or the leader being faulty. As a result, if the block ends up being finalized it is only once a descendant block is finalized.
  This behavior makes reconfiguration challenging as in a reconfiguration, the protocol needs to change its voting logic for descendant blocks
  based on a predecessor block which may not end up part of the chain.

## 2. Epoch management

### ICM epochs vs Simplex epochs

The epoch management for ICM is defined as follows:
Let $E$ be the current epoch with a start time of $T_{E}$ and end time of $T_{E} + D$.
The first block whose build time is later than $T_{E} + D$ and is denoted B<sup>E</sup><sub>end</sub>, is the last block of epoch $E$.
The next block belongs to epoch $E + 1$ and its P-chain height is defined to be the P-chain height of B<sup>E</sup><sub>end</sub>.
The start time of epoch $E + 1$ is defined to be the build time of B<sup>E</sup><sub>end</sub>.

The purpose of ICM epochs is to aggregate validator changes across all L1s into coarse grained units of reference.

Unlike ICM epochs, A Simplex epoch doesn't change unless the validator set of the L1 changes,
as its role is to convey the validator set that is used to validate the quorum certificate of the block.

Distinguishing themselves from ICM epochs, Simplex epoch numbers aren't successive, but are defined to be the sequence numbers of the blocks that seal them.
More concretely, let $B_k$ be the last block of epoch $e$.
Then, the next epoch is numbered $k$ and not $e+1$.

We denote such a block $B_k$ with sequence number $k$ which is part of epoch $e$ as the $sealing~block$ of epoch $e$.

In order to support ICM epochs, the Simplex protocol facilitates the encoding of both ICM epochs and Simplex epochs.
Whenever a block is built, the block builder encodes the ICM epoch information (`EpochStartTime`, `EpochNumber`, `PChainEpochHeight`) in the same manner as the proposerVM encodes it,
and also encodes the Simplex epoch as follows below:

### Simplex epoch encoding

The Simplex epoch information is encoded in the block as:

```
{
   PChainReferenceHeight uint64
   EpochNumber uint64
   NextPChainReferenceHeight uint64
}
```

- The validator set of the epoch numbered `EpochNumber` is therefore derived from `PChainHeight`.

- The `NextPChainReferenceHeight` is the P-chain height of the next epoch and is only encoded in the last block of epoch `EpochNumber`, otherwise it is set to `0`.

- The genesis block does not contain the Simplex epoch information, and it is implicitly set to be the zero values of the fields above.

### Simplex epoch update mechanism

Simplex updates its epoch information by block proposers building blocks with new updated epoch information, and the epoch information is then updated once the
block is finalized. If a block contains incorrect epoch information, the block will be rejected by the nodes when they verify it.

Whenever a block proposer builds a block $B_{i+1}$ built on top of block $B_i$, it adopts the `PChainReferenceHeight` of block $B_i$, since $B_{i+1}$ is in the same epoch.
It then samples its P-chain height and if it detects that the validator set
is different from the validator set that is derived from the P-chain height of `PChainReferenceHeight`,
it sets `NextPChainReferenceHeight` to the P-chain height it sampled.

When the block $B_{i+1}$ is verified by nodes other than the block proposer, they check whether:

- The `PChainReferenceHeight` hasn't changed from the previous block $B_i$.
- `NextPChainReferenceHeight > PChainReferenceHeight`.
- They have locally the P-chain height corresponding to `NextPChainReferenceHeight`.
- The validator set derived from the P-chain height corresponding to `NextPChainReferenceHeight` is different from the validator set derived by the P-chain height corresponding to `PChainReferenceHeight`.

If all above conditions hold, then the epoch information of block $B_{i+1}$ is considered correct, and block $B_{i+2}$ then belongs to epoch `NextPChainReferenceHeight`.
When block $B_{i+2}$ is built, it will have its `EpochNumber` set to $i+2$ and its `PChainReferenceHeight` set to the `NextPChainReferenceHeight` of $B_{i+1}$ and
its `NextPChainReferenceHeight` set to either `0` or a new P-chain height, depending on the sampling of the P-chain height done by its block proposer.

## 3. Consensus protocol metadata management using a Metadata State-Machine (MSM)

### Motivation and introduction

Consensus systems may occasionally need to change the state of the consensus protocol uniformly and have the change appear as an atomic change.
The simplest way of achieving a uniform and atomic state change is to encode the state change in a block and then have the block be finalized.
Some consensus protocols (e.g. non-pipelined variants of PBFT) may support such a manner of state change, but others require a more sophisticated approach.

Consider for example, the Simplex consensus protocol, where a block may be notarized in some round but not finalized in that round,
but only in later rounds.
In such a case, not enough nodes will finalize the block in that round, and in order for that block to be finalized, additional blocks need to be built on top of it,
and finalized. It is only when the additional blocks are finalized, that the aforementioned block can be considered as finalized.
Clearly, a state change that takes places after a block is finalized cannot be made atomic, and therefore a different approach is needed.

An example of a state change of the consensus protocol that cannot be made atomic is one that requires nodes to contribute self generated data (e.g. randomness) via proposing blocks.
If we assume that nodes cannot send transactions to a mem-pool like clients do, then the only way of nodes contributing self generated data is by having each node propose a block in its turn,
and then accumulating the data generated by each node's block.

To that end, we introduce the Metadata State-Machine (MSM) which lies between the consensus instance and the VM.
The role of the MSM is to mask away the complexity of such state changes from the consensus protocol, let the consensus protocol
be able to keep the existing application abstraction, and to prevent it from deviating from its usual standard operation.
More specifically, the MSM detects when the state of the consensus protocol needs to change, and then it hijacks the block building and verification mechanisms of the VM,
until the state change can be atomically applied to the consensus protocol instance.

Similarly to the proposerVM, the MSM wraps the inner VM block with its own block wrapper and encodes the state transition data in the wrapped block.

However, it is important to distinguish between the MSM and a VM. The MSM is a very simple construction and in most cases it is simple enough
to be implemented as a state transition function that receives as input a block, and outputs a new block.
Unlike the VM, it has no transaction memory-pool, and it cannot perform any DB lookup, nor it can do any disk or network I/O.

When building a block $B_{i+1}$, the MSM has access to $B_i$, and to the finalization certificate of the sealing block (if applicable).
The MSM intercepts all interaction between the Simplex consensus instance and the VM, namely the `BlockBuilder`, `Storage`, as well as the verification functions of blocks.
Access to the finalization certificate of the sealing block is needed in order to know when the epoch has been sealed in order to transition
to the next epoch.


### Epoch change using the MSM

When the MSM builds a block, it computes the Simplex epoch information and encodes it in the wrapped block.
It then proceeds to ask the VM to build the block, and then it wraps the block built by the VM with its own block wrapper.

Aside from the Simplex epoch information, the MSM encodes additional information in the wrapped block, which assists it in
determining when the current epoch can be terminated and a new epoch can be started:

```
{
   SealingBlockSeq uint64
}
```

Therefore, together with the Simplex epoch information, we get the following block structure:

```
{
   PChainReferenceHeight uint64
   EpochNumber uint64
   NextPChainReferenceHeight uint64
   SealingBlockSeq uint64
}
```

We next describe how the MSM uses the above metadata to manage the Simplex epoch change.

When the VM builds a block and the MSM determines that it is the sealing block of the current epoch, in order to progress to the next epoch,
the MSM needs to make sure that the sealing block is finalized, otherwise the block might not end up being part of the blockchain but the node would
regardless transition to the next epoch.
To that end, the MSM builds artificial blocks that do not contain transactions, on top of the sealing block until it is finalized.

Such artificial blocks are termed "Telocks" and they are built by the MSM without any interaction with the VM.
The name "Telock" is a portmanteau of "Telos" which is "end" in Greek, and "Block".

Hereafter, we will refer to an MSM block as a block made by the MSM and that wraps either a VM block or a Telock.

The role of telocks is to collect and aggregate information about the state of the consensus protocol in a consensus agnostic manner.
Telocks are treated by the Simplex consensus protocol as regular blocks, yet they only get stored in the WAL and are purged when the first block of the next epoch is accepted.
Telocks (as their name implies) can only exist at the end of an epoch, and never at the beginning or in the middle of an epoch.

Since Telocks are purged once an epoch changes, their block sequence numbers are also available for reuse in the next epoch.
For example, if in epoch `e` the sealing block is $B_k$, there can be several telocks $B_{k+1}$, $B_{k+2}, ..., B_{k+l}$ belonging to epoch $e$.
In the successive epoch $k$, the first block will have a sequence of $B_{k+1}$, the second block will have a sequence of $B_{k+2}$ and so on and so forth.

As mentioned before, the MSM advances its state by building blocks and looking at the previous block and the sealing block's finalization certificate if such exists.
The MSM uses the following rules to determine how to update the metadata:

1. If the current block (the block being built) is the sealing block of the current epoch, `SealingBlockSeq` is set to the current block sequence
   and `NextPChainReferenceHeight` is set to be the P-chain height sampled.
2. If the current block isn't the last block of the current epoch, it is either in the epoch of the sealing block or in the successive epoch:
- 2.1 If `SealingBlockSeq` of the previous block and is smaller than the current block sequence but is greater than `0`, then:
  - 2.1.1 If the finalization certificate of the sealing block is available, then the MSM sets `SealingBlockSeq` to be 0,
    sets `PChainReferenceHeight` to the `NextPChainReferenceHeight` of the sealing block, `NextPChainReferenceHeight` to 0, and asks the VM to build a new block.
    The block that will be built, will be part of the next epoch (`SealingBlockSeq`).
  - 2.1.2 Else, if the finalization certificate of the sealing block is not available, then the MSM creates a telock with the same metadata information as the previous block.
- 2.2 If the `SealingBlockSeq` of the previous block is `0`, then the current block also has `SealingBlockSeq` set to `0`.
  This is because the previous block is neither a sealing block nor a telock.


Let's observe an example of a series of epoch information transitions made by the MSM:

Suppose the current epoch information is as follows:
```
{
   PChainReferenceHeight: 100
   EpochNumber: 20
   NextPChainReferenceHeight: 0
   SealingBlockSeq: 0
}
```

Then, a block with sequence of 30 is built and a higher P-chain height (151) at which the validator set is different from the epoch's validator set is sampled.
The block builder then applies rule (1) and encodes the following epoch information in the block:
```
{
   PChainReferenceHeight: 100
   EpochNumber: 20
   NextPChainReferenceHeight: 151
   SealingBlockSeq: 30
}
```

Further suppose that block 30 is notarized but the block builder of block 31 did not finalize it at the time of building block 31.
In such a case, following rule (2.1.2) the MSM will make the Simplex protocol instance build a telock and not ask the VM to build an inner block.
The epoch information of the telock with sequence 31 will be the same as the epoch information of block 30, and it will look like this:
```
{
   PChainReferenceHeight: 100
   EpochNumber: 20
   NextPChainReferenceHeight: 151
   SealingBlockSeq: 30
}
```

Even if the rest of the nodes finalized block 30, they will still agree to accept block 31 with metadata as above,
as it is always possible that the block builder of block 31 did not have the finalization certificate of the sealing block.

Finally, if the block builder of block 32 finalized the sealing block, then its MSM will ask the VM to build a block, and will encode the following epoch information:

```
{
   PChainReferenceHeight: 151
   EpochNumber: 31
   NextPChainReferenceHeight: 0
   SealingBlockSeq: 0
}
```

## 4. Onboarding new nodes across epochs

When onboarding a new node to the system, the node needs to replicate all blocks, and verify their finalization certificates before processing them.
If the VM supports state sync, then a prefix of the blocks might be skipped if the node isn't a full node.

Nevertheless, a robust and complete technique that can verify finalization certificates across epochs is needed.

The challenge in verifying finalization certificates across epochs when onboarding a new node is that if the node is replicating the blocks
of the P-chain in parallel, the process is asynchronous to the replication of the Simplex driven chain.
When replicating a block belonging to the Simplex driven chain, the P-chain height of its epoch may still be replicating,
or it might have already been replicated but cannot be easily reconstructed because there is no index between P-chain height to the validator set.
Therefore, even if the P-chain is fully replicated prior to the blocks of the Simplex driven VM, it is impossible to verify the finalization certificate
of previous epochs.

### Determining how to validate the finalization certificates of an epoch

We therefore add auxiliary information to the epoch information, the block validation descriptor:

```
{
   PChainReferenceHeight uint64
   EpochNumber uint64
   NextPChainReferenceHeight uint64
   SealingBlockSeq uint64
   BlockValidationDescriptor {
      Type: uint8
      Val: []byte
   }
}
```

The block validation descriptor, describes how to validate the blocks of the next epoch.
When Simplex uses a threshold public key, it contains the threshold public key of the next epoch.
Otherwise, it contains the public keys of the validator set of the next epoch.
For storage and bandwidth efficiency reasons, the block validation descriptor is only encoded in epoch sealing blocks, and in the rest of the blocks it is empty.

### Replicating a Simplex chain

When replicating a chain driven by Simplex, the onboarding node first replicates the P-chain and acquires the latest validator set of the chain.
It then proceeds to replicate only the sealing blocks of all epochs by querying a majority of nodes for the sealing blocks, and dropping candidate responses
that aren't found in the responses of enough nodes.

Once all sealing blocks are replicated and their content (but not finalization certificate) is verified by querying multiple nodes,
it is possible for the onboarding node to discern whether it has missed replicating some sealing blocks among the sealing blocks it has replicated,
thanks to the fact that a sealing block $B_l$ will have the epoch number $k$ of its previous sealing block $B_k$.

Therefore, it is only required to query for the latest block in order to be able to recursively find out the block sequences of all previous sealing blocks.

After replicating the sealing blocks, the onboarding node has the public key(s) of the validator set for each epoch.
It can thereafter parallelize the replication and finalization certificate verification of all blocks in the chain that reside in between the sealing blocks.
