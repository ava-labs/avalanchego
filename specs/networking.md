# Networking — P2P Transport & Message Layer

## 1. Purpose

This document specifies the **peer-to-peer (P2P) transport and message layer** of AvalancheGo: how a node discovers, authenticates, connects to, and exchanges messages with other nodes. It covers two distinct layers that are easy to conflate:

- **The `network` transport** (`network/`, `network/peer/`, `network/throttling/`, `message/`) — the low-level layer that owns TCP connections, TLS-based peer identity, the handshake, peer discovery/gossip, message framing, rate limiting, and the per-peer message queue. It speaks the *wire protocol* (`p2p.Message` protobufs) and hands inbound consensus/app messages off to the consensus router (`snow/networking/router`).
- **The `network/p2p` application SDK** — a higher-level library built *on top of* the `AppRequest`/`AppResponse`/`AppGossip` message ops, letting VMs register application protocols (gossip, ACP-118 warp-signature aggregation, state sync requests) without touching the transport. It does **not** open sockets; it routes the `App*` messages that the transport already delivered.

The boundary between transport and consensus is the `sender.ExternalSender` (outbound) and `router.ExternalHandler` (inbound) interfaces — see [§6](#6-component-boundaries--relationships). The consensus engine, `ChainRouter`, and per-chain `Handler` are documented in [consensus.md](./consensus.md).

---

## 2. Responsibilities & Scope

In scope (this layer owns):

- Establishing and tearing down authenticated TCP connections (TLS 1.3, no CA).
- Peer identity = hash of the leaf certificate public key (`ids.NodeID`).
- The application-level handshake (`Handshake` + `PeerList`), version/clock/ACP compatibility checks.
- Peer discovery: outbound dialing with exponential backoff, `PeerList` gossip, bloom-filtered `GetPeerList`.
- IP signing (TLS + BLS) and verification to authenticate claimed `IP:Port` pairs.
- Wire message framing (4-byte big-endian length prefix), protobuf (de)serialization, optional zstd compression.
- Inbound and outbound rate limiting (bytes, bandwidth, CPU, disk, message count).
- Per-peer outbound message queue and the read/write/ping goroutines.
- Ping/Pong liveness + perceived-uptime exchange.
- The `network/p2p` SDK: registering app handlers, issuing app requests, push/pull gossip, warp signature aggregation.

Out of scope (documented elsewhere): consensus engines, `ChainRouter`/`Handler`, timeout/benchlist ([consensus.md](./consensus.md)); node startup wiring ([node.md](./node.md)); warp message format internals ([platformvm.md](./platformvm.md)).

---

## 3. Package / File Layout

```
message/                          # Wire message serialization (the "what")
├── ops.go                        # Op taxonomy (Ping..Simplex), Unwrap/ToOp
├── messages.go                   # InboundMessage/OutboundMessage, msgBuilder, zstd compression
├── creator.go                    # Creator = OutboundMsgBuilder + InboundMsgBuilder
├── outbound_msg_builder.go       # Builds Handshake/Ping/Get/Put/AppRequest/... outbound
├── inbound_msg_builder.go        # Builds InboundMessage (used in tests / internal)
├── internal_msg_builder.go       # Internal (non-wire) ops: Connected/Disconnected/Notify/...
└── fields.go                     # GetDeadline and per-message field accessors

network/                          # The transport (the "how")
├── network.go                    # Network interface + impl: accept loop, dial, peer lifecycle
├── config.go                     # Config struct (all tunables)
├── ip_tracker.go                 # Bloom-filtered known-peer tracking + gossip selection
├── tracked_ip.go                 # Per-IP dial backoff state
├── dialer/dialer.go              # Outbound TCP dialer (throttled)
├── peer/
│   ├── peer.go                   # Peer: read/write/ping goroutines, handshake handlers
│   ├── config.go                 # peer.Config (shared deps for all peers)
│   ├── upgrader.go               # TLS upgrade (conn -> nodeID + cert)
│   ├── tls_config.go             # TLS 1.3 config, InsecureSkipVerify + custom cert validation
│   ├── ip_signer.go              # Signs our dynamic IP (TLS + BLS) lazily
│   ├── ip.go                     # UnsignedIP/SignedIP, Sign/Verify
│   ├── message_queue.go          # throttled + blocking per-peer outbound queues
│   ├── msg_length.go             # 4-byte length prefix read/write
│   ├── set.go                    # peer.Set: concurrent, sampleable peer collection
│   └── network.go                # peer.Network interface (callbacks into network)
└── throttling/
    ├── inbound_msg_throttler.go      # composite: buffer + bytes + bandwidth + CPU + disk
    ├── inbound_msg_byte_throttler.go # sybil-safe byte buffer (vdr + at-large allocations)
    ├── inbound_msg_buffer_throttler.go # max in-flight messages per node
    ├── bandwidth_throttler.go        # per-node token bucket (golang.org/x/time/rate)
    ├── inbound_resource_throttler.go # CPU/disk SystemThrottler
    ├── outbound_msg_throttler.go     # sybil-safe outbound byte allocation
    ├── inbound_conn_upgrade_throttler.go # rate-limit TLS upgrades per IP
    └── dial_throttler.go             # rate-limit outbound dials

network/p2p/                      # Application SDK (built on App* ops)
├── network.go                    # p2p.Network: AddHandler, NewClient, ConnectionHandler
├── router.go                     # routes App* to registered handlers by uvarint prefix
├── handler.go                    # Handler interface; NoOp/Validator/Throttler wrappers
├── client.go                     # Client: AppRequest/AppRequestAny/AppGossip
├── throttler.go                  # SlidingWindowThrottler (per-node request limit)
├── peer_tracker.go               # PeerTracker: bandwidth-aware peer selection (state sync)
├── validators.go                 # ValidatorSet/ValidatorSubset adapters over snow/validators
├── node_sampler.go               # NodeSampler interface
├── gossip/                       # Push/Pull gossip framework
│   ├── gossip.go                 # Gossiper, PushGossiper, PullGossiper, Every
│   ├── handler.go                # server side: responds to pull, ingests push
│   ├── bloom.go                  # rolling bloom filter of known gossipables
│   └── message.go                # MarshalAppRequest/Response/Gossip (sdk protobuf)
└── acp118/                       # ACP-118 warp signature request/aggregation
    ├── handler.go                # signs warp messages on request (caches signatures)
    └── aggregator.go             # SignatureAggregator: collects a BLS multi-sig
```

---

## 4. Core Types & Interfaces

### 4.1 Transport: `Network`

```go
// network/network.go:62
type Network interface {
    sender.ExternalSender       // Send(msg, SendConfig, subnetID, allower) -> sentTo
    health.Checker
    peer.Network                // Connected/Disconnected/AllowConnection/Track/Peers/KnownPeers
    StartClose()
    Dispatch() error            // accept loop; blocks until closed
    ManuallyTrack(nodeID, ip)
    PeerInfo(nodeIDs) []peer.Info
    NodeUptime() (UptimeResult, error)
}
```

`network` (`network/network.go:118`) holds the listener, dialer, two TLS upgraders (server/client), the `ipTracker`, two `peer.Set`s (`connectingPeers`, `connectedPeers`), the `trackedIPs` dial map, the outbound throttler, and the `router.ExternalHandler` it delivers inbound messages to. `NewNetwork` is at `network/network.go:176`.

### 4.2 Transport: `Peer`

```go
// network/peer/peer.go:54
type Peer struct {
    *Config
    conn          net.Conn
    cert          *staking.Certificate   // leaf cert; defines node identity
    id            ids.NodeID
    messageQueue  MessageQueue           // outbound queue
    ip            *SignedIP              // claimed in Handshake
    version       *version.Application
    trackedSubnets set.Set[ids.ID]
    gotHandshake      utils.Atomic[bool] // got their Handshake
    finishedHandshake utils.Atomic[bool] // got Handshake AND PeerList
    ...
}
```

`Start` (`network/peer/peer.go:142`) spawns **three goroutines**: `readMessages`, `writeMessages`, `sendNetworkMessages` (`numExecuting = 3`). The peer closes once all three exit.

### 4.3 Transport: identity & IP authentication

```go
// network/peer/upgrader.go:24
type Upgrader interface {
    Upgrade(net.Conn) (ids.NodeID, net.Conn, *staking.Certificate, error)
}
// network/peer/ip.go:71
type SignedIP struct {
    UnsignedIP                       // AddrPort + Timestamp
    TLSSignature      []byte
    BLSSignature      *bls.Signature
    BLSSignatureBytes []byte
}
func (ip *SignedIP) Verify(cert *staking.Certificate, maxTimestamp time.Time) error // ip.go:81
```

### 4.4 Message layer

```go
// message/messages.go:38 / :60
type InboundMessage  struct { NodeID; Op; Message fmt.Stringer; Expiration; onFinishedHandling func() }
type OutboundMessage struct { BypassThrottling bool; Op; Bytes []byte; BytesSavedCompression int }

// message/creator.go:16
type Creator interface { OutboundMsgBuilder; InboundMsgBuilder }
func NewCreator(metrics, compressionType, maxMessageTimeout) (Creator, error) // creator.go:26

type Op byte // message/ops.go:15  (the wire opcode)
func ToOp(*p2p.Message) (Op, error)        // ops.go:248
func Unwrap(*p2p.Message) (fmt.Stringer, error) // ops.go:185
```

The on-wire envelope is the protobuf `p2p.Message` (oneof). `msgBuilder.marshal` (`messages.go:114`) optionally wraps the marshaled bytes in a `CompressedZstd` field and re-marshals (recursive packing avoids a separate "is-compressed" flag). `parseInbound` (`messages.go:239`) reverses this and computes the `Expiration` from the message's deadline (clamped to `maxMessageTimeout`).

### 4.5 SDK: handler, client, router

```go
// network/p2p/handler.go:42 — server side of a VM protocol
type Handler interface {
    AppGossip(ctx, nodeID, gossipBytes)
    AppRequest(ctx, nodeID, deadline, requestBytes) ([]byte, *common.AppError)
}

// network/p2p/network.go:87
type Network struct { sender common.AppSender; router *router; connectionHandlers []ConnectionHandler }
func (n *Network) AddHandler(handlerID uint64, h Handler) error    // network.go:139
func (n *Network) NewClient(handlerID uint64, ns NodeSampler) *Client // network.go:128

// network/p2p/client.go:34
type Client struct { handlerPrefix []byte; router *router; sender common.AppSender; nodeSampler NodeSampler }
func (c *Client) AppRequest(ctx, nodeIDs, bytes, onResponse AppResponseCallback) error // client.go:62
func (c *Client) AppRequestAny(ctx, bytes, onResponse) error                            // client.go:46
func (c *Client) AppGossip(ctx, SendConfig, bytes) error                                // client.go:116
```

Reserved handler IDs (`network/p2p/handler.go:24`): `TxGossipHandlerID=0`, `AtomicTxGossipHandlerID=1`, `SignatureRequestHandlerID=2` (ACP-118), `FirewoodProofHandlerID=3`.

---

## 5. Lifecycle & Data Flow

### 5.1 Connection establishment + handshake

```
Outbound (dialer)                         Inbound (listener)
─────────────────                         ──────────────────
network.dial(nodeID, trackedIP)           network.Dispatch() Accept loop
 └ exponential backoff (tracked_ip.go)     └ inboundConnUpgradeThrottler.ShouldUpgrade(ip)
 └ dialer.Dial(ip)  ──TCP──►                  (rate-limit upgrades per IP)
        │                                      │
        ▼                                      ▼
 network.upgrade(conn, clientUpgrader)    network.upgrade(conn, serverUpgrader, isIngress=true)
   set ReadHandshakeTimeout deadline
   Upgrader.Upgrade(conn): TLS 1.3 handshake (RequireAnyClientCert, InsecureSkipVerify,
        VerifyConnection=ValidateCertificate) → leaf cert → nodeID = ids.NodeIDFromCert(cert)
   reject if nodeID == self, !AllowConnection, closing, or already connecting/connected
        │
        ▼
   peer.Start(...) ── adds peer to connectingPeers; spawns 3 goroutines

writeMessages (peer.go:472):  send Handshake FIRST (version, time, signed IP, tracked subnets,
                              supported/objected ACPs, bloom filter + salt, requestAllSubnets)
readMessages  (peer.go:342):  on inbound Handshake → handleHandshake (peer.go:814):
                              - networkID match, |clockSkew| <= MaxClockDifference
                              - parse version; record peer version
                              - <= maxNumTrackedSubnets (16); ACPs not overlapping
                              - verify SignedIP against leaf cert (TLS sig) + BLS proof-of-possession
                              - shouldDisconnect()? (version incompatible / bad BLS) → close
                              - gotHandshake=true; reply with PeerList (bypassThrottling=true)
                              on inbound PeerList → handlePeerList (peer.go:1126):
                              - if !finishedHandshake: Network.Connected(); finishedHandshake=true;
                                close(onFinishHandshake)
                              - Network.Track(discoveredIPs)  → may dial new peers
```

A connection that fails any check calls `StartClose` and the peer's three goroutines unwind; `peer.close` (`peer.go:327`) calls `Network.Disconnected(id)` exactly once (when the last goroutine exits).

### 5.2 Sending and receiving a wire message

```
OUTBOUND
 consensus/sender (or peer internal) → network.Send (network.go:321)
   getPeers(named) + samplePeers(random)            ─ pick recipients
   for each peer: Peer.Send(msg)  → MessageQueue.Push (message_queue.go:90)
        outboundMsgThrottler.Acquire(msg, nodeID)    ─ sybil-safe byte allocation; drop if over
        enqueue (unbounded deque), Signal writer
 writeMessages goroutine: PopNow/Pop → writeMessage (peer.go:559)
        4-byte BE length prefix (msg_length.go:writeMsgLen, max DefaultMaxMessageSize=2 MiB)
        net.Buffers{lenBytes, msgBytes} → conn ; throttler.Release on pop

INBOUND
 readMessages goroutine (peer.go:342):
   read 4-byte length → readMsgLen (reject > 2 MiB)
   InboundMsgThrottler.Acquire(ctx, msgLen, nodeID)  ─ blocks on buffer+bandwidth+CPU+disk+bytes
   io.ReadFull(msgBytes) ; ResourceTracker.StartProcessing
   Creator.Parse(bytes, nodeID, onFinishedHandling)  ─ protobuf + optional zstd decompress
        (malformed → metric++, onFinishedHandling(), CONTINUE; connection NOT dropped)
   Peer.handle(msg) (peer.go:715):
        Ping/Pong/Handshake/GetPeerList/PeerList  → handled in-package (transport)
        else, if !finishedHandshake               → drop
        else                                      → Router.HandleInbound(ctx, msg)  ──► consensus
```

### 5.3 SDK app message flow

```
VM code → p2p.Client.AppRequest(nodeIDs, bytes, cb)
   bytes := PrefixMessage(uvarint(handlerID), bytes)              (client.go:79)
   router.requestID += 2 (SDK uses ODD request IDs)              (router.go:84)
   common.AppSender.SendAppRequest(...)  → message.Creator.AppRequest → transport.Send
        ... over the wire as AppRequestOp ...
remote transport delivers AppRequest → Router.HandleInbound → ChainHandler → engine.AppRequest
        → p2p.Network.AppRequest (network.go:94) → router.AppRequest (router.go:111)
        → ParseMessage strips uvarint prefix → responder.AppRequest → Handler.AppRequest
        → SendAppResponse / SendAppError back
local: transport delivers AppResponse → p2p.Network.AppResponse → router.AppResponse
        → clearAppRequest(requestID) → pending.callback(ctx, nodeID, resp, nil)
```

---

## 6. Component Boundaries & Relationships

| Boundary | Upstream (caller) | Downstream (callee) | Interface |
|---|---|---|---|
| Consensus → transport (outbound) | `snow/networking/sender` | `network.network.Send` | `sender.ExternalSender` (`snow/networking/sender/external_sender.go:16`) |
| Transport → consensus (inbound) | `peer.handle` | `ChainRouter` | `router.ExternalHandler` (`snow/networking/router/inbound_handler.go:31`): `HandleInbound`, `Connected`, `Disconnected` |
| Network ↔ Peer | `network` | `peer.Peer` | `peer.Network` (`network/peer/network.go`): `Connected/Disconnected/AllowConnection/Track/Peers/KnownPeers` |
| Peer → message codec | `peer.readMessages`/`writeMessages` | `message.Creator` | `Parse` / `Handshake`/`Ping`/... builders |
| Engine ↔ SDK | `snow` engine `AppHandler` calls | `p2p.Network` | `common.AppHandler` (p2p.Network implements `AppRequest/AppResponse/AppRequestFailed/AppGossip`, `network.go:94`) |
| SDK → outbound | `p2p.Client` | `common.AppSender` | `SendAppRequest/SendAppResponse/SendAppError/SendAppGossip` (implemented by `snow/networking/sender`, which calls `ExternalSender.Send`) |
| SDK → VM protocol | `p2p.router` | VM `Handler` | `p2p.Handler` (`network/p2p/handler.go:42`) |

The **critical network↔consensus boundary**: the transport never interprets consensus/app payloads. In `Peer.handle` (`peer.go:715`), only the five handshake-family ops (`Ping`, `Pong`, `Handshake`, `GetPeerList`, `PeerList`) are consumed inside `network`. Everything else is forwarded verbatim via `p.Router.HandleInbound(context.Background(), msg)` (`peer.go:749`) — but only after `finishedHandshake` is set. Outbound, consensus produces an `*message.OutboundMessage` and calls `Network.Send`, which selects recipient peers and enqueues. The `p2p` SDK sits a layer above: it is itself an `AppHandler` plugged into a chain's engine, so its messages travel the same `App*` ops over the same transport.

---

## 7. Key Behaviors, Invariants, Edge Cases, Concurrency, Security

### Identity & TLS (no CA)
- All connections are TLS 1.3 with `ClientAuth: RequireAnyClientCert` and `InsecureSkipVerify: true` — **CA verification is intentionally skipped** (`tls_config.go:30`). Instead, `VerifyConnection: ValidateCertificate` (`tls_config.go:48`) enforces that the leaf cert public key is well-formed (ECDSA must be P-256; RSA must be well-formed). The **node identity is `ids.NodeIDFromCert(leaf)`** (`upgrader.go:78`) — the hash of the leaf certificate. Quantstamp audited this as safe.
- A node drops any connection whose negotiated `nodeID == MyNodeID` (`network.go:1053`).

### IP authentication
- A node signs its own `(IP, Port, Timestamp)` with **both** the TLS key and the BLS key (`ip.go:35`, `IPSigner.GetSignedIP` `ip_signer.go:47`, signed lazily and memoized until the dynamic IP changes). The `Timestamp` defeats replay and lets peers track the freshest IP.
- On receiving a claimed IP (in `Handshake` or `PeerList`), the recipient verifies the TLS signature against the cert and rejects timestamps beyond `now + MaxClockDifference` (`ip.go:81`). BLS proof-of-possession is verified for registered validators (`peer.go:696`, `shouldDisconnect`), but only once per validation period (`txIDOfVerifiedBLSKey`) to avoid signature work on the P-chain accept path.
- `network.track` (`network.go:698`) optimistically skips signature verification for IPs it doesn't need (`ipTracker.ShouldVerifyIP`) — a deliberate, significant perf optimization.

### Handshake correctness
- The `Handshake` must be the **first** message written (`writeMessages`, `peer.go:502`); a second `Handshake` triggers close (`peer.go:815`).
- Rejection causes: wrong `networkID`, clock skew > `MaxClockDifference`, incompatible version, > 16 tracked subnets, overlapping supported/objected ACPs, oversized bloom salt (> 32 bytes), invalid IP/port, bad TLS/BLS signature.
- Handshake completes only after **both** a `Handshake` and a `PeerList` are received (`finishedHandshake`); `Network.Connected` (→ `router.Connected`) fires exactly once at that point (`peer.go:1132`).

### Message framing & robustness
- Every wire message is `[4-byte BE length][protobuf bytes]`. Max size `DefaultMaxMessageSize = 2 MiB` (`utils/constants/networking.go`). Oversized lengths are rejected before reading the body.
- **Malformed messages do not drop the connection** — the reader logs, increments `NumFailedToParse`, releases the throttler slot, and continues (`peer.go:446`). (Malformed *handshake-family* messages, by contrast, do close the peer.)
- Compression: zstd only; `BytesSavedCompression` is tracked for metrics. Decompression bounds the output via the zstd compressor initialized with the max message size.

### Ping/Pong & liveness
- `sendNetworkMessages` (`peer.go:605`) sends a `Ping` every `PingFrequency` carrying our perceived uptime of the peer (0–100). The peer replies `Pong`; an unsolicited `Pong` or `Ping` uptime > 100 closes the connection (`peer.go:752`, `peer.go:796`). RTT is recorded from `lastPingSent`.
- Read deadlines use `PongTimeout` (`nextTimeout`, `peer.go:1197`): if no bytes arrive within the window the connection times out.
- On each ping tick, the peer re-checks `AllowConnection` and `shouldDisconnect` to react to validator-set changes without a P-chain callback.

### Peer discovery / PeerList gossip
- `ipTracker` (`network/ip_tracker.go`) maintains a bloom filter of known peers (params: `targetFalsePositiveProbability = .001`, reconstructed when entries exceed expected or on `PeerListBloomResetFreq`, with a fresh 32-byte salt). At most `maxIPEntriesPerNode = 2` entries per node thwart bloom-flooding griefing.
- `runTimers` (`network.go:1220`) periodically `pullGossipPeerLists` — sends `GetPeerList` (with our bloom + salt) to one random Primary Network validator (`peer.StartSendGetPeerList`). The responder returns only peers **not** in the requester's bloom filter that are connected and validator/bootstrapper-qualified (`network.Peers`, `network.go:560`). Eventual goal: `gossipable ⊆ trackable` so steady-state gossip traffic → 0 (see `network/README.md`).
- Outbound dial uses randomized exponential backoff between `InitialReconnectDelay` and `MaxReconnectDelay` (`tracked_ip.go:46`); the dial goroutine self-cleans when the node leaves the desired set (`WantsConnection`), preventing leaks (`network.go:900`).

### Throttling (resource isolation)
- **Inbound** (`inbound_msg_throttler.go:151`, sybil-safe): `Acquire` must pass *all* of — buffer slots (max in-flight msgs/node), per-node bandwidth token bucket (1 token = 1 byte), CPU usage, disk usage, then byte buffer (validator allocation sized by stake weight + an at-large allocation). The returned `ReleaseFunc` must be called exactly once or the throttler leaks (enforced by the `onFinishedHandling` invariant in `readMessages`).
- **Outbound** (`outbound_msg_throttler.go:65`, sybil-safe): bytes are drawn first from a per-node at-large allocation, then from a stake-weighted validator allocation; if insufficient, the message is **dropped** (`Acquire` returns false). `BypassThrottling` messages (e.g. handshake `PeerList`) skip this entirely.
- **Connection-level**: `inboundConnUpgradeThrottler` rate-limits TLS upgrades per source IP; `dialer` is rate-limited; both prevent goroutine spin-up from the accept loop.

### Concurrency & locking
- Lock order invariant (`network.go:110`): `peersLock` before `manuallyTrackedIDsLock`.
- Per peer: exactly one reader, one writer, one ping goroutine. The reader is the *only* caller of `InboundMsgThrottler.Acquire` for a given nodeID (the throttler requires ≤1 concurrent blocking Acquire per node). `peer.Set` (`peer/set.go`) provides concurrent indexed/sampleable access.
- The outbound queue (`throttledMessageQueue`, `message_queue.go:54`) is an unbounded deque guarded by a `sync.Cond`; on close it releases throttler bytes and fires `SendFailed` for every dropped message.

### SDK specifics
- The SDK router uses **odd-numbered request IDs incremented by 2** (`router.go:84`, `client.go:109`) so its IDs never collide with the consensus engine's even IDs sharing the same `AppSender`.
- App request/gossip payloads are prefixed with `binary.AppendUvarint(handlerID)` (`PrefixMessage`, `client.go:138`); responses are *not* prefixed (the requestID maps to the awaiting callback). Unknown handler IDs → `SendAppError(ErrUnregisteredHandler)` for requests, silent drop for gossip.
- `ValidatorHandler` (`handler.go:193`) drops messages from non-validators; `DynamicThrottlerHandler` (`handler.go:69`) and `SlidingWindowThrottler` (`throttler.go`) cap per-peer request rate (limit scales inversely with validator count, 4σ headroom).

---

## 8. Configuration / Parameters

From `network/config.go` and defaults in `utils/constants/networking.go` (wired via `config/config.go`):

| Parameter | Default | Meaning |
|---|---|---|
| `DefaultMaxMessageSize` | 2 MiB | Max framed message size |
| `PingFrequency` | `3/4 * PingPongTimeout` | Ping interval |
| `MaxClockDifference` | 1 min | Allowed handshake clock skew |
| `PeerListNumValidatorIPs` | 15 | Max IPs per `PeerList` response |
| `PeerListPullGossipFreq` | 2 s | `GetPeerList` cadence |
| `PeerListBloomResetFreq` | 1 min | Bloom rebuild + new salt |
| `InitialReconnectDelay` / `MaxReconnectDelay` | — | Dial backoff bounds |
| Inbound bandwidth `RefillRate` / `MaxBurstSize` | `MaxBurstSize ≥ MaxMessageSize` | Per-node token bucket |
| Outbound/inbound `VdrAllocSize` / `AtLargeAllocSize` / `NodeMaxAtLargeBytes` | `NodeMaxAtLargeBytes = MaxMessageSize` | Sybil-safe byte allocations |
| `RequireValidatorToConnect` / `ConnectToAllValidators` / `AllowPrivateIPs` | — | Connection policy |
| `maxNumTrackedSubnets` | 16 | Subnets a peer may advertise |
| `maxBloomSaltLen` | 32 | Max handshake/GetPeerList salt |

Compression type and `maxMessageTimeout` are passed to `message.NewCreator`.

---

## 9. Cross-References

- [overview.md](./overview.md) — system map; where networking sits.
- [consensus.md](./consensus.md) — `ChainRouter`/`Handler`, `snow/networking/{router,sender,timeout,benchlist}`; the consumer of `HandleInbound` and producer of `Send`.
- [simplex.md](./simplex.md) — the `SimplexOp` message and Simplex BFT engine.
- [node.md](./node.md) — node startup: TLS/BLS key loading, `NewNetwork` wiring, listener/dialer construction.
- [vm-framework.md](./vm-framework.md) — how VMs obtain a `p2p.Network`/`Client` and register `AppHandler`s.
- [platformvm.md](./platformvm.md) — warp message format consumed by `acp118`.
- [primitives.md](./primitives.md) — `ids.NodeID`, BLS, bloom filter, `staking.Certificate`.
- [api.md](./api.md) — `info`/`health` endpoints surfacing `PeerInfo`, `NodeUptime`, network health.
