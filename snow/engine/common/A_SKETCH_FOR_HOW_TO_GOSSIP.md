# A Sketch about how gossiping is handled ~~in Primary Network VMs~~ in PlaformVM

## Intro

This is a draft to discuss some implementation points. It could become another README.md similar to what we did for Snowman++.

## Minimum viable Product definition

1. Mempool gossip functionalities are unlocked for P-chain only at first, at VM level. Immediately after mempool gossiping will be introduced for C-chain, and common elements refactored.
1. A VM-specific mempool has fixed size. Txes are accepted only if there is enough space. If mempool is at max capacity, incoming tx is simply dropped (no smart tx replacement to start).
1. Transactions are gossiped as soon as are accepted in the mempool; they are served as long as they stay in the mempool. A newly accepted tx ID is broadcasted around to a random subset of peers with `AppGossip` message; full tx can be learned by interested peers by a `AppRequest`/`AppResponse` message exchange.
1. A tx accepted into a mempool must be syntactically verified. Moreover all the feasible semantic verifications must be carried out (TODO: refine with specifics).
1. Zero-confirmation tx are not currently supported; in general a limit will be placed to the length of zero-confirmation transactions chain allowed into the mempool.
1. Rejected transaction IDs are stored in a cache to avoid re-requests.
1. An activation time for mempool gossiping must be introduced to allow coordination with snowman++ publication.
1. No banning of misbehaving peers so far. We rely on per-user throttling as recently deployed to preserve resources.

### Open points

1. What is the high level rationale for a mempool in our system?

## Messages Structure

### AppRequest

```go
type AppRequest struct {
    Version int
    RequestedIDs []ids.ID
}

```

`Version` is for future proofing, and should allow different messages format and protocol.  
If `RequestedIDs` is empty any tx in node mempool is requested.
A maximum size for `RequestedIDs` must be set and each `AppRequest` with larger number of `RequestedIDs` would be discarded

OPEN POINT: What if an `AppRequestFailure` is received in response? Should one or more random different node be selected to discover the tx? Or should be wait for other nodes to gossip about the tx?

### AppResponse

```go
type AppResponse struct {
    Version int
    Txes [][]bytes
}

```

### AppGossip

```go
type AppGossip struct {
    Version int
    GossipedIDs []ids.ID
}

```

Multiple tx IDs can go there in principle.

Probably the same maximum size used for `AppRequest.RequestedIDs` must be set for `GossipedIDs`.

OPEN POINT: Where are target nodes for `AppGossip` selected? At VM level or engine level?
