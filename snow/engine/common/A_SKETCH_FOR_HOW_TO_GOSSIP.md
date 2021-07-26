# A Sketch about how gossiping is handled in Primary Network VMs

## Intro

This is a draft to discuss some implementation points. It could become another README.md similar to what we did for Snowman++.

All the proposal below are meant to be implemented for all Primary Network VMs.  

### Open Points

1. Verify a common structure is possible for all Primary Network VMs.
1. In this very first pass, I am not considering any mechanism to ban a misbehaving node (for whatever definition of misbehaving node, that btw, I believe we should clearly state belows). Is it acceptable?

## Internal data structures

Borrowing from Bitcoin, I would use the following `inv` object as base for all app-level gossiping:

```go
type Inv struct {
    TxType int
    TxID   ids.ID
}
```

List of valid `TxType`s are VM specific.  
Why `TxType`? `TxType` gives convenience of knowing tha tx type before receiving and decoding it. It could be that, say on `AppRequest` failure, a node can decide if/how re-attempt to find the tx, depending on its type.

Again borrowing from Bitcoin, I would extend VMs' mempool with a `map` like:

```go
mapRelay map[Inv] []byte
```

where `mapRelay` contains the serialized version of the tx whose ID is in the key.

A tx should be added in `mapRelay` as soon as it enters the mempool.  
A tx should be removed from `mapRelay`  once it is confirmed or rejected. This means that a serialzied copy of each tx will be in `mapRelay` after that tx has left the mempool (until confirmed/rejected) to serve other nodes which may want to know about it.

Upon `AppRequest` a node will look for the requested txes into its `mapRelay`.

### Open Points

1. How to make sure `mapRelay` size does not explode? How should tx be prioritized in case a maximum size is specified?
1. This structure could be placed somewhere into the engine, and work as blueprint for future VMs? Or should it be taken private to each VM?

## Messages Structure

### AppRequest

```go
type AppRequest struct {
    Version int
    RequestedIDs []inv    
}

```

`Version` is for future proofing, and should allow different messages format and protocol.  
If `RequestedIDs` is empty any tx in node mempool is requested.
A maximum size for `RequestedIDs` must be set and each `AppRequest` with larger number of `RequestedIDs` would be discarded

What if an `AppRequestFailure` is received in response? Should one or more random different node be selected to discover the tx? Or should be wait for other nodes to gossip about the tx?

### AppResponse

```go
type AppResponse struct {
    Version
    Txes [][]bytes 
}

```

### AppGossip

```go
type AppGossip struct {
    Version int
    GossipedIDs []inv    
}

```

Multiple `invs` can go there in principle.

Probably the same maximum size used for `AppRequest.RequestedIDs` must be set for `GossipedIDs`.

## Open Points

1. Where are target nodes for `AppGossip` selected? At VM level or engine level?
