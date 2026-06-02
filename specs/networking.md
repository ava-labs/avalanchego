# Networking — P2P Layer

## Directory Layout

```
network/
├── network.go          # Network interface and implementation
├── config.go           # Network configuration
├── metrics.go          # Network metrics
├── ip_tracker.go       # IP discovery and gossip management
├── tracked_ip.go       # Per-peer IP state and backoff
├── dialer/             # Outbound TCP connections with dial rate limiting
├── peer/               # Peer lifecycle, TLS upgrade, message queues
│   ├── peer.go         # Core peer goroutines
│   ├── upgrader.go     # TLS connection upgrade
│   ├── tls_config.go   # TLS configuration
│   ├── message_queue.go # Outbound message queue
│   └── ip_signer.go    # IP address signing
├── throttling/         # Inbound and outbound rate limiting
└── p2p/                # Higher-level P2P abstractions (gossip, handlers)
    └── gossip/         # Push/pull gossip subsystem
```

---

## 1. Network Interface

```go
// network/network.go
type Network interface {
    sender.ExternalSender  // Send messages to specific peers
    health.Checker
    peer.Network           // Peer lifecycle callbacks
    StartClose()
    Dispatch() error       // Accept inbound TCP connections
    ManuallyTrack(nodeID ids.NodeID, ip netip.AddrPort)
    PeerInfo(nodeIDs []ids.NodeID) []peer.Info
    NodeUptime() (UptimeResult, error)
}
```

**Implementation internals:**
```go
type network struct {
    config *Config
    peersLock sync.RWMutex
    connectingPeers peer.Set  // handshake in progress
    connectedPeers  peer.Set  // handshake complete
    ipTracker       *ipTracker
    onCloseCtx      context.Context
}
```

---

## 2. Peer Lifecycle

### 2.1 States

```
1. DIALING          — network.dial() goroutine connecting to IP
2. TLS UPGRADE      — connToIDAndCert() performs TLS handshake; extracts nodeID from cert
3. CONNECTING       — peer.Start() spawned; handshake messages being exchanged
4. CONNECTED        — PeerList received, finishedHandshake=true, in connectedPeers
5. OPERATIONAL      — data messages exchanged (Ping/Pong, consensus, app)
6. DISCONNECTING    — connection closes; Disconnect() called; cleanup
```

### 2.2 Peer Start

```go
// network/peer/peer.go
func Start(config, conn, cert, nodeID, messageQueue, isIngress) *Peer {
    // Three goroutines:
    // 1. readMessages()         — deserialize and dispatch inbound messages
    // 2. writeMessages()        — serialize and send outbound messages
    // 3. sendNetworkMessages()  — periodic Ping + GetPeerList
}
```

### 2.3 Handshake Message (`Handshake`)

Sent as the first message by both sides. Fields:

| Field | Description |
|-------|-------------|
| `network_id` | Must match the peer's expected network |
| `my_time` | Unix timestamp (clock skew detection, max ~1 min) |
| `ip_addr`, `ip_port` | Claimed public address |
| `ip_signing_time` | Timestamp of IP signature |
| `ip_node_id_sig` | TLS signature over `(IP:port, timestamp)` |
| `ip_bls_sig` | BLS signature over `(IP:port, timestamp)` |
| `upgrade_time` | Latest scheduled upgrade this node knows about |
| `tracked_subnets` | Subnets this peer validates (max 16) |
| `supported_acps`, `objected_acps` | Avalanche Consensus Proposal versions (no overlap) |
| `known_peers` | Bloom filter of known validator IPs |

**Validation steps:**
1. Only one Handshake per connection.
2. `network_id` must match.
3. Clock skew must be within `MaxClockDifference`.
4. TLS and BLS IP signatures verified.
5. If peer is a validator, BLS key checked against validator set.
6. Tracked subnets ≤ 16.
7. No overlap in supported/objected ACPs.

After handshake, server sends a `PeerList` response (completing the handshake on the client side).

### 2.4 IP Discovery (`network/ip_tracker.go`)

The `ipTracker` categorizes tracked nodes:
- **Manually tracked** — explicit via `ManuallyTrack()`
- **Validator tracked** — from validator set callbacks
- **Subnet validators** — for specific subnets

Per-node state:
```go
type trackedNode struct {
    manuallyTracked  bool
    validatedSubnets set.Set[ids.ID]
    trackedSubnets   set.Set[ids.ID]
    ip               *ips.ClaimedIPPort
}
```

**Bloom filter reset:** Every `PeerListBloomResetFreq` (~5 min), `ipTracker.ResetBloom()` recomputes the filter for all known validators. The filter prevents re-gossiping IPs the peer already knows.

### 2.5 Dial with Exponential Backoff

```go
// network/tracked_ip.go
func (ip *trackedIP) increaseDelay() {
    // delay *= rand(1, 2)
    // clamped to [InitialReconnectDelay, MaxReconnectDelay]
}
```

On IP update from a peer, the old dial goroutine is cancelled and a new one started with the updated address (but the backoff delay is carried over).

---

## 3. Message Protocol

### 3.1 Wire Format

Messages are length-prefixed:
```
[4-byte big-endian length][protobuf-encoded p2p.Message]
```

`p2p.Message` is defined in `proto/p2p/p2p.proto`. It has a `oneof message` with all possible message types. Optional zstd compression:
```protobuf
message Message {
    oneof message {
        bytes           compressed_zstd = 2;  // wrapped compressed Message
        Ping            ping            = 11;
        Pong            pong            = 12;
        Handshake       handshake       = 13;
        // ... all other types
    }
}
```

### 3.2 All Message Types

| Op | Protobuf field | Phase | Direction | Handler |
|----|----------------|-------|-----------|---------|
| Ping (11) | `ping` | Operational | Bi-dir | `handlePing()` |
| Pong (12) | `pong` | Operational | Bi-dir | `handlePong()` |
| Handshake (13) | `handshake` | Init | First sent | `handleHandshake()` |
| GetPeerList (35) | `get_peer_list` | Post-handshake | Either | `handleGetPeerList()` |
| PeerList (14) | `peer_list` | Handshake/pull | Response | `handlePeerList()` |
| GetStateSummaryFrontier (15) | `get_state_summary_frontier` | State sync | Request | Engine |
| StateSummaryFrontier (16) | `state_summary_frontier` | State sync | Response | Engine |
| GetAcceptedStateSummary (17) | `get_accepted_state_summary` | State sync | Request | Engine |
| AcceptedStateSummary (18) | `accepted_state_summary` | State sync | Response | Engine |
| GetAcceptedFrontier (19) | `get_accepted_frontier` | Bootstrap | Request | Engine |
| AcceptedFrontier (20) | `accepted_frontier` | Bootstrap | Response | Engine |
| GetAccepted (21) | `get_accepted` | Bootstrap | Request | Engine |
| Accepted (22) | `accepted` | Bootstrap | Response | Engine |
| GetAncestors (23) | `get_ancestors` | Bootstrap | Request | Engine |
| Ancestors (24) | `ancestors` | Bootstrap | Response | Engine |
| Get (25) | `get` | Consensus | Request | Engine |
| Put (26) | `put` | Consensus | Response | Engine |
| PushQuery (27) | `push_query` | Consensus | Request | Engine |
| PullQuery (28) | `pull_query` | Consensus | Request | Engine |
| Chits (29) | `chits` | Consensus | Response | Engine |
| AppRequest (30) | `app_request` | App | Request | VM |
| AppResponse (31) | `app_response` | App | Response | VM |
| AppGossip (32) | `app_gossip` | App | Push | VM |
| AppError (34) | `app_error` | App | Error | VM |
| Simplex (36) | `simplex` | Simplex consensus | Various | Simplex engine |

### 3.3 Simplex Sub-messages

The `Simplex` message (field 36) carries:
```protobuf
message Simplex {
    bytes chain_id = 1;
    oneof message {
        BlockProposal      block_proposal      = 2;
        Vote               vote                = 3;
        EmptyVote          empty_vote          = 4;
        Vote               finalize_vote       = 5;
        QuorumCertificate  notarization        = 6;
        EmptyNotarization  empty_notarization  = 7;
        QuorumCertificate  finalization        = 8;
        ReplicationRequest replication_request = 9;
        ReplicationResponse replication_response = 10;
    }
}
```

### 3.4 Message Dispatch in `peer.handle()`

```
handlePing()          ← p2p.Ping
handlePong()          ← p2p.Pong
handleHandshake()     ← p2p.Handshake
handleGetPeerList()   ← p2p.GetPeerList  (after handshake only)
handlePeerList()      ← p2p.PeerList     (after handshake only)
Router.HandleInbound()← everything else  (dropped if !finishedHandshake)
```

`Router.HandleInbound()` calls `ChainRouter.HandleInbound()` which dispatches the message to the per-chain `Handler.Push()`. The per-chain Handler then routes the message to the correct consensus engine (Snowman or Avalanche) or Simplex engine. See [consensus.md — Section 1.7 Handler](consensus.md#17-handler-snownetworkinghandlerhandlergo).

---

## 4. Peer Gossip and PeerList Exchange

### 4.1 GetPeerList

Sent by two mechanisms:
1. **Periodic pull** (`network.pullGossipPeerLists()`): Frequency ~1 min, samples 1 validator peer.
2. **Peer signal** (`peer.StartSendGetPeerList()`): Called when a new IP is tracked.

```protobuf
message GetPeerList {
    BloomFilter known_peers = 1;  // filter + salt
    bool all_subnets        = 2;  // request all subnets or only tracked ones
}
```

### 4.2 PeerList Response

```protobuf
message PeerList {
    repeated ClaimedIPPort claimed_ip_ports = 1;
}

message ClaimedIPPort {
    bytes  x509_certificate = 1;
    bytes  ip_addr          = 2;
    uint32 ip_port          = 3;
    uint64 timestamp        = 4;
    bytes  signature        = 5;  // TLS signature over (IP:port, timestamp)
    bytes  tx_id            = 6;  // P-chain validator tx
}
```

Server filters response using the bloom filter from `GetPeerList` to avoid sending known IPs. Returned IPs are tracked via `ipTracker.AddIP()` and dialed if not already connected.

### 4.3 IP Signature Verification

Every IP claim requires two signatures:
- **TLS signature** (`ip_node_id_sig`): ECDSA over `(IP:port, timestamp)`.
- **BLS signature** (`ip_bls_sig`): BLS over `(IP:port, timestamp)` — validators only.

Both must be valid before an IP is accepted.

---

## 5. Throttling

### 5.1 Inbound Message Throttling (5-layer composite)

All five checks must pass before a message is processed:

**1. Buffer throttle** (`inbound_msg_buffer_throttler.go`):
- Limits concurrent messages from a single peer: `MaxProcessingMsgsPerNode`.

**2. Byte throttle** (`inbound_msg_byte_throttler.go`):
- Sybil-safe: validators get allocation proportional to stake.
  - Validator pool: `VdrAllocSize × (stake / totalStake)`
  - At-large pool: `AtLargeAllocSize`, capped per non-validator at `NodeMaxAtLargeBytes`
- Queues messages waiting for bytes (FIFO).
- Stake weights are read from `validators.Manager.TotalWeight(primaryNetworkID)` and `GetWeight(primaryNetworkID, nodeID)` — the same `validators.Manager` interface managed by Snow consensus. See [consensus.md — Section 1.9 Validator Management](consensus.md#19-validator-management-snowvalidators).

**3. Bandwidth throttle** (`bandwidth_throttler.go`):
- Token bucket per peer: `RefillRate` bytes/sec, `MaxBurstSize` burst.
- Uses `golang.org/x/time/rate.Limiter`.

**4. CPU throttle** (`inbound_resource_throttler.go`):
- Blocks reading if peer's CPU usage exceeds target from `CPUTargeter`.

**5. Disk throttle** (`inbound_resource_throttler.go`):
- Same as CPU but for disk I/O.

**Resource release:**
```go
onFinishedHandling := throttler.Acquire(ctx, msgSize, nodeID)
// ... process message ...
onFinishedHandling()  // releases byte and CPU/disk allocations
```

### 5.2 Outbound Message Throttling

Same sybil-safe byte allocation as inbound (validator pool + at-large pool). Applied in `peer/message_queue.go` via `OutboundMsgThrottler.Acquire(msg, nodeID)`. Returns false if bytes not available — message is dropped.

### 5.3 Connection Upgrade Throttling

```go
// network/throttling/inbound_conn_upgrade_throttler.go
// Rate-limits how many new inbound TLS upgrades happen per cooldown window.
// Per-IP basis: prevents multi-connection DoS.
```

Config: `UpgradeCooldown`, `MaxRecentConnsUpgraded`.

### 5.4 Dial Throttling

```go
// network/throttling/dial_throttler.go
// Token bucket on outbound dial attempts: ThrottleRps.
```

---

## 6. TLS and Certificate Identity

> **Staking certificate management:** The TLS certificate used here is the node's staking identity certificate. Generation, storage, and NodeID derivation are managed by the `staking/` package. See [api.md — Section 7 Staking Certificate Management](api.md#7-staking-certificate-management-staking).

### 6.1 TLS Configuration

```go
// network/peer/tls_config.go
func TLSConfig(cert tls.Certificate, keyLogWriter io.Writer) *tls.Config {
    return &tls.Config{
        Certificates:       []tls.Certificate{cert},
        ClientAuth:         tls.RequireAnyClientCert,  // mutual TLS
        InsecureSkipVerify: true,                       // no hostname check
        MinVersion:         tls.VersionTLS13,
        VerifyConnection:   ValidateCertificate,        // custom validation
    }
}
```

Certificate must be ECDSA (P-256) or valid RSA. NodeID = RIPEMD160(SHA256(DER cert)).

### 6.2 Upgrade Flow

```
Accept TCP connection
  → tls.Server(conn, config) or tls.Client(conn, config)
  → TLS handshake
  → Extract peer cert from ConnectionState.PeerCertificates[0]
  → staking.ParseCertificate(cert.Raw)
  → ids.NodeIDFromCert(cert) → 20-byte NodeID
```

---

## 7. Uptime Tracking (from Peer's Perspective)

Each peer sends `Ping` messages with its perceived uptime of our node (0–100%):
```
peer.observedUptime → atomic uint32
```

`network.NodeUptime()` computes a stake-weighted average across all connected validator peers:
```go
UptimeResult {
    WeightedAveragePercentage float64
    RewardingStakePercentage  float64
}
```

`RewardingStakePercentage` is the fraction of total stake held by peers that report our uptime ≥ `UptimeRequirement`.

> **Feeds into Snow uptime tracking:** In addition to the peer-reported view above, the PlatformVM maintains its own authoritative uptime via `snow/uptime.Manager`. The PlatformVM calls `uptimeManager.Connect(nodeID)` / `Disconnect(nodeID)` as peers join and leave, tracking cumulative online time. This accumulated time determines whether a validator earns staking rewards at the end of their staking period. See [consensus.md — Section 1.10 Uptime Tracking](consensus.md#110-uptime-tracking-snowuptime) and [platformvm.md](platformvm.md).

---

## 8. Dialer (`network/dialer/`)

```go
type Dialer interface {
    Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error)
}
```

- Calls `d.dialer.DialContext(ctx, "tcp", ip.String())`.
- `ThrottleRps` limits outbound dial rate.
- `ConnectionTimeout` caps TCP handshake time.

---

## 9. P2P Higher-Level Abstractions (`network/p2p/`)

Used by VMs for application-level messaging:

```go
// network/p2p/network.go
type Network struct {
    handlers map[uint64]Handler  // handler ID → handler
    peers    *Peers
}

type Handler interface {
    AppRequest(ctx, nodeID, deadline, requestBytes) ([]byte, error)
    AppGossip(ctx, nodeID, gossipBytes) error
}
```

Gossip sub-package (`network/p2p/gossip/`) implements push and pull gossip:
- **Push gossip**: Broadcast new items to sampled peers.
- **Pull gossip**: Periodically request items with a bloom filter; sender returns items not in filter.

ACP-118 handler (`network/p2p/acp118/`) implements the cross-subnet warp signing protocol.

> **Gossip consumers:**
> - [PlatformVM](platformvm.md) uses `network/p2p/gossip` via `vms/platformvm/network/` to gossip pending transactions (push + pull gossip). The PlatformVM registers a gossip handler under `p2p.TxGossipHandlerID`.
> - [SAEVM (vms.md)](vms.md) uses `network/p2p/gossip` via `vms/saevm/txgossip/` for Ethereum transaction propagation. A `txgossip.Set` acts as the gossipable mempool.
>
> **ACP-118 / Warp signing:** The PlatformVM registers an `acp118.Handler` (backed by a `signatureRequestVerifier`) so that peers can request BLS signatures over Warp messages. This is how cross-subnet Warp messages collect the required validator signatures. See [platformvm.md](platformvm.md).
