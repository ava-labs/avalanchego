# C-Chain / EVM Execution

## 1. Purpose

The **C-Chain** ("Contract Chain") is Avalanche's Ethereum-compatible smart-contract
blockchain. Unlike the X-Chain ([avm.md](avm.md)) and P-Chain ([platformvm.md](platformvm.md)),
which are **UTXO-based**, the C-Chain uses an **account-based** model and runs the
Ethereum Virtual Machine (EVM): Solidity contracts, `eth_*` JSON-RPC, ECDSA-signed
EVM transactions, accounts/balances/nonces, and a Merkle-Patricia state trie.

This document covers the two EVM VM implementations vendored in this repo and how
they relate:

- **Coreth** (`graft/coreth/`, `graft/evm/`) — the **production** C-Chain VM. A
  full go-ethereum-derived (via [libevm](https://github.com/ava-labs/libevm)) client
  that runs in-process by default (registered at `constants.EVMID`) and can also run
  as an out-of-process **rpcchainvm plugin** ([vm-framework.md](vm-framework.md)). It
  implements synchronous execution (a block is executed before it is accepted),
  atomic import/export, warp, dynamic fees, etc.
  > **Deep dive:** Coreth has its own dedicated, exhaustive spec —
  > **[coreth.md](coreth.md)** — covering the two-layer VM + extension seam,
  > synchronous Verify/Accept, libevm extras, the network-upgrade phases, dynamic
  > fees (AP3 window → ACP-176 → ACP-226), atomic txs & the atomic trie, Warp/ICM,
  > state sync, the trie backends (hashdb/pathdb/Firewood), and the JSON-RPC surface.
- **SAE-EVM** (`vms/saevm/`) — the in-tree, minimal C-Chain VM implementing
  **Streaming Asynchronous Execution** ([ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution)),
  where blocks are *accepted first and executed asynchronously afterward*. It was
  recently added (commit "sae: Implement minimal C-Chain VM") and reuses Coreth/libevm
  primitives plus shared helpers in `vms/evm/`. Its internal name is `strevm`
  (`vms/saevm/README.md:1`).
  > **Deep dive:** the SAE-EVM has its own dedicated, exhaustive spec —
  > **[saevm.md](saevm.md)** — covering the Accept→Execute→Settle model, the async
  > executor, the gas-time fee engine, the hook extension interface, the C-Chain
  > specialization, and the JSON-RPC surface. The sections below summarize SAE only
  > enough to contrast it with Coreth; consult [saevm.md](saevm.md) for the details.

Both run as a `block.ChainVM` under the **Snowman** linear consensus engine
([consensus.md](consensus.md)), and both bridge UTXOs to/from the X/P chains via
**atomic shared memory** ([chains/atomic](primitives.md)).

---

## 2. Responsibilities & Scope

The C-Chain VM is responsible for:

- **EVM execution**: applying signed EVM transactions to account state, running
  contract bytecode, producing receipts/logs, and computing a new state root.
- **Block lifecycle**: parse / build / verify / accept / reject blocks for Snowman
  (linear chain, one parent, no UTXO conflict graph).
- **Atomic import/export**: moving AVAX (and, pre-Banff, other assets) between the
  C-Chain's account state and X/P-Chain UTXOs via shared memory. This is the only
  point where the account model touches the UTXO model.
- **Dynamic fees**: base-fee / gas-target adjustment per [ACP-176](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates/README.md)
  and dynamic minimum block delay per [ACP-226](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md).
- **State storage**: persisting account/contract state in a Merkle trie backed by a
  trie database (hash-based `triedb` or **Firewood**), plus the atomic-request trie.
- **APIs**: the standard `eth`/`debug`/`txpool` namespaces (Coreth) and the
  Avalanche-specific `avax` namespace (import/export, `getUTXOs`).

**Out of scope** of this doc: line-by-line go-ethereum/libevm internals (kept
high-level), and the consensus engine itself (see [consensus.md](consensus.md)).

---

## 3. Package / File Layout

### 3.1 `graft/` — what "graft" means

`graft/` is the migration staging area for whole repos pulled into the monorepo via
subtree merge (`graft/README.md`). Grafted code does **not** initially adhere to repo
standards and is refactored in place via stacked PRs. Each graft keeps its own Go
module so it can build with its own dependency set:

- `graft/coreth/go.mod` → module `github.com/ava-labs/avalanchego/graft/coreth`,
  depends on `avalanchego`, `graft/evm`, `libevm`, `firewood-go-ethhash`.
- `graft/evm/go.mod` → module `github.com/ava-labs/avalanchego/graft/evm`,
  depends on `avalanchego`, `libevm`, `firewood`.

So "graft" = vendored/forked upstream code (Coreth + shared evm libs), tracked as a
separate module, gradually migrated toward `vms/`.

### 3.2 `graft/coreth/` — production C-Chain VM (high level)

| Path | Role |
|------|------|
| `graft/coreth/plugin/main.go` | Plugin entrypoint: registers libevm extras, serves the VM as an **rpcchainvm** over gRPC (`rpcchainvm.Serve(..., factory.NewPluginVM())`, `graft/coreth/plugin/main.go:36`). |
| `graft/coreth/plugin/factory/factory.go` | `vms.Factory` with VM ID `evm` (`factory.go:18`); wraps `&evm.VM{}` with the atomic VM (`atomicvm.WrapVM`). |
| `graft/coreth/plugin/evm/vm.go` | The core `evm.VM` implementing `block.ChainVM` (synchronous execution). |
| `graft/coreth/plugin/evm/atomic/` | Atomic transactions (import/export), atomic state trie, atomic mempool, atomic-aware VM wrapper. |
| `graft/coreth/core/` | EVM blockchain, state processing, `extstate` (multicoin balances). |
| `graft/coreth/eth/`, `internal/`, `ethclient/` | `eth`/`debug` RPC backends, gas price oracle, filters. |
| `graft/coreth/consensus/`, `miner/` | Dummy consensus engine + block builder. |
| `graft/coreth/params/` | Avalanche upgrade schedule (Apricot, Banff, Cortina, Durango, Etna, Granite, …). |
| `graft/coreth/warp/` | Avalanche Warp Messaging (cross-subnet signed messages). |

### 3.3 `graft/evm/` — shared avalanche-EVM libraries

Own module; reusable building blocks not specific to the C-Chain:
`graft/evm/firewood/` (Firewood trie DB integration), `graft/evm/triedb/`,
`graft/evm/sync/` (state sync), `graft/evm/message/` (P2P app messages),
`graft/evm/precompileconfig/`, `graft/evm/rpc/`, `graft/evm/constants/`,
`graft/evm/utils/`. Imported by both Coreth and `vms/saevm/`.

### 3.4 `vms/evm/` — shared avalanche-EVM helpers (in `avalanchego` module)

| Path | Role |
|------|------|
| `vms/evm/acp176/` | ACP-176 dynamic gas target & price-discovery fee state machine (`acp176.go`). |
| `vms/evm/acp226/` | ACP-226 dynamic minimum block delay (`acp226.go`). |
| `vms/evm/database/` | `ethdb`↔`avalanchego database` adaptor (`database.New`). |
| `vms/evm/predicate/` | Transaction predicate (e.g. warp) encoding into block headers. |
| `vms/evm/sync/`, `vms/evm/uptimetracker/`, `vms/evm/emulate/`, `vms/evm/metrics/` | State-sync helpers, validator-uptime tracking, EVM emulation, metrics. |

### 3.5 `vms/saevm/` — SAE-EVM (in-tree minimal C-Chain VM)

| Path | Role |
|------|------|
| `vms/saevm/sae/` | Generic SAE `VM` (consensus-facing, not C-Chain-specific). `vm.go` (construction/lifecycle), `consensus.go` (Accept/Reject/SetPreference), `blocks.go` (Parse/Build/Verify/GetBlock), `block_builder.go`, `rpc/` (`eth_*` backend). |
| `vms/saevm/cchain/` | The **C-Chain** layer atop `sae.VM`: `vm.go` (`Initialize`), `hooks.go` (block-build/exec hooks + tx↔Op adaptation), `api.go` (`avax` RPC: `issueTx`, `getUTXOs`, `getAtomicTx`), `state/` (atomic trie + tx index), `tx/` (Import/Export tx types), `txpool/` (atomic tx pool). |
| `vms/saevm/blocks/` | SAE `Block` wrapping `types.Block` with async-execution + settlement state. `block.go`, `execution.go` (MarkExecuted), `settlement.go` (MarkSettled/Synchronous), `invariants.go`. |
| `vms/saevm/saexec/` | The **async execution engine** (`Executor`): a FIFO queue of accepted blocks executed on a background goroutine. `saexec.go`, `execution.go`. |
| `vms/saevm/saedb/` | Trie-DB lifecycle + reference-counted state-root retention (`Tracker`). |
| `vms/saevm/adaptor/` | Generic `ChainVM[BP]` → Snowman `block.ChainVM` adaptor (block doesn't need to know the VM). |
| `vms/saevm/hook/` | `hook.Points` interface: the injection points a chain (e.g. cchain) provides to the generic SAE VM, plus `hook.Op` (non-EVM balance mutations). |
| `vms/saevm/gastime/` | ACP-176 fee state coupled with a gas-clock (`gastime.go`, `acp176.go`). |
| `vms/saevm/gasprice/` | Gas-tip / fee-history estimator for RPC. |
| `vms/saevm/proxytime/` | "Gas time": a virtual clock measured in gas, used for settlement timing. |
| `vms/saevm/params/` | SAE constants: `Lambda` (min gas/tx denominator), `Tau` (settlement delay). |
| `vms/saevm/txgossip/` | EVM-tx mempool gossip over the `network/p2p` gossip system. |
| `vms/saevm/types/` | Shared interfaces: `ExecutionResults`, `BlockSource`, `HeaderSource`. |

---

## 4. Core Types & Integration Points

### 4.1 SAE VM and the Snowman boundary

- `sae.VM` — generic SAE machine; implements everything except `Initialize`
  (`vms/saevm/sae/vm.go:50`). Holds the `ethdb.Database`, the async `Executor`,
  the mempool, the p2p network, the RPC provider, and atomic pointers to
  `preference`, `last.accepted`, `last.settled` blocks.
- `adaptor.ChainVM[BP]` / `adaptor.Convert` — converts a generic VM into a Snowman
  `block.ChainVM` (`vms/saevm/adaptor/adaptor.go:22`, `:61`). `Block.Accept/Reject/Verify`
  delegate to `AcceptBlock`/`RejectBlock`/`VerifyBlock` on the VM
  (`adaptor.go:116`-`:133`), so SAE blocks carry no VM reference of their own.
- `cchain.VM` — embeds `*sae.VM` and adds cross-chain pieces
  (`vms/saevm/cchain/vm.go:36`). `cchain.VM.Initialize` is the harness that builds
  the `ethdb`, genesis, atomic `state.State`, hooks, and the underlying `sae.VM`
  (`cchain/vm.go:52`).

### 4.2 Hook points (chain → generic SAE VM)

`hook.PointsG[T]` is how `cchain` injects C-Chain behaviour into the generic engine
(`vms/saevm/hook/hook.go:43`). Key methods (implemented by `cchain.hooks`,
`vms/saevm/cchain/hooks.go:44`):

- `BuildHeader` / `BuildBlock` / `BlockRebuilderFrom` — header & block assembly,
  embedding atomic txs as **ExtData** (`hooks.go:192`, `:314`, `:79`).
- `GasConfigAfter` — returns ACP-176 gas target + price config for the next block
  (`hooks.go:117`).
- `EndOfBlockOps` / `PotentialEndOfBlockOps` — turn atomic txs into `hook.Op`s applied
  after EVM txs, with UTXO-conflict filtering against in-flight ancestor blocks
  (`hooks.go:135`, `:230`).
- `AfterExecutingBlock` — transfers non-AVAX (multicoin) balances and applies
  cross-chain state to the atomic trie + shared memory (`hooks.go:160`).

`hook.Op` (`hook/hook.go:151`) is the non-EVM state mutation primitive: `Burn`
(debit + nonce bump, with min-balance guard) and `Mint` (credit) maps over
addresses, applied via `Op.ApplyTo` (`hook/hook.go:171`). Import → Mint; Export →
Burn.

### 4.3 Atomic transactions & shared memory

- `tx.Tx` — signed atomic tx; `Unsigned` is `Import` or `Export`
  (`vms/saevm/cchain/tx/tx.go:53`). Converts to a `hook.Op` via `AsOp`
  (`tx.go:140`) and to shared-memory `chainsatomic.Requests` via `AtomicRequests`
  (`tx.go:252`).
- `tx.Import` — consumes source-chain UTXOs from shared memory, mints scaled AVAX into
  EVM accounts (`vms/saevm/cchain/tx/import.go:36`). `asOp` builds the Mint map
  (`import.go:211`); `atomicRequests` returns `RemoveRequests` for the consumed UTXOs
  (`import.go:232`).
- `tx.Export` — burns EVM-account AVAX and produces UTXOs in shared memory for the
  destination chain (`vms/saevm/cchain/tx/export.go`).
- `state.State` — the atomic-request trie + tx-by-ID index; `Apply` writes the trie
  and applies atomic ops to shared memory **atomically with** the DB batch
  (`vms/saevm/cchain/state/state.go:139`, `:177`). Byte-compatible with Coreth's
  `AtomicTrie`/`AtomicRepository` so no migration is needed (`state.go:39`).

### 4.4 Execution engine

- `saexec.Executor` — owns the trie-DB `Tracker`, a bounded queue, and the
  `lastExecuted` pointer (`vms/saevm/saexec/saexec.go:34`). `Enqueue` pushes an
  accepted block (`execution.go:36`); `processQueue` executes FIFO on a goroutine
  (`execution.go:62`); `Execute` runs the EVM + end-of-block ops starting from the
  parent's post-execution state (`execution.go:149`).

### 4.5 Fee mechanism types

- `acp176.State` (`vms/evm/acp176/acp176.go`) — gas target excess `q` and gas state;
  drives dynamic gas limit + base price. `gastime` couples this with a gas-denominated
  clock (`vms/saevm/gastime/gastime.go`, `SettledGasTime` at `hook/hook.go:221`).
- `acp226.DelayExcess` (`vms/evm/acp226/acp226.go:31`) — dynamic minimum block delay.

---

## 5. Block Execution + Atomic Import/Export Flow

### 5.1 Coreth (synchronous — production)

```
BuildBlock / ParseBlock
   └─ select EVM txs + atomic (import/export) txs from mempools
Verify
   └─ EXECUTE the block (apply EVM txs, apply atomic ops to a candidate state)
      → verify state root, gas, atomic conflicts vs shared memory
Accept
   └─ commit EVM state trie + atomic trie
   └─ atomic.SharedMemory.Apply(requests, batch)   // UTXO add/remove, atomic w/ DB
```
Execution happens **before** acceptance: a block is fully executed during `Verify`,
and its results are committed at `Accept`.

### 5.2 SAE-EVM (asynchronous — ACP-194)

```
WaitForEvent ──(pending txs)──► engine asks to build
BuildBlock(ctx)
   └─ hooks.BuildHeader(parent)                         cchain/hooks.go:192
   └─ select EVM txs (mempool) + atomic txs (PotentialEndOfBlockOps, conflict-filtered)
   └─ hooks.BuildBlock → eth Block w/ atomic txs as ExtData   cchain/hooks.go:314

Snowman: Verify → Accept   (NO execution yet)
   VM.AcceptBlock(b)                                    sae/consensus.go:41
   ├─ write block, canonical hash, tx-lookup (DB batch)
   ├─ mark blocks settled by b (b.MarkSettled)          settlement: D→M→I ordering
   ├─ store last.accepted; emit acceptedBlocks
   └─ exec.Enqueue(b)            ── async ──►            saexec/execution.go:36

Executor.processQueue (background goroutine)            saexec/execution.go:62
   └─ Execute(b, ...)                                   saexec/execution.go:149
      ├─ open state @ parent.PostExecutionStateRoot     (saedb.Tracker)
      ├─ advance gas clock (gastime); compute base fee
      ├─ apply EVM transactions
      ├─ hooks.EndOfBlockOps → []hook.Op; Op.ApplyTo    Import=Mint / Export=Burn
      ├─ hooks.AfterExecutingBlock                      cchain/hooks.go:160
      │     ├─ transfer non-AVAX (multicoin) balances
      │     └─ state.Apply(height, txs)                 cchain/state/state.go:139
      │           ├─ update atomic trie (sorted by txID for byte-identical root)
      │           └─ SharedMemory.Apply(ops, batch)     ← UTXO add/remove, atomic w/ DB
      └─ b.MarkExecuted → persist state root, set head, emit ChainHeadEvent
```

Key consequence: under SAE, EVM state is **lagging** consensus. Blocks are accepted
before they execute; the "last executed" head trails the "last accepted" head, and a
block is only *settled* (its results referenceable by later blocks) after `Tau`
(5 s, `params/params.go`) has elapsed in gas-time. During **bootstrapping**, the VM
forces `WaitUntilExecuted` after accept to stop consensus running ahead of execution
(`sae/consensus.go:94`).

### 5.3 UTXO ↔ EVM bridging summary

| Direction | Source | Shared-memory op | EVM-state op |
|-----------|--------|------------------|--------------|
| **Import** (X/P → C) | consumed UTXOs | `RemoveRequests` | Mint scaled AVAX to account |
| **Export** (C → X/P) | burned account balance | `PutRequests` (new UTXOs) | Burn from account (debit + nonce) |

AVAX is scaled by `x2cRate = 1e9` between nAVAX (X/P) and aAVAX (C-Chain wei)
(`tx/tx.go:202`). Import/Export are restricted to AVAX between the C-Chain and either
the P-Chain or the X-Chain on the same subnet (`import.go:109`).

---

## 6. Component Boundaries & Relationships

### 6.1 EVM ↔ Snowman engine

The C-Chain is a **linear** chain: one parent per block, no UTXO conflict set, so it
uses **Snowman** (not Avalanche DAG consensus). See [consensus.md](consensus.md).

- **Coreth** implements `block.ChainVM` directly; consensus calls
  `BuildBlock`/`ParseBlock`/`Verify`/`Accept`/`Reject`/`SetPreference`. Execution is
  inside `Verify`.
- **SAE-EVM** implements a generic `adaptor.ChainVM[*blocks.Block]`, converted to
  `block.ChainVM` via `adaptor.Convert` (`adaptor.go:61`). The crucial difference:
  `Accept` does **not** execute; it enqueues for async execution
  (`sae/consensus.go:85`). `Reject` is a no-op because execution only follows
  acceptance (`sae/consensus.go:131`). `WaitForEvent` signals the engine when the
  mempool has pending txs (`sae/vm.go:361`).

### 6.2 EVM ↔ atomic memory (X/P chains)

The only coupling between the account world and the UTXO world is **shared memory**
(`chains/atomic`, see [primitives.md](primitives.md)). The C-Chain VM:

1. Reads source-chain UTXOs from `snowCtx.SharedMemory.Get` to verify import credentials
   (`import.go:173`) and to serve `avax.getUTXOs` (`api.go:139`).
2. Writes UTXO add/remove via `SharedMemory.Apply`, committed **in the same batch** as
   its own state so a crash cannot double-apply (`state/state.go:177`).

Atomic txs are *not* EVM transactions: they ride in the block as **ExtData**
(`customtypes.BlockExtData`, parsed in `hooks.go:135`) and are translated into
`hook.Op` balance mutations during execution.

### 6.3 graft vs in-tree

- **Coreth (graft)** is the deployed C-Chain VM today. It is a separate Go module and
  has historically been compiled into a standalone plugin binary and run **out of
  process** via `rpcchainvm` (`plugin/main.go:36`).
- **SAE-EVM (in-tree)** is the next-generation, in-process VM under
  `vms/saevm/`. It still *depends on* Coreth/graft packages for battle-tested pieces:
  atomic intrinsic gas (`graft/coreth/plugin/evm/upgrade/ap5`), `extstate` multicoin
  balances (`graft/coreth/core/extstate`), custom header/block types
  (`graft/coreth/plugin/evm/customtypes`), and the atomic-trie key layout
  (`graft/coreth/plugin/evm/atomic/state`). So SAE reuses Coreth code while replacing
  the *execution/consensus integration* with the streaming-async model.

### 6.4 rpcchainvm relationship

`rpcchainvm` ([vm-framework.md](vm-framework.md)) lets a VM run as a separate gRPC
process. Coreth's plugin uses it. Two artefacts in SAE reveal the legacy:

- `cchain.VM.Initialize` wraps the DB with `prefixdb.NewNested(ethDBPrefix, …)`
  *specifically because* Coreth ran under rpcchainvm and the prefix was never compacted
  — SAE preserves byte-compatibility (`cchain/vm.go:73`).
- The atomic-trie `state.State` similarly uses `prefixdb.NewNested` to stay
  byte-identical with the Coreth-era on-disk trie (`state/state.go:84`).

`graft/coreth/plugin/factory/factory.go` also exposes a `vms.Factory` returning the
VM directly (ID `evm`), so the same code can be registered in-process or served as a
plugin.

### 6.5 State storage (firewood / triedb)

EVM account/contract state lives in a Merkle-Patricia trie behind a trie database:

- **Hash-based `triedb`** (libevm `triedb.HashDefaults`) — used by SAE's cchain
  harness (`cchain/vm.go:77`) and the atomic-request trie (`state/state.go:88`).
- **Firewood** (`graft/evm/firewood`, `firewood-go-ethhash/ffi`) — a high-performance
  on-disk trie DB used in production; see [database.md](database.md).

In SAE, `saedb.Tracker` reference-counts state roots so the trie DB only commits/prunes
once a root is no longer needed by consensus (`saexec/saexec.go:31`, `:70`); the count
is decremented when a settling block drops it (`sae/consensus.go:108`).

---

## 7. Key Behaviors, Gas/Fees, Invariants, Edge Cases

### 7.1 Dynamic fees (ACP-176)

`acp176` is an exponential price-discovery mechanism: a `TargetExcess` value `q` sets
the per-second gas target; the gas price is `MinGasPrice * e^(excess / conversion)`.
Constants (`vms/evm/acp176/acp176.go`): `MinTargetPerSecond = 1_000_000`,
`TargetToPriceUpdateConversion = 87` (≈ price doubles at most every ~60 s),
`MaxTargetChangeRate = 1024`. SAE binds this to a gas clock in `gastime` and exposes
the next-block config via `hooks.GasConfigAfter` (`cchain/hooks.go:117`).

### 7.2 Dynamic minimum block delay (ACP-226)

`acp226.DelayExcess` yields a minimum block delay `MinDelayMilliseconds * e^(excess /
ConversionRate)` (`vms/evm/acp226/acp226.go:34`), `InitialDelayExcess ≈ 2000 ms`. SAE
threads a `MinDelayExcess` field through the block header
(`cchain/hooks.go:217`, TODO: full encoding).

### 7.3 SAE-specific invariants

- **Settlement delay `Tau`**: a block's execution results are only settled into a later
  block after `Tau` = 5 s of gas-time has elapsed (`params/params.go`). Availability and
  finality of results are immediate at execution; settlement only governs when results
  feed *consensus* invariants.
- **Per-tx minimum gas** `ceil(gasLimit / Lambda)` with `Lambda = 2` — every tx is
  charged for at least half its limit, bounding execution queue growth
  (`hook/hook.go:203`, `params/params.go`).
- **Accept ordering (D→M→I→X)**: disk writes, then in-memory settlement, then internal
  pointers, then external events — enforced in `AcceptBlock` (`sae/consensus.go:41`).
- **Ancestry GC**: once a block is settled its parent/lastSettled pointers are severed
  so the linked list can be garbage-collected; `blocks.InMemoryBlockCount` observes this
  (`blocks/block.go:43`, `:70`).
- **Idempotent recovery**: on restart, accepted-but-unexecuted blocks are re-enqueued
  and re-executed; `state.Apply` skips heights `≤ currentHeight` so shared memory is
  never double-applied (`state/state.go:139`).

### 7.4 Atomic-tx edge cases

- Atomic txs must not conflict on inputs with *in-flight* (accepted-but-unsettled)
  ancestor blocks; `PotentialEndOfBlockOps` walks ancestors and unions their input IDs
  before admitting a tx (`cchain/hooks.go:230`, `:293`).
- Atomic-trie merge is sorted by txID to guarantee a **byte-identical** trie root across
  nodes (`state/state.go:192`).
- Import credentials are re-verified against current shared-memory UTXOs even for
  mempool txs, since the pool may have validated against stale state
  (`cchain/hooks.go:273`).

### 7.5 EVM vs X/P differences

| | C-Chain (EVM) | X/P-Chain |
|---|---|---|
| Model | Account / balance / nonce | UTXO |
| Consensus | Snowman (linear) | Snowman (P), Avalanche/Snowman (X) |
| State | MPT state trie (triedb/firewood) | UTXO set + indexed state |
| Tx model | EVM txs + atomic ExtData txs | UTXO txs |
| Fees | Dynamic base fee (ACP-176) | Per-tx fees |

---

## 8. Configuration / Params

- **SAE `Config`** (`vms/saevm/sae/vm.go:97`): `MempoolConfig` (libevm legacypool),
  `DBConfig` (triedb config + commit interval, `saedb.Config`), `RPCConfig`,
  `ExcessAfterLastSynchronous` (initial ACP-176 excess), `Now` clock.
- **cchain defaults** (`vms/saevm/cchain/vm.go:104`): `legacypool.DefaultConfig` with
  `NoLocals = true` (no preferential treatment for local txs); `maxTxPoolSize = 1024`
  atomic txs; `TrieDBConfig = triedb.HashDefaults`.
- **SAE constants** (`vms/saevm/params/params.go`): `Lambda = 2`, `Tau = 5 s`,
  `MaxFullBlocksInOpenQueue = 2`, `MaxQueueWallTime`.
- **ACP-176 constants** (`vms/evm/acp176/acp176.go`) and **ACP-226 constants**
  (`vms/evm/acp226/acp226.go`) as above.
- **Coreth config** (`graft/coreth/plugin/evm/config`): standard C-Chain chain config
  passed via AvalancheGo chain-config; `eth` namespace enabled by default, others
  (`personal`, `txpool`, `debug`) opt-in (`graft/coreth/README.md`).
- **Genesis**: SAE currently parses libevm `core.Genesis` JSON
  (`cchain/vm.go:81`); TODO to adopt Coreth's genesis format.

---

## 9. Cross-References

- **[coreth.md](coreth.md) — the exhaustive Coreth (production C-Chain VM) deep dive**
  (two-layer VM, synchronous execution, libevm extras, upgrades, dynamic fees,
  atomic txs, Warp/ICM, state sync, trie backends).
- **[saevm.md](saevm.md) — the exhaustive SAE-EVM (`strevm`) deep dive** (async
  execution model, executor, gas-time fees, hooks, C-Chain layer, JSON-RPC).
- [overview.md](overview.md) — system-wide map of the Primary Network and its chains.
- [consensus.md](consensus.md) — Snowman linear consensus driving the C-Chain VM.
- [simplex.md](simplex.md) — alternative consensus protocol.
- [vm-framework.md](vm-framework.md) — `block.ChainVM`, `rpcchainvm`, VM lifecycle.
- [chains.md](chains.md) — how chains/VMs are created and wired into the node.
- [platformvm.md](platformvm.md) / [avm.md](avm.md) — the UTXO-based P/X chains that
  import/export coordinate with.
- [primitives.md](primitives.md) — `chains/atomic` shared memory, IDs, codecs.
- [database.md](database.md) — triedb / Firewood / prefixdb storage layers.
- [networking.md](networking.md) — `network/p2p` gossip used by the tx mempool.
- [node.md](node.md) — node bootstrap and chain manager.
- [api.md](api.md) — JSON-RPC surface (`eth`, `debug`, `avax`).
- ACP-176 (dynamic gas), ACP-194 (SAE), ACP-226 (dynamic block delay) — linked inline.
