# Consensus — Snow and Simplex

## Directory Layout

```
snow/
├── consensus/
│   ├── snowball/     # Probabilistic voting primitives (Slush, Snowflake, Snowball)
│   ├── snowman/      # Linear chain consensus (Topological)
│   ├── avalanche/    # DAG consensus
│   ├── snowstorm/    # Transaction voting (for DAG)
│   └── simplex/      # Simplex parameters
├── engine/
│   ├── common/       # Shared engine interfaces (VM, Sender, bootstrapper)
│   ├── snowman/      # Snowman engine + bootstrapper
│   └── avalanche/    # Avalanche DAG engine + bootstrapper
├── validators/       # Validator set management
├── uptime/           # Validator uptime tracking
└── networking/
    ├── handler/      # Per-chain message dispatcher
    ├── sender/       # Outbound message dispatch
    ├── router/       # Network → handler routing
    ├── timeout/      # Request timeout management
    └── benchlist/    # Misbehaving peer benching
simplex/              # Simplex BFT consensus engine
```

---

## Part 1: Snow Consensus

### 1.1 Decision State Machine

Every item (block, transaction, vertex) tracked by consensus has a lifecycle:

```go
// snow/choices/status.go
type Status uint32
const (
    Unknown    Status = iota  // Not yet seen
    Processing                // Being voted on
    Rejected                  // Lost consensus
    Accepted                  // Won consensus
)
```

The `Decidable` interface:
```go
type Decidable interface {
    ID() ids.ID
    Accept(context.Context) error
    Reject(context.Context) error
    Status() Status
}
```

### 1.2 Snowball Voting Primitives (`snow/consensus/snowball/`)

The protocol is a hierarchy of voting algorithms, each adding more state to increase safety.

#### Slush (stateless preference)
Tracks current preference, switches on any poll.

#### Snowflake (confidence counters)
Adds a confidence counter array per termination condition. Finalizes when confidence reaches beta.

```go
type binarySnowflake struct {
    binarySlush
    alphaPreference       int
    terminationConditions []terminationCondition  // [(alphaConf, beta), ...]
    confidence            []int                   // one per condition
    finalized             bool
}

func (sf *binarySnowflake) RecordPoll(count, choice int) {
    if sf.finalized { return }
    if count < sf.alphaPreference {
        sf.RecordUnsuccessfulPoll()  // resets all confidence counters
        return
    }
    if choice != sf.Preference() {
        clear(sf.confidence)  // preference changed, reset confidence
    }
    sf.binarySlush.RecordSuccessfulPoll(choice)
    for i, tc := range sf.terminationConditions {
        if count < tc.alphaConfidence {
            clear(sf.confidence[i:])
            return
        }
        sf.confidence[i]++
        if sf.confidence[i] >= tc.beta {
            sf.finalized = true
            return
        }
    }
}
```

#### Snowball (accumulated preference)
Extends Snowflake with a preference_strength counter. Preference changes only when the alternative has accumulated more successful polls.

#### Interface Hierarchy

```go
// Unary (1 choice — confirm or not)
type Unary interface {
    RecordPoll(count int)
    RecordUnsuccessfulPoll()
    Finalized() bool
    Extend(originalPreference int) Binary  // Convert to binary
    Clone() Unary
}

// Binary (2 choices)
type Binary interface {
    Preference() int
    RecordPoll(count, choice int)
    RecordUnsuccessfulPoll()
    Finalized() bool
}

// N-ary (multiple choices)
type Nnary interface {
    Add(newChoice ids.ID)
    Preference() ids.ID
    RecordPoll(count int, choice ids.ID)
    RecordUnsuccessfulPoll()
    Finalized() bool
}

// General consensus (bag-based voting)
type Consensus interface {
    Add(newChoice ids.ID)
    Preference() ids.ID
    RecordPoll(votes bag.Bag[ids.ID]) bool
    RecordUnsuccessfulPoll()
    Finalized() bool
}
```

#### Tree Structure
`Tree` (`snow/consensus/snowball/tree.go`) manages a forest of `Nnary` instances — one per distinct prefix of a choice ID — enabling efficient voting over a large choice space with shared prefixes.

#### Default Parameters
```go
var DefaultParameters = Parameters{
    K:                    20,   // validators to query per poll
    AlphaPreference:      15,   // threshold to change preference
    AlphaConfidence:      15,   // threshold to increment confidence
    Beta:                 20,   // consecutive polls to finalize
    ConcurrentRepolls:    4,
    OptimalProcessing:    10,
    MaxOutstandingItems:  256,
    MaxItemProcessingTime: 30 * time.Second,
}
```

Constraints: `k/2 < alphaPreference ≤ alphaConfidence ≤ k`, `0 < concurrentRepolls ≤ beta`.

---

### 1.3 Snowman Consensus (Linear Chain) (`snow/consensus/snowman/`)

Snowman is built on top of Snowball and operates on blocks organized in a tree rooted at genesis.

#### Block Interface
```go
type Block interface {
    snow.Decidable     // ID(), Accept(), Reject()
    Parent() ids.ID
    Verify(context.Context) error
    Bytes() []byte
    Height() uint64
    Timestamp() time.Time
}
```

#### Topological Algorithm (`topological.go`)

The `Topological` struct tracks the entire processing tree:

```go
type Topological struct {
    params snowball.Parameters
    blocks map[ids.ID]*snowmanBlock  // all processing blocks
    preferredIDs set.Set[ids.ID]     // all blocks on preferred chain
    preferredHeights map[uint64]ids.ID
    preference ids.ID                // tip of preferred chain
    lastAccepted ids.ID
}

type snowmanBlock struct {
    blk        Block
    sb         snowball.Consensus  // votes on this block's children
    children   map[ids.ID]Block
    shouldFalter bool
}
```

**Vote Application (Kahn's algorithm):**
1. Collect votes from a `bag.Bag[ids.ID]` (one vote per sampled validator).
2. Build a DAG of all blocks that received votes.
3. Walk leaves to root using topological order (Kahn): each block propagates its vote to its parent.
4. For each block, call `sb.RecordPoll()` to accumulate votes.
5. If a block's snowball finalizes, call `Accept()` on it and all its ancestors; `Reject()` on siblings.

**Preference update:** After each poll, re-walk from genesis to find the longest chain of consecutive winning children.

**Unhealthiness triggers:**
- `NumProcessing() > MaxOutstandingItems`
- Any block processing for `> MaxItemProcessingTime`

---

### 1.4 Avalanche (DAG) Consensus (`snow/consensus/avalanche/`)

Used by the X-Chain. Vertices are collections of transactions arranged in a DAG.

```go
type Vertex interface {
    choices.Decidable
    Parents() ([]Vertex, error)
    Height() (uint64, error)
    Txs(context.Context) ([]snowstorm.Tx, error)
    Bytes() []byte
}
```

Transactions within vertices use `snowstorm.Tx`:
```go
type Tx interface {
    choices.Decidable
    Dependencies() ([]ids.ID, error)
    InputIDs() (set.Set[ids.ID], error)
    Status() choices.Status
}
```

Conflict detection: two transactions conflict if they share an input ID. The DAG consensus rejects transactions once a conflicting transaction is accepted.

---

### 1.5 Engine State Machine

#### Engine States
```go
// snow/state.go
const (
    Initializing State = iota
    StateSyncing        // Syncing state summaries from peers
    Bootstrapping       // Fetching and replaying history
    NormalOp            // Live consensus operation
)
```

Transitions:
```
Initializing → StateSyncing (if VM supports it)
StateSyncing → Bootstrapping (VM sends StateSyncDone message)
Bootstrapping → NormalOp (all required blocks accepted)
```

#### Snowman Engine (`snow/engine/snowman/engine.go`)

Core state:
```go
type Engine struct {
    Config
    requestID uint32
    polls     poll.Set            // outstanding polls by requestID
    pending   map[ids.ID]snowman.Block  // waiting for parent
    blocked   *job.Scheduler           // jobs waiting on block resolution
}
```

Key flows:

**Issuing a block:**
1. Receive `Put(nodeID, requestID, blockBytes)` — parse, verify, add to consensus.
2. If parent not in consensus, add to `pending`, schedule retry when parent resolved.
3. Once parent present, call `Consensus.Add(block)`.
4. Send `PushQuery` / `PullQuery` to K sampled validators.

**Recording votes:**
1. Receive `Chits(nodeID, requestID, votes)` — match to poll by requestID.
2. Poll records vote for nodeID; when all K validators respond (or timeout), emit result bag.
3. Call `Consensus.RecordPoll(voteBag)`.
4. Topological finalizes blocks; `Accept()`/`Reject()` called.
5. Call `VM.SetPreference(consensus.Preference())`.

**VM notification:** VM sends `PendingTxs` message → engine calls `VM.BuildBlock()`.

---

### 1.6 Sender Interface (`snow/engine/common/sender.go`)

```go
type Sender interface {
    SendGetAcceptedFrontier(ctx, nodeIDs, requestID)
    SendGetAccepted(ctx, nodeIDs, requestID, containerIDs)
    SendGetAncestors(ctx, nodeIDs, requestID, containerID)
    SendGet(ctx, nodeIDs, requestID, containerID)
    SendPullQuery(ctx, nodeIDs, requestID, containerID)
    SendPushQuery(ctx, nodeIDs, requestID, containerID, container)
    SendChits(ctx, nodeIDs, requestID, containerIDs, ...)
    SendAppRequest(ctx, nodeIDs, appRequestBytes) error
    SendAppResponse(ctx, nodeID, appResponseBytes) error
    SendCrossChainAppRequest(ctx, chainID, appRequestBytes) error
    SendCrossChainAppResponse(ctx, chainID, appResponseBytes) error
}
```

The sender implementation (`snow/networking/sender/sender.go`) wraps the `ExternalSender` (network layer), registers requests with the router for timeout tracking, and routes loopback messages directly. Before sending to any peer, the sender checks `timeouts.IsBenched(chainID, nodeID)`; if benched, the request is immediately counted as failed (see [Benchlist](#part-4-benchlist)).

> **Used by networking layer:** All outbound consensus messages are routed through [`network.Network`](networking.md#1-network-interface) which implements `ExternalSender`. See [networking.md — Section 1](networking.md#1-network-interface).

---

### 1.7 Handler (`snow/networking/handler/handler.go`)

Each chain has exactly one Handler which runs message processing goroutines.

```go
type Handler interface {
    health.Checker
    Context() *snow.ConsensusContext
    SetEngineManager(engineManager *EngineManager)
    Push(ctx context.Context, msg Message)
    Start(ctx context.Context, recoverPanic bool)
    Stop(ctx context.Context)
    AwaitStopped(ctx context.Context) (time.Duration, error)
}
```

Internals:
- Two queues: `syncMessageQueue` (blocking ops) and `asyncMessageQueue` (non-blocking).
- Worker pool processes messages from both queues.
- `EngineManager` selects correct engine (Avalanche vs. Snowman) by engine type in message.
- Messages dropped if sender not in validator set (for some message types).

> **Message source:** Inbound messages arrive from the networking layer via `Router.HandleInbound()` → `ChainRouter` → `Handler.Push()`. See [networking.md — Section 3.4 Message Dispatch](networking.md#34-message-dispatch-in-peerhandle).

---

### 1.8 Snow Context

```go
// snow/context.go
type Context struct {
    NetworkID       uint32
    SubnetID        ids.ID
    ChainID         ids.ID
    NodeID          ids.NodeID
    PublicKey       *bls.PublicKey
    NetworkUpgrades upgrade.Config
    XChainID        ids.ID
    CChainID        ids.ID
    AVAXAssetID     ids.ID
    Log             logging.Logger
    Lock            sync.RWMutex
    SharedMemory    atomic.SharedMemory
    BCLookup        ids.AliaserReader
    Metrics         metrics.MultiGatherer
    WarpSigner      warp.Signer
    ValidatorState  validators.State  // P-Chain interface for validator lookups
    ChainDataDir    string
}

type ConsensusContext struct {
    *Context
    PrimaryAlias   string
    Registerer     Registerer
    BlockAcceptor  Acceptor
    TxAcceptor     Acceptor
    VertexAcceptor Acceptor
    State          utils.Atomic[EngineState]
    Executing      utils.Atomic[bool]
    StateSyncing   utils.Atomic[bool]
}
```

Acceptors are notified on every block/tx/vertex acceptance, enabling the indexer and other subsystems to react.

> **Chain Manager creates and injects this context:** `chains.Manager.createChain()` constructs the `ConsensusContext` (including `BlockAcceptor`, `TxAcceptor`, `VertexAcceptor`) and passes it to the engine and VM. See [chains.md — Section 1.3](chains.md#13-chain-creation-flow).
>
> **Acceptor registrations:** The node-level `BlockAcceptorGroup` / `TxAcceptorGroup` / `VertexAcceptorGroup` are passed into the chain manager config. The indexer (`indexer.NewIndexer`) registers itself via `acceptorGroup.RegisterAcceptor()` to index every accepted block. Additional registrations may be made for cross-chain bridges or audit subsystems. See [chains.md — Section 1](chains.md#1-chain-manager-chains).

---

### 1.9 Validator Management (`snow/validators/`)

#### Validator struct
```go
type Validator struct {
    NodeID    ids.NodeID
    PublicKey *bls.PublicKey
    TxID      ids.ID
    Weight    uint64
}
```

#### Manager interface
```go
type Manager interface {
    AddStaker(subnetID, nodeID, pk, txID, weight) error
    AddWeight(subnetID, nodeID, weight) error
    RemoveWeight(subnetID, nodeID, weight) error
    GetWeight(subnetID, nodeID) uint64
    GetValidator(subnetID, nodeID) (*Validator, bool)
    TotalWeight(subnetID) (uint64, error)
    Sample(subnetID, size) ([]ids.NodeID, error)
    RegisterCallbackListener(ManagerCallbackListener)
    RegisterSetCallbackListener(subnetID, SetCallbackListener)
}
```

Callbacks (`ManagerCallbackListener`) fire on validator add/remove/weight-change, enabling the network layer to update IP tracking and the engine to update its sampler.

#### Validator State (P-Chain interface)
```go
type State interface {
    GetMinimumHeight(context.Context) (uint64, error)
    GetCurrentHeight(context.Context) (uint64, error)
    GetSubnetID(ctx, chainID) (ids.ID, error)
    GetValidatorSet(ctx, height, subnetID) (map[ids.NodeID]*GetValidatorOutput, error)
    GetCurrentValidatorSet(ctx, subnetID) (map[ids.ID]*GetCurrentValidatorOutput, uint64, error)
}
```

Used by ProposerVM and Warp for cross-height validator lookups.

> **Consumers of `validators.State`:**
> - [ProposerVM (vms.md)](vms.md#part-2-proposervm) calls `ValidatorState.GetValidatorSet(ctx, pChainHeight, subnetID)` to determine the proposer window for a given P-Chain height.
> - [ProposerVM (vms.md)](vms.md#part-2-proposervm) calls `ValidatorState.GetCurrentHeight()` and `GetMinimumHeight()` when building/verifying blocks.
> - Simplex `BLSVerifier` is initialized from `config.Params.InitialValidators` which is derived from the current validator set at epoch start.
> - The `ValidatorState` is stored on `snow.Context.ValidatorState` and injected by the Chain Manager for every chain.

---

### 1.10 Uptime Tracking (`snow/uptime/`)

```go
type Manager interface {
    StartTracking(nodeIDs []ids.NodeID) error
    StopTracking(nodeIDs []ids.NodeID) error
    Connect(nodeID ids.NodeID) error
    Disconnect(nodeID ids.NodeID) error
    CalculateUptime(nodeID ids.NodeID) (time.Duration, time.Time, error)
    CalculateUptimePercent(nodeID ids.NodeID) (float64, error)
    CalculateUptimePercentFrom(nodeID ids.NodeID, startTime time.Time) (float64, error)
}
```

State is persisted per nodeID: `(cumulativeDuration, lastUpdated)`. On `Connect`, records the connection time. On `Disconnect`, accumulates elapsed time to the cumulative duration. The actual `Manager` interface in `snow/uptime/manager.go` is split into `Tracker` (Start/Stop tracking, Connect/Disconnect) and `Calculator` (CalculateUptime*, CalculateUptimePercent*) sub-interfaces, and also includes a `StartedTracking() bool` predicate.

> **P-Chain reads uptime to decide rewards:** `vms/platformvm` creates an `uptime.Manager` (backed by the P-Chain state DB) and uses `CalculateUptimePercentFrom(nodeID, startTime)` when determining if a validator earns staking rewards at the end of their staking period. See [platformvm.md](platformvm.md).
>
> **Networking layer feeds uptime:** The network layer tracks peer connectivity and calls `uptimeManager.Connect(nodeID)` / `Disconnect(nodeID)` as peers connect and disconnect. See [networking.md — Section 7](networking.md#7-uptime-tracking-from-peers-perspective).

---

## Part 2: Simplex Consensus

Located in `simplex/`. Uses the external `github.com/ava-labs/simplex` library for the core protocol logic, with avalanchego providing storage, VM integration, and BLS signing.

### 2.1 Protocol Overview

Simplex is a semi-synchronous BFT protocol:
- Tolerates `f < n/3` Byzantine faults.
- Organizes consensus into **epochs** scoped to a validator set.
- Each round has a single **leader** who proposes a block.
- Two-phase commit: **Notarization** (quorum on block) then **Finalization** (quorum on notarization).
- Leader selection: deterministic round-robin over sorted validators.

### 2.2 Data Structures

**Block:**
```go
// simplex/block.go
type Block struct {
    digest       simplex.Digest   // SHA256(serialized block) [32 bytes]
    metadata     simplex.ProtocolMetadata
    vmBlock      snowman.Block    // wrapped application block
    blockTracker *blockTracker
    blacklist    simplex.Blacklist
}
```

**ProtocolMetadata (protobuf):**
```protobuf
message ProtocolMetadata {
    uint32 version = 1;
    uint64 epoch   = 2;  // epoch number (changes with validator set)
    uint64 round   = 3;  // round within epoch
    uint64 seq     = 4;  // global sequence number (block height)
    bytes  prev    = 5;  // digest of previous block
}
```

**Vote:**
```protobuf
message Vote {
    BlockHeader block_header = 1;  // metadata + digest
    Signature   signature    = 2;  // signer NodeID + 96-byte BLS sig
}
```

**EmptyVote:** Broadcast when proposer times out. Contains only `metadata` (no block).

**QuorumCertificate:** Aggregated BLS signature from `n - f` validators.
```go
type QC struct {
    sig     *bls.Signature  // aggregated (96 bytes)
    signers []ids.NodeID
    verifier *BLSVerifier
}
```

**QuorumRound (replication):**
```protobuf
message QuorumRound {
    bytes               block               = 1;  // optional
    QuorumCertificate   notarization        = 2;
    EmptyNotarization   empty_notarization  = 3;  // if round skipped
    QuorumCertificate   finalization        = 4;
}
```

### 2.3 Block Proposal and Voting

**Proposal phase:**
1. Leader calls `BlockBuilder.BuildBlock()` — retries until block produced.
2. Broadcasts `BlockProposal` message (block bytes + vote from leader).
3. Other nodes validate proposal; broadcast `Vote`.

**Notarization phase:**
1. Each node aggregates `Vote` messages.
2. When `n - f` votes collected: create `Notarization` QC.
3. Broadcast notarization to all peers.

**Finalization phase:**
1. Each node on receiving notarization: broadcast `FinalizeVote`.
2. When `n - f` finalize votes collected: create `Finalization` QC.
3. Block is finalized; delivered to VM via `tree.Accept(vmBlock)`.

**Round timeout (empty voting):**
- Nodes advance a local clock ticker every `MaxNetworkDelay / 10`.
- If no proposal seen by `MaxNetworkDelay`: broadcast `EmptyVote`.
- `n - f` empty votes → `EmptyNotarization`; advance to next round.

### 2.4 BLS Signing (`simplex/bls.go`)

```go
type BLSSigner struct {
    chainID   ids.ID
    networkID uint32
    signBLS   SignFunc
}

// Payload signed includes chainID and networkID to prevent cross-chain replay
encodedPayload := encodedSimplexSignedPayload{
    NetworkID: networkID,
    ChainID:   chainID,
    Message:   message,
}
```

Verification:
```go
type BLSVerifier struct {
    nodeID2PK           map[ids.NodeID]*bls.PublicKey
    canonicalNodeIDs    []ids.NodeID   // sorted for bitset compression
    canonicalNodeIDIndices map[ids.NodeID]int
}
```

QC validation: all signers unique, in validator set, and aggregated signature verifies.

> **BLS cipher suites:** Simplex uses `bls.Verify()` which operates on the standard `CiphersuiteSignature` domain — the same suite used for Warp message signing. The `CiphersuiteProofOfPossession` (used for staking PoP proofs) is distinct and not used here. See [api.md — Section 9.2 BLS](api.md#92-bls-utilscryptobls).
>
> **BLS key source:** The `signBLS` function and validator public keys originate from `snow.Context.PublicKey` and `ValidatorState`. Node BLS keys are managed as part of staking identity. See [api.md — Section 7.2 BLS Key](api.md#72-bls-key).

### 2.5 Storage (`simplex/storage.go`)

```go
type Storage struct {
    numBlocks          atomic.Uint64
    db                 database.KeyValueReaderWriter
    genesisBlock       *Block
    lastIndexedDigest  simplex.Digest
    blockTracker       *blockTracker
}

// Database key format:
// Finalization: "f" || BigEndian(seqNumber)  [9 bytes]
// Blacklist:    "b" || BigEndian(seqNumber)  [9 bytes]
```

`Index(block, finalization)` persists a finalized block; `Retrieve(seq)` returns it.

Note: The actual `Storage` struct in `simplex/storage.go` also holds a `QCDeserializer` (for deserializing quorum certificates from bytes), a `vm block.ChainVM` reference (for retrieving VM blocks by height), and a `logging.Logger`. Serialization uses Canoto encoding, not protobuf, for the storage layer.

> **Database dependency:** Simplex storage uses `database.KeyValueReaderWriter` — the same database abstraction used throughout avalanchego. See [database.md](database.md) for the full interface hierarchy.

### 2.6 Engine (`simplex/engine.go`)

```go
func (e *Engine) Start(ctx context.Context, _ uint32) error
func (e *Engine) Simplex(ctx context.Context, nodeID ids.NodeID, msg *p2p.Simplex) error
func (e *Engine) tick()  // advances time, drives timeout transitions
```

The engine wraps the `simplex.Epoch` from the library, translating between P2P message format and the library's internal types.

### 2.7 VM Integration via blockTracker

```go
type blockTracker struct {
    lock    sync.Mutex
    simplexDigestsToBlock map[simplex.Digest]*Block
    tree    tree.Tree     // VM block parent-child relationships
    vm      block.ChainVM
}
```

- `Verify(vmBlock)`: calls `vmBlock.Verify()` and `vm.SetPreference(vmBlock.ID())`.
- `Accept(digest)`: calls `tree.Accept(vmBlock.ID())` — accepts the block, rejects competitors.
- `Reject(digest)`: calls `vmBlock.Reject()`.

---

## Part 3: Poll Mechanics

### 3.1 Poll Interface (`snow/consensus/snowman/poll/`)

```go
type Poll interface {
    Vote(vdr ids.NodeID, vote ids.ID)
    Drop(vdr ids.NodeID)   // timeout or disconnect
    Finished() bool
    Result() bag.Bag[ids.ID]
}

type Set interface {
    Add(requestID uint32, vdrs bag.Bag[ids.NodeID]) bool
    Vote(requestID uint32, vdr ids.NodeID, vote ids.ID) []bag.Bag[ids.ID]
    Drop(requestID uint32, vdr ids.NodeID) []bag.Bag[ids.ID]
    Len() int
}
```

A poll is complete when all sampled validators have voted or timed out. Early termination is possible (eagerly finalized polls for performance).

### 3.2 Bag (vote collection)

```go
type Bag[T comparable] struct {
    counts      map[T]int
    size        int
    threshold   int
    metThreshold set.Set[T]
}
```

Supports threshold queries: `bag.Threshold()` returns elements with count ≥ threshold. Used to quickly determine if alphaPreference/alphaConfidence is met.

---

## Part 4: Benchlist

`snow/networking/benchlist/` tracks validators that have been unresponsive:
- A validator is benched if it fails to respond to requests within a threshold.
- Benched validators are excluded from polls until the bench period expires.
- Prevents a few slow/faulty validators from blocking consensus.

The Sender checks `timeouts.IsBenched(chainID, nodeID)` before sending each request; benched validators receive an immediate synthetic failure response rather than an actual network message. This means benched validators do not participate in Snow polls until the bench period expires.

> **Sybil resistance note:** Benchlist decisions are per-chain, not global. A validator benched on one chain can still be polled on another chain. The bench period is independent of stake weight.

---

## Acceptance Propagation to VM

```
Consensus finalizes block B
  ↓
Topological.RecordPoll() marks B accepted
  ↓
Block.Accept() [Block implements snowman.Block]
  ↓
ConsensusContext.BlockAcceptor.Accept(ctx, blockID, blockBytes)
  ↓
Registered acceptors fire (indexer, cross-chain bridge, etc.)
```

VM preference (uncommitted tip) updated via `VM.SetPreference(consensus.Preference())` after every poll.
