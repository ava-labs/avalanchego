# SAE-EVM (`strevm`) — Streaming Asynchronous Execution C-Chain VM

> Deep dive on the in-tree, next-generation C-Chain VM. This is the detailed
> companion to the C-Chain summary in [evm.md](evm.md); read that first for the
> Coreth-vs-SAE framing and the broader EVM context. Here we go to the bottom of
> the **Streaming Asynchronous Execution (ACP-194)** model, the block/execution/
> settlement data model, the async executor, the gas-time fee engine, the hook
> extension interface, the C-Chain specialization, and the Ethereum JSON-RPC
> surface built on top of all of it.

The package internal name is **`strevm`** (`vms/saevm/README.md`); it is the
reference implementation of [ACP-194 "Streaming Asynchronous Execution"](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution).
It is under active development — Go APIs are explicitly unstable and many
behaviors are marked `TODO` in code (catalogued in §16).

---

## 1. The one idea you must understand first: Accept → Execute → Settle

Classic EVM VMs (Coreth, go-ethereum) are **synchronous**: a block is fully
executed *during* `Verify`, and its header carries the post-execution state root.
Consensus cannot accept a block until everyone has executed it.

SAE **decouples consensus from execution**. A block moves through three distinct,
monotonic lifecycle stages, each lagging the previous one:

```
   consensus              local EVM             results agreed
   agreement              execution             by consensus
   ─────────              ─────────             ─────────────
   ACCEPTED   ──(async)──►  EXECUTED  ──(≥Tau)──►  SETTLED
   (the tip)              (the EVM head)         ("safe"/"final")

   tracked by             tracked by             tracked by
   vm.last.accepted       exec.lastExecuted      vm.last.settled
```

- **Accepted** — consensus has irreversibly agreed on the block and its ordering.
  Under SAE acceptance *is* finality (no re-orgs). The block is persisted as the
  canonical block at its height, but **its transactions may not have run yet**.
- **Executed** — the local node has run the block's EVM transactions, produced
  receipts, and committed the post-execution state. Execution happens on a
  single background goroutine, strictly in height order, *after* acceptance.
- **Settled** — the execution *results* of an earlier block (its post-execution
  state root and gas-time) have been embedded into a **later** block's header,
  at least `Tau` (5 s of gas-time) after the earlier block finished executing.
  Settlement is how the network reaches consensus on execution *results*.

The header's `Root` field is therefore **reinterpreted**: in SAE it is *not* this
block's post-execution state root — it is the post-execution state root of the
**last block this block settles** (`blocks/export.go:31`, `SettledStateRoot()`).
A block's own post-execution root lives off-header in its `executionResults`.

This single design choice ripples through every part of the package: the block
data model carries settlement references, the executor is a queue, the fee engine
runs in "gas-time" rather than wall-clock, and the entire Ethereum JSON-RPC layer
has to reconcile a chain head (executed) that lags the consensus tip (accepted).

The authoritative ordering/consistency contract is `vms/saevm/docs/invariants.md`;
that doc, not the code, is declared the source of truth. Its height mapping:

| SAE stage | go-ethereum `rawdb` concept | RPC block tag |
|-----------|------------------------------|---------------|
| Accepted  | Canonical                    | `pending`     |
| Executed  | Head ("latest")              | `latest`      |
| Settled   | Finalized                    | `safe` & `finalized` |

---

## 2. Responsibilities, scope, and relation to Coreth

**SAE-EVM is responsible for:**
- Implementing a Snowman `block.ChainVM` whose blocks are accepted first and
  executed asynchronously, with a bounded execution lag.
- Running EVM transactions (via libevm/`core.ApplyTransaction`) on a single
  executor goroutine, producing receipts and committing state.
- A continuous, deterministic ACP-176 fee mechanism expressed in "gas-time".
- Cross-chain (atomic) import/export transactions bridging the UTXO world
  (X/P-chains) to the EVM account model, via AvalancheGo shared memory.
- A full Ethereum JSON-RPC surface (`eth`/`net`/`web3`/`debug`/`txpool` + custom
  `avax.*`) that presents the async chain as if it were synchronous to tooling.
- Push/pull transaction gossip over AvalancheGo's p2p SDK.

**Out of scope / deferred to others:**
- It reuses Coreth primitives (`graft/coreth` — `customtypes`, `extstate`
  multicoin, atomic-trie layout, ACP-176/226 reference impls in `vms/evm/`) but
  **replaces the consensus/execution integration**. See [evm.md §3](evm.md) for
  what "graft" means and the Coreth production VM.
- It depends on AvalancheGo's shared memory ([chains.md](chains.md)), the snow
  engine + ProposerVM wrapper ([consensus.md](consensus.md), [vm-framework.md](vm-framework.md)),
  the database stack ([database.md](database.md)), and IDs/codec/crypto
  ([primitives.md](primitives.md)).

**Generic vs C-Chain.** The package is split into a **generic SAE VM** (`sae/`,
`saexec/`, `blocks/`, `gastime/`, `hook/`, `adaptor/`, …) that knows nothing about
the C-Chain, and a **C-Chain specialization** (`cchain/`) that plugs into it via
the `hook.PointsG[T]` interface. In principle any chain could implement the hooks;
the C-Chain is the first consumer.

---

## 3. Package & file layout

```
vms/saevm/
├── README.md, docs/invariants.md   # ACP-194 ref; the consistency source-of-truth
│
│  ── generic SAE VM ──
├── adaptor/      adaptor.go          # generic ChainVM[BP] → AvalancheGo block.ChainVM
├── sae/          vm.go sae.go consensus.go blocks.go block_builder.go
│                 recovery.go always.go health.go dbconv.go http.go p2p.go rpc.go
│   └── rpc/      rpc.go server.go backend.go + per-namespace impls (§13)
├── saexec/       saexec.go execution.go context.go receipts.go subscription.go
├── blocks/       block.go execution.go settlement.go export.go access.go
│                 snow.go invariants.go execution.canoto.go
├── worstcase/    state.go             # pre-execution worst-case gas/balance bounds
│
│  ── fee / time engine ──
├── gastime/      gastime.go acp176.go config.go (+ .canoto.go)   # continuous ACP-176
├── proxytime/    proxytime.go (+ .canoto.go)                     # wall↔proxy-unit clock
├── gasprice/     estimator.go block_cache.go                     # tip/fee estimation
├── intmath/      intmath.go           # deterministic overflow-safe integer math
├── params/       params.go            # Lambda, Tau, queue bounds
│
│  ── storage ──
├── saedb/        saedb.go tracker.go  # trie/snapshot lifecycle over a KV DB
├── types/        types.go             # BlockSource, HeaderSource, ExecutionResults
│
│  ── chain extension boundary ──
├── hook/         hook.go              # the generic↔chain extension interface
│
│  ── C-Chain specialization ──
├── cchain/       vm.go api.go hooks.go
│   ├── state/    state.go codec.go    # atomic trie + shared-memory commit
│   ├── tx/       tx.go import.go export.go fx.go codec.go   # atomic txs
│   └── txpool/   txpool.go            # cross-chain (atomic) mempool
│
│  ── p2p / gossip ──
├── txgossip/     txgossip.go pushpull.go priority.go blockchain.go
│
└── (test/helpers) saetest/ blockstest/ txtest/ cmputils/ hookstest/
```

---

## 4. The generic VM abstraction & the adaptor

**Why:** AvalancheGo's `snowman.Block` puts state-mutating methods (`Verify`,
`Accept`, `Reject`) *on the block*. SAE wants blocks to be inert data and the VM
to own all mutation, so it defines its own generic VM interface and a thin adaptor.

`adaptor.ChainVM[BP BlockProperties]` (`adaptor/adaptor.go:22-37`) — Snowman-like
VM, but the block-state methods are **hoisted onto the VM**, parameterized by the
block type `BP`:

```go
type ChainVM[BP BlockProperties] interface {
    common.VM
    GetBlock(context.Context, ids.ID) (BP, error)
    ParseBlock(context.Context, []byte) (BP, error)
    BuildBlock(context.Context, *block.Context) (BP, error)   // block.Context MAY be nil
    VerifyBlock(context.Context, *block.Context, BP) error    // hoisted from snowman.Block
    AcceptBlock(context.Context, BP) error
    RejectBlock(context.Context, BP) error
    SetPreference(context.Context, ids.ID, *block.Context) error
    LastAccepted(context.Context) (ids.ID, error)
    GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
}
```

`BlockProperties` (`adaptor/adaptor.go:41-47`) is the read-only subset of
`snowman.Block`: `ID/Parent/Bytes/Height/Timestamp`. `*blocks.Block` satisfies it
(`blocks/snow.go:20-53`).

`adaptor.Convert[BP](vm) ChainVMWithContext` (`adaptor/adaptor.go:61-63`) wraps a
generic VM in `&adaptor[BP]{vm}`, producing a `ChainVMWithContext` =
`block.ChainVM` + `BuildBlockWithContextChainVM` + `SetPreferenceWithContextChainVM`
(`adaptor/adaptor.go:52-56`) — exactly what AvalancheGo consumes. The adaptor:
- passes through `common.VM` methods and the non-block-returning methods, and
- wraps block-returning results in `Block[BP]` (`adaptor/adaptor.go:71-148`), which
  implements `snowman.Block` + `block.WithVerifyContext` by delegating
  `Verify→vm.VerifyBlock`, `Accept→vm.AcceptBlock`, `Reject→vm.RejectBlock`.
- **always opts into context-aware verify** (`ShouldVerifyWithContext → true`,
  `adaptor/adaptor.go:126-128`).

So the chain layer implements `adaptor.ChainVM[*blocks.Block]`; the concrete
`*sae.VM` (via the `*sae.SinceGenesis[T]` harness, `sae/always.go:22`) satisfies it.

```
AvalancheGo snow engine
   │  block.ChainVM (+context)
   ▼
adaptor[*blocks.Block]            (adaptor/adaptor.go)
   │  ChainVM[*blocks.Block]
   ▼
sae.VM  /  cchain.VM              (the generic VM + C-Chain hooks)
```

---

## 5. The VM: struct, construction, lifecycle (`sae/vm.go`, `sae/always.go`)

### 5.1 The `VM` struct (`sae/vm.go:50-87`)

Key fields (the frontiers are the heart of the model):

- `last struct { accepted, settled atomic.Pointer[blocks.Block]; synchronous uint64 }`
  (`vm.go:66-69`) — the **accepted** and **settled** frontiers; `synchronous` is the
  height of the last pre-SAE/genesis block.
- `preference atomic.Pointer[blocks.Block]` — the block to build on.
- `exec *saexec.Executor` — owns the **executed** frontier and the async queue.
- `consensusCritical *syncMap[common.Hash, *blocks.Block]` — in-memory cache of
  blocks that are (a) undergoing a consensus decision, or (b) needed to inform
  consensus invariants (the accepted history up to & including the last-settled
  block) (`vm.go:71-74`). Entries are GC'd as blocks settle.
- `db ethdb.Database`; `xdb saetypes.ExecutionResults` (height-indexed execution
  results, separate keyspace from rawdb).
- `mempool *txgossip.Set` (the eth/legacypool mempool + bloom for gossip),
  `blockBuilder blockBuilder`, `rpcProvider *rpc.Provider`.
- Embeds `*p2p.Network`; `Peers`, `ValidatorPeers`.
- `acceptedBlocks event.FeedOf[*blocks.Block]`; `newTxs chan struct{}` (build signal).
- `toClose []io.Closer` — closed in **reverse order** in `Shutdown` (dependents
  appended after dependencies) (`vm.go:84-86`, `:405-411`).

`Config` (`vm.go:97-105`): `MempoolConfig legacypool.Config`, `DBConfig saedb.Config`,
`RPCConfig rpc.Config`, `ExcessAfterLastSynchronous gas.Gas`, `Now func() time.Time`.

### 5.2 `NewVM` construction phases (`sae/vm.go:113-304`)

`NewVM[T hook.Transaction](ctx, hooks hook.PointsG[T], cfg, snowCtx, chainConfig,
db, lastSynchronous *types.Block, sender) (*VM, error)`. The harness must supply a
**last-synchronous block** (MAY be genesis). Construction, each phase scoped so an
error is `errors.Join`'d with `vm.close()`:

1. Defaults `cfg.Now`; builds `vm`; registers the `"sae"` metrics registry.
2. Opens `xdb = hooks.ExecutionResultsDB(<ChainDataDir>/sae_execution_results)`.
3. Wraps `lastSynchronous` in `blocks.New`; records `last.synchronous`.
4. **Sync→Async boundary**: `lastSync.MarkSynchronous(...)` (self-settles the
   pre-SAE block) and `canonicaliseLastSynchronous` (idempotently writes its
   canonical/head/finalized hashes) (`vm.go:158-165`, `:311-328`).
5. **Block state recovery**: builds a `recovery`, finds the last block whose
   execution root was committed to disk, constructs `saexec.New`, **replays all
   accepted-but-unexecuted blocks** (`rec.executeAllAccepted`), rebuilds
   `consensusCritical`, and sets `last.settled`, `last.accepted`, `preference` to
   `exec.LastExecuted()` (`vm.go:168-204`). See §10 (recovery).
6. **Mempool**: `txgossip.NewBlockChain` over the executor → `legacypool.New` →
   `txpool.New` → `txgossip.NewSet` (with bloom); wires `signalNewTxsToEngine`.
7. **Block builder**: `blockBuilderG[T]{hooks, now, log, exec, mempool, source}`.
8. **P2P gossip**: registers a `gossip.System` tx-gossip handler at
   `p2p.TxGossipHandlerID`; starts two `gossip.Every` goroutines — **pull every
   1 s, push every 100 ms** (`vm.go:241-292`).
9. **RPC**: `rpc.New(chain{vm, vm.exec}, cfg.RPCConfig)` (`vm.go:294-301`).

### 5.3 Lifecycle methods

- **Initialize** — not on `*VM`; provided by the `SinceGenesis[T]` harness
  (`sae/always.go:44-72`): builds the eth DB (`newEthDB`, `dbconv.go`), sets up the
  trie DB, unmarshals genesis JSON, `core.SetupGenesisBlock`, then `NewVM(...,
  genesis.ToBlock(), ...)`. Treats the chain as **async since genesis** (genesis is
  the last-synchronous block). The C-Chain provides its own `Initialize`
  (`cchain/vm.go:52`, §12.1).
- **SetState** (`vm.go:395-398`) — stores the `snow.State` in an atomic
  (`Bootstrapping`/`NormalOp` checked during Accept/Verify).
- **WaitForEvent** (`vm.go:361-387`) — returns `common.PendingTxs` as soon as the
  mempool is non-empty; otherwise blocks on the `newTxs` channel (a buffered-size-1
  signal fed by the txpool's `NewTxsEvent` subscription, `:333-355`). Building is
  thus mempool-driven; there is **no periodic "always" builder** (the only
  background goroutines are the two gossip loops, the txpool fan-in, and the
  executor's `processQueue`).
- **CreateHandlers** (`sae/http.go:20-30`) — `{"/rpc": rpcServer, "/ws":
  rpcServer.WebsocketHandler(["*"])}`. `NewHTTPHandler` returns `nil,nil` (EVM VMs
  don't use the HTTP/2 chain-ID-routing path).
- **Shutdown** (`vm.go:401-411`) — closes `toClose` in reverse.
- **HealthCheck** (`sae/health.go:9-11`) — stub returning `nil,nil` (not yet
  implemented).

---

## 6. The block data model (`blocks/`)

### 6.1 `blocks.Block` (`blocks/block.go:31-64`)

Wraps a libevm `*types.Block` and tracks the SAE lifecycle. Must be built via
`New` (`block.go:81`). Fields:

- `b *types.Block` — the wrapped EVM block (header, txs, ExtData via libevm extras).
- `ancestry atomic.Pointer[ancestry]` (`{parent, lastSettled *Block}`,
  `settlement.go:24-26`). **Invariant** (`block.go:33-42`): non-nil **iff the block
  has not itself been settled**. On settlement the pointer is set to nil to **sever
  the linked list so ancestors can be garbage-collected** ("sacrifice the ancestors
  to the GC Overlord"). A synchronous block is always considered settled ⇒ ancestry
  nil.
- `synchronous bool` — true only for genesis / last pre-SAE block (self-settling).
- `bounds *WorstCaseBounds` — builder's predicted worst-case gas/balance bounds
  (early-warning checks during execution; §9).
- `execution atomic.Pointer[executionResults]` — non-nil **iff `MarkExecuted`
  succeeded**.
- `interimExecutionTime atomic.Pointer[proxytime.Time[gas.Gas]]` — highest-known
  gas-time during execution; lets `LastToSettleAt` reason about partial progress.
- `executed chan struct{}` / `settled chan struct{}` — closed after the respective
  stage completes; back `WaitUntilExecuted`/`WaitUntilSettled`.

`InMemoryBlockCount` + a `runtime.AddCleanup` finalizer (`block.go:66-91`) validate
that the ancestry-severing GC strategy actually frees blocks.

### 6.2 Block-as-data accessors (`blocks/export.go`)

Deliberately renamed where SAE semantics differ from go-ethereum:

- **`SettledStateRoot()` (`export.go:31`)** — wraps `types.Block.Root()`. The KEY
  reinterpretation: the header `Root` is the post-execution state root of the **last
  block this block settles**, *not* this block's own post-state.
- `BuildTime()` (`export.go:39`) — `types.Block.Time()`; the canonical inclusion
  time of the block's txs, distinct from execution time.
- `EthBlock/Body/Header/Hash/ParentHash/Number/Transactions` — straightforward.

`blocks/snow.go` implements `adaptor.BlockProperties`: `ID`=hash, `Parent`=parent
hash, `Bytes`=RLP of the eth block, `Height`=number, `Timestamp`=second-resolution
build time.

### 6.3 Execution results & the canoto payload (`blocks/execution.go`)

`executionResults` (`execution.go:42-62`) is the durable, per-block execution
output, **canoto**-serialized into `xdb`:

```go
type executionResults struct {
    byGas         gastime.Time `canoto:"value,1"`        // gas-time execution finished
    baseFee       uint256.Int  `canoto:"fixed repeated uint,2"`  // actual base fee
    receiptRoot   common.Hash  `canoto:"fixed bytes,3"`
    stateRootPost common.Hash  `canoto:"fixed bytes,4"`  // this block's OWN post-state
    ephemeralExecutionResults                            // NOT serialized
}
type ephemeralExecutionResults struct { byWall time.Time; receipts types.Receipts }
```

- Receipts are deliberately **not** canoto-encoded — they already live in rawdb
  (`rawdb.WriteReceipts`); they're carried in memory only to feed the settling
  block. Wall-time is metrics-only.
- Canoto enforces ascending field order and rejects zero values on unmarshal
  (`execution.canoto.go`).

`MarkExecuted(...)` (`execution.go:88-125`) records execution; **at most once** —
a second call is **fatal** (the DB head block would already be corrupted,
`errMarkBlockExecutedAgain`, `:161-167`). It writes the canoto result + receipts to
disk, then transitions in the strict order **Disk → Memory → Internal → External**
(`markExecutedOnDisk` then `markExecutedAfterDiskArtefacts`, `:145-175`): xdb put +
sync + (optional) set-as-head-block + batch write; then CAS the `execution` pointer
(memory); then store the internal `lastExecuted` pointer; then `close(executed)`.

Reads (`Executed`, `WaitUntilExecuted`, `ExecutedByGasTime`, `ExecutedBaseFee`,
`Receipts`, `PostExecutionStateRoot`, `:188-252`) **block on `executed`** until the
block has run. `RestoreExecutionArtefacts` (`:259`) reloads results from disk on
recovery (note: SAE does not support blob txs, `:271`).

### 6.4 Lifecycle stages & invariants (`blocks/invariants.go`)

`LifeCycleStage` = `NotExecuted(=Accepted) → Executed → Settled` (`:158-170`).
`CheckInvariants(expect)` (`:178-208`) validates the execution-pointer state matches
the expected stage, the receipt root matches `DeriveSha(receipts)`, and — for
not-yet-settled blocks — that `SettledStateRoot() == LastSettled().PostExecutionStateRoot()`.

---

## 7. The asynchronous execution engine (`saexec/`)

### 7.1 `Executor` (`saexec/saexec.go:34-52`)

Embeds the `saedb.Tracker` (state/trie lifecycle); holds `queue chan
*blocks.Block`, `lastExecuted atomic.Pointer[blocks.Block]`, geth-style event feeds
(`headEvents`, `chainEvents`, `logEvents`), a per-tx `receipts` store, a
`chainContext` (BLOCKHASH cache), `chainConfig`, `db`, `xdb`.

**Single-goroutine, FIFO, in height order:**
- `New(...)` (`saexec.go:60-99`) starts exactly one `go processQueue()`.
- The queue is a buffered channel of capacity `2 * CommitInterval` (`saexec.go:84`),
  sized so a restart's re-enqueue of every block since the last trie commit doesn't
  overflow (and to bound how far consensus may run ahead — §9).
- `Enqueue(ctx, block)` (`saexec.go:36-58`) creates per-tx receipt buffers then
  pushes to the channel; returns `errExecutorClosed` once closed.
- `processQueue()` (`saexec.go:62-98`) pops and calls `execute`. On `errFatal` it
  `log.Fatal`s (terminating the process, with a playbook link
  `github.com/ava-labs/strevm/issues/28`); any other error stops the goroutine
  (after which `Enqueue` returns `errExecutorClosed`).
- `execute(b)` (`saexec.go:102-116`) — **chain-continuity guard**: returns an error
  unless `lastExecuted.Hash() == b.ParentHash()`, which safely handles consensus
  re-delivering an already-accepted block.

### 7.2 `Execute` — the pure EVM step (`saexec/execution.go:149-278`)

`Execute(b, stateOpener, maxNumTxs, hooks, config, chainCtx, receiptStore, log)`:

1. Opens state at the **parent's post-execution root** (`parent.PostExecutionStateRoot()`),
   not the parent header root. Clones the parent's gas clock and advances it to the
   block time (`gasClock.BeforeBlock(hooks.BlockTime(header))`).
2. Computes `baseFee` from the **gas clock** (not the header), checks it against the
   worst-case bound, and writes it into the header.
3. Per-tx loop: `CheckSenderBalanceBound`, `core.ApplyTransaction` (any error →
   `errFatal`), then `perTxClock.Tick(receipt.GasUsed)` and
   `b.SetInterimExecutionTime(perTxClock)` — this publishes intra-block progress so
   `LastToSettleAt` can reason mid-block. Fixes receipt block hashes & effective gas
   price (header was mutated), and publishes each receipt into the `receiptStore`
   **per-tx** (so it's queryable the instant that tx runs).
4. End-of-block ops via `hooks.EndOfBlockOps` (atomic txs become mint/burn here,
   §11/§12): each op is balance-checked, gas-accounted, ticked, and `ApplyTo`'d.
5. `hooks.AfterExecutingBlock`, then `gasClock.AfterBlock(consumed, target, cfg)`
   rescales the clock to the post-block target (`(target,cfg)=hooks.GasConfigAfter`).

`afterExecution` (`execution.go:280-306`) commits: `StateDB.Commit` → `Tracker.MaybeCommit`
→ `Tracker.Track(root)` → **strict ordering** `b.MarkExecuted(...)` then
`sendPostExecutionEvents` (external, fires the geth chain-head/log feeds).

### 7.3 Receipts & subscriptions

- `saexec.Receipt` (`receipts.go:42-46`) couples `*types.Receipt` + `Signer` + `Tx`
  — everything an RPC response needs.
- `RecentReceipt(ctx, txHash)` (`receipts.go:66-73`) returns a receipt **as soon as
  that tx executes, even while later txs in the same block are still running**,
  blocking (honoring ctx) on an `eventual.Value` if the tx is enqueued-but-pending;
  `(nil,false,nil)` if unknown. The receipt store is a 256-bucket `syncMap` keyed by
  the first hash byte to cut lock contention; buffers are freed via a GC finalizer
  on the block.
- `sendPostExecutionEvents` (`subscription.go:12-29`) fires geth `ChainHeadEvent`,
  `ChainEvent`, and logs feeds **at execution time, not acceptance time** — so geth
  `newHeads` subscriptions track the executed head.

### 7.4 State/trie storage: `saedb.Tracker` (`saedb/tracker.go`)

The `Tracker` adds trie/snapshot lifecycle management on top of a flat KV database:
- `Track(root)`/`Untrack(root)` (`tracker.go:88-171`) ref-count in-memory state roots
  in hash-DB mode (no-op for path-DB); the VM untracks a block's post-state once it
  is no longer consensus-critical.
- `MaybeCommit(settledRoot, executionRoot, height)` (`tracker.go:107-128`) — the
  commit policy where the execution-vs-settlement split surfaces in storage:
  **archival** nodes commit every `executionRoot`; non-archival nodes commit the
  `settledRoot` only at `CommitInterval` (default 4096) boundaries.
- `Close(lastRoot)` (`tracker.go:186-205`) **flattens snapshot layers to disk**
  (no journaling) — valid precisely because SAE never re-orgs.

---

## 8. Settlement in depth (`blocks/settlement.go`, `sae/block_builder.go`)

"Settlement" is how the network reaches consensus on **execution results**. A block
$b$ settles a contiguous range of earlier executed blocks by embedding the last
one's post-execution root + gas-time into $b$'s header.

### 8.1 Which blocks a block settles

- `Settles()` (`settlement.go:189-194`) returns the half-open range
  `(parent.LastSettled().Height(), b.LastSettled().Height()]`. **Every executed
  block is settled by exactly one later block** (disjoint contiguous ranges). A
  synchronous block settles itself.
- `Range(start, end)` (`settlement.go:204-221`) returns blocks in `(start, end]`
  ascending.

### 8.2 The `Tau` delay and `LastToSettleAt`

`LastToSettleAt(hooks, settleAt, parent)` (`settlement.go:237-314`) returns the last
block that has finished executing by gas-time `settleAt` (and whose child has *not*),
plus an `ok` flag. `ok=false` means the execution stream is lagging and the answer
isn't yet determinable — the caller must retry later. Walking up from `parent` it
classifies each block by comparing `hooks.BlockTime`, its `interimExecutionTime`,
and its final `execution.byGas` against `settleAt`. The loop is guaranteed to
terminate because the synchronous (genesis/last-pre-SAE) block is always settled.

In the builder, `lastToSettle` (`block_builder.go:341-375`) calls
`LastToSettleAt(hooks, blockTime − Tau, parent)` — **a block settles whatever was
last to finish executing by `blockTime − Tau` of gas-time**. It also enforces the
block-time floor `blockTime.Unix() ≥ TauSeconds`, monotonic ≥ parent time, and
`≤ now + maxFutureBlockDuration(10 s)`. If `!ok` it returns `errExecutionLagging`.

`Tau = 5 s` (`params/params.go:20`). Critically, **settlement affects neither
availability nor finality** (both are immediate at acceptance / per-tx execution);
it only governs when results enter a header.

### 8.3 Settlement at acceptance (`sae/consensus.go:AcceptBlock`)

`AcceptBlock` (`consensus.go:41-123`) is annotated step-by-step with the formal
Disk→Memory→Internal→External notation from `invariants.md`:

1. **Disk batch**: if `Settles()` is non-empty write `rawdb.WriteFinalizedBlockHash(b.LastSettled().Hash())`
   (SAFE==FINALIZED; LAST reserved for last-*executed*); write the block, tx-lookup
   entries, and the canonical hash (= "accepted"). One atomic `batch.Write()`.
2. **Settle** each block in `b.Settles()` in ascending order via
   `s.MarkSettled(&vm.last.settled)`.
3. **Internal/external**: store `last.accepted`, send the `acceptedBlocks` event,
   then **`vm.exec.Enqueue(ctx, b)`** — *acceptance is what schedules execution*.
4. **Bootstrapping backpressure**: if `consensusState == snow.Bootstrapping`,
   **synchronously** `b.WaitUntilExecuted(ctx)` — because the bootstrap engine loops
   Verify→Accept and treats any error as fatal, so the VM must not let acceptance
   outrun execution during bootstrap.
5. **GC** consensus-critical blocks below the new last-settled.

`RejectBlock` (`consensus.go:131-134`) is a near no-op (only deletes from
`consensusCritical`): execution happens only after acceptance and there are no
re-orgs, so a rejected block was never executed and nothing rolls back.

`MarkSettled` (`settlement.go:39-64`) CASes `ancestry → nil` (re-settle =
`errBlockResettled`; a concurrent ancestry change is **fatal**), stores the settled
pointer, and closes `settled`.

---

## 9. Block building & worst-case accounting (`sae/block_builder.go`, `worstcase/`)

Because execution lags, the builder cannot know a candidate tx's exact post-state.
It instead uses **worst-case accounting** to only include txs guaranteed valid at
execution time, and writes worst-case bounds into the block for the executor to
sanity-check.

### 9.1 Build / rebuild / verify

- `build` (`block_builder.go:60-72`) pulls `mempool.TransactionsByPriority` and the
  hook's `BlockBuilder`.
- `rebuild` (`:74-124`), used during `Verify`, deterministically reconstructs a
  peer's proposed block: recover senders, convert the candidate's txs to lazy txs,
  obtain a deterministic builder via `hooks.BlockRebuilderFrom(ethBlock)`, then run
  the same `buildWithTxs` path. **Verification = rebuild-and-compare-hashes**
  (`sae/blocks.go:119-155`): `VerifyBlock` rebuilds and errors with `errHashMismatch`
  on any divergence, then copies ancestry and stores the block in `consensusCritical`.
- `buildWithTxs` (`:135-339`): build the header (`builder.BuildHeader`); determine
  the settled block (`lastToSettle`); replay unsettled ancestors through a
  `worstcase.State`; **set `hdr.Root = lastSettled.PostExecutionStateRoot()`** (the
  settled root, §1); set `GasLimit`/`BaseFee` from the worst-case state; greedily
  include candidate txs (each validated by `state.ApplyTx` first); include
  end-of-block ops; gather settled receipts; call `builder.BuildBlock(...)` with the
  `hook.Settled{Height, GasUnix, GasNumerator, Excess}` struct; finally
  `blocks.New(...)` + `SetWorstCaseBounds`.

### 9.2 `worstcase.State` (`worstcase/state.go`) and the execution-lag bound

Built on a **settled** block (`NewState`, `:67-88`); opens state at its post-exec
root; clock = its execution gas-time. The crucial mechanism that **bounds how far
consensus can run ahead of execution**: `StartBlock` (`:111-143`) computes a
worst-case max block size and, if `qSize > MaxFullBlocksInOpenQueue * maxBlockSize`,
returns `ErrQueueFull` — so **blocks cannot be built once the unexecuted queue
exceeds ~2 full blocks of gas** (the builder treats this as normal backpressure,
logged at Debug). `ApplyTx`/`Apply` (`:180-322`) mirror the executor's checks
(validate the tx, recover sender, worst-case balance via the `txToOp` burn model,
gas-limit/fee-cap/nonce checks) so an included tx cannot fail at execution. The soft
bounds (`CheckBaseFeeBound`, `CheckSenderBalanceBound`, `CheckOpBurnerBalanceBounds`,
`blocks/invariants.go:54-105`) log errors but never fail execution — they are an
early-warning system for mispredicted bounds.

---

## 10. Recovery on restart (`sae/recovery.go`)

On startup the trie DB may be committed behind the last accepted block. Recovery
(`recovery.go`) rebuilds in-memory state:

- `lastCommittedBlock()` (`:47-66`) finds the highest block whose execution root is
  durable on disk (`saedb.LastHeightWithExecutionRootCommitted`); this becomes the
  executor's starting `lastExecuted`.
- `executeAllAccepted(ctx, exec)` (`:82-107`) **re-enqueues every canonical block
  after the last-executed and waits** — the replay of accepted-but-unexecuted
  blocks. The executor queue capacity (`2*CommitInterval`) is sized to absorb it.
- `consensusCriticalBlocks(exec)` (`:117-202`) walks back from the last-executed,
  restoring each block's execution artefacts, re-deriving settlement, reconstructing
  ancestry, wiring the `syncMap`'s Track/Untrack callbacks to the executor's
  `Tracker`, and validating each block's invariants.

---

## 11. Gas-time & the continuous ACP-176 fee engine

SAE expresses fees and timing in **gas-time** — a clock that advances by gas
consumed rather than wall-clock seconds. This makes the fee mechanism a *continuous*
re-derivation of the block-discrete ACP-176 (`vms/evm/acp176`).

### 11.1 `proxytime` (`vms/saevm/proxytime/proxytime.go`)

`Time[D ~uint64]` models time measured in an arbitrary unit `D` at a configurable
`hertz` (units/second), with invariant `fraction < hertz`. In gastime, `D = gas.Gas`
and `hertz = 2·target`. Operations are **strictly monotonic** — `Tick` and
`FastForwardTo` only advance; `SetRate` rounds the fraction *up* to never move
backwards. All arithmetic uses `math/bits` 128-bit primitives for determinism and
overflow safety.

### 11.2 `gastime.Time` (`vms/saevm/gastime/gastime.go`, `acp176.go`)

Embeds `*proxytime.Time[gas.Gas]` plus ACP-176 `target` (T) and `excess` (x) and a
`GasPriceConfig`. Key constants/formulas:

- `TargetToRate = 2` ⇒ rate `R = 2·T`; **1 second of gas-time ⇔ consuming `2·T` gas**.
- Excess scaling factor `K = TargetToExcessScaling · T` (default `87·T`, where
  `87 ≈ 60/ln 2` ⇒ base fee can at most double ~every 60 s of sustained 2× load).
- **Base fee** `Price = max(MinPrice, MinPrice · e^(x/K))` via `gas.CalculatePrice`
  (the EIP-4844 "fake exponential", `vms/components/gas/gas.go:85`).
- `Tick(g)` (`gastime.go:153`): advance proxytime by `g`, then `excess += g·(R−T)/R
  = g/2` (since R=2T) — the continuous analog of ACP-176 `ConsumeGas`.
- `FastForwardTo` (`:168`): on idle, decay `excess` by `s·T + f·T/R` — the analog of
  ACP-176 `AdvanceTime`.
- `AfterBlock(used, target, cfg)` (`acp176.go:44`): the per-block commit — tick by
  gas used, rescale excess across any target/config change (keeping price constant),
  update target & rate, enforce min-excess.

`GasPriceConfig` (`gastime/config.go`): `TargetToExcessScaling` (must be non-zero,
default 87), `MinPrice` (default 1), `StaticPricing` (pins price at `MinPrice`,
forces excess 0). All canoto-serialized.

### 11.3 Other fee/math packages

- `intmath` (`intmath/intmath.go`) — `BoundedAdd/Sub/Multiply`, `MulDiv`,
  `MulDivCeil`, `CeilDiv`. **`CeilDiv` implements the ACP-194 minimum gas charge**:
  a tx with gas limit `g` is charged at least `ceil(g/Lambda)` (`hook.MinimumGasConsumption`,
  `Lambda = 2`).
- `gasprice` (`gasprice/estimator.go`, `block_cache.go`) — suggests a tip from a
  **gas-weighted 40th-percentile** of recent accepted-block tips (40th, deliberately
  below median, to avoid a self-induced fee spiral), and serves `eth_feeHistory`.
  Note it accumulates tx *gas limits* (not gas charged) because SAE sequences txs
  without executing them at build time. The next-block base-fee bound comes from the
  last-accepted block's `WorstCaseBounds().LatestEndTime.BaseFee()`.
- `params` (`params/params.go`): `Lambda=2`, `Tau=5s`/`TauSeconds=5`,
  `MaxFullBlocksInOpenQueue=2`, `MaxFullBlocksInClosedQueue=3`,
  `MaxQueueWallTime = 3·5s·2 = 30 s` (max wall time a block should sit in the queue).

ACP-226 (`vms/evm/acp226`) provides a dynamic minimum block delay using the same
exponential kernel; it complements gas-time pacing. (Both are referenced by C-Chain
header construction but several fields are still hardcoded — §16.)

---

## 12. The hook extension interface (`hook/hook.go`)

This is the boundary between the generic SAE VM and any specializing chain. The
generic VM is parameterized by `hook.PointsG[T]` and `T : hook.Transaction`.

`PointsG[T]` (`hook.go:43-52`) = `Points` + `BlockBuilder[T]` + `BlockRebuilderFrom`.

`Points` (`hook.go:56-85`) — non-generic extension points the VM calls:
- `ExecutionResultsDB(dataDir)` — open the height-indexed execution-results DB.
- `GasConfigAfter(*Header) (target, GasPriceConfig)` — fee config effective after a block.
- `BlockTime(*Header) time.Time` — exact block time (may include sub-second).
- `SettledBy(*Header) Settled` — settlement info carried by a header.
- `EndOfBlockOps(*Block) ([]Op, error)` — non-EVM ops applied after EVM txs.
- `CanExecuteTransaction`, `BeforeExecutingBlock`, `AfterExecutingBlock` — execution hooks.

`BlockBuilder[T]` (`hook.go:88-128`): `BuildHeader`, `PotentialEndOfBlockOps` (an
`iter.Seq[T]` of candidate custom txs), `BuildBlock`. `BlockRebuilderFrom(ethBlock)`
must reproduce an **identical** block (used in verification).

`Op` (`hook.go:151-198`) — the unit of non-EVM state change, and the mint/burn
primitive: `{ID, Gas, GasFeeCap, Burn map[addr]AccountDebit, Mint map[addr]uint256}`.
`Op.ApplyTo(stateDB)` validates each burn's `MinBalance`/`Amount`/balance, increments
the **state's** nonce (replay-safe), `SubBalance`s, then `AddBalance`s the mints
(sum of mints MAY exceed sum of burns). `Settled` (`hook.go:210-215`) = `{Height,
GasUnix, GasNumerator, Excess}` — enough to reconstruct the settled gas clock.

---

## 13. The C-Chain specialization (`cchain/`)

### 13.1 `cchain.VM` wiring (`cchain/vm.go`)

`VM` embeds `*sae.VM` and adds `state *state.State`, `txpool *txpool.Txpool`, and an
`onClose` teardown list. `Initialize` (`vm.go:52-130`):
- Opens the eth DB under **`prefixdb.NewNested("ethdb")` (uncompacted)** for
  byte-compatibility with the layout Coreth used as an rpcchainvm plugin (`vm.go:73-76`).
- Parses genesis as a `core.Genesis` (TODO: move to Coreth's genesis format),
  `core.SetupGenesisBlock`.
- Builds the atomic `state.New`, the atomic `txpool.NewPending`, and the
  `hooks := newHooks(snowCtx, state, pending)`, then `sae.NewVM(ctx, hooks, ...,
  genesis.ToBlock(), appSender)`.
- Mempool config uses `NoLocals = true` (no preferential treatment for locally
  submitted txs).
- `CreateHandlers` merges the SAE `/rpc` + `/ws` handlers with the C-Chain `avax`
  service mounted at `/avax`. `WaitForEvent` races the SAE event loop against the
  atomic txpool's `AwaitTxs`.

### 13.2 The C-Chain hooks (`cchain/hooks.go`)

`hooks` embeds a `builder` + the atomic `state`. Behaviors:
- `BlockRebuilderFrom` parses atomic txs from `customtypes.BlockExtData` and returns
  a deterministic builder fixed to those txs and the block's time — guaranteeing
  identical reconstruction.
- `EndOfBlockOps` parses ext-data atomic txs and converts each to a `hook.Op`
  (this is how atomic txs feed the mint/burn engine).
- `AfterExecutingBlock` wraps the statedb in `extstate` (multicoin), applies
  non-AVAX transfers per tx, then `state.Apply(height, txs)` to commit the atomic
  trie + shared memory.
- `BuildHeader`/`BuildBlock` construct/marshal the libevm block with atomic txs in
  ExtData; atomic-tx gas now counts in `Header.GasUsed` (no longer `ExtDataGasUsed`).
- `GasConfigAfter`/`SettledBy`/`BlockTime` are partly **hardcoded** today (target
  `1_000_000`, scaling `87`, min price `1`; zero `Settled`; second-resolution time)
  — flagged TODO to extract from the header (§16).

### 13.3 Atomic (cross-chain) transactions (`cchain/tx/`)

Atomic txs bridge the UTXO world to the EVM account model. They are **not** EVM
transactions — they ride in the block's **ExtData** and are applied as end-of-block
`hook.Op`s after the EVM txs.

- `Tx = {Unsigned, Creds}` (`tx.go:53-56`); `Unsigned` is implemented only by
  `*Import` and `*Export`. `Tx.ID() = Hash256(Bytes())`.
- **Gas/fee model** (`tx.go:165-233`): `gasUsed = AtomicTxIntrinsicGas + numBytes +
  numSigs·CostPerSignature`; `gasPrice = ScaleAVAX(burned)/gasUsed`.
- **AVAX denomination scaling** (`tx.go:202-220`): `ScaleAVAX(v) = v · 1e9` converts
  X/P-chain **nAVAX (uint64)** to C-chain **aAVAX (uint256, wei-like)**.

`Import` (`tx/import.go`) — foreign-chain → C-Chain, **mints** AVAX:
- Carries `ImportedInputs []*avax.TransferableInput` (consumed source UTXOs) and
  `Outs []Output` (EVM addresses to credit).
- `verifyCredentials` fetches the UTXOs from shared memory (`sm.Get(SourceChain,
  utxoIDs)`) and runs `secp256k1fx.VerifyTransfer`.
- `asOp` builds `Mint[addr] += ScaleAVAX(amount)` (the mint side);
  `atomicRequests` returns `RemoveRequests` (consume the source UTXOs).

`Export` (`tx/export.go`) — C-Chain → foreign chain, **burns** AVAX:
- Carries `Ins []Input` (EVM address+nonce+amount) and `ExportedOutputs
  []*avax.TransferableOutput`.
- Input identity is `AccountInputID(address, nonce)` (8-byte nonce ++ 20-byte
  address), guaranteed not to collide with UTXO IDs.
- `verifyCredentials` recovers the signing pubkey (cached `secp256k1.RecoverCache`)
  and checks `pk.EthAddress() == in.Address`.
- `asOp` builds `Burn[addr] = {Nonce, Amount=ScaleAVAX(amount), MinBalance=Amount}`
  (the burn side); `atomicRequests` returns `PutRequests` (produce destination UTXOs).

`fx.go` provides a package-level bootstrapped `secp256k1fx.Fx` for UTXO signature
verification. `codec.go` uses deliberate `SkipRegistrations` so that imported/exported
UTXOs serialize **byte-identically to the P/X-chains** (cross-chain shared-memory
compatibility).

### 13.4 Atomic state & shared-memory commit (`cchain/state/state.go`)

`State` manages an **atomic trie** whose DB prefixes deliberately match old Coreth
(`atomicTrieDB`, `atomicTrieMetaDB`, `atomicTxDB`) so no migration is needed. The
trie is built over `prefixdb.NewNested(triePrefix, db)` (nested + uncompressed to
match Coreth's versiondb wrapping).

`Apply(height, txs)` (`state.go:139-189`) — the commit path:
1. **Idempotent**: no-op when `height ≤ currentHeight` (shared memory is not safe to
   re-apply on restart).
2. Sorts txs by `ID()` (required for a byte-identical trie), merges per-chain
   `Put`/`Remove` requests, updates the trie root.
3. Builds a local DB batch (txs + last-height + root) and calls
   **`snowCtx.SharedMemory.Apply(ops, batch)`** — committing the local batch
   **atomically with the shared-memory mutation**, so a crash cannot double-apply.

This is the single bridge between the C-Chain and AvalancheGo's cross-chain
[atomic shared memory](chains.md).

### 13.5 The atomic mempool (`cchain/txpool/txpool.go`)

A **separate** cross-chain tx pool, distinct from the eth/legacypool mempool inside
`sae.VM`. It validates against the **executed** state (an *unbuffered* subscription
ensures it never references state older than last-settled, which SAE may have
pruned). `Add` (`txpool.go:181-235`): sanity-check → verify credentials against
shared memory → verify the op against last-executed state → insert into a min-heap
ordered by `GasFeeCap`, evicting/rejecting on UTXO-input conflicts and at the 1024
cap (strict fee-beats-to-evict). Conflict detection is **UTXO/account-input-set
based**, not nonce-gap based as in a standard eth pool. `Iter()` yields txs in
**decreasing** gas-price order for block building.

### 13.6 The C-Chain `avax.*` API (`cchain/api.go`)

A gorilla-RPC service mounted at `/ext/bc/C/avax`: `GetUTXOs` (UTXOs exported *to*
the C-Chain, paginated via `SharedMemory.Indexed`), `IssueTx` (submit an atomic tx
to the atomic txpool), `GetAtomicTx` (look up a stored atomic tx + its block height).

---

## 14. The Ethereum JSON-RPC surface over an async chain (`sae/rpc/`)

The hardest external-interface problem: present an async chain (head lags the tip)
as if it were a synchronous Ethereum chain to unmodified tooling (MetaMask, ethers,
etc.). The whole `rpc/` package is built around reconciling the three frontiers.

### 14.1 Structure

`rpc.New` (`rpc/rpc.go:82`) builds a `backend` that satisfies the union
`GethBackends = ethapi.Backend + tracers.Backend + filters.Backend + BloomOverrider`
(`backend.go:20-25`) by composing the VM's `Chain` interface (`rpc/rpc.go:37-59`)
with a gas-price estimator, the mempool, and a bloom indexer. `server` (`server.go:28`)
registers the namespaces: **`web3`, `net`, `txpool`, `eth`** (split across several
backend objects), plus **`debug`** gated on config (`EnableDBInspecting` /
`EnableProfiling` / `!DisableTracing`).

### 14.2 How async is hidden — the key tricks

- **Block-tag mapping** (`blocks/access.go:114-134`): `latest` = executed head,
  `pending` = accepted head, `safe`/`finalized` = settled head. `CurrentHeader` /
  the bloom indexer track the **executed** head (`indexing.go:29-31`).
- **Only canonical blocks are served** (`readers.go:31-33`): canonical ⇒ eventually
  executed ⇒ eventually has results; non-canonical → `ErrNonCanonicalBlock`; a height
  above the accepted tip → `ErrFutureBlockNotResolved`.
- **Faked synchronous header** (`stateful.go:86-131`): geth's `ethapi` reads the
  state root and base fee from header fields, assuming synchronous execution. So
  `StateAndHeaderByNumberOrHash` **overwrites** `hdr.Root` with the block's
  *post-execution* state root and `hdr.BaseFee` with the executed base fee before
  opening state. (Marked brittle — relies on the faked header hash never being used
  downstream.)
- **Receipts for unexecuted blocks return `nil`, not an error** (`receipts.go`):
  `getReceipts` returns nils for not-yet-executed blocks; `GetBlockReceipts` and
  `PendingBlockAndReceipts` deliberately return nil to avoid breaking geth.
- **Per-tx receipt immediacy** (`immediateReceipts`, `receipts.go:102-126`):
  `eth_getTransactionReceipt` first tries the executor's `RecentReceipt` cache
  (available the instant a tx executes, even mid-block), then falls back to disk.
- **No-op subscriptions** (`subscriptions.go:18-32`): chain-side, removed-logs, and
  pending-logs subscriptions are no-ops — SAE has no re-orgs and "pending logs are an
  oxymoron". `newHeads` fires on **execution** (chain-head events come from the
  executor).
- **Bloom override** (`indexing.go:44-50`): the indexer recomputes each block's bloom
  from persisted receipts rather than trusting the header's bloom (which is the
  *settled* block's bloom).

### 14.3 Custom `eth_*` Avalanche extensions (`custom.go`)

`eth_getChainConfig`, `eth_baseFee` (upper-bound estimate of the next block's base
fee from `WorstCaseBounds`), `eth_callDetailed` (returns gas/revert-decoded errors),
`eth_suggestPriceOptions` (slow/normal/fast tiers, doubling the base-fee estimate to
stay valid across rising blocks), and `eth_newAcceptedTransactions` (a subscription
firing on **acceptance, before execution**).

### 14.4 Transaction gossip (`txgossip/`)

A clean **MECE split** (`pushpull.go:24-28`): RPC-submitted txs go through `SendTx`
(added locally + **push-gossiped**, but *not* added to the bloom filter); the bloom
filter exists only for **pull gossip, which happens only between validators**
(validators are advised not to expose RPC and to add txs via `BloomSet.Add`). Push
runs every 100 ms, pull every 1 s. `TransactionsByPriority` (`priority.go`) orders
the mempool by decreasing effective gas tip for block building. The mempool's view
of "the chain" (`blockchain.go`) is the **executed** head, and `StateAt` ignores its
argument and always returns the latest post-execution state.

---

## 15. Component boundaries & relationships

```
                AvalancheGo snow engine + ProposerVM wrapper   [consensus.md, vm-framework.md]
                        │  block.ChainVM (+context)
                        ▼
                  adaptor[*blocks.Block]                       (adaptor/)
                        │  ChainVM[*blocks.Block]
        ┌───────────────┴───────────────────────────────────────────────┐
        │   cchain.VM  →  sae.VM  (generic)                               │
        │     │ Accept → Enqueue                                          │
        │     ▼                                                           │
        │   saexec.Executor ── async, single goroutine ──► EVM execution  │
        │     │  state via saedb.Tracker                                  │
        │     ▼                                                           │
        │   hook.PointsG[T]  ◄── C-Chain implements (cchain/hooks.go)     │
        └───────┬───────────────────────────┬───────────────────┬────────┘
                │                            │                   │
                ▼                            ▼                   ▼
        atomic shared memory          eth JSON-RPC          p2p tx gossip
        (cchain/state →               (sae/rpc/ over        (txgossip/ over
         SharedMemory.Apply)           three frontiers)      network/p2p)
        [chains.md]                   [api.md]              [networking.md]
```

| Boundary | Interface / mechanism | Where |
|----------|----------------------|-------|
| Engine ↔ VM | `adaptor.ChainVM[*blocks.Block]` → `block.ChainVM` | `adaptor/adaptor.go` |
| Generic VM ↔ chain | `hook.PointsG[T]`, `hook.Op` (mint/burn) | `hook/hook.go`, `cchain/hooks.go` |
| Accept ↔ execute | `exec.Enqueue` (async FIFO queue) | `sae/consensus.go:85` |
| Execute ↔ state | `saedb.Tracker` / `StateDBOpener` | `saedb/tracker.go` |
| VM ↔ cross-chain | `snowCtx.SharedMemory.Apply(ops, batch)` | `cchain/state/state.go:172` |
| VM ↔ tooling | eth/net/web3/debug JSON-RPC over three frontiers | `sae/rpc/` |
| VM ↔ peers | `p2p.Network` + tx gossip handler | `sae/p2p.go`, `txgossip/` |
| Fee engine | continuous ACP-176 in gas-time | `gastime/`, `vms/evm/acp176` |

---

## 16. Key invariants, ordering, concurrency, and fatal conditions

**Lifecycle ordering** (`docs/invariants.md` is the source of truth):
- `Settled ⇒ Executed ⇒ Accepted` for any block.
- Every realization happens in the order **Disk → Memory → Internal indicator →
  External indicator** (`AcceptBlock`, `MarkExecuted`, `MarkSettled` each annotate
  this explicitly).
- `f(b_n)` side effects happen after `f(b_{n-1})` (parent-first); a block is accepted
  only after the blocks it settles have been settled.

**Concurrency model:**
- All cross-thread frontier pointers are `atomic.Pointer`; readiness is signaled by
  closing per-block `executed`/`settled` channels (after disk+memory+internal).
- Execution is **single-threaded** (one `processQueue` goroutine); the
  `lastExecuted.Hash()==b.ParentHash()` guard makes re-delivered Accepts safe.
- `consensusCritical` is a `syncMap` whose store/delete callbacks drive the
  `Tracker`'s state-root ref-counting; blocks are GC'd (ancestry severed at
  settlement) and receipt buffers freed via `runtime.AddCleanup`.
- `MarkSynchronous`/`Synchronous` are **not** concurrency-safe — they run before the
  VM starts.

**Bounds:** execution may lag consensus by at most ~`MaxFullBlocksInOpenQueue` (2)
full blocks of gas before block-building is blocked (`worstcase.State.StartBlock →
ErrQueueFull`); closed-queue cap 3; expected max queue wall time 30 s. Settlement
lags execution by ≥ `Tau` (5 s) of gas-time.

**Fatal conditions** (`log.Fatal`, process exit, playbook `strevm/issues/28`):
re-`MarkExecuted` a block; an `errFatal` from execution (a tx errored rather than
reverted, or an end-of-block op failed); a block's ancestry changing during
settlement.

**Byte-compatibility invariants** (no migration from Coreth): ethdb under
`prefixdb.NewNested("ethdb")` uncompacted; atomic-trie prefixes match Coreth; tx
codec skip-registrations preserve UTXO type IDs; the atomic trie sorts txs by ID
before merging.

**Development status / notable TODOs** (the package is pre-stability):
- C-Chain `GasConfigAfter`/`SettledBy`/`BlockTime` hardcoded — must be extracted
  from the header (`cchain/hooks.go:117-133`); ACP-176/226 excess, ms timestamps, and
  warp predicate results not yet encoded into headers (`cchain/hooks.go:193-216,
  331-335`).
- Warp/ICM message persistence is a TODO (`AfterExecutingBlock` currently discards
  receipts, `cchain/hooks.go:177`).
- Full chain-manager wiring (selecting SAE for the C-Chain) — see [evm.md](evm.md);
  Coreth remains the production C-Chain VM.
- RPC `StateAndHeaderByNumberOrHash` header-faking and tracing method-suppression are
  flagged brittle (`sae/rpc/stateful.go:33-35, 99-102`).
- The bloom indexer needs a state-sync checkpoint seed (`sae/rpc/rpc.go:103-105`).
- Coreth's atomic-trie `commitInterval` must be reduced to 1 before the SAE
  transition (`cchain/state/state.go`, issue #5375).

---

## 17. Configuration & parameters

| Parameter | Value | Meaning | Source |
|-----------|-------|---------|--------|
| `Lambda` | 2 | min gas charged per tx = `ceil(gasLimit/Lambda)` | `params/params.go:14` |
| `Tau` / `TauSeconds` | 5 s / 5 | min gas-time between a block executing and being settled | `params/params.go:20-23` |
| `MaxFullBlocksInOpenQueue` | 2 | execution-lag bound (blocks of gas) before building blocks | `params/params.go:29` |
| `MaxFullBlocksInClosedQueue` | 3 | closed-queue cap | `params/params.go:33` |
| `MaxQueueWallTime` | 30 s | max wall time a block should sit in the exec queue | `params/params.go:38` |
| `TargetToRate` | 2 | gas-time rate `R = 2·target` | `gastime/gastime.go:88` |
| `DefaultTargetToExcessScaling` | 87 | excess factor `K = 87·target` (~60 s doubling) | `gastime/config.go:33` |
| `DefaultMinPrice` | 1 | min base fee | `gastime/config.go:37` |
| default trie `CommitInterval` | 4096 | non-archival settled-root commit cadence | `saedb/tracker.go:32` |
| executor queue capacity | `2·CommitInterval` | absorbs restart replay | `saexec/saexec.go:84` |
| pull / push gossip period | 1 s / 100 ms | tx gossip cadence | `sae/vm.go:275-280` |
| `maxFutureBlockSeconds` | 10 | reject blocks too far in the future | `sae/blocks.go:33-36` |
| C-Chain hardcoded target | 1_000_000 | (TODO: from header) gas target | `cchain/hooks.go:117-123` |
| AVAX scale (`_x2cRate`) | 1e9 | nAVAX → aAVAX (wei-like) | `cchain/tx/tx.go:202` |
| atomic mempool cap | 1024 | max pending atomic txs | `cchain/vm.go` |

---

## 18. Cross-references

- [evm.md](evm.md) — C-Chain overview, Coreth (production VM), graft model, the
  SAE-vs-Coreth comparison and rpcchainvm relationship.
- [vm-framework.md](vm-framework.md) — the `block.ChainVM`/`snowman.Block` contract,
  the ProposerVM/MeterVM/TracedVM wrapper stack, AppHandler.
- [consensus.md](consensus.md) — the Snowman engine, bootstrapping, the Accept/Verify
  call sequence the VM responds to.
- [chains.md](chains.md) — cross-chain atomic shared memory consumed by the C-Chain.
- [database.md](database.md) — the database stack, prefixdb namespacing, merkledb.
- [api.md](api.md) — how `CreateHandlers` plugs the JSON-RPC server into the node API server.
- [primitives.md](primitives.md) — IDs, codec, secp256k1 (atomic-tx signatures), BLS.
- [networking.md](networking.md) — the p2p SDK and gossip the tx gossip rides on.
- [overview.md](overview.md) — top-level architecture.
