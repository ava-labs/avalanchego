# Chains, Subnets, and Cross-Chain Atomic Memory

## 1. Purpose

This document specifies the **orchestration layer** that sits between the node and the
running blockchains: the **chain `Manager`** (`chains/`), the **`Subnet` abstraction**
(`subnets/`), and the **cross-chain atomic shared memory** (`chains/atomic/`).

The chain manager is the component that turns an abstract chain description
(`ChainID` + `SubnetID` + `VMID` + genesis) into a fully wired, running blockchain:
a database partition, a VM wrapped in a stack of decorators, a consensus engine, a
network message handler, a sender, and a router registration. Subnets group chains,
hold their consensus parameters, and track bootstrapping status. Atomic memory is the
shared database that lets one chain (e.g. the X-Chain) hand UTXOs to another chain
(e.g. the P- or C-Chain) atomically.

This document covers **wiring and orchestration**. The internals of each VM
(see [platformvm.md](platformvm.md), [avm.md](avm.md), [evm.md](evm.md)) and the
consensus algorithms themselves (see [consensus.md](consensus.md),
[simplex.md](simplex.md)) are cross-referenced, not duplicated.

## 2. Responsibilities & Scope

**In scope (`chains` package):**
- Building a chain end-to-end: DB prefixing, VM construction & wrapper stack, engine,
  bootstrapper, state-syncer, handler, sender, router registration, health checks,
  metrics.
- Selecting the engine family (Snowman linear chain vs. Avalanche DAG) from the VM type.
- Queuing and ordering chain creation (P-Chain first, others after P-Chain bootstraps).
- Chain aliasing, registrants, and the `LinearizableVM` wrapper.

**In scope (`subnets` package):**
- The `Subnet` abstraction: tracked bootstrapping chains, consensus config, and the
  validator-only/allow-list access policy.

**In scope (`chains/atomic` package):**
- The shared-memory model used for X↔P↔C atomic transfers: `Memory`, `SharedMemory`,
  `Apply`/`Get`/`Indexed`, prefixed value/index databases, atomic batch commits, and
  the gRPC plugin transport (`gsharedmemory`).

**Out of scope:** VM transaction logic, consensus voting (Snowball/Snowman/Simplex),
networking transport, P-Chain validator-set management. These are cross-referenced.

## 3. Package / File Layout

| Path | Role |
| --- | --- |
| `chains/manager.go` | The `Manager` interface and `manager` impl; the heart of chain wiring. |
| `chains/subnets.go` | `Subnets` registry: per-node map of running `subnets.Subnet` instances. |
| `chains/registrant.go` | `Registrant` interface — notified on each chain creation. |
| `chains/linearizable_vm.go` | `initializeOnLinearizeVM` / `linearizeOnInitializeVM` wrappers for DAG→linear transition. |
| `chains/test_manager.go` | No-op `Manager` for tests. |
| `subnets/subnet.go` | `Subnet` interface + `subnet` impl (bootstrap tracking, `Allower`). |
| `subnets/config.go` | `Config`: validator-only, allowed nodes, Snow/Simplex params, proposervm history. |
| `subnets/no_op_allower.go` | `Allower` that permits everything. |
| `chains/atomic/memory.go` | `Memory`: per-pair shared DB, ref-counted locks, `sharedID` hashing. |
| `chains/atomic/shared_memory.go` | `SharedMemory` interface + `sharedMemory` impl (`Get`/`Indexed`/`Apply`). |
| `chains/atomic/state.go` | `state`: value DB + trait index DB, tombstone semantics. |
| `chains/atomic/prefixes.go` | inbound/outbound × smaller/larger value/index prefix scheme. |
| `chains/atomic/writer.go` | `WriteAll`: replay & commit multiple batches atomically. |
| `chains/atomic/codec.go` | Linear codec for `dbElement` and ID pairs. |
| `chains/atomic/gsharedmemory/` | gRPC client/server exposing `SharedMemory` to out-of-process VMs. |
| `chains/atomic/README.md` | Canonical narrative for the shared-memory design. |

## 4. Core Types

### Manager (`chains/manager.go`)

- `Manager` interface — `chains/manager.go:126`. Embeds `ids.Aliaser`. Key methods:
  `QueueChainCreation` (`:133`), `AddRegistrant` (`:137`), `IsBootstrapped` (`:146`),
  `StartChainCreator` (`:150`), `Shutdown` (`:152`).
- `ChainParameters` — `chains/manager.go:156`: `{ID, SubnetID, GenesisData, VMID,
  FxIDs, CustomBeacons}`. `CustomBeacons` is only set for the P-Chain.
- `ManagerConfig` — `chains/manager.go:186`: the full set of node-supplied
  dependencies (DB, `VMManager`, `Router`, `Net`, `Validators`, `AtomicMemory`,
  `TimeoutManager`, `CriticalChains`, `Subnets`, upgrade schedule, etc.).
- `manager` struct — `chains/manager.go:250`: holds the chain map
  (`chains map[ids.ID]handler.Handler`), the blocking creation queue
  (`chainsQueue`), the `unblockChainCreatorCh` gate, and per-subsystem metric
  gatherers.
- `chain` struct — `chains/manager.go:171`: `{Name, Context, VM, Handler}` — the
  product of `buildChain`.
- Construction: `New(*ManagerConfig)` — `chains/manager.go:290`.

### Subnet (`subnets/subnet.go`, `chains/subnets.go`)

- `Subnet` interface — `subnets/subnet.go:24`. Embeds `common.BootstrapTracker` and
  `Allower`. Methods: `AddChain` (`:28`), `Config` (`:31`), `IsBootstrapped`,
  `Bootstrapped`, `AllBootstrapped`, `IsAllowed`.
- `subnet` impl — `subnets/subnet.go:36`: tracks `bootstrapping`/`bootstrapped` chain
  sets and a `PreemptionSignal` fired when the last chain finishes bootstrapping
  (`Bootstrapped`, `:63`).
- `Allower.IsAllowed` — `subnets/subnet.go:92`: permits a peer if it is this node, the
  subnet is not validator-only, the peer is a validator, or it is in `AllowedNodes`.
- `subnets.Config` — `subnets/config.go:21`: `ValidatorOnly`, `AllowedNodes`,
  `SnowParameters`/`SimplexParameters` (mutually exclusive — `ValidConsensusConfiguration`,
  `:66`), and `ProposerNumHistoricalBlocks`.
- `Subnets` registry — `chains/subnets.go:18`: node-level map from `SubnetID` to a
  live `subnets.Subnet`. `GetOrCreate` (`:28`) lazily creates a subnet, defaulting its
  config to the Primary Network config when none is supplied. `NewSubnets` (`:66`)
  requires a Primary Network config and pre-creates the Primary Network subnet.

### SharedMemory (`chains/atomic/`)

- `Memory` — `chains/atomic/memory.go:28`: owns the base `database.Database` and a map
  of ref-counted locks keyed by `sharedID`. `NewSharedMemory(chainID)` (`:41`) returns
  a per-chain view. `GetSharedDatabase`/`ReleaseSharedDatabase` (`:53`, `:64`) provide
  a locked nested prefix DB for a chain pair.
- `SharedMemory` interface — `chains/atomic/shared_memory.go:28`: `Get`, `Indexed`,
  `Apply`. The per-chain impl `sharedMemory` (`:60`) embeds `{m, thisChainID}`.
- `Requests` / `Element` — `chains/atomic/shared_memory.go:15`, `:22`: a batch of
  `RemoveRequests` and `PutRequests`; an `Element` is `{Key, Value, Traits}`.
- `state` — `chains/atomic/state.go:43`: a `valueDB` (key→`dbElement`) plus an
  `indexDB` (trait→keys via `linkeddb`). `dbElement.Present` (`:25`) implements the
  tombstone scheme.
- `sharedID(id1, id2)` — `chains/atomic/memory.go:104`: orders the two IDs, marshals
  the pair, and hashes it, so both chains derive the same shared partition.

## 5. Diagrams

### 5.1 Chain-creation wiring (`buildChain` → `createSnowmanChain`)

```
ChainParameters{ID, SubnetID, VMID, FxIDs, GenesisData}
        │
        ▼
  manager.buildChain (manager.go:479)
        │  ┌─ create chain data dir + chain logger
        │  ├─ build snow.ConsensusContext  (SharedMemory = AtomicMemory.NewSharedMemory(ID))
        │  ├─ VMManager.GetFactory(VMID).New(log)  → raw VM
        │  └─ resolve Fx factories
        │
        ├── vm is block.ChainVM ───────────────► createSnowmanChain (manager.go:1066)
        └── vm is vertex.Linearizable… ────────► createAvalancheChain (manager.go:610)

  createSnowmanChain wiring:

   base DB (ManagerConfig.DB)
     └─ meterdb  (per-chain metrics)
         └─ prefixdb[ chainID ]                       ← per-chain partition
             ├─ prefixdb["vm"]            → vmDB      (passed to VM.Initialize)
             └─ prefixdb["interval_bs"]   → bootstrap state

   VM wrapper stack (inner → outer), what the engine actually calls:
     rawVM (block.ChainVM)
        └─ tracedvm     (if TracingEnabled)
            └─ proposervm.New(...)        ← snowman++ block timing/signing
                └─ metervm  (if MeterVMEnabled)
                    └─ tracedvm "proposervm"
                        └─ block.ChangeNotifier (cn)  ← drives WaitForEvent
     vm.Initialize(ctx, vmDB, genesis, upgrade, config, fxs, messageSender)

   sender.New(ctx, MsgCreator, Net, Router, TimeoutManager, ENGINE_TYPE_CHAIN, sb)
        │
   engine gear (created in order: engine, bootstrapper, state-syncer):
     snowgetter.New(vm, sender)                      ← AllGetsServer
     smcon.Topological{Snowflake}                    ← consensus
     smeng.New(Config{Ctx, VM, Sender, Validators,
                      Params=sb.Config().SnowParameters, ...})
     smbootstrap.New(...)  → engine.Start
     syncer.New(...)       → bootstrapper.Start
        │
   handler.New(ctx, cn, vm.WaitForEvent, vdrs, ... sb, ...)
     h.SetEngineManager(EngineManager{Chain:{StateSyncer, Bootstrapper, Consensus}})
        │
        ▼
   chain{Name, Context, VM, Handler}
        │
  manager.createChain (manager.go:378) finishes the wiring:
     ├─ m.chains[ID] = handler
     ├─ Alias(ID, ID.String())
     ├─ notifyRegistrants(name, ctx, vm)
     ├─ Router.AddChain(handler)          ← messages now routable to this chain
     └─ handler.Start(...)                ← begins processing
```

`createAvalancheChain` (`manager.go:610`) builds **two** engines under one handler — a
`DAG` engine (Avalanche) and a `Chain` engine (Snowman) — plus the linearization
wrappers (§ 6.3) so the chain can transition from DAG to linear consensus.

### 5.2 Atomic-memory flow (export from ChainA, import to ChainB)

```
                base DB ("shared memory" prefix, node.go:1268 → atomic.NewMemory)
                         │
   sharedID = hash(order(ChainA, ChainB))   (memory.go:104)
                         │
        prefixdb[ sharedID ]  ← bidirectional channel for the pair
           ├─ prefix 0/1 : smaller-chain value/index DB
           └─ prefix 2/3 : larger-chain  value/index DB   (prefixes.go)

  EXPORT  (ChainA accepts an export block):
    smA = AtomicMemory.NewSharedMemory(ChainA)
    smA.Apply(                                  (shared_memory.go:115)
        { ChainB: Requests{ PutRequests:[Element{utxoKey, utxoBytes, [ownerAddrs]}] } },
        chainA_db_batch... )
      │  versiondb wraps base DB
      │  Put → written to ChainB's INBOUND state (outbound view from A)
      │  vdb.CommitBatch() + WriteAll(batch, chainA_db_batch...)
      ▼
    ONE atomic write commits {shared-memory put} ∪ {ChainA's own DB ops}.

  DISCOVER (wallet/API):
    smB.Indexed(ChainA, [ownerAddrs], ...) → UTXO bytes (shared_memory.go:85)

  IMPORT  (ChainB accepts an import block):
    smB.Apply(
        { ChainA: Requests{ RemoveRequests:[utxoKey] } },
        chainB_db_batch... )                    (shared_memory.go:115)
      │  Remove → ChainB's inbound state; if not yet present, writes a tombstone
      │           (dbElement.Present=false, state.go:148) so bootstrapping can
      │           consume a UTXO before the producing chain replays its add.
      ▼
    ONE atomic write commits {shared-memory remove} ∪ {ChainB's own DB ops}.
```

## 6. Component Boundaries & Relationships

### 6.1 Manager ↔ node

The node owns the singletons and injects them via `ManagerConfig`
(`node/node.go:1125`): the base `DB`, `VMManager`, `chainRouter`, `Net`, validator
`Manager`, `TimeoutManager`, `APIServer`, and `AtomicMemory` (`node/node.go:1269`).
The node also builds the `Subnets` registry (`node/node.go:1120`) and passes it in.

Control flow at startup:
1. Node calls `StartChainCreator(platformParams)` (`node/node.go:918`,
   `manager.go:1517`). This **synchronously** creates the P-Chain (its `VM.Initialize`
   must finish first because it seeds node-wide state), then launches
   `dispatchChainCreator` in a goroutine.
2. The P-Chain VM, once bootstrapped, queues every other chain via
   `QueueChainCreation` (`manager.go:353`), called from
   `platformvm/config.Internal.CreateChain` (`vms/platformvm/config/internal.go:108`).
3. `dispatchChainCreator` (`manager.go:1534`) blocks on `unblockChainCreatorCh` until
   the P-Chain bootstrap callback closes it (`manager.go:1165`), then drains the queue.

The manager can shut the whole node down via `ShutdownNodeFunc` if a **critical chain**
(X, P, or C — `CriticalChains`) fails to build (`manager.go:402`).

### 6.2 Manager ↔ VM / engine / handler

The manager never touches consensus voting or VM transaction logic; it only assembles
the gears and hands message routing to the `handler`:

- **VM type drives engine family** (`manager.go:554`): a `block.ChainVM` →
  `createSnowmanChain`; a `vertex.LinearizableVMWithEngine` → `createAvalancheChain`.
  Any other type → `errUnknownVMType`. A non-P-Chain using `PlatformVMID` is rejected
  (`errCreatePlatformVM`, `manager.go:480`).
- **VM wrapper stack** (inner→outer): raw VM → `tracedvm` → `proposervm` → `metervm`
  → `tracedvm` → `block.ChangeNotifier`. The `proposervm` adds snowman++ block timing
  and BLS/staking signatures; `metervm` records metrics; `ChangeNotifier` (`cn`)
  exposes `WaitForEvent` to the handler.
- **DB partitioning**: each chain gets `prefixdb[chainID]` over a per-chain `meterdb`,
  further split into `vm` and bootstrapping prefixes (`manager.go:95`–`104`). The VM
  receives only its `prefixdb["vm"]` sub-database.
- **Consensus params** come from the **subnet**, not the VM:
  `sb.Config().SnowParameters` (`manager.go:849`, `:1274`). If nil, build fails with a
  hint that Simplex may be configured. `sampleK` is clamped to the bootstrap weight.
- **Engine selection note:** in this revision the engine is always
  `smcon.Topological{SnowflakeFactory}` for the Chain engine; the handler dispatches
  Simplex messages (`snow/networking/handler/handler.go:742`) and `subnets.Config`
  carries `SimplexParameters`, but the Manager's engine construction here is
  Snowman/Avalanche. See [simplex.md](simplex.md) for the Simplex engine.
- **Handler** is built once per chain (`handler.New`) and receives the `EngineManager`
  bundling `{StateSyncer, Bootstrapper, Consensus}` for each engine family
  (`manager.go:1039`, `:1441`). The handler is the boundary between the network and
  consensus; the manager registers it with the `Router` only after the chain map and
  alias are set (`createChain`, `manager.go:459`).
- **Registrants** (`registrant.go:12`) are notified before message processing starts
  (`notifyRegistrants`, `manager.go:1573`) — this is how API handlers and indexers
  attach to a new VM.

### 6.3 Linearizable VM wrapper (`chains/linearizable_vm.go`)

The Avalanche→Snowman transition is mediated by two adapters:

- `initializeOnLinearizeVM` (`linearizable_vm.go:27`) wraps the DAG VM. When the
  Avalanche engine calls `Linearize(stopVertexID)`, this adapter instead calls
  `Initialize` on the proposervm-wrapped inner VM (`:52`) and closes
  `waitForLinearize`, switching `WaitForEvent` over to the linear VM (`:43`).
- `linearizeOnInitializeVM` (`linearizable_vm.go:71`) wraps the linear VM. Its
  `Initialize` is redirected to `Linearize(stopVertexID)` (`:82`), so the proposervm's
  normal `Initialize` call drives linearization.

`createAvalancheChain` (`manager.go:766`–`834`) builds both adapters so the same
underlying VM database and genesis serve both the DAG bootstrap phase and the
post-linearization Snowman phase. For the X-Chain, `StopVertexID` comes from
`Upgrades.CortinaXChainStopVertexID` (`manager.go:1021`).

### 6.4 Atomic memory ↔ VMs

`AtomicMemory` (`*atomic.Memory`) is a single node-wide object over the
`"shared memory"` DB prefix (`node.go:1268`). Each chain receives its own
`SharedMemory` view through `ConsensusContext.SharedMemory`
(`manager.go:511`). A VM:

- writes UTXOs to a peer chain via `Apply` with `PutRequests` (export);
- removes UTXOs it consumes via `Apply` with `RemoveRequests` (import);
- discovers spendable UTXOs via `Indexed`/`Get`.

Because the shared-memory DB and the VM's own `vmDB` descend from the same base
database, `Apply` can fold the VM's own batch into the shared-memory commit
(`WriteAll`, `writer.go:10`) so the two are **one atomic write**. Out-of-process
(plugin) VMs reach shared memory over gRPC: `gsharedmemory.Client` (`shared_memory_client.go:19`)
marshals `Apply`/`Get`/`Indexed`, and `gsharedmemory.Server` (`shared_memory_server.go:19`)
reconstructs the requests and batches against the node's local DB. `filteredBatch`
(`filtered_batch.go:12`) collapses a batch's puts/deletes before transmission.

## 7. Key Behaviors, Invariants, Ordering, Concurrency

**Chain-creation ordering.**
- The **P-Chain is always first** and is created synchronously (`StartChainCreator`,
  `manager.go:1526`); it initializes the node-wide `validatorState` (`manager.go:1126`).
- All other chains are queued and only drained after `unblockChainCreatorCh` closes,
  which happens when the P-Chain finishes bootstrapping (`bootstrapFunc`,
  `manager.go:1165`). Invariant: only chains on **tracked subnets** are queued
  (enforced by the P-Chain before calling `QueueChainCreation`).
- `QueueChainCreation` de-dups via `Subnet.AddChain` (`subnets/subnet.go:76`) — a chain
  already staged/bootstrapping is skipped (`manager.go:354`).

**Critical chains.** X, P, C are in `CriticalChains`; a build failure for one calls
`ShutdownNodeFunc(1)` (`manager.go:402`), and these chains are started without panic
recovery (`manager.go:475`).

**Subnet bootstrap tracking.** A subnet is "bootstrapped" when its `bootstrapping` set
is empty (`subnets/subnet.go:56`). The bootstrap engine is wired as the subnet's
`BootstrapTracker` (`BootstrapTracker: sb`, `manager.go:970`, `:1397`); when a chain
finishes, `Bootstrapped` (`:63`) fires the preemption signal. The node-level
`bootstrapped` health check reports any subnet still bootstrapping
(`manager.go:1474`).

**Primary Network as a special subnet.** `constants.PrimaryNetworkID` is created
eagerly in `NewSubnets` (`chains/subnets.go:80`) and its `Config` is the default for
any subnet lacking explicit config (`chains/subnets.go:39`). The P-Chain, X-Chain, and
C-Chain all run on it. Validator beacons for the P-Chain come from
`ChainParameters.CustomBeacons` rather than the shared validator manager
(`manager.go:568`).

**Atomic-memory invariants & concurrency.**
- `Apply` is **atomic**: all shared-memory mutations plus the caller's DB batches are
  committed in a single `versiondb` write (`shared_memory.go:130`–`164`).
- The underlying DB of the supplied batches **must** be the same base DB as
  SharedMemory's (interface invariant, `shared_memory.go:53`).
- **Deadlock avoidance:** `Apply` sorts `sharedID`s before locking
  (`shared_memory.go:127`) so concurrent applies acquire pair-locks in a global order.
- **Ref-counted locks:** `makeLock`/`releaseLock` (`memory.go:71`, `:88`) track callers
  per `sharedID`; the map entry is dropped at count 0. `GetSharedDatabase` returns the
  lock already held; `ReleaseSharedDatabase` must be paired one-to-one or it panics.
- **Directionality:** a chain can only **put** to a peer's inbound state and only
  **remove/read** from its own inbound state. Implemented by swapping
  `inbound`/`outbound` prefixes (`prefixes.go:33`) and ordering by chain ID.
- **Tombstones:** `RemoveValue` on a not-yet-present key writes `Present=false`
  (`state.go:148`); a later `SetValue` of that key cancels and deletes the marker
  (`state.go:86`). This lets the P-Chain consume a C-Chain UTXO during bootstrapping
  before the C-Chain replays the producing block. Double-put / double-remove are
  errors (`errDuplicatePut`, `errDuplicateRemove`).
- **Conflict checking is the VM's job:** shared memory only confirms presence; the VM
  must verify no processing ancestor block double-spends the same atomic UTXO (see
  `chains/atomic/README.md` § "Issue an import transaction").
- **Indexed pagination:** `getKeys` (`state.go:197`) iterates traits in sorted order,
  de-duplicates keys via a hashed set, and returns `lastTrait`/`lastKey` cursors.

## 8. Configuration (Subnet Configs)

Per-subnet config is loaded from `{subnetID}.json` under `--subnet-config-dir` and
maps to `subnets.Config` (`subnets/config.go:21`). See `subnets/config.md` for the
full CLI↔JSON key table.

| Field | Meaning |
| --- | --- |
| `validatorOnly` | If true, the node only exchanges this subnet's chain messages with validators (private subnet). Enforced by `IsAllowed` (`subnet.go:92`). |
| `allowedNodes` | Extra NodeIDs allowed when `validatorOnly` is true. Setting it without `validatorOnly` is an error (`config.go:77`). |
| `snowParameters` | Snowball/Snowman consensus parameters (k, alpha, beta, …). Used as `consensusParams` in engine build (`manager.go:849`). |
| `simplexParameters` | Simplex consensus parameters. Mutually exclusive with `snowParameters` (`ValidConsensusConfiguration`, `config.go:66`). |
| `consensusParameters` | Deprecated alias for `snowParameters`. |
| `proposerNumHistoricalBlocks` | Number of historical snowman++ blocks the proposervm retains per chain (`config.go:54`); 0 = keep all. Passed to `proposervm.Config` (`manager.go:787`, `:1209`). |

A subnet with neither Snow nor Simplex params set fails `ValidParameters`
(`config.go:76`) and the Manager refuses to build a chain whose `SnowParameters` are
nil (`manager.go:842`).

## 9. Cross-References

- [overview.md](overview.md) — where the chain manager fits in node startup.
- [node.md](node.md) — node wiring of `ManagerConfig`, `Subnets`, and `AtomicMemory`.
- [consensus.md](consensus.md) — Snowman/Avalanche engines selected by the Manager.
- [simplex.md](simplex.md) — the Simplex engine referenced by `subnets.Config`.
- [networking.md](networking.md) — `Router`, `Sender`, `handler`, peer/timeout managers.
- [vm-framework.md](vm-framework.md) — `block.ChainVM`, `vertex.LinearizableVM`,
  proposervm/metervm/tracedvm wrappers, `WaitForEvent`.
- [platformvm.md](platformvm.md) — the P-Chain, which queues all other chains and
  seeds validator state.
- [avm.md](avm.md), [evm.md](evm.md) — X-Chain (DAG→linearized) and C-Chain VMs.
- [database.md](database.md) — `prefixdb`, `meterdb`, `versiondb`, `linkeddb`, batches.
- [primitives.md](primitives.md) — `ids.ID`, aliasing, codecs, hashing.
- [api.md](api.md) — registrant-driven API handler attachment.
```
