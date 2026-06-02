# Virtual Machines — AVM, ProposerVM, RPCChainVM, SAEVM

## Directory Layout

```
vms/
├── avm/               # Asset VM (X-Chain)
│   ├── txs/           # Transaction types and executors
│   ├── block/         # Block types
│   ├── state/         # UTXO and chain state
│   ├── utxo/          # UTXO selection helpers
│   └── fxs/           # Feature extension registry
├── proposervm/        # Snowman++ proposer selection wrapper
│   ├── block/         # PostFork block types
│   └── proposer/      # Windower (proposer selection)
├── rpcchainvm/        # gRPC plugin host
│   └── runtime/       # Subprocess lifecycle
├── saevm/             # Streaming Asynchronous Execution VM (C-Chain)
│   ├── sae/           # Core execution engine
│   ├── saexec/        # Transaction executor
│   ├── saedb/         # State DB tracking layer
│   ├── cchain/        # C-Chain VM wrapper
│   ├── blocks/        # Block structure
│   └── txgossip/      # Transaction gossip
├── fx/                # Feature extension interfaces
├── secp256k1fx/       # SECP256K1 FX (signature verification)
├── nftfx/             # NFT FX
├── propertyfx/        # Property/permission FX
├── components/avax/   # UTXO, TransferableInput/Output
└── txs/               # Cross-VM transaction types (used by avm)
```

---

## Part 1: AVM — Asset VM (X-Chain)

### 1.1 Transaction Types

All transactions extend `BaseTx`:
```go
type BaseTx struct {
    TypeID      uint32
    NetworkID   uint32
    BlockchainID ids.ID
    Outs        []*TransferableOutput
    Ins         []*TransferableInput
    Memo        []byte
}
```

**BaseTx** — Plain UTXO-to-UTXO value transfer.

**CreateAssetTx** — Define a new asset.
```go
type CreateAssetTx struct {
    BaseTx
    Name         string          // human-readable name
    Symbol       string          // ticker symbol
    Denomination byte            // display divisor
    States       []*InitialState // per-FX initial outputs
}
```
Asset ID = hash of this transaction. `States` contains FX-indexed initial UTXO distributions.

**OperationTx** — FX-specific state operations (mint, burn, transfer NFT, etc.).
```go
type OperationTx struct {
    BaseTx
    Ops []*Operation
}
type Operation struct {
    Asset   avax.Asset      // which asset
    UTXOIDs []*avax.UTXOID  // inputs consumed
    Op      FxOperation     // FX-specific: MintOperation, TransferOperation, etc.
}
```
Operations sorted canonically before signing.

**ImportTx** — Pull UTXOs from atomic memory (another chain). See [Atomic Memory](chains.md#2-atomic-memory).
```go
type ImportTx struct {
    BaseTx
    SourceChain  ids.ID
    ImportedIns  []*TransferableInput
}
```

**ExportTx** — Push UTXOs to atomic memory (another chain). See [Atomic Memory](chains.md#2-atomic-memory).
```go
type ExportTx struct {
    BaseTx
    DestinationChain ids.ID
    ExportedOuts     []*TransferableOutput
}
```

### 1.2 UTXO Model

```go
type UTXO struct {
    UTXOID avax.UTXOID   // TxID + OutputIndex
    Asset  avax.Asset    // AssetID
    Out    verify.State  // FX-specific output (secp256k1fx.TransferOutput, etc.)
}
```

**State** holds the UTXO set under prefix `"utxo"`. Address-indexed UTXOs enable wallet queries.

**UTXO selection** (`utxo/spender.go`):
```go
// Spend(): select UTXOs covering required amounts, return inputs + signers
// SpendNFT(): select specific NFT UTXO
// SpendAll(): consume all UTXOs for an address
// Mint(): produce new UTXOs via minting operations
```

### 1.3 FX (Feature Extension) System

```go
// vms/fx/fx.go
type Fx interface {
    Initialize(vm interface{}) error
    Bootstrapping() error
    Bootstrapped() error
    VerifyTransfer(tx, in, cred, utxo interface{}) error
    VerifyOperation(tx, op, cred interface{}, utxos []interface{}) error
}
```

Three built-in FX implementations:

**secp256k1fx** — SECP256K1 threshold signatures. Uses [SECP256K1 cryptographic primitives](api.md#91-secp256k1-utilscryptosecp256k1).
- `TransferInput/Output`: standard UTXO with M-of-N threshold.
- `MintOutput/MintOperation`: minting authority (threshold on mint key set).
- `Credential`: array of secp256k1 signatures.

**nftfx** — Non-Fungible Tokens.
- `TransferOutput/TransferOperation`: NFT transfer with `GroupID` (uint32) and `Payload` (arbitrary bytes).
- `MintOutput/MintOperation`: mint new NFTs with assigned group IDs.
- NFT identity: `AssetID + GroupID + OutputIndex`.

**propertyfx** — Permission/governance tokens.
- `OwnedOutput`: threshold ownership of a property.
- `MintOperation/BurnOperation`: controlled minting/burning.
- Used for governance tokens with programmatic transfer rules.

**FxCredential structure:**
```go
type FxCredential struct {
    FxID       ids.ID
    Credential verify.Verifiable
}
```

One credential per FX input in a transaction.

### 1.4 Block Types

```go
type Block interface {
    ID() ids.ID
    Parent() ids.ID
    Height() uint64
    Timestamp() time.Time
    MerkleRoot() ids.ID
    Bytes() []byte
    Txs() []*txs.Tx
}

type StandardBlock struct {
    PrntID      ids.ID
    Hght        uint64
    Time        uint64     // Unix seconds
    Root        ids.ID     // Merkle root of txs
    Transactions []*txs.Tx
}
```

BlockID = SHA256(serialized block). Blocks are a linear chain; each references parent by ID. Height increases monotonically.

### 1.5 Transaction Execution (Visitor Pattern)

Three-phase execution:
1. **SyntacticVerifier** — format and field validation.
2. **SemanticVerifier** — UTXO availability, signature correctness, FX constraints.
3. **Executor** — state mutation.

```go
// txs/executor/executor.go
func (e *Executor) BaseTx(tx *txs.BaseTx) error {
    avax.Consume(e.State, tx.Ins)    // delete consumed UTXOs
    avax.Produce(e.State, tx.TxID, tx.Outs)  // create new UTXOs
}

func (e *Executor) CreateAssetTx(tx *txs.CreateAssetTx) error {
    // Asset ID = tx hash; produce initial UTXOs per FX
    for i, initialState := range tx.States {
        fx.VerifyOperation(tx, initialState, cred, nil)
        // produce FX outputs with new assetID
    }
}

func (e *Executor) ImportTx(tx *txs.ImportTx) error {
    // Record atomic request to remove from source chain's outbound namespace
    e.AtomicRequests[tx.SourceChain] = removeRequest(tx.ImportedIns)
    avax.Consume(e.State, tx.Ins)
    avax.Produce(e.State, tx.TxID, tx.Outs)
}

func (e *Executor) ExportTx(tx *txs.ExportTx) error {
    // Record atomic request to put on destination chain's inbound namespace
    e.AtomicRequests[tx.DestinationChain] = putRequest(tx.ExportedOuts)
    avax.Consume(e.State, tx.Ins)
}
```

The `AtomicRequests` map is committed to [Atomic Memory](chains.md#2-atomic-memory) via `SharedMemory.Apply()` atomically with the block acceptance batch.

### 1.6 State Storage (`avm/state/`)

```go
type State interface {
    // UTXOs
    GetUTXO(utxoID avax.UTXOID) (*avax.UTXO, error)
    PutUTXO(utxo *avax.UTXO)
    DeleteUTXO(utxoID avax.UTXOID)
    
    // Blocks
    GetBlock(blkID ids.ID) (blocks.Block, error)
    PutBlock(blk blocks.Block)
    
    // Transactions
    GetTx(txID ids.ID) (*txs.Tx, error)
    PutTx(tx *txs.Tx)
    
    // Metadata
    GetLastAccepted() ids.ID
    SetLastAccepted(ids.ID)
    GetTimestamp() time.Time
    SetTimestamp(time.Time)
}
```

All state changes buffered in [VersionDB](database.md#24-versiondb); committed atomically on block acceptance. This ensures UTXO mutations and atomic memory writes land in the same database batch.

---

## Part 2: ProposerVM

### 2.1 Purpose

ProposerVM implements **Snowman++**: a soft leader election layer on top of any Snowman VM, reducing consensus latency by limiting who can propose in early time windows. It is driven by the [Consensus Engine](consensus.md#15-engine-state-machine) and wraps the inner VM (AVM, PlatformVM, RPCChainVM, etc.). See [ChainVM Interface](#54-chainvm-interface-summary) for the interface it implements and [VM Wrapping Order](#53-vm-wrapping-order) for its position in the stack.

### 2.2 Block Types

**preForkBlock** — Before activation timestamp.
- No header modification.
- BlockID = inner block ID.
- Pure pass-through.

**postForkBlock** — After activation.
```go
type statelessUnsignedBlock struct {
    ParentID     ids.ID   // parent proposer block ID
    Timestamp    int64    // Unix seconds
    PChainHeight uint64   // P-Chain height at proposal time
    Certificate  []byte   // proposer's TLS certificate
    Block        []byte   // serialized inner VM block
}
// Signature = ECDSA over SHA256(serialized statelessUnsignedBlock)
```
BlockID computed from the unsigned portion only. Signature verifies proposer identity.

**postForkOption** — After oracle block (e.g., proposal with two options).
- No signature required (deterministic child).
- `ParentID` references the oracle block.

**postForkGraniteBlock** (Granite upgrade) — Extends postForkBlock with epoch:
```go
type Epoch struct {
    PChainHeight uint64
    Number       uint64
    StartTime    int64
}
```

### 2.3 Proposer Selection (`proposer/windower.go`)

```go
type Windower interface {
    Proposers(ctx, blockHeight, pChainHeight, maxWindows) ([]ids.NodeID, error)
    Delay(ctx, blockHeight, pChainHeight, validatorID, maxWindows) (time.Duration, error)
    ExpectedProposer(ctx, blockHeight, pChainHeight, slot) (ids.NodeID, error)
    MinDelayForProposer(ctx, blockHeight, pChainHeight, nodeID, startSlot) (time.Duration, error)
}
```

The Windower calls [validators.State](consensus.md#19-validator-management) (`ctx.ValidatorState.GetValidatorSet()`) to retrieve the historical validator set at `pChainHeight`.

**Algorithm:**
1. Seed = `chainSource XOR blockHeight` (where `chainSource` = first 8 bytes of ChainID).
2. Retrieve validators active at `pChainHeight` via `validators.State.GetValidatorSet()`.
3. Sort validators canonically by NodeID.
4. Weighted random sampling without replacement using Mersenne Twister (MT19937) seeded by the XOR value.
5. Return first `maxWindows` validators (default 6 for verification, up to 60 for building).

**Post-Durango slot-based scheme:**
- Each slot is 5 seconds wide.
- `ExpectedProposer(blockHeight, pChainHeight, slot)` returns the node assigned to that slot.
- Seed = `chainSource XOR blockHeight XOR bits.Reverse64(slot)` (slot reversed to avoid seed-space collisions).

**Time windows:**
- Window duration: 5 seconds.
- Validator `i` in the list may propose starting at `parent.Timestamp + (i × 5s)`.
- After `MaxVerifyWindows × 5s` = 30 seconds: any validator may propose.

### 2.4 Verification Flow

**postForkBlock.Verify():**
1. Timestamp > parent timestamp.
2. PChainHeight ≥ parent's PChainHeight; ≤ current P-Chain height.
3. Timestamp within ±10 second skew bound.
4. Verify proposer is in the expected window.
5. Verify TLS signature against proposer's certificate.
6. Delegate inner block verification to inner VM.

**postForkOption.Verify():**
- Parent must be a postForkBlock with oracle options.
- Verifies option is among parent's valid choices.

### 2.5 State (`proposervm/state/`)

Stores mapping of proposer block ID → inner block ID and proposer block metadata. Uses [database](database.md) for persistence. Required to:
- Look up inner blocks from proposer block IDs.
- Reconstruct proposer blocks from inner blocks after restart.

### 2.6 VM Wrapping Architecture

```
Consensus Engine
  ↓
ProposerVM (implements block.ChainVM)
  ├── Exposes proposer block types
  ├── Translates between inner VM blocks and proposer blocks
  ├── Calls Windower.Delay() to determine build time
  └── Delegates block operations to inner VM
     ↓
Inner VM (AVM, PlatformVM, RPCChainVM, etc.)
```

ProposerVM wraps the [ChainVM Interface](#54-chainvm-interface-summary). After ProposerVM, a MeterVM and second TracedVM layer are applied by the [Chain Manager](chains.md#14-vm-wrapping-stack).

### 2.7 State Sync Support

ProposerVM supports state sync by tracking proposer block info alongside state summaries. After a state sync, inner blocks are re-verified to restore the proposer/inner block mapping.

---

## Part 3: RPCChainVM

### 3.1 gRPC Plugin Architecture

External VMs run as separate OS processes and communicate over gRPC. The RPCChainVM implements the [ChainVM Interface](#54-chainvm-interface-summary) remotely, allowing any language to implement a VM. ProposerVM wraps the RPCChainVM client just like any other ChainVM.

```
AvalancheGo Process                    VM Subprocess
─────────────────                    ─────────────────
VMClient (block.ChainVM impl)  ←──►  VMServer (gRPC server)
  ├── RPCDatabase client                 ├── Application logic
  ├── SharedMemory proxy                 ├── RPCDatabase server
  ├── AppSender proxy                    ├── SharedMemory server
  └── ValidatorState proxy               └── AppSender server
```

### 3.2 Plugin Handshake Protocol

**Step 1:** Parent creates TCP listener on ephemeral port; starts gRPC Runtime server.

**Step 2:** Parent spawns subprocess with env var `AVALANCHE_VM_RUNTIME_ENGINE_ADDR=<addr:port>`.

**Step 3:** Subprocess reads env var, connects to Runtime server, sends `Initialize` RPC with its own gRPC address.

**Step 4:** Parent receives subprocess's address, creates gRPC client, checks protocol version.

**Timeout:** If no handshake within the configured `HandshakeTimeout`: `ErrHandshakeFailed`.

**Version mismatch:** Returns `ErrProtocolVersionMismatch`.

### 3.3 VM gRPC Service (from `proto/vm/vm.proto`)

Key RPC methods:

| RPC | Input | Output | Notes |
|-----|-------|--------|-------|
| `Initialize` | context, genesis, config | chain ID, last accepted | Full initialization |
| `SetState` | state enum | — | Bootstrapping→NormalOp |
| `BuildBlock` | optional PChainHeight | id, parent, bytes, height, ts | Builds next block |
| `ParseBlock` | bytes | id, parent, height, status | Deserialize block |
| `GetBlock` | blockID | bytes, status | Retrieve cached block |
| `BlockVerify` | bytes, optional PChainHeight | timestamp | Validate block |
| `BlockAccept` | blockID | — | Finalize |
| `BlockReject` | blockID | — | Discard |
| `WaitForEvent` | — | event enum | Block until PendingTxs or StateSyncDone |
| `AppRequest` | nodeID, deadline, bytes | — | VM-defined request |
| `AppResponse` | nodeID, bytes | — | VM-defined response |
| `AppGossip` | nodeID, bytes | — | VM-defined gossip |
| `StateSyncEnabled` | — | enabled bool | |
| `GetLastStateSummary` | — | height, bytes | |
| `GetAncestors` | blockID, maxItems, maxSize | blocks | Ancestry chain |
| `Gather` | — | Prometheus metrics | |
| `Health` | — | details | |

### 3.4 Database Proxy (`database/rpcdb/`)

All database operations proxied via gRPC using [RPCDB](database.md#27-rpcdb). The VM subprocess receives a database client at initialization:

**Server** holds an actual `database.Database` and exposes it via gRPC.
**Client** implements `database.Database` by calling gRPC methods.

Iterator batching: `IteratorNext` returns up to 128 KiB of key-value pairs per RPC call.

### 3.5 Process Lifecycle

```go
type Config struct {
    Stderr           io.Writer
    Stdout           io.Writer
    HandshakeTimeout time.Duration
    Log              logging.Logger
}
```

- Process death detected via gRPC connection loss.
- Stderr/stdout piped to parent logs.
- Graceful shutdown via SIGTERM.

---

## Part 4: SAEVM — Streaming Asynchronous Execution VM

### 4.1 What SAE Means

**Streaming Asynchronous Execution ([ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution)):** The EVM block execution is decoupled from block proposal. Blocks can be proposed while the previous block is still executing. Reduces proposal latency from O(execution_time) to O(proposal_time).

### 4.2 Architecture

```
SAE VM (vms/saevm/)
├── cchain/         # C-Chain wrapper with cross-chain support
│   ├── vm.go       # Implements block.ChainVM
│   ├── state/      # Cross-chain import/export state
│   ├── hooks.go    # Pre/post block execution hooks
│   └── api.go      # JSON-RPC handlers
├── sae/            # Core execution engine
│   └── vm.go       # Block queue and execution loop
├── saexec/         # Async transaction executor
│   └── saexec.go   # Executes blocks from queue
└── saedb/          # State DB tracking layer
    └── saedb.go    # Deferred write tracking
```

### 4.3 Execution Model

```go
type Executor struct {
    *saedb.Tracker        // state DB with deferred writes
    queue chan *blocks.Block  // FIFO block queue
    lastExecuted *blocks.Block
    chainContext *chainContext
    chainConfig  *params.ChainConfig
    db           ethdb.Database
}
```

**Flow:**
1. `Enqueue(block)` — non-blocking, adds to FIFO queue.
2. Background goroutine dequeues and executes transactions sequentially.
3. `saedb.Tracker` records all state changes.
4. Events (ChainHeadEvent, Logs) emitted after each block.
5. State committed to disk at configurable intervals (e.g., every 100 blocks).

**Key invariant:** Execution order is strictly FIFO. A block can be proposed once its parent is *enqueued* (not necessarily *executed*).

### 4.4 Block Structure

```go
type Block struct {
    Header               *types.Header    // EVM header
    Body                 *types.Body      // transactions
    PostExecutionStateRoot common.Hash    // set after execution
}
```

### 4.5 State DB Tracking (`saedb/`)

```go
type Tracker struct {
    // Tracks dirty state from pending executions
    // Defers writes to reduce per-block I/O
    // On shutdown: in-flight executions re-queued for recovery
}
```

### 4.6 C-Chain Wrapper (`cchain/`)

```go
type VM struct {
    *sae.VM               // embedded execution engine
    ctx    *snow.Context
    state  *state.State   // cross-chain import/export tracking
    txpool *txpool.Txpool // pending EVM transaction pool
}
```

**Cross-chain integration** via [Atomic Memory](chains.md#2-atomic-memory):
- **Imports**: P2P RPC accepts import txs; `state` tracks pending imports; executed atomically with EVM txs via hooks using `SharedMemory.Apply()`.
- **Exports**: EVM logs monitored for export events; export requests batched per destination chain and written to atomic memory.

**Hook points:**
```go
type Hooks struct {
    OnBlockProposal func(block *blocks.Block) error   // pre-execute: process imports
    OnBlockCommit   func(block *blocks.Block) error   // post-execute: finalize exports
    OnNewTxs        func()                           // txpool update
}
```

**Event types for `WaitForEvent()`:**
- `PendingTxs` — new tx in pool
- `NewTopoEvent` — new block committed
- `StopVertexEvent` — consensus stopped

### 4.7 Gas Pricing (`gasprice/`)

Dynamic EIP-1559-style gas pricing. Base fee adjusts per block based on utilization relative to target block gas.

### 4.8 Transaction Gossip (`txgossip/`)

EVM transactions gossiped to peers using push/pull protocol via [P2P gossip](networking.md#9-p2p-higher-level-abstractions) (`network/p2p/gossip`). Prevents redundant retransmission via bloom filters.

---

## Part 5: Common VM Infrastructure

### 5.1 MeterVM (`vms/metervm/`)

Wraps any VM to collect [Prometheus metrics](api.md#5-metrics-api) via the metrics API:
- Counts and durations for every VM method call.
- Labels: `Initialize`, `BuildBlock`, `ParseBlock`, `GetBlock`, `Verify`, `Accept`, `Reject`, etc.

### 5.2 TracedVM (`vms/tracedvm/`)

Wraps any VM to add OpenTelemetry distributed tracing. Each VM method creates a span. Useful for diagnosing cross-subsystem latency.

### 5.3 VM Wrapping Order

For a typical Snowman (linear) chain, the actual wrapping order in `chains/manager.go` is:

```
InnerVM
  ↓ TracedVM (if tracing enabled, inner layer)
  ↓ ProposerVM
  ↓ MeterVM (if metering enabled)
  ↓ TracedVM (outer layer, if tracing enabled)
  ↓ ChangeNotifier
  ↓ Handler (message dispatcher)
  ↓ Consensus Engine
```

See [Chain Manager VM Wrapping Stack](chains.md#14-vm-wrapping-stack) for the chains-side view of this stack. The ProposerVM wraps the inner VM because it needs to intercept block operations. MeterVM is placed outside ProposerVM so it measures the full proposer overhead. The ChangeNotifier sits outermost to detect when the preferred block changes.

### 5.4 ChainVM Interface Summary

The `ChainVM` interface is the primary contract between the consensus engine and any VM. **ProposerVM wraps this interface**, adding proposer headers before passing operations to the inner VM. **RPCChainVM implements it remotely** via gRPC so external processes can serve as VMs.

```go
type ChainVM interface {
    common.VM
    
    // Block management
    BuildBlock(context.Context) (snowman.Block, error)
    ParseBlock(context.Context, []byte) (snowman.Block, error)
    GetBlock(context.Context, ids.ID) (snowman.Block, error)
    SetPreference(context.Context, ids.ID) error
    LastAccepted(context.Context) (ids.ID, error)
}

// common.VM includes:
type VM interface {
    Initialize(ctx, chainCtx, db, genesis, upgrade, config, fxs, appSender) error
    SetState(context.Context, snow.State) error
    Shutdown(context.Context) error
    Version(context.Context) (string, error)
    CreateHandlers(context.Context) (map[string]http.Handler, error)
    WaitForEvent(context.Context) (common.Message, error)
    health.Checker
    validators.Connector
    AppHandler
}
```

Optional extension interfaces:
- `BuildBlockWithContextChainVM`: Passes P-Chain height to `BuildBlock`.
- `StateSyncableVM`: Enables state sync.
- `BatchedChainVM`: `GetAncestors` for batch block fetching.
