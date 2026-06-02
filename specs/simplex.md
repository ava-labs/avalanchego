# Simplex BFT Consensus Integration

Simplex is a deterministic, epoch/round-based Byzantine Fault Tolerant (BFT) consensus protocol, integrated into AvalancheGo as an alternative to the probabilistic Snowman engine (see [consensus.md](./consensus.md)). The protocol logic itself lives in the external `github.com/ava-labs/simplex` library; the `simplex/` package in this repo is the **adapter layer** that plugs that library into AvalancheGo's VM framework, networking stack, BLS crypto, codec, and storage. Unlike Snowman — where a leaderless network repeatedly samples peers until a block crosses a confidence threshold (probabilistic finality) — Simplex elects a per-round leader who proposes a block, collects a BFT quorum (`> (n+f)/2`) of BLS signatures into a *notarization*, then a second quorum into a *finalization certificate*. Once a finalization certificate exists, the block is **deterministically and irreversibly final** with no reorg possibility. This makes Simplex suitable for permissioned/known-validator subnets that need fast deterministic finality.

---

## 1. Responsibilities & scope

The `simplex/` adapter package is responsible for:

- Implementing the abstract interfaces the external `simplex` library requires (`Block`, `BlockBuilder`, `Storage`, `Communication`, `Signer`, `SignatureVerifier`, `QuorumCertificate`, `SignatureAggregator`, `BlockDeserializer`, `QCDeserializer`).
- Wrapping an AvalancheGo `snowman.Block` / `block.ChainVM` so Simplex can build, verify, accept, and reject application blocks.
- Translating Simplex protocol messages to/from AvalancheGo's `p2p.Simplex` wire protobuf and routing them through the `sender`/`message` networking layer.
- Producing and verifying **BLS multi-signatures** (aggregate signatures) that back quorum certificates.
- Persisting finalizations + blacklists to a key/value DB and exposing the chain to the library as `Storage`.
- Presenting a `common.Engine` (`Engine`) so the chain handler can drive it like any other consensus engine.

Out of scope (handled by the upstream library, not this package): the actual round state machine, leader rotation, timeout/failover logic, write-ahead-log recovery, replication state, and the `Quorum(n)` math. This spec documents the adapter and references library types where they define the contract.

---

## 2. Package / file layout

Adapter package (this repo): `simplex/`

| File | Responsibility |
|------|----------------|
| `simplex/engine.go` | `Engine` — the `common.Engine` wrapper; wires the `simplex.Epoch`, ticks time, routes `p2p.Simplex` messages. |
| `simplex/config.go` | `Config` + `SimplexChainContext` — all dependencies the engine needs. |
| `simplex/block.go` | `Block` (impl of `simplex.Block`/`VerifiedBlock`), `blockDeserializer`, `blockTracker` (accept/reject of inner VM blocks). |
| `simplex/block_builder.go` | `BlockBuilder` — drives `vm.BuildBlock` with backoff, used by the leader. |
| `simplex/bls.go` | `BLSSigner`, `BLSVerifier`, message-to-sign encoding. |
| `simplex/qc.go` | `QC` (quorum certificate), `QCDeserializer`, `SignatureAggregator`, `Quorum` usage, bitset of signers. |
| `simplex/comm.go` | `Comm` — `simplex.Communication`; broadcast/send via the network sender. |
| `simplex/messages.go` | Simplex → `p2p.Simplex` outbound conversions. |
| `simplex/inbound.go` | `p2p.Simplex` → Simplex inbound conversions. |
| `simplex/storage.go` | `Storage` — finalization/blacklist persistence keyed by sequence. |
| `simplex/codec.go` | `Codec` (linearcodec) used to encode the signed payload envelope. |
| `simplex/{block,qc,storage}.canoto.go` | Canoto-generated (DO NOT EDIT) marshalers for the on-wire/at-rest structs. |

Subnet-level parameters (separate package): `snow/consensus/simplex/parameters.go` — `Parameters`, `ValidatorInfo`.

External protocol library: `github.com/ava-labs/simplex` (vendored via Go modules; e.g. `epoch.go`, `api.go`, `metadata.go`, `blacklist.go`, `wal/`). Wire protobuf: `proto/p2p/p2p.proto` (`message Simplex` and friends), generated to `proto/pb/p2p/p2p.pb.go`.

---

## 3. Core types & interfaces

### 3.1 Engine

`Engine` implements `common.Engine` but **NoOps every Snowman/bootstrap handler** (gets, queries, chits, ancestors, etc.), because Simplex does not use the pull/push gossip model. Only `Simplex(...)` (the dedicated message handler), `Start`, `Shutdown`, and `HealthCheck` do real work.

```go
// simplex/engine.go:33
type Engine struct {
    common.AllGetsServer        // all NoOp
    common.QueryHandler         // NoOp ...
    common.AppHandler           // delegated to the VM
    validators.Connector        // delegated to the VM
    vm block.ChainVM

    epoch              *simplex.Epoch   // the actual protocol state machine
    blockDeserializer  *blockDeserializer
    quorumDeserializer *QCDeserializer
    tickInterval       time.Duration
    shutdown           chan struct{}
}
```

- `NewEngine(ctx, *Config) (*Engine, error)` — `simplex/engine.go:62`. Builds BLS auth, comm, block tracker, storage, block builder, deserializers, then constructs a `simplex.EpochConfig` (`engine.go:127`) and calls `simplex.NewEpoch`.
- `Start` — `simplex/engine.go:175`: calls `epoch.Start()` then launches `go e.tick()`.
- `tick` — `simplex/engine.go:197`: a `time.Ticker` that calls `epoch.AdvanceTime(t)`; the protocol's timeouts/failover are time-driven, so the adapter must feed it wall-clock ticks. Interval is `min(MaxNetworkDelay, MaxRebroadcastWait)/10` (`getTickInterval`, `engine.go:191`).
- `Simplex(ctx, nodeID, *p2p.Simplex)` — `simplex/engine.go:211`: converts the protobuf to a `*simplex.Message` (`p2pToSimplexMessage`, `engine.go:221`) and hands it to `epoch.HandleMessage(msg, nodeID[:])`.
- `Gossip`/`Notify` are deliberate no-ops (`engine.go:253`, `:259`) — Simplex manages its own messaging and listens to VM events directly.

### 3.2 Block (`simplex.Block` + `simplex.VerifiedBlock`)

```go
// simplex/block.go:34
type Block struct {
    digest       simplex.Digest          // 32-byte hash of Bytes()
    metadata     simplex.ProtocolMetadata // Version, Epoch, Round, Seq, Prev
    vmBlock      snowman.Block           // the inner application block
    blockTracker *blockTracker
    blacklist    simplex.Blacklist
}
```

- Serialized form is the Canoto struct `canotoSimplexBlock{ Metadata, InnerBlock, Blacklist }` (`block.go:64`). `Bytes()` (`block.go:81`) marshals `metadata.Bytes()` ++ `vmBlock.Bytes()` ++ `blacklist.Bytes()`.
- `BlockHeader()` (`block.go:73`) returns `{ProtocolMetadata, Digest}` — the succinct collision-free reference that votes/QCs sign over.
- `Verify(ctx)` (`block.go:96`): rejects genesis (`Seq == 0`), checks the inner block's `Parent()` matches the `Prev` digest's tracked block (`verifyParentMatchesPrevBlock`, `block.go:115`), then `blockTracker.verifyAndTrackBlock`.

### 3.3 blockTracker — inner-block accept/reject

`blockTracker` (`simplex/block.go:164`) bridges Simplex's digest world to the VM's `snowman.Block` accept/reject world using a `utils/tree.Tree`:

- `verifyAndTrackBlock` (`block.go:200`): verifies the inner `vmBlock`, sets it as the VM preference (`vm.SetPreference`), records digest→block and adds to the tree. Idempotent if already verified.
- `indexBlock(ctx, digest)` (`block.go:225`): on finalization, calls `tree.Accept` on the chosen inner block, which **rejects all competing siblings**, and prunes tracked digests with lower `Seq`.

### 3.4 BLS signing & verification (`simplex.Signer` / `SignatureVerifier`)

```go
// simplex/bls.go:30
type BLSSigner struct {
    chainID   ids.ID
    networkID uint32
    signBLS   SignFunc // SW or HW BLS signer
}
// simplex/bls.go:37
type BLSVerifier struct {
    nodeID2PK              map[ids.NodeID]*bls.PublicKey
    networkID              uint32
    chainID                ids.ID
    canonicalNodeIDs       []ids.NodeID         // sorted -> stable bitset indices
    canonicalNodeIDIndices map[ids.NodeID]int
}
```

- `NewBLSAuth(*Config)` (`bls.go:46`) builds both from `Params.InitialValidators` (compressed BLS pubkeys). NodeIDs are `utils.Sort`-ed to give a **canonical ordering** that the QC bitset relies on.
- Every signature is over a domain-separated envelope `encodedSimplexSignedPayload{NetworkID, ChainID, Message}` (`bls.go:76`) encoded with `Codec` — this binds signatures to a specific network+chain to prevent cross-chain replay.
- `Verify` (`bls.go:91`) looks up the signer's pubkey and checks `bls.Verify` over the re-encoded envelope.

### 3.5 Quorum certificate & aggregation

```go
// simplex/qc.go:35
type QC struct {
    verifier *BLSVerifier
    sig      *bls.Signature   // aggregated BLS signature
    signers  []ids.NodeID
}
// on-wire: canotoQC{ Sig [96]byte (bls.SignatureLen), Signers []byte (bitset) }  (qc.go:42)
```

- `SignatureAggregator.Aggregate([]Signature)` (`qc.go:162`): requires `>= Quorum(n)` signatures, truncates to exactly the quorum size, validates each signer is in the membership set, parses each BLS sig, and `bls.AggregateSignatures` into one. Produces a `QC`.
- `QC.Verify(msg)` (`qc.go:60`): asserts exactly `Quorum(n)` signers, rejects duplicates, `bls.AggregatePublicKeys` for the signer set, and verifies the single aggregate signature against the domain-separated envelope — this is what makes a QC cheap to verify (one pairing check, not `n`).
- `Quorum(n)` (defined in the library `epoch.go:3435`): `f = (n-1)/3; return (n+f)/2 + 1`, i.e. a `> 2/3` BFT supermajority. `SignatureAggregator.IsQuorum` (`qc.go:205`) currently does **one node = one vote** (PoA); PoS weighting is a noted future extension.
- Signers are serialized as a `set.Bits` **bitset** over the canonical NodeID order (`createSignersBitSet`, `qc.go:115`) and reconstructed via `signersFromBytes`/`filterNodes` (`qc.go:220`, `:237`), with a strict round-trip equality check to reject malformed bitsets.

### 3.6 Communication (`simplex.Communication`)

```go
// simplex/comm.go:27
type Comm struct {
    subnetID, chainID ids.ID
    broadcastNodes    set.Set[ids.NodeID] // all validators except self
    allNodes          []simplex.NodeID
    sender            sender.ExternalSender
    msgBuilder        message.OutboundMsgBuilder
}
```

- `Send` / `Broadcast` (`comm.go:81`, `:102`) convert a `*simplex.Message` to a `p2p.Simplex` (`simplexMessageToOutboundMessage`, `comm.go:112`), wrap via `msgBuilder.SimplexMessage`, and emit through `sender.Send` with `subnets.NoOpAllower`.
- `NewComm` (`comm.go:41`) **requires this node to be in the validator set** — returns `errNodeNotFound` otherwise.

### 3.7 Storage (`simplex.Storage`)

```go
// simplex/storage.go:41
type Storage struct {
    numBlocks         atomic.Uint64 // == chain height
    db                database.KeyValueReaderWriter
    genesisBlock      *Block
    lastIndexedDigest simplex.Digest
    deserializer      *QCDeserializer
    blockTracker      *blockTracker
    vm                block.ChainVM
}
```

- The **inner blocks live in the VM**; Storage only persists the per-seq **finalization** (key `"f"+seq`) and **blacklist** (key `"b"+seq`) as Canoto blobs (`finalizationKey`/`blacklistKey`, `storage.go:195`/`:202`).
- `Index` (`storage.go:145`) is the finalization commit path: validates `Seq == numBlocks`, `Prev == lastIndexedDigest`, `Digest == finalization.Digest`, non-nil QC; persists finalization+blacklist; calls `blockTracker.indexBlock` (accept inner block + reject siblings); then bumps height and `lastIndexedDigest`. These invariant checks guarantee a single, gap-free, hash-linked finalized chain.
- `Retrieve(seq)` (`storage.go:109`) reconstructs a `Block` from the VM block at that height plus the stored finalization+blacklist; `seq==0` returns the genesis with an empty finalization.

### 3.8 Config & Parameters

```go
// simplex/config.go:20
type Config struct {
    Ctx                SimplexChainContext // NodeID, ChainID, SubnetID, NetworkID
    Log                logging.Logger
    Sender             sender.ExternalSender
    OutboundMsgBuilder message.OutboundMsgBuilder
    VM                 block.ChainVM
    DB                 database.KeyValueReaderWriter
    WAL                simplex.WriteAheadLog       // crash recovery
    SignBLS            SignFunc
    Params             *simplexparams.Parameters
}

// snow/consensus/simplex/parameters.go:24
type Parameters struct {
    MaxNetworkDelay    time.Duration   // default 5s
    MaxRebroadcastWait time.Duration   // default 5s
    InitialValidators  []ValidatorInfo // NodeID + compressed BLS PublicKey
}
```

---

## 4. Protocol / lifecycle flow

A round proceeds (leader = deterministic function of round over the validator set, handled by the library):

```
                  ┌──────────────────────── one Simplex round (per Epoch) ────────────────────────┐
 leader:          │ BlockBuilder.BuildBlock(metadata, blacklist)                                   │
                  │   └─ vm.WaitForEvent==PendingTxs ─▶ vm.BuildBlock ─▶ newBlock ─▶ Verify         │
                  │   └─ Broadcast BlockProposal (block + leader's Vote)                            │
                  ▼                                                                                 │
 all validators:  receive BlockProposal ─▶ Block.Verify (parent/prev check, vm.Verify,             │
                  set preference) ─▶ broadcast Vote (BLS sig over BlockHeader envelope)             │
                  ▼                                                                                 │
 NOTARIZATION:    collect Quorum(n) Votes ─▶ SignatureAggregator.Aggregate ─▶ QC                   │
                  ─▶ broadcast Notarization {BlockHeader, QC}                                       │
                  ▼                                                                                 │
 FINALIZE VOTE:   on notarization, broadcast FinalizeVote (BLS sig over the finalization)          │
                  ▼                                                                                 │
 FINALIZATION:    collect Quorum(n) FinalizeVotes ─▶ aggregate ─▶ Finalization {BlockHeader, QC}   │
                  ─▶ Storage.Index(block, finalization)  ◀── DETERMINISTIC, IRREVERSIBLE FINALITY   │
                  │       └─ blockTracker.indexBlock ─▶ tree.Accept(inner) + reject siblings        │
                  └────────────────────────────────────────────────────────────────────────────────┘

 If the leader is silent/faulty: validators broadcast EmptyVote ─▶ Quorum ─▶ EmptyNotarization
   (round advances with an "empty block", no Seq consumed) — this is the liveness/failover path,
   driven by Engine.tick ─▶ epoch.AdvanceTime crossing MaxProposalWait / MaxRebroadcastWait.

 Lagging node: sends ReplicationRequest{seqs, latestRound};
   peer answers ReplicationResponse{QuorumRound...} carrying block+notarization/finalization to catch up.
```

Message routing both directions: inbound `p2p.Simplex` → `Engine.Simplex` → `p2pToSimplexMessage` (`engine.go:221`, dispatch table in `inbound.go`) → `epoch.HandleMessage`; outbound `*simplex.Message` from the epoch → `Comm.Send/Broadcast` → `simplexMessageToOutboundMessage` (`messages.go`) → `p2p.Simplex` on the wire.

---

## 5. Component boundaries & relationships

| Boundary | Upstream (calls in) | Interface | Downstream (calls out) |
|----------|--------------------|-----------|------------------------|
| **Chain handler ↔ Engine** | Snow networking handler routes `p2p.Simplex` and lifecycle calls | `common.Engine` (`engine.go:26`) | drives `simplex.Epoch` |
| **Engine ↔ protocol lib** | `Engine` constructs & feeds `simplex.Epoch` | `simplex.EpochConfig` (`engine.go:127`); `epoch.{Start,Stop,HandleMessage,AdvanceTime}` | calls back into all adapter impls below |
| **lib ↔ application** | library asks for blocks/persistence | `BlockBuilder`, `Storage`, `Block`, `BlockDeserializer` | `block.ChainVM` (`BuildBlock`, `Verify`, `SetPreference`, `LastAccepted`, `GetBlock`, `WaitForEvent`) — see [vm-framework.md](./vm-framework.md) |
| **lib ↔ crypto** | library signs/verifies/aggregates | `Signer`, `SignatureVerifier`, `SignatureAggregator`, `QCDeserializer` | `utils/crypto/bls` (`Sign`, `Verify`, `AggregateSignatures`, `AggregatePublicKeys`) — see [primitives.md](./primitives.md) |
| **lib ↔ network** | library sends/broadcasts | `simplex.Communication` (`comm.go`) | `sender.ExternalSender` + `message.OutboundMsgBuilder` — see [networking.md](./networking.md) |
| **lib ↔ persistence** | library indexes/retrieves | `simplex.Storage` (`storage.go`) | `database.KeyValueReaderWriter` + the VM — see [database.md](./database.md) |
| **lib ↔ recovery** | library replays on restart | `simplex.WriteAheadLog` | provided in `Config.WAL` (library `wal/`) |
| **AppHandler / Connector** | network app messages & peer connect events | embedded in `Engine` | delegated straight to `config.VM` (`engine.go:161`) |

Key contrast with [consensus.md](./consensus.md) (Snowman): Snowman's engine issues `PushQuery`/`PullQuery`/`Chits` and bootstraps via `Get`/`Ancestors`; the Simplex `Engine` NoOps all of those and instead carries a single dedicated `Simplex` message type plus an internal replication subprotocol. Snowman finality is probabilistic (confidence after repeated sampling); Simplex finality is a cryptographic certificate.

---

## 6. Key behaviors, invariants & BFT assumptions

- **Safety quorum:** `Quorum(n) = (n+f)/2 + 1` with `f = (n-1)/3`. With at most `f` Byzantine validators, two conflicting blocks can never both gather a quorum at the same round (the two quorums' honest intersection forbids it) ⇒ no two finalized blocks at the same seq. Requires `n >= 3f+1`.
- **Liveness:** progress under partial synchrony — once the network delay is below `MaxNetworkDelay`, honest leaders get blocks notarized; faulty/slow leaders are skipped via `EmptyNotarization` after `MaxProposalWait`/`MaxRebroadcastWait` elapse (fed by `Engine.tick`).
- **Domain separation:** every signed message is wrapped with `{NetworkID, ChainID}` (`bls.go:82`), preventing signature reuse across chains/networks.
- **Canonical signer indexing:** QC bitsets index into a `utils.Sort`-ed validator list (`bls.go:135`); `signersFromBytes` enforces an exact byte round-trip (`qc.go:222`) so a malformed/oversized bitset is rejected.
- **Aggregate verification:** a QC is verified with a single `bls.Verify` over an aggregated public key — O(1) pairings regardless of signer count, but each individual vote was verified on receipt before aggregation.
- **Inner-block linkage invariant:** `Block.Verify` requires `vmBlock.Parent() == trackedBlock(metadata.Prev).vmBlock.ID()` (`block.go:115`), tying Simplex's digest chain to the VM's ID chain.
- **Storage commit invariants** (`storage.go:145`): seq must equal current height, `Prev` must equal `lastIndexedDigest`, header digest must equal the finalization digest, QC must be non-nil — enforcing a gap-free, hash-linked, single finalized chain.
- **Genesis:** `Seq==0`, has no finalization and is never verified (`block.go:98`, `storage.go:111`); blacklist initialized to `len(InitialValidators)` (`storage.go:220`).
- **Idempotency:** re-verifying an already-tracked block is a no-op (`block.go:205`); `Shutdown` is guarded by `sync.Once` (`engine.go:273`).
- **Edge cases:** genesis-finalization replication responses with a nil QC are dropped (`messages.go:233`); unknown/nil p2p fields produce a debug log and a dropped message rather than an error (`engine.go:213`); `BuildBlock` uses exponential backoff `10ms → 5s` and only builds after a `common.PendingTxs` VM event (`block_builder.go`).
- **Blacklist:** carried inside every block and persisted per-seq; the library uses it to exclude misbehaving/inactive validators from participation accounting (library `blacklist.go`).

---

## 7. Configuration / parameters

- **Subnet config** (`snow/consensus/simplex/parameters.go`): a chain opts into Simplex when its subnet config provides non-nil `SimplexParameters` (checked in `chains/manager.go:844`, `:1269`). `Parameters.Verify` requires positive delays and a non-empty `InitialValidators` list (`parameters.go:35`). Defaults: `MaxNetworkDelay = MaxRebroadcastWait = 5s` (`parameters.go:30`).
- **`ValidatorInfo`** = `{NodeID, PublicKey(compressed BLS)}` — the static membership set for the epoch (PoA today; PoS noted as future).
- **Tick interval** = `min(MaxNetworkDelay, MaxRebroadcastWait)/10` (`engine.go:191`).
- **`EpochConfig`** (built in `engine.go:127`): `MaxProposalWait`, `MaxRebroadcastWait`, `ID = NodeID`, the deserializers/aggregator/signer/verifier/comm/storage/WAL/block-builder, starting `Epoch` from the last block, `StartTime = now`, `ReplicationEnabled = true`.
- **`CodecVersion`** = `warp.CodecVersion + 1` with a `linearcodec` (`codec.go:14`); used only for the signed-payload envelope.
- **Canoto** is used for the byte-stable serialization of `Block`, `QC`, and `Finalization`/`Blacklist` (the `*.canoto.go` generated files; `//go:generate go tool canoto` directives in `block.go`, `qc.go`, `storage.go`). `ProtocolMetadata`/`BlockHeader` use the library's own fixed-layout big-endian encoding (`metadata.go`).

> Integration status note: as of this revision the `Engine` is fully implemented but the wiring that instantiates it from `chains/manager.go` based on `SimplexParameters` is in progress — the manager currently only logs that a chain "is configured with simplex". Confirm against `chains/manager.go` (see [chains.md](./chains.md)) before assuming a chain runs Simplex in production.

---

## 8. Cross-references

- [overview.md](./overview.md) — where Simplex sits among consensus options.
- [consensus.md](./consensus.md) — Snowman/Avalanche/Snowball (probabilistic consensus; contrast only).
- [vm-framework.md](./vm-framework.md) — `block.ChainVM`, `snowman.Block`, `WaitForEvent`.
- [networking.md](./networking.md) — `sender.ExternalSender`, `message.OutboundMsgBuilder`, `p2p.Simplex`.
- [primitives.md](./primitives.md) — BLS signing/aggregation, `ids.NodeID`, `set.Bits`, codecs, Canoto.
- [database.md](./database.md) — `database.KeyValueReaderWriter` used by `Storage`.
- [chains.md](./chains.md) — chain manager, subnet config, engine selection.
- [node.md](./node.md) — node-level configuration of subnets/validators.
