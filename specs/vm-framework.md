# VM Framework & the VM Wrapper Stack

## 1. Purpose

A **Virtual Machine (VM)** in AvalancheGo defines the *application* of a blockchain: the
representation of state, the set of valid operations on that state, how those operations are
encoded into blocks, and the APIs exposed to users. **Consensus is agnostic to the
application** — the Snowman engine treats the VM as a black box that builds, parses, stores,
and decides blocks (see [consensus.md](consensus.md)).

This document describes the **framework and wrapper stack that every Snowman VM shares**,
*not* the business logic of any particular VM (for those, see
[platformvm.md](platformvm.md), [avm.md](avm.md), [evm.md](evm.md)). Specifically:

- The `ChainVM` / `Block` interface contract and its optional extensions.
- The decorator stack (`ProposerVM → MeterVM → TracedVM → inner VM`) and the Snowman++
  congestion-control mechanism.
- The `rpcchainvm` gRPC plugin model that lets a VM run as a separate process.
- The state-sync VM interface, the `AppHandler` (VM-to-VM p2p), and the `Fx` feature-extension
  framework.

## 2. Responsibilities & Scope

| In scope | Out of scope |
|---|---|
| `ChainVM`/`Block` contract, optional VM interfaces | PlatformVM/AVM/EVM tx semantics, fee logic |
| Block uniquification & caching helper (`components/chain`) | Application state machines |
| ProposerVM (Snowman++) | Snowman consensus algorithm internals → [consensus.md](consensus.md) |
| MeterVM, TracedVM | Network message transport → [networking.md](networking.md) |
| rpcchainvm (gRPC plugin boundary) | Database engines → [database.md](database.md) |
| Fx framework (`secp256k1fx`, `nftfx`, `propertyfx`) | The P-Chain validator state it reads |
| `vms.Manager`, `vms.Factory`, `vms/registry` | Chain creation/bootstrap orchestration → [chains.md](chains.md) |

## 3. Package / File Layout

```
snow/engine/snowman/block/        — Snowman VM & Block interface contract
  vm.go                           — ChainVM, Getter, Parser, ParseFunc
  batched_vm.go                   — BatchedChainVM + GetAncestors/BatchedParseBlock helpers
  state_syncable_vm.go            — StateSyncableVM
  state_summary.go                — StateSummary
  state_sync_mode.go              — StateSyncMode (Skipped/Static/Dynamic)
  block_context_vm.go             — Context, BuildBlockWithContextChainVM, WithVerifyContext
snow/engine/common/
  vm.go                           — common.VM (base interface for all consensus VMs)
  message.go                      — Message (PendingTxs, StateSyncDone)
  engine.go                       — AppHandler / AppRequestHandler / ...
  sender.go                       — AppSender (VM → network)
  fx.go                           — common.Fx wrapper {ID, Fx}
snow/decidable.go                 — snow.Decidable (ID/Accept/Reject)
snow/consensus/snowman/block.go   — snowman.Block (Parent/Verify/Bytes/Height/Timestamp)

vms/manager.go                    — vms.Manager, vms.Factory
vms/registry/                     — VMRegistry (dynamic plugin discovery & install)
vms/proposervm/                   — Snowman++ wrapper (see README.md there)
  vm.go, block.go, config.go
  pre_fork_block.go, post_fork_block.go, post_fork_option.go
  height_indexed_vm.go            — GetBlockIDAtHeight + historical pruning
  proposer/windower.go            — proposer selection / windowing
vms/metervm/                      — Prometheus latency wrapper
vms/tracedvm/                     — OpenTelemetry span wrapper
vms/rpcchainvm/                   — out-of-process VM over gRPC
  vm_client.go (node side), vm_server.go (plugin side)
  factory.go, vm.go (Serve)
  runtime/                        — VMRE: process lifecycle (subprocess/)
  ghttp/, gruntime/               — gRPC bridges for http.Handler & runtime
vms/fx/factory.go                 — fx.Factory
vms/secp256k1fx/, nftfx/, propertyfx/ — feature extensions
vms/components/{avax,chain,gas,verify} — shared building blocks
vms/components/chain/state.go     — the block uniquification/caching helper
```

## 4. Core Interfaces

### 4.1 `common.VM` — the base of every consensus VM

`snow/engine/common/vm.go:17`

```go
type VM interface {
    AppHandler              // VM-to-VM p2p message handling (§6.3)
    health.Checker          // HealthCheck(ctx) (any, error)
    validators.Connector    // Connected / Disconnected peer callbacks

    Initialize(ctx, chainCtx *snow.Context, db database.Database,
        genesisBytes, upgradeBytes, configBytes []byte,
        fxs []*Fx, appSender AppSender) error
    SetState(ctx, state snow.State) error          // Bootstrapping → NormalOp etc.
    Shutdown(ctx) error
    Version(ctx) (string, error)
    CreateHandlers(ctx) (map[string]http.Handler, error)  // /ext/bc/<chainID>/<ext>
    NewHTTPHandler(ctx) (http.Handler, error)
    WaitForEvent(ctx) (Message, error)             // blocks until PendingTxs etc.
}
```

`chainCtx.Lock` (`snow/engine/common/vm.go:34`) is a RW lock **shared between the VM and the
engine**; the engine holds the write lock whenever it calls into the VM. `WaitForEvent`
returns `common.Message` — `PendingTxs` (build a block) or `StateSyncDone`
(`snow/engine/common/message.go:13`).

### 4.2 `ChainVM` — the Snowman VM

`snow/engine/snowman/block/vm.go:28`

```go
type ChainVM interface {
    common.VM
    Getter    // GetBlock(ctx, blkID) (snowman.Block, error)
    Parser    // ParseBlock(ctx, []byte) (snowman.Block, error)

    BuildBlock(ctx) (snowman.Block, error)            // build on the preferred block
    SetPreference(ctx, blkID ids.ID) error            // blk has no verified children
    LastAccepted(ctx) (ids.ID, error)                 // genesis if nothing accepted
    GetBlockIDAtHeight(ctx, height uint64) (ids.ID, error) // ErrNotFound if pruned/synced
}
```

- `Getter.GetBlock` (`vm.go:63`) — must return verified & accepted blocks; rejected blocks
  need not be retrievable. Returns `database.ErrNotFound` when unknown.
- `Parser.ParseBlock` (`vm.go:77`) — must parse the *full* byte array; **all historical
  blocks must be parseable**. May do syntactic verification.

### 4.3 `snowman.Block` — what consensus decides

`snow/consensus/snowman/block.go:23`, embeds `snow.Decidable` (`snow/decidable.go:16`).

```go
type Decidable interface {
    ID() ids.ID
    Accept(ctx) error
    Reject(ctx) error
}
type Block interface {       // snowman
    Decidable
    Parent() ids.ID
    Verify(ctx) error        // parent already verified; if nil → Accept|Reject guaranteed
    Bytes() []byte
    Height() uint64
    Timestamp() time.Time
}
```

Blocks are guaranteed to be Verified/Accepted/Rejected **in topological order**. A block's
status is `Processing`, `Accepted`, or `Rejected`; the latter two are `Decided`.

### 4.4 Optional VM extensions (discovered via type-assertion)

| Interface | File:line | Purpose |
|---|---|---|
| `BatchedChainVM` | `block/batched_vm.go:25` | `GetAncestors`, `BatchedParseBlock` — batch network calls (esp. for rpcchainvm bootstrap) |
| `StateSyncableVM` | `block/state_syncable_vm.go:15` | `StateSyncEnabled`, `Get/Parse/GetStateSummary` — sync to a recent state instead of bootstrapping from genesis |
| `BuildBlockWithContextChainVM` | `block/block_context_vm.go:34` | `BuildBlockWithContext(ctx, *Context)` — build with P-Chain height context |
| `SetPreferenceWithContextChainVM` | `block/block_context_vm.go:45` | `SetPreferenceWithContext` |
| `WithVerifyContext` (on a Block) | `block/block_context_vm.go:57` | `ShouldVerifyWithContext` / `VerifyWithContext(ctx, *Context)` |

`block.Context` (`block_context_vm.go:17`) currently carries only `PChainHeight`. These are
called *if and only if the proposervm is activated* — otherwise the plain `BuildBlock`/`Verify`
are used. Helpers `GetAncestors`/`BatchedParseBlock` (`batched_vm.go:37,110`) fall back to
per-block logic when the VM doesn't implement the batched interface (returning
`ErrRemoteVMNotImplemented`).

### 4.5 State sync interfaces

`StateSummary` (`block/state_summary.go:14`) has `ID/Height/Bytes` and `Accept(ctx)
(StateSyncMode, error)`. `StateSyncMode` (`block/state_sync_mode.go`):

- `StateSyncSkipped` — VM declines (bootstrap instead).
- `StateSyncStatic` — engine waits for the VM to finish syncing before bootstrapping.
- `StateSyncDynamic` — engine proceeds immediately; the VM must behave as fully synced
  (LastAccepted/GetBlock/ParseBlock/Verify invariants must hold) while syncing asynchronously.

## 5. The VM Wrapper Stack

Every chain's VM is the *innermost* application VM wrapped by a chain of decorators. The
authoritative assembly is in `chains/manager.go` (`createSnowmanChain` ~`:766-811`, and the
DAG/X-chain path ~`:1190-1230`):

```
              ┌─────────────────────────────────────────────┐
  Engine ───► │ TracedVM            (if TracingEnabled)       │  vms/tracedvm
              │  └─ MeterVM          (if MeterVMEnabled)      │  vms/metervm
              │      └─ ProposerVM   (Snowman++ always wraps) │  vms/proposervm
              │          └─ [TracedVM]  (inner, if tracing)   │
              │              └─ inner ChainVM                  │  application VM
              │                  (often an rpcchainvm.VMClient)│  vms/rpcchainvm
              └─────────────────────────────────────────────┘
```

Key facts about ordering (verified against `chains/manager.go`):

1. **ProposerVM always wraps the inner VM** (`proposervm.New`, `manager.go:782`). The inner
   VM is optionally `TracedVM`-wrapped *first* (`manager.go:771`) so proposervm's calls into
   the inner VM are traced.
2. **MeterVM wraps the ProposerVM** when enabled (`manager.go:807`), then **TracedVM wraps
   the MeterVM** when tracing is on (`manager.go:810`). So the engine sees, from outside in:
   `Traced(Meter(ProposerVM(Traced(inner))))`.
3. Each wrapper re-detects the optional interfaces of the layer below by type-assertion in its
   constructor (e.g. `metervm.NewBlockVM`, `vms/metervm/block_vm.go:43-46`;
   `proposervm.New`, `vms/proposervm/vm.go:128-131`) and only forwards the extension if the
   inner layer supports it. This is why `_ block.BatchedChainVM = (*blockVM)(nil)` etc. assert
   conformance at each layer.

### 5.1 BuildBlock / Verify data flow

```
  Engine.BuildBlock
    └► TracedVM.BuildBlock (span)
       └► MeterVM.BuildBlock (timer)
          └► ProposerVM.BuildBlock                       vm.go:340
               getBlock(preferred) → preferred.buildChild(ctx)
                 selectChildPChainHeight() ───────────► reads P-Chain ValidatorState
                 windower.MinDelayForProposer() ───────► am I in my slot?
                 inner.BuildBlockWithContext(ctx,{PChainHeight})  (or BuildBlock)
                 wrap inner block in postForkBlock (sign w/ StakingLeafSigner)
          ◄── meter records build latency
  ◄── traced span ends

  Engine.Verify(blk)
    └► ... ► ProposerVM postForkBlock.Verify        post_fork_block.go:83
               header checks: signature, PChainHeight monotonic & ≤ current,
                 timestamp monotonic & < now+maxSkew, proposer-window timing
               verifyAndRecordInnerBlk()             vm.go:837
                 Tree.Get(inner) → uniquify shared inner block
                 inner.VerifyWithContext / inner.Verify   (exactly once if valid)
                 verifiedBlocks[postForkID] = postFork
```

## 6. Component Boundaries & Relationships

### 6.1 Engine ↔ VM boundary

The Snowman engine never inspects block contents; it only calls the `ChainVM`/`Block`
interface. The contract is governed by a few hard invariants (§7). The engine and VM share
`chainCtx.Lock`; the engine holds the lock when calling the VM, so VM methods generally must
not re-acquire it (the proposervm's async `WaitForEvent`/`timeToBuild` is a notable case that
*does* take `vm.ctx.Lock` itself — `vms/proposervm/vm.go:481`).

`WaitForEvent` is the build trigger: the VM blocks until it has work, then returns
`PendingTxs`; the engine must eventually call `BuildBlock`. ProposerVM intercepts this
(`vm.go:439`): if it is not yet this node's proposer slot it *buffers* by waiting before
forwarding the inner VM's event — so the engine may not see `PendingTxs` promptly (see
`vms/README.md`).

### 6.2 Node ↔ external-VM gRPC boundary (rpcchainvm)

An application VM can run as a **separate OS process**, communicating with AvalancheGo over
gRPC. This is the `rpcchainvm` boundary, built on a HashiCorp-go-plugin-style handshake.

- **Plugin side** (`vms/rpcchainvm/vm.go:35`, `Serve`): the VM binary reads
  `AVALANCHE_VM_RUNTIME_ENGINE_ADDR` (`runtime/runtime.go:12`), dials the node's *runtime*
  server, sends `Initialize(protocolVersion, vmAddr)` (`gruntime/`), then serves the
  `vmpb.VM` service (`vm_server.go`). `VMServer` adapts gRPC requests onto a real
  `block.ChainVM`.
- **Node side** (`factory.go` → `vm_client.go`): `rpcchainvm.NewFactory` (`factory.go:28`)
  `os.Exec`s the binary via `subprocess.Bootstrap`, dials it, and returns a `VMClient`
  (`vm_client.go:84`) that *implements* `block.ChainVM` by translating each call into a gRPC
  request. From the chain manager's perspective the `VMClient` is just an inner `ChainVM`,
  wrapped by the same proposervm/metervm/tracedvm stack.
- **Protocol version** is `version.RPCChainVMProtocol` (`version/constants.go:18`, currently
  `45`); mismatch fails the handshake with `ErrProtocolVersionMismatch`.
- **Reverse channels — node services exposed back to the plugin.** During
  `VMClient.Initialize` (`vm_client.go:158-205`) the node stands up gRPC *servers* for the
  resources the VM needs, and passes their addresses in `InitializeRequest`:
  - the **database** over gRPC (`database/rpcdb`, served at `DbServerAddr`),
  - **shared memory** for atomic cross-chain ops (`chains/atomic/gsharedmemory`),
  - the **AppSender** (`appsender.NewServer`) so the plugin can send p2p messages,
  - **validator state** (`gvalidators`), **alias lookup** (`galiasreader`), and the
    **warp signer** (`gwarp`).
  HTTP handlers returned by the VM are bridged with `ghttp/` (`http_client.go`/`http_server.go`)
  so the plugin's API handlers serve through the node's HTTP server.
- The `runtime/` package (the **VMRE**, see `runtime/README.md`) owns process lifecycle:
  `Tracker` registers a `Stopper` per VM; shutdown sends `SIGTERM`. The plugin ignores signals
  until the node signals shutdown (`vm.go:54`).

The `VMClient` embeds `*chain.State` (`vm_client.go:85`, initialized `vm_client.go:239`),
giving every rpcchainvm the uniquification/caching layer for free (§7.1).

### 6.3 AppHandler — VM-to-VM p2p

`common.VM` embeds `AppHandler` (`engine.go:350`), composed of:

- `AppRequestHandler.AppRequest(ctx, nodeID, requestID, deadline, request)` (`engine.go:356`)
- `AppResponseHandler.AppResponse(...)` and `AppRequestFailed(..., *AppError)` (`engine.go:375`)
- `AppGossipHandler.AppGossip(ctx, nodeID, msg)` (`engine.go:402`)

Outbound, the VM is given an `AppSender` (`snow/engine/common/sender.go`, AppSender block):
`SendAppRequest`, `SendAppResponse`, `SendAppError`, `SendAppGossip(ctx, SendConfig, bytes)`.
`SendConfig` selects recipients (specific NodeIDs, N validators, N non-validators, N peers).
This is the substrate VMs use for application-level gossip and request/response (e.g. mempool
gossip); the bytes are opaque to consensus. See [networking.md](networking.md) for transport.

### 6.4 VM ↔ Fx boundary (feature extensions)

An **Fx (feature extension)** is a pluggable module that defines a *family* of transferable
outputs, inputs, and credentials, plus the logic to verify that a credential authorizes
spending an output. This decouples reusable cryptographic primitives (e.g. secp256k1
multisig) from a specific VM.

- Wrapping type: `common.Fx{ ID ids.ID; Fx interface{} }` (`snow/engine/common/fx.go`),
  passed to `VM.Initialize(..., fxs []*Fx, ...)`.
- Construction: `fx.Factory` (`vms/fx/factory.go`) with `New() any`; e.g.
  `secp256k1fx.Factory` (`vms/secp256k1fx/factory.go`) and its `ID =
  "secp256k1fx"`.
- The VM-facing contract is VM-specific. The AVM defines `fxs.Fx`
  (`vms/avm/fxs/fx.go:29`): `Initialize(vm) / Bootstrapping / Bootstrapped /
  VerifyTransfer(tx,in,cred,utxo) / VerifyOperation(tx,op,cred,utxos)`. The PlatformVM uses a
  similar `VerifyPermission`-style contract.
- `secp256k1fx.Fx.Initialize` (`vms/secp256k1fx/fx.go:42`) registers its serializable types
  into the VM's codec: `TransferInput`, `MintOutput`, `TransferOutput`, `MintOperation`,
  `Credential`. At verification time it recovers signatures (using a `RecoverCache`) and
  checks them against the output's `OutputOwners`.
- The provided extensions:
  - `secp256k1fx` — standard ECDSA outputs/inputs and the `Credential` carrying signatures;
    used by both the X-Chain (AVM) and P-Chain (PlatformVM).
  - `nftfx` — non-fungible token outputs/operations (AVM).
  - `propertyfx` — mintable "property" ownership (AVM).
- `rpcchainvm.VMClient` does **not** support Fxs across the gRPC boundary: `Initialize` returns
  `errUnsupportedFXs` if `len(fxs) != 0` (`vms/rpcchainvm/vm_client.go:135`). Fxs are an
  in-process construct.

## 7. Key Behaviors & Invariants

### 7.1 Block uniquification (the central invariant)

While a block is *in consensus* — i.e. `Verify()` returned nil but `Accept`/`Reject` has not
yet been called — **every VM method that returns that block must return the *same* object
reference** the engine already holds (`vms/README.md`, "Uniquifying Blocks"). This lets the
engine rely on object identity and lets the VM avoid double-applying state.

- Before verification, or after a block is decided, a non-unique copy is acceptable.
- This requirement is hard to satisfy correctly, so `vms/components/chain/state.go` provides a
  `State` helper that wraps a VM's `getBlock`/`parseBlock`/`buildBlock` with caches:
  `verifiedBlocks` (a map — the uniquification set), plus LRU caches for decided, unverified,
  and missing blocks. `rpcchainvm` and other VMs embed it (`VMClient.State`).
- In ProposerVM this is implemented at *two* levels: the outer `verifiedBlocks` map
  (`vms/proposervm/vm.go:92`) for post-fork blocks, and a `tree.Tree` of inner blocks
  (`verifyAndRecordInnerBlk`, `vm.go:837`) so that multiple proposervm blocks wrapping the
  *same* inner block share one inner-block object.

### 7.2 Verify ⇒ Accept|Reject guarantee, idempotency

If `Verify`/`VerifyWithContext` returns nil, the engine guarantees a subsequent `Accept` or
`Reject` (unless the node shuts down). The proposervm preserves this even though it adds its
own header checks: it only calls the inner block's `Verify` once a *valid* inner block is seen,
and an inner block is **rejected only when a sibling is accepted** — because distinct
proposervm blocks can wrap the same inner block, an inner block must not be rejected merely
because one wrapper is rejected (`vms/proposervm/README.md`, validations section). `Accept`/
`Reject` must be effectively idempotent within the decided set and not be called twice on the
same processing block.

### 7.3 ProposerVM execution modes

- **Pre-fork** (before the Apricot Phase 4 activation time): blocks are `preForkBlock`s — thin
  wrappers that don't change the inner block's ID or bytes (`vm.go:759`). Only pre-fork blocks
  verify successfully.
- **Post-fork**: `postForkBlock`s attach a signed header (changing ID & serialization);
  `postForkOption`s wrap oracle-block options and are *not* signed (deterministic from their
  oracle parent).
- **Fork transition**: any block whose timestamp ≥ activation must have post-fork children.
- The proposervm maintains its own height index and may prune history
  (`height_indexed_vm.go`); `repairAcceptedChainByHeight` (`vm.go:610`) rolls the proposervm
  back to match the inner VM after a state sync or failed sync, because the two databases are
  committed non-atomically.

### 7.4 Edge cases

- **State sync rollback**: on `SetState(NormalOp)` after `StateSyncing`, proposervm repairs
  its height index (`vm.go:326-337`).
- **GetAncestors after pruning/sync**: returns `nil` (empty) on `ErrNotFound` to tell peers to
  stop asking (`batched_vm.go:66`).
- **P-Chain ahead of last accepted**: building can transiently fail until the P-Chain height
  advances; proposervm returns "don't build yet" rather than an error (`vm.go:536`).
- **Fuji P-Chain-height override**: a hard-coded grace window keeps the child P-Chain height
  pinned for legacy Fuji blocks (`vm.go:890-915`).

## 8. Configuration & Parameters (Snowman++)

Proposer windowing constants (`vms/proposervm/proposer/windower.go:24`):

| Constant | Value | Meaning |
|---|---|---|
| `WindowDuration` | 5 s | length of each proposer submission window |
| `MaxVerifyWindows` | 6 | proposers sampled for *verification*; `MaxVerifyDelay` = 30 s |
| `MaxBuildWindows` | 60 | windows considered when computing local build delay (5 min) |
| `MaxLookAheadSlots` | 720 | cap on slot search in `MinDelayForProposer` (1 h) |
| `maxSkew` | 10 s | max future timestamp tolerated (`vms/proposervm/block.go:29`) |

ProposerVM `Config` (`vms/proposervm/config.go:16`): `Upgrades`, `MinBlkDelay`
(`DefaultMinBlockDelay` = 1 s, `vm.go:51`), `NumHistoricalBlocks` (0 = keep all, `vm.go:54`),
`StakingLeafSigner` + `StakingCertLeaf` (block signing), `Registerer`.

**Proposer-selection algorithm** (`vms/proposervm/README.md`; `windower.Proposers`,
`windower.go:118`): validators active at the block's recorded P-Chain height are read from the
P-Chain, sorted canonically by NodeID, then sampled without replacement weighted by stake
using a PRNG seeded by `chainID ^ blockHeight`. Pre-Durango each sampled proposer `i` may
build after `i × WindowDuration`; post-Durango each *slot* maps to one expected proposer
(`ExpectedProposer`/`MinDelayForProposer`). After `MaxBuildWindows × WindowDuration` (or when
no validators are available — `ErrAnyoneCanPropose`) anyone may propose. The chainID in the
seed ensures different chains get different proposer sequences.

VM management: `vms.Manager` (`vms/manager.go:37`) maps a `vmID` → `vms.Factory`
(`manager.go:22`, `New(log) (interface{}, error)`) and tracks versions; `RegisterFactory`
(`manager.go:78`) instantiates once to capture `Version()`. `vms/registry` adds dynamic
discovery: `VMRegistry.Reload` (`registry/vm_registry.go`) installs newly-found plugin VMs via
the manager. `rpcchainvm.NewFactory` is the factory that launches an external binary.

## 9. Cross-References

- [overview.md](overview.md) — system map and where VMs sit.
- [consensus.md](consensus.md) — the Snowman engine that drives `ChainVM`/`Block`.
- [simplex.md](simplex.md) — the alternative consensus engine.
- [chains.md](chains.md) — chain creation; where the wrapper stack is assembled.
- [networking.md](networking.md) — transport behind `AppSender`/`AppHandler`.
- [node.md](node.md) — process lifecycle, plugin discovery, configuration.
- [platformvm.md](platformvm.md), [avm.md](avm.md), [evm.md](evm.md) — the concrete VMs and
  their Fx usage.
- [database.md](database.md) — storage (and `rpcdb` over the gRPC boundary).
- [api.md](api.md) — how `CreateHandlers`/`NewHTTPHandler` surface VM APIs.
- [primitives.md](primitives.md) — `ids`, codecs, crypto used by Fxs and blocks.
