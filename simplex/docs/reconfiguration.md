# Reconfiguring a Simplex L1

In this document, we describe the reconfiguration technique of Simplex.
We start by briefly recalling the Simplex consensus protocol, following by describing the challenges of reconfiguring a consensus system,
after which we describe how epochs address these challenges.
Specifically, we distinguish between [ICM epochs](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/181-p-chain-epoched-views/README.md) and Simplex epochs and then explain how Simplex epochs are implemented.
We then introduce the MSM (Metadata State-Machine) which is a consensus agnostic functionality that is used to manage metadata for consensus systems.
Once we have explained how epochs and metadata are maintained, we describe the Simplex reconfiguration mechanism and how it is accomplished by using the MSM.
We then explain how a new node that onboards the system can validate all blocks in the chain from genesis despite the fact that the validator set changed over time.
Lastly, we outline how Simplex blocks are encoded.

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
  This behavior makes reconfiguration challenging, since in a reconfiguration, the protocol needs to change its voting logic for descendant blocks
  based on a predecessor block which may not end up part of the chain.

## 2. Epoch management

### ICM epochs vs Simplex epochs

The epoch management for ICM is defined as follows:
Let $E$ be the current epoch with a start time of $T_{E}$ and end time of $T_{E} + D$.
The first block whose build time is at least $T_{E} + D$ and is denoted B<sup>E</sup><sub>end</sub>, is the last block of epoch $E$.
The next block belongs to epoch $E + 1$ and its P-chain height is defined to be the P-chain height of B<sup>E</sup><sub>end</sub>.
The start time of epoch $E + 1$ is defined to be the build time of B<sup>E</sup><sub>end</sub>.

The purpose of ICM epochs is to aggregate validator changes across all L1s into coarse grained units of reference.

Unlike ICM epochs, A Simplex epoch doesn't change unless the validator set of the L1 changes,
as its role is to convey the validator set that is used to validate the quorum certificate of the block.

Further distinguishing themselves from ICM epochs, Simplex epoch numbers aren't successive, but are defined to be the sequence numbers of the blocks that seal them.
More concretely, let $B_k$ be the last block of epoch $e$.
Then, the next epoch is numbered $k$ and not $e+1$.

We denote such a block $B_k$ with sequence number $k$ which is part of epoch $e$ as the $sealing~block$ of epoch $e$.
The validator set for epoch $k$ is derived from the P-chain height of the sealing block $B_k$.

In order to support ICM epochs, the Simplex protocol facilitates the encoding of both ICM epochs and Simplex epochs.
Whenever a block is built, the block builder encodes the ICM epoch information (`EpochStartTime`, `epoch_number`, `PChainEpochHeight`) in the same manner as the proposerVM encodes it,
and also encodes the Simplex epoch information, as well as some additional auxiliary information that may be used by other protocols, as explained below:

```proto
message SimplexEpochInfo {
   uint64 p_chain_reference_height = 1;
   uint64 epoch_number = 2;
   uint64 next_p_chain_reference_height = 3;
   uint64 prev_vm_block_seq = 4; // The sequence of the previous VM block
   uint64 sealing_block_seq = 5; // The sequence number of the sealing block
   BlockValidationDescriptor block_validation_descriptor = 6; // Describes how to validate the blocks of the next epoch
   NextEpochApprovals next_epoch_approvals = 7; // The epoch change approvals of the next epoch by at least n-f nodes.
}
```

- The validator set of the epoch numbered `epoch_number` is derived from `p_chain_reference_height`.

- The `next_p_chain_reference_height` is the P-chain height of the next epoch, otherwise it is set to `0`.

- The `prev_vm_block_seq` is the sequence number of the previous VM block, and it is used to efficiently find the last VM block upon startup.

- The `sealing_block_seq` is the sequence number of the sealing block of the epoch, and it is set to `0` before the sealing block is created.
  It is used to determine when the Simplex instance can transition to the next epoch. It is safe to transition to the next epoch once the sealing block is finalized.

- The `block_validation_descriptor` describes how to validate the blocks of the next epoch.
  It is encoded in blocks starting from the block that has `next_p_chain_reference_height > 0` until and including the sealing block.
  It is used to verify the quorum certificates of blocks that are built in the next epoch.

- The `next_epoch_approvals` is a canoto message that contains the approvals of the next epoch by at least `n-f` nodes.
  It is required in order to create the sealing block of the epoch and indicates that the nodes comprising the next epoch's validator set
  are ready to start the next epoch. It prevents cases where nodes in the current epoch creating a sealing block and moving to the next epoch
  while the nodes of the next epoch still replicating blocks and cannot yet participate in the consensus protocol.

- The genesis block does not contain the Simplex epoch information, and it is implicitly set to be the zero values of the fields above.


### Auxiliary information encoding

Apart from the ICM epoch information and the Simplex epoch information, the block also contains auxiliary information
that may be used by other protocols that require their information to be collected via the process of block building by the protocol participants.
A common way for such information collection is to have each participant supply its input as part of the auxiliary information in the block, when it is its turn to build a block.

The auxiliary information is encoded as a canoto message with the following schema:

```proto
message AuxiliaryInfo {
   bytes info = 1; // The auxiliary information. Can be empty if no auxiliary information is provided.
   uint64 prev_aux_info_seq = 2; // The sequence number of the previous block containing auxiliary information.
   uint32 application_id = 3; // The application ID of the auxiliary information (if applicable).
}
```

The `prev_aux_info_seq` field points to the previous block that contained auxiliary information, or `0` if the chain has no such block.
As most blocks are not expected to contain auxiliary information, this provides a way for the application that uses the auxiliary information to
efficiently find all blocks that contain auxiliary information without having to iterate over all blocks in the chain.

### Simplex epoch update mechanism

Simplex updates its epoch information by block proposers building blocks with new updated epoch information, and the epoch information is then updated once the
block is finalized. If a block contains incorrect epoch information, the block will be rejected by the nodes when they verify it.

Whenever the `i`'th node builds a block $B_{k+1}$ built on top of block $B_k$, it adopts the `p_chain_reference_height` of block $B_k$, since $B_{k+1}$ is in the same epoch.
It then samples its P-chain height and if it detects that the validator set
is different from the validator set that is derived from the P-chain height of `p_chain_reference_height`,
it sets `next_p_chain_reference_height` to the sampled P-chain height.

When the block $B_{k+1}$ built by the `i`'th node is verified by nodes other than the block proposer, they ensure that:

- The `p_chain_reference_height` hasn't changed from the previous block $B_k$.
- `next_p_chain_reference_height > p_chain_reference_height` or `next_p_chain_reference_height == 0`.
- If `next_p_chain_reference_height > 0`, the node has observed the P-chain height exists in the P-chain.
- If `next_p_chain_reference_height > 0`, the validator set derived from the P-chain height corresponding to is different from the validator set derived by the P-chain height corresponding to `p_chain_reference_height`.

If block $B_{k+1}$ is considered to be the sealing block of its epoch, then the next block - block $B_{k+2}$ belongs to epoch `k+1`.
When block $B_{k+2}$ is built, it will have its `epoch_number` set to $k+1$ and its `p_chain_reference_height` set to the `next_p_chain_reference_height` of $B_{k+1}$ and
its `next_p_chain_reference_height` set to `0`.

## 3. Consensus protocol metadata management using a Metadata State-Machine (MSM)

### Motivation and introduction


Consensus systems may occasionally need to change the configuration or state of the consensus protocol uniformly and have the change appear as an atomic change.
The simplest way of achieving a uniform and atomic state change is to encode the configuration or state change in a block and then have the block be finalized,
and apply the change to the consensus protocol immediately after the block is finalized.
Some consensus protocols (e.g. non-pipelined variants of PBFT) may facilitate such mechanisms, but others require a more sophisticated approach.

Consider for example, the Simplex consensus protocol, where a block `B` may be notarized in some round but not finalized in that round,
but only in later rounds.
In such a case, in order for that block to be finalized, an additional and unknown number of blocks need to be built on top of it,
and one of them needs to be finalized. The number of blocks that need to be built on top of `B` is unknown because it is only known in retrospect, if enough nodes have sent finalization votes for `B`.

It is impossible to apply a configuration or state change to a consensus protocol if it is only known in retrospect when it should be applied.

To that end, we introduce the Metadata State-Machine (MSM) which lies between the consensus instance and the VM.
The role of the MSM is to mask away the complexity of such state changes from the consensus protocol, let the consensus protocol
be able to keep the existing application abstraction, and to prevent it from deviating from its usual standard operation.
More specifically, the MSM detects when the state of the consensus protocol needs to change, and then it hijacks the block building and verification mechanisms of the VM,
until the state change can be safely applied to the consensus protocol instance.

Similarly to the proposerVM, the MSM wraps the inner VM block with its own block wrapper and encodes the state transition data in the wrapped block.

However, it is important to distinguish between the MSM and a VM. The MSM is a very simple construction and in most cases it is simple enough
to be implemented as a state transition function that receives as input the previous state along with auxiliary information, and outputs the new state.
Unlike the VM, it has no transaction memory-pool, and it cannot perform any DB lookup, nor can it do any disk or network I/O.

When building a block $B_{i+1}$, the MSM has access to {$B_i$, $B_{i-1}$, ...} and to the corresponding finalization certificates, if applicable.
The MSM intercepts all interaction between the Simplex consensus instance and the VM, namely the `BlockBuilder`, `Storage`, as well as the verification functions of blocks.
Access to the finalization certificate of previous blocks is needed in order to know when the epoch has been sealed in order to transition
to the next epoch. This is done by fetching for the finalization certificate of the sealing block of the current epoch.

#### Forcing chain progress

In some cases, there might not be any user activity, so blocks would not be naturally built by the underlying VM.
However, the protocol may still need to extend the chain due to external reasons such as a change in the validator set,
or auxiliary information that needs to be collected by a protocol independent to Simplex.

To that end, the MSM will build blocks that do not contain any transactions. Such blocks are called $Metablocks$, and they are built by the MSM without any interaction with the VM.
A Metablock can carry auxiliary information that is needed by the Simplex consensus protocol, and can be a sealing block of an epoch.
An epoch is not expected to contain a lot of metablocks, as the MSM will always prefer building a VM block over a metablock.
The only way for a metablock to be built is for the MSM to determine that the current epoch should be sealed or the MSM has been
given auxiliary information as input to be included in the next block, but there isn't any user activity that would trigger the VM to build a block.


### Criteria for creation of the sealing block of an epoch

In order to create the sealing block, the following criteria need to be met:

1. There is agreement upon which P-chain height will be used to select the validator set of the next epoch.

2. At least `n-f` nodes belonging to the next epoch have approved the epoch change.


The first criterion is satisfied by the `next_p_chain_reference_height` being greater than `0`.
Since nodes check their P-chain possess the P-chain height corresponding to `next_p_chain_reference_height`,
a block containing a specific `next_p_chain_reference_height` indicates that at least `f+1` correct nodes have observed that P-chain height in the P-chain.

The second criterion is satisfied by the `next_epoch_approvals` field containing an aggregated BLS signature from at least `n-f` nodes
belonging to the next epoch. Unlike the previous field, this field also needs to be updated by nodes in the next epoch that might not be in the current epoch.
To that end, nodes in the next epoch create a `NextEpochApprovals` message with `node_ids` belonging only to themselves, and broadcast it to the nodes in the current epoch.
Then, when a node in the current epoch builds a block, it sets the bits in the `node_ids` field corresponding to the union of the existing and new approving nodes, and aggregates the signatures of the nodes.
The signature is over the `aux_info_digest` and the `next_p_chain_reference_height` fields, to ensure that the nodes in the next epoch agree not only on the validator set,
but also on any kind of application specific auxiliary information that might be needed by the next epoch. One example for such auxiliary information
can be messages in a cryptographic protocol that are to be used in the next epoch.

The `NextEpochApprovals` may only change from block to block as long as the following rules are followed:

- Additional nodes may be added to the `node_ids` field, but a node may only be removed as long as the total number of node ids doesn't decrease.

- The `aux_info_digest` and the `next_p_chain_reference_height` fields may only change if the number of `node_ids` increases.

- The `signature` field should always match the aggregated signature of the nodes corresponding to the `node_ids` field over the `aux_info_digest` and the `next_p_chain_reference_height` fields.

The aforementioned rules imply that in order to ensure liveness, the MSM needs to keep in its memory various combinations of different `aux_info_digest` and `next_p_chain_reference_height` values,
and always try to propose the one that has the most signatures supporting it. A node may sign multiple different combinations of `aux_info_digest` and `next_p_chain_reference_height` values.
Therefore, if an application that relies on the auxiliary information is susceptible to malicious nodes picking the combinations,
it needs to have measures put in place.


### Epoch change using the MSM

When the MSM builds a block, it computes the Simplex epoch information and encodes it in the wrapped block.
It then proceeds to ask the VM to build the block, and then it wraps the block built by the VM with its own simplex block.

We next describe how the MSM uses the simplex epoch information to perform the Simplex epoch change transition.

When the VM builds a block and the MSM determines that it is the sealing block of the current epoch, in order to progress to the next epoch,
the MSM needs to make sure that the sealing block is finalized, otherwise the block might not end up being part of the blockchain but the node would
regardless transition to the next epoch.
To that end, the MSM builds artificial blocks that do not contain transactions, on top of the sealing block until it is finalized.

Such artificial blocks are termed "Telocks" and they are built by the MSM without any interaction with the VM.
The name "Telock" is a portmanteau of "Telos" which is "end" in Greek, and "Block".

Hereafter, we will refer to an MSM block as a block made by the MSM and that wraps either a VM block or a Telock.

Telocks are treated by the Simplex consensus protocol as regular blocks, yet they only get stored in the WAL and are purged when the first block of the next epoch is accepted.

Telocks (as their name implies) can only exist at the end of an epoch, and never at the beginning or in the middle of an epoch.

Since Telocks are purged once an epoch changes, their block sequence numbers are also available for reuse in the next epoch.
For example, if in epoch `e` the sealing block is $B_k$, there can be several telocks $B_{k+1}$, $B_{k+2}, ..., B_{k+l}$ belonging to epoch $e$.
In the successive epoch $k$, the first block will have a sequence of $B_{k+1}$, the second block will have a sequence of $B_{k+2}$ and so on and so forth.

In order to know when the current epoch can be terminated and the next epoch can be instantiated, the MSM uses the `sealing_block_seq` field of the Simplex epoch information.
The `sealing_block_seq` is set to `0` in all blocks that are built except from when the block builder detects that the criteria for the creation of the sealing block of the current epoch have been met.
A telock with a `sealing_block_seq > 0` that is finalized, indicates that at least `f+1` correct nodes have finalized the sealing block of the current epoch.
This means that under the assumption of up to `f` failures, the sealing block and its corresponding finalization certificate can always be replicated by nodes that haven't finalized it
and by doing so, move to the next epoch.


#### Example of epoch change

Let's observe an example of a series of epoch information transitions made by the MSM.

Suppose the current epoch information is as follows (fields with zero values are omitted for succinctness):

```
{
  p_chain_reference_height: 100
  epoch_number: 20
}
```

Then, a block with sequence of 30 is built and a higher P-chain height (151) at which the validator set is different from the epoch's validator set is sampled.
Therefore, the `next_p_chain_reference_height` is set to `151` and accordingly the `block_validation_descriptor`
is set to the public keys of the validator set derived from the P-chain height of `151`:
```
{
    p_chain_reference_height: 100
    epoch_number: 20
    next_p_chain_reference_height: 151
    block_validation_descriptor: [TGVhcm4gdG8g==, dmFsdWUgeW91cnNlbGY==, ... , XBwaW5lc3MuCg==]
}
```

Later some other nodes build blocks and sample higher P-chain heights, however the `next_p_chain_reference_height` remains `151` since it is the first
sampled P-chain height that indicates a different validator set. The same applies for the `block_validation_descriptor`.

At this point, the block builders start collecting approvals from the nodes belonging to the next epoch:

```
{
    p_chain_reference_height: 100
    epoch_number: 20
    next_p_chain_reference_height: 151
    block_validation_descriptor: [TGVhcm4gdG8g==, dmFsdWUgeW91cnNlbGY==, ... , XBwaW5lc3MuCg==]
    next_epoch_approvals: {
        node_ids: 00000101
        aux_info_digest: "VGhlIGhhcmRlc3Q=="
        signature: "iBpcyB0aGUgZ2x=="
    }
}
```

Once enough approvals are gathered, the sealing block can be built:

```
{
    p_chain_reference_height: 100
    epoch_number: 20
    next_p_chain_reference_height: 151
    sealing_block_seq: 40
    block_validation_descriptor: [TGVhcm4gdG8g==, dmFsdWUgeW91cnNlbGY==, ... , XBwaW5lc3MuCg==]
    next_epoch_approvals: {
        node_ids: 00110101
        aux_info_digest: "VGhlIGhhcmRlc3Q=="
        signature: "erpcyBqaGUg05z=="
    }
}
```

All blocks in the next epoch, epoch `40` will have the following epoch information:

```
{
    p_chain_reference_height: 151
    epoch_number: 40
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

The block validation descriptor, describes how to validate the blocks of the next epoch.
It contains the public keys of the validator set of the next epoch.
For storage and bandwidth efficiency reasons, the block validation descriptor is only encoded in blocks that have the `next_p_chain_reference_height`
defined, as a preparation for the creation of the epoch sealing block, and in the rest of the blocks it is empty.

### Replicating a Simplex chain

When replicating a chain driven by Simplex, the onboarding node first replicates the P-chain and acquires the latest validator set of the chain.
It then proceeds to replicate only the latest sealing blocks by querying a majority of nodes for the sealing blocks, and dropping candidate responses
that aren't found in the responses of enough nodes.

Thanks to the fact that a sealing block $B_l$ will have the epoch number $k$ of its previous sealing block $B_k$, it is possible to recursively find out the sealing blocks
of all previous epochs once the latest sealing block has been verified by a majority poll.

After replicating the sealing blocks, the onboarding node has the public key(s) of the validator set for each epoch.
It can thereafter parallelize the replication and finalization certificate verification of all blocks in the chain that reside in between the sealing blocks.

After replicating the blocks for the L1, the node can then proceed to process the blocks in-order using the VM.


## 4. Simplex block encoding

The blocks created by Simplex therefore have the following structure:


```
_________________________
____________________    |
|                  |  O |
|      Inner       |  u |
|      Block       |  t |
|                  |  e |
|__________________|  r |
|                       |
|                     B |
|                     l |
|  ICM epoch info     o |
|                     c |
| Simplex Epoch info  k |
|                       |
|   Auxiliary info      |
|_______________________|
```


```proto
message SimplexBlock {
   bytes inner_block = 1; // The inner block built by the VM, opaque to Simplex.
   OuterBlock outer_block = 2; // The outer block that wraps the inner block without the inner block.
   bytes protocol_metadata = 3 // The Simplex protocol metadata, set by the Simplex consensus protocol.
}
```

where `OuterBlock` is a protobuf message that contains the ICM epoch information, the Simplex epoch information, and auxiliary information:

```proto
message OuterBlock {
  ICMEpochInfo icm_epoch_info = 1; // The ICM epoch information.
  SimplexEpochInfo simplex_epoch_info = 2; // The Simplex epoch information.
  AuxiliaryInfo auxiliary_info = 3; // The auxiliary information.
}
```

The digest of the simplex block is computed as follows:

Let $h_i$ be the hash of the inner block.
The digest of the Simplex block is the hash of the following encoding:

```proto
message HashPreImage {
   bytes h_i = 1; // The inner block hash
   OuterBlock outer_block = 2;
   bytes protocol_metadata = 3;
}
```


This way of hashing the block allows any holder of a finalization certificate for the block to authenticate the block while hiding the content of the inner block.

The `ICM epoch info` is encoded as a canoto message with the following schema:

```proto
message ICMEpochInfo {
   uint64 epoch_start_time = 1; // The start time
   uint64 epoch_number = 2; // The epoch number
   uint64 p_chain_epoch_height = 3; // The P-chain height of the epoch
```

The Simplex epoch information is a canoto encoded message with the following schema:

```proto
message NodeBLSMapping {
    bytes node_id = 1; // The nodeID
    bytes bls_key = 2; // The BLS key of the node
  }

message BlockValidationDescriptor {
  oneof BlockValidationType {
      AggregatedMembership aggregated_membership = 1;
      // Future types can be added here
  }
}

type AggregatedMembership {
  repeated NodeBLSMapping members = 1; // The BLS keys of the nodes in the next epoch
}

message NextEpochApprovals {
  bytes node_ids = 1; // The nodeIDs of nodes belonging to the next epoch that approve the epoch change.
                      // In practice, this is a bit-map that corresponds to the nodes in the block_validation_descriptor.
  bytes aux_info_digest = 2; // The digest of the auxiliary information that is approved by the nodes.
  bytes signature = 3; // The signature of the nodes that approve the epoch change.
}

message SimplexEpochInfo {
   uint64 p_chain_reference_height = 1;
   uint64 epoch_number = 2;
   uint64 next_p_chain_reference_height = 3;
   uint64 prev_vm_block_seq = 4; // The sequence of the previous VM block
   uint64 sealing_block_seq = 5; // The sequence number of the sealing block
   BlockValidationDescriptor block_validation_descriptor = 6; // Describes how to validate the blocks of the next epoch
   NextEpochApprovals next_epoch_approvals = 7; // The epoch change approvals of the next epoch by at least n-f nodes.
}
```
