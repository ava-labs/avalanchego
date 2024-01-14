# Avalanche Networking

## Table of Contents

- [Overview](#overview)
- [Peers](#peers)
  - [Lifecycle](#lifecycle)
    - [Bootstrapping](#bootstrapping)
    - [Connecting](#connecting)
      - [Peer Handshake](#peer-handshake)
    - [Connected](#connected)
      - [PeerList Gossip](#peerlist-gossip)
        - [Messages](#messages)
        - [Gossip](#gossip)

## Overview

Avalanche is a decentralized [p2p](https://en.wikipedia.org/wiki/Peer-to-peer) (peer-to-peer) network of nodes that work together to run the Avalanche blockchain protocol.

The `network` package implements the networking layer of the protocol which allows a node to discover, connect to, and communicate with other peers.

## Peers

Peers are defined as members of the network that communicate with one another to participate in the Avalanche protocol.

Peers communicate by enqueuing messages between one another. Each peer on either side of the connection asynchronously reads and writes messages to and from the remote peer. Messages include both application-level messages used to support the Avalanche protocol, as well as networking-level messages used to implement the peer-to-peer communication layer.

```mermaid
sequenceDiagram
    loop 
        Peer-1->>Peer-2: Write outbound messages
        Peer-2->>Peer-1: Read incoming messages
    end
    loop
        Peer-2->>Peer-1: Write outbound messages
        Peer-1->>Peer-2: Read incoming messages
    end
```

### Lifecycle

#### Bootstrapping

When starting an Avalanche node, a node needs to be able to initiate some process that eventually allows itself to become a participating member of the network. In traditional web2 systems, it's common to use a web service by hitting the service's DNS and being routed to an available server behind a load balancer. In decentralized p2p systems however, connecting to a node is more complex as no single entity owns the network. [Avalanche consensus](https://docs.avax.network/overview/getting-started/avalanche-consensus) requires a node to repeatedly sample peers in the network, so each node needs some way of discovering and connecting to every other peer to participate in the protocol.

In Avalanche, nodes connect to an initial set of bootstrapper nodes known as **beacons** (this is user-configurable). Once connected to a set of beacons, a node is able to discover other nodes in the network. Over time, a node eventually discovers other peers in the network through `PeerList` messages it receives through:

- The handshake initiated between two peers when attempting to connect to a peer (see [Connecting](#connecting)).
- Responses to periodically sent `GetPeerList` messages requesting a `PeerList` of unknown peers (see [Connected](#connected)).

#### Connecting

##### Peer Handshake

Upon connection to any peer, a handshake is performed between the node attempting to establish the outbound connection to the peer and the peer receiving the inbound connection.

When attempting to establish the connection, the first message that the node attempting to connect to the peer in the network is a `Handshake` message describing compatibility of the candidate node with the peer. As an example, nodes that are attempting to connect with an incompatible version of AvalancheGo or a significantly skewed local clock are rejected by the peer.

```mermaid
sequenceDiagram
Note over Node,Peer: Initiate Handshake
Note left of Node: I want to connect to you!
Note over Node,Peer: Handshake message
Node->>Peer: AvalancheGo v1.0.0
Note right of Peer: My version v1.9.4 is incompatible with your version v1.0.0.
Peer-xNode: Connection dropped
Note over Node,Peer: Handshake Failed
```

If the `Handshake` message is successfully received and the peer decides that it wants a connection with this node, it replies with a `PeerList` message that contains metadata about other peers that allows a node to connect to them. Upon reception of a `PeerList` message, a node will attempt to connect to any peers that the node is not already connected to to allow the node to discover more peers in the network.

```mermaid
sequenceDiagram
Note over Node,Peer: Initiate Handshake
Note left of Node: I want to connect to you!
Note over Node,Peer: Handshake message
Node->>Peer: AvalancheGo v1.9.4
Note right of Peer: LGTM!
Note over Node,Peer: PeerList message
Peer->>Node: Peer-X, Peer-Y, Peer-Z
Note over Node,Peer: Handshake Complete
```

Once the node attempting to join the network receives this `PeerList` message, the handshake is complete and the node is now connected to the peer. The node attempts to connect to the new peers discovered in the `PeerList` message. Each connection results in another peer handshake, which results in the node incrementally discovering more and more peers in the network as more and more `PeerList` messages are exchanged.

#### Connected

Some peers aren't discovered through the `PeerList` messages exchanged through peer handshakes. This can happen if a peer is either not randomly sampled, or if a new peer joins the network after the node has already connected to the network.

```mermaid
sequenceDiagram
Node ->> Peer-1: Handshake - v1.9.5
Peer-1 ->> Node: PeerList - Peer-2
Note left of Node: Node is connected to Peer-1 and now tries to connect to Peer-2.
Node ->> Peer-2: Handshake - v1.9.5
Peer-2 ->> Node: PeerList - Peer-1
Note left of Node: Peer-3 was never sampled, so we haven't connected yet!
Node --> Peer-3: No connection
```

To guarantee that a node can discover all peers, each node periodically sends a `GetPeerList` message to a random peer.

##### PeerList Gossip

###### Messages

A `GetPeerList` message requests that the peer sends a `PeerList` message. `GetPeerList` messages contain a bloom filter of already known peers to reduce useless bandwidth on `PeerList` messages. The bloom filter reduces bandwidth by enabling the `PeerList` message to only include peers that aren't already known.

A `PeerList` is the message that is used to communicate the presence of peers in the network. Each `PeerList` message contains signed networking-level metadata about a peer that provides the necessary information to connect to it.

Once peer metadata is received, the node will add that data to its bloom filter to prevent learning about it again.

###### Gossip

Handshake messages provide a node with some knowledge of peers in the network, but offers no guarantee that learning about a subset of peers from each peer the node connects with will result in the node learning about every peer in the network.

To provide an eventual guarantee that all peers learn of one another, each node periodically requests peers from a random peer.

To optimize bandwidth, each node tracks the most recent IPs of validators. The validator's nodeID and timestamp are inserted into a bloom filter which is used to select only necessary IPs to gossip.

As the number of entries increases in the bloom filter, the probability of a false positive increases. False positives can cause recent IPs not to be gossiped when they otherwise should be, slowing down the rate of `PeerList` gossip. To prevent the bloom filter from having too many false positives, a new bloom filter is periodically generated and the number of entries a validator is allowed to have in the bloom filter is capped. Generating the new bloom filter both removes stale entries and modifies the hash functions to avoid persistent hash collisions.

A node follows the following steps for of `PeerList` gossip:

```mermaid
sequenceDiagram
Note left of Node: Initialize bloom filter
Note left of Node: Bloom: [0, 0, 0]
Node->>Peer-123: GetPeerList [0, 0, 0]
Note right of Peer-123: Any peers can be sent.
Peer-123->>Node: PeerList - Peer-1
Note left of Node: Bloom: [1, 0, 0]
Node->>Peer-123: GetPeerList [1, 0, 0]
Note right of Peer-123: Either Peer-2 or Peer-3 can be sent.
Peer-123->>Node: PeerList - Peer-3
Note left of Node: Bloom: [1, 0, 1]
Node->>Peer-123: GetPeerList [1, 0, 1]
Note right of Peer-123: Only Peer-2 can be sent.
Peer-123->>Node: PeerList - Peer-2
Note left of Node: Bloom: [1, 1, 1]
Node->>Peer-123: GetPeerList [1, 1, 1]
Note right of Peer-123: There are no more peers left to send!
```
