### Replicating Blocks for Non-Validator Nodes

**Motive**
The current implementation of Simplex sends messages only to validator nodes. Messages can be sent directly to a specific node or broadcast to all nodes validating the chain. This limitation means Simplex cannot support non-validating nodes listening for updates to the chain. Namely, non-validating nodes such as API nodes and light clients have no way of querying the blockchain for accepted blocks. This document outlines the method for allowing non-validators to replicate accepted blocks.

**Goal**: Allow nodes not participating in consensus (non-validators) to be able to replicate and verify accepted blocks.

**Standard Replication Path**: Requesting Missing Sequences
When a validating node falls behind it will eventually receive a finalization for a `seq x` that is greater than its current known seq. A finalization for a higher sequence means the node has fallen behind, and blocks for sequences up to and including `seq x` are guaranteed to be accepted into the chain. In this case, the node will begin the replication process which involves requesting the sequences it is missing (all sequences from its last accepted seq to `x`). Eventually, the nodes will respond and the lagging node will have caught up.
Simplex’s `HandleMessage` function will process the replication request and respond with a response.

```go
func (e *Epoch) HandleMessage(msg *Message, from NodeID) error {
```

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

Non-validators should re-use this logic and can ignore any `QuorumRound` that do not have finalizations. This main benefit of reusing the same logic is that we can take advantage of the in-memory `rounds` map which will reduce the number of times we read from storage. Secondarily, this would be a minimalistic change that won't require maintaining a separate code path.

**Broadcasting Finalizations**

In order to learn about blocks that were accepted, non-validators need to receive finalizations. To accommodate this, we should expand the `Communication` implementation to also broadcast to non-validator nodes whenever the message being broadcasted is a `FinalizationMessage`. Once a non-validator receives a finalization message for a higher sequence, it can then request the full block via the standard replication path.
