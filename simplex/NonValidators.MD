### Replicating Blocks for Non-Validator Nodes

**Motive**
The current implementation of Simplex sends messages only to validator nodes. Messages can be sent directly to a specific node or broadcast to all nodes validating the chain. This limitation means Simplex cannot support non-validating nodes listening for updates to the chain. Namely, non-validating nodes such as API nodes and light clients have no way of querying the blockchain for accepted blocks. This document outlines the method for allowing non-validators to replicate accepted blocks.

**Goal**: Allow nodes not participating in consensus (non-validators) to be able to replicate and verify accepted blocks.

**Broadcasting Finalizations**

In order to learn about blocks that were accepted, non-validators need to receive finalizations. To accommodate this, we should expand the `Communication` implementation to also broadcast to non-validator nodes whenever the message being broadcasted is a `FinalizationMessage`. This does not require any changes to the `Simplex` protocol itself, as the `Communication` component is already an isolated component. It would be up to the `Communication` struct and `Engine` that wraps `Simplex` to maintain a list of non-validator nodes and broadcast finalization messages to them.

Once a non-validator receives a finalization message for a higher sequence, it can then request the full block via the standard replication path described below.

**Standard Replication Path**: Requesting Missing Sequences
When a validating node falls behind it will eventually receive a finalization for a `seq x` that is greater than its current known seq. A finalization for a higher sequence means the node has fallen behind, and blocks for sequences up to and including `seq x` are guaranteed to be accepted into the chain. In this case, the node will begin the replication process which involves requesting the sequences it is missing (all sequences from its last accepted seq to and including `x`). Eventually, the nodes will respond and the lagging node will have caught up.

Simplex currently only accepts and processes messages from validators. These messages can be normal consensus messages or replication requests/responses.

`ReplicationRequests` have the following structure:

```go
type ReplicationRequest struct {
   Seqs        []uint64 // sequences we are requesting
   LatestRound uint64   // latest round that we are aware of
}

```

A `ReplicationResponse` has the following structure:

```go
type ReplicationResponse struct {
   Data        []QuorumRound
   LatestRound *QuorumRound
}
// QuorumRound represents a round that has achieved quorum on either
// (empty notarization), (block & notarization), or (block, finalization)
type QuorumRound struct {
   Block             Block
   Notarization      *Notarization
   Finalization      *Finalization
   EmptyNotarization *EmptyNotarization
}
```

Non-validators should re-use this logic and can ignore any `QuorumRound` that do not have finalizations. This main benefit of reusing the same logic is that we can take advantage of the in-memory `rounds` map which will reduce the number of times we read from storage. Secondarily, this would be a minimalistic change that won't require maintaining a separate code path. In order to allow this, `Simplex` needs to be modified to accept `ReplicationRequests` from non-validator nodes.

**Processing Replication Responses**

When a non-validator receives a `ReplicationResponse`, it needs to verify the `QuorumRounds` by verifying the aggregated BLS signature amongst the corresponding validator set. Since different epochs can be [configured](https://github.com/ava-labs/avalanchego/pull/4124) with different validator sets, the non-validator needs to ensure it has the correct validator set for the epoch in which the `Finalization` was created. In the proposed implementation, the `Epoch` number represents the sequence of the sealing block the `Finalization` was created for. The non-validator node can follow [this proposed logic](https://github.com/ava-labs/avalanchego/pull/4124/files#diff-b9b433e0fcc085f654c562e9eaadb1e4de29b7fe6ee4bdc970280b895b3c078bR305) to verify the missing `Finalizations`:

**Bringing It All Together**

Non-validators do not need to participate in consensus, so they can run a simplified version of `Simplex` that only processes `FinalizationMessages` and `ReplicationRequests/Responses`. The non-validator can maintain its own list of known validator nodes to send `ReplicationRequests` to. Whenever it receives a `FinalizationMessage` for a higher sequence than it currently knows, it can initiate the replication process by sending `ReplicationRequests` to the validators, or first syncing the correct validator set if necessary. Once it receives the `ReplicationResponses`, it can verify and store the accepted blocks.
