# Avalanche Networking

## Table of Contents

- [Overview](#overview)
- [Peers](#peers)
  - [Message Handling](#message-handling)
  - [Peer Handshake](#peer-handshake)
  - [Ping-Pong Messages](#ping-pong-messages)
- [Peer Discovery](#peer-discovery)
    - [Inbound Connections](#inbound-connections)
    - [Outbound Connections](#outbound-connections)
      - [IP Authentication](#ip-authentication)
    - [Bootstrapping](#bootstrapping)
    - [PeerList Gossip](#peerlist-gossip)
      - [Bloom Filter](#bloom-filter)
      - [GetPeerList](#getpeerlist)
      - [PeerList](#peerlist)
      - [Avoiding Persistent Network Traffic](#avoiding-persistent-network-traffic)

## Overview

Avalanche is a decentralized [p2p](https://en.wikipedia.org/wiki/Peer-to-peer) (peer-to-peer) network of nodes that work together to run the Avalanche blockchain protocol.

The `network` package implements the networking layer of the protocol which allows a node to discover, connect to, and communicate with other peers.

All connections are authenticated using [TLS](https://en.wikipedia.org/wiki/Transport_Layer_Security). However, there is no reliance on any certificate authorities. The `network` package identifies peers by the public key in the leaf certificate.

## Peers

Peers are defined as members of the network that communicate with one another to participate in the Avalanche protocol.

Peers communicate by enqueuing messages between one another. Each peer on either side of the connection asynchronously reads and writes messages to and from the remote peer. Messages include both application-level messages used to support the Avalanche protocol, as well as networking-level messages used to implement the peer-to-peer communication layer.

```mermaid
sequenceDiagram
    actor Morty
    actor Rick
    loop 
        Morty->>Rick: Write outbound messages
        Rick->>Morty: Read incoming messages
    end
    loop
        Rick->>Morty: Write outbound messages
        Morty->>Rick: Read incoming messages
    end
```

### Message Handling

All messages are prefixed with their length. Reading a message first reads the 4-byte message length from the connection. The rate-limiting logic then waits until there is sufficient capacity to read these bytes from the connection.

A peer will then read the full message and attempt to parse it into either a networking message or an application message. If the message is malformed the connection is not dropped. The peer will simply continue to the next sent message.

### Peer Handshake

Upon connection to a new peer, a handshake is performed between the node attempting to establish the outbound connection to the peer and the peer receiving the inbound connection.

When attempting to establish the connection, the first message that the node sends is a `Handshake` message describing the configuration of the node. If the `Handshake` message is successfully received and the peer decides that it will allow a connection with this node, it replies with a `PeerList` message that contains metadata about other peers that allows a node to connect to them. See [PeerList Gossip](#peerlist-gossip).

As an example, nodes that are attempting to connect with an incompatible version of AvalancheGo or a significantly skewed local clock are rejected.

```mermaid
sequenceDiagram
    actor Morty
    actor Rick
    Note over Morty,Rick: Connection Created
    par
        Morty->>Rick: AvalancheGo v1.0.0
    and
        Rick->>Morty: AvalancheGo v1.11.4
    end
    Note right of Rick: v1.0.0 is incompatible with v1.11.4.
    Note left of Morty: v1.11.4 could be compatible with v1.0.0!
    par
        Rick-->>Morty: Disconnect
    and
        Morty-XRick: Peerlist
    end
    Note over Morty,Rick: Handshake Failed
```

Nodes that mutually desire the connection will both respond with `PeerList` messages and complete the handshake.

```mermaid
sequenceDiagram
    actor Morty
    actor Rick
    Note over Morty,Rick: Connection Created
    par
        Morty->>Rick: AvalancheGo v1.11.0
    and
        Rick->>Morty: AvalancheGo v1.11.4
    end
    Note right of Rick: v1.11.0 is compatible with v1.11.4!
    Note left of Morty: v1.11.4 could be compatible with v1.11.0!
    par
        Rick->>Morty: Peerlist
    and
        Morty->>Rick: Peerlist
    end
    Note over Morty,Rick: Handshake Complete
```

### Ping-Pong Messages

Peers periodically send `Ping` messages containing perceived uptime information. This information can be used to monitor how the node is considered to be performing by the network. It is expected for a node to reply to a `Ping` message with a `Pong` message.

```mermaid
sequenceDiagram
    actor Morty
    actor Rick
    Note left of Morty: Send Ping
    Morty->>Rick: I think your uptime is 95%
    Note right of Rick: Send Pong
    Rick->>Morty: ACK
```

## Peer Discovery

When starting an Avalanche node, a node needs to be able to initiate some process that eventually allows itself to become a participating member of the network. In traditional web2 systems, it's common to use a web service by hitting the service's DNS and being routed to an available server behind a load balancer. In decentralized p2p systems, however, connecting to a node is more complex as no single entity owns the network. [Avalanche consensus](https://build.avax.network/docs/quick-start/avalanche-consensus) requires a node to repeatedly sample peers in the network, so each node needs some way of discovering and connecting to every other peer to participate in the protocol.

### Inbound Connections

It is expected for Avalanche nodes to allow inbound connections. If a validator does not allow inbound connections, its observed uptime may be reduced.

### Outbound Connections

Avalanche nodes that have identified the `IP:Port` pair of a node they want to connect to will initiate outbound connections to this `IP:Port` pair. If the connection is not able to complete the [Peer Handshake](#peer-handshake), the connection will be re-attempted with an [Exponential Backoff](https://en.wikipedia.org/wiki/Exponential_backoff).

A node should initiate outbound connections to an `IP:Port` pair that is believed to belong to another node that is not connected and meets at least one of the following conditions:
- The peer is in the initial bootstrapper set.
- The peer is in the default bootstrapper set.
- The peer is a Primary Network validator.
- The peer is a validator of a tracked Subnet.
- The peer is a validator of a Subnet and the local node is a Primary Network validator.

#### IP Authentication

To ensure that outbound connections are being made to the correct `IP:Port` pair of a node, all `IP:Port` pairs sent by the network are signed by the node that is claiming ownership of the pair. To prevent replays of these messages, the signature is over the `Timestamp` in addition to the `IP:Port` pair.

The `Timestamp` guarantees that nodes provided an `IP:Port` pair can track the most up-to-date `IP:Port` pair of a peer.

### Bootstrapping

In Avalanche, nodes connect to an initial set (this is user-configurable) of bootstrap nodes.

### PeerList Gossip

Once connected to an initial set of peers, a node can use these connections to discover additional peers.

Peers are discovered by receiving [`PeerList`](#peerlist) messages during the [Peer Handshake](#peer-handshake). These messages quickly provide a node with knowledge of peers in the network. However, they offer no guarantee that the node will connect to and maintain connections with every peer in the network.

To provide an eventual guarantee that all peers learn of one another, nodes periodically send a [`GetPeerList`](#getpeerlist) message to a randomly selected Primary Network validator with the node's current [Bloom Filter](#bloom-filter) and `Salt`.

#### Gossipable Peers

The peers that a node may include into a [`GetPeerList`](#getpeerlist) message are considered `gossipable`.


#### Trackable Peers

The peers that a node would attempt to connect to if included in a [`PeerList`](#peerlist) message are considered `trackable`.

#### Bloom Filter

A [Bloom Filter](https://en.wikipedia.org/wiki/Bloom_filter) is used to track which nodes are known.

The parameterization of the Bloom Filter is based on the number of desired peers.

Entries in the Bloom Filter are determined by a locally calculated [`Salt`](https://en.wikipedia.org/wiki/Salt_(cryptography)) along with the `NodeID` and `Timestamp` of the most recently known `IP:Port`. The `Salt` is added to prevent griefing attacks where malicious nodes intentionally generate hash collisions with other virtuous nodes to reduce their connectivity.

The Bloom Filter is reconstructed if there are more entries than expected to avoid increasing the false positive probability. It is also reconstructed periodically. When reconstructing the Bloom Filter, a new `Salt` is generated.

To prevent a malicious node from arbitrarily filling this Bloom Filter, only `2` entries are added to the Bloom Filter per node. If a node's `IP:Port` pair changes once, it will immediately be added to the Bloom Filter. If a node's `IP:Port` pair changes more than once, it will only be added to the Bloom Filter after the Bloom Filter is reconstructed.

#### GetPeerList

A `GetPeerList` message contains the Bloom Filter of the currently known peers along with the `Salt` that was used to add entries to the Bloom Filter. Upon receipt of a `GetPeerList` message, a node is expected to respond with a `PeerList` message.

#### PeerList

`PeerList` messages are expected to contain `IP:Port` pairs that satisfy all of the following constraints:
- The Bloom Filter sent when requesting the `PeerList` message does not contain the node claiming the `IP:Port` pair.
- The node claiming the `IP:Port` pair is currently connected.
- The node claiming the `IP:Port` pair is either in the default bootstrapper set, is a current Primary Network validator, is a validator of a tracked Subnet, or is a validator of a Subnet and the peer is a Primary Network validator.

#### Avoiding Persistent Network Traffic

To avoid persistent network traffic, it must eventually hold that the set of [`gossipable peers`](#gossipable-peers) is a subset of the [`trackable peers`](#trackable-peers) for all nodes in the network.

For example, say there are 3 nodes: `Rick`, `Morty`, and `Summer`.

First we consider the case that `Rick` and `Morty` consider `Summer` [`gossipable`](#gossipable-peers) and [`trackable`](#trackable-peers), respectively.
```mermaid
sequenceDiagram
    actor Morty
    actor Rick
    Note left of Morty: Not currently tracking Summer
    Morty->>Rick: GetPeerList
    Note right of Rick: Summer isn't in the bloom filter
    Rick->>Morty: PeerList - Contains Summer
    Note left of Morty: Track Summer and add to bloom filter
    Morty->>Rick: GetPeerList
    Note right of Rick: Summer is in the bloom filter
    Rick->>Morty: PeerList - Empty
```
This case is ideal, as `Rick` only notifies `Morty` about `Summer` once, and never uses bandwidth for their connection again.

Now we consider the case that `Rick` considers `Summer` [`gossipable`](#gossipable-peers), but `Morty` does not consider `Summer` [`trackable`](#trackable-peers).
```mermaid
sequenceDiagram
    actor Morty
    actor Rick
    Note left of Morty: Not currently tracking Summer
    Morty->>Rick: GetPeerList
    Note right of Rick: Summer isn't in the bloom filter
    Rick->>Morty: PeerList - Contains Summer
    Note left of Morty: Ignore Summer
    Morty->>Rick: GetPeerList
    Note right of Rick: Summer isn't in the bloom filter
    Rick->>Morty: PeerList - Contains Summer
```
This case is suboptimal, because `Rick` told `Morty` about `Summer` multiple times. If this case were to happen consistently, `Rick` may waste a significant amount of bandwidth trying to teach `Morty` about `Summer`.