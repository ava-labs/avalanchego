# Coreth — the Production C-Chain VM (grafted go-ethereum / libevm)

> Deep dive on **Coreth**, the production Avalanche C-Chain VM. This is the
> detailed companion to the C-Chain summary in [evm.md](evm.md) (read that first
> for the Coreth-vs-SAE framing) and the mirror of [saevm.md](saevm.md) for the
> *current, shipping* EVM. Where SAE-EVM is the experimental async successor,
> Coreth is what actually validates the C-Chain on Mainnet/Fuji today.

Coreth lives under two grafted Go modules:
- **`graft/coreth/`** — the C-Chain VM proper (a fork of go-ethereum built on
  [libevm](https://github.com/ava-labs/libevm)).
- **`graft/evm/`** — shared EVM libraries (state-sync protocol, trie databases,
  Firewood) with its own `go.mod`, reused by Coreth and subnet-evm.

"graft" = a vendored upstream repository migrated into this monorepo as a separate
module, refactored toward repo standards via stacked PRs (`graft/README.md`). The
code retains go-ethereum's LGPL headers and structure.

Coreth is registered as the C-Chain VM at `constants.EVMID` (= the bytes `'evm'`)
in `node/node.go:1227` via `coreth.Factory{}`.

---

## 1. The one idea: synchronous execution under Snowman

Coreth is **synchronous**: a block's transactions are fully executed against the
EVM state *during* `Verify`, before consensus accepts it. The block header carries
the post-execution state root, receipt root, and bloom — exactly like go-ethereum.
Consensus (Snowman, [consensus.md](consensus.md)) supplies *ordering and finality*;
the geth layer supplies *execution*. This is the opposite of SAE-EVM's
accept-then-execute model ([saevm.md](saevm.md) §1).

```
   snow engine          wrappedBlock.Verify              wrappedBlock.Accept
   ───────────          ───────────────────              ───────────────────
   Verify(block) ──►  syntacticVerify + semanticVerify  ──►  precompile accepts
                      InsertBlockManual(block, writes)        blockChain.Accept
                      = FULL EVM EXECUTION + state commit     versiondb CommitBatch
                      (state root in header verified here)    shared-memory Apply
```

Because consensus gives **deterministic, re-org-free finality**, Coreth makes a
hard simplification over upstream geth: there is **no fork-choice, no
re-org below the last-accepted block, no `eth`/`snap` P2P sync protocol, and no
downloader**. The geth "consensus engine" is a stub (`dummy`, §6) whose only job
is to enforce Avalanche header rules and apply atomic-tx state changes.

---

## 2. Responsibilities, scope, and the layered structure

**Coreth is responsible for:**
- Executing EVM transactions synchronously and committing account/contract state.
- Parsing/building/verifying/accepting blocks for the Snowman engine.
- Atomic import/export transactions bridging AVAX (and, pre-Banff, multicoin
  assets) between the account model and X/P-chain UTXOs via shared memory.
- Avalanche-specific **dynamic fees** across many network-upgrade phases (legacy
  rolling fee window → ACP-176 → ACP-226 block timing).
- **Avalanche Warp Messaging / ICM**: signing message hashes with the node's BLS
  key and verifying cross-subnet messages via a stateful precompile + predicates.
- Fast **state sync** (EVM state + atomic trie), with hashdb / pathdb / **Firewood**
  trie backends.
- The full Ethereum JSON-RPC surface plus the Avalanche `avax`/`admin`/`warp`
  namespaces and the cross-node state-sync request/response protocol.

**Two-module / two-layer / two-router structure** (worth internalizing early —
nearly everything in Coreth is "Avalanche thing wrapping or coexisting with a geth
thing"):

| Axis | Inner / upstream | Outer / Avalanche |
|------|------------------|-------------------|
| **VM layers** | `evm.VM` (`plugin/evm/vm.go`) | atomic-wrapping `VM` (`plugin/evm/atomic/vm/vm.go`) added by `WrapVM` |
| **libevm extras** | geth `Header`/`Block`/`StateAccount`/`ChainConfig` | Avalanche fields injected via libevm "extras" registration |
| **Consensus engine** | geth `consensus.Engine` interface | `dummy.DummyEngine` (difficulty=1, no PoW/PoS) |
| **Networking** | SDK `network/p2p` router (odd requestIDs) | legacy coreth `network` handler (even requestIDs) |
| **Modules** | `graft/coreth` (the VM) | `graft/evm` (shared sync/trie/Firewood) |

---

## 3. Package & file layout

```
graft/coreth/                                  # the C-Chain VM (go.mod)
├── plugin/
│   ├── factory/factory.go                     # registers EVMID; WrapVM(&evm.VM{})
│   └── evm/
│       ├── vm.go (~1200 lines)                # the INNER evm.VM
│       ├── wrapped_block.go                   # snowman.Block: Verify/Accept/Reject (sync exec)
│       ├── block_builder.go                   # build trigger / WaitForEvent
│       ├── vm_extensible.go, extension/config.go  # the extension mechanism (subnet-evm seam)
│       ├── eth_gossiper.go, network_handler.go, sync_adapter.go
│       ├── customheader/                       # dynamic fees: base_fee, dynamic_fee_*, gas_limit,
│       │                                       #   block_gas_cost, min_delay_excess, extra, time
│       ├── customtypes/                        # libevm extras: header_ext, block_ext, state_account_ext
│       ├── config/                             # VM config struct & defaults
│       ├── client/                             # avalanchego-facing C-Chain client (avax/admin)
│       └── atomic/                             # CROSS-CHAIN (UTXO bridge)
│           ├── tx.go import_tx.go export_tx.go codec.go   # atomic tx types
│           ├── state/  atomic_trie, atomic_backend, atomic_repository, atomic_state
│           ├── txpool/ mempool, tx_heap, txs                # atomic mempool
│           ├── sync/   extender, syncer, summary            # atomic-trie state sync
│           └── vm/     vm.go (WrapVM), block_extension.go, tx_semantic_verifier.go, bonus_blocks.go, api.go
├── core/              blockchain, state_processor, state_transition, genesis,
│                      predicate_check, block_validator, state_manager, evm, txindexer
├── consensus/dummy/   consensus.go             # the stub engine
├── params/            config, config_extra, config_libevm, hooks_libevm  # chain config + upgrades + libevm hooks
├── eth/               backend, api_backend, api, api_admin, api_debug, state_accessor
├── miner/             worker, ordering          # block production
├── warp/              backend, service, verifier_backend, client  # ICM signing
├── precompile/        contract, contracts/warp, modules, registry  # stateful precompiles
├── network/           network.go                # legacy coreth p2p router
└── nativeasset/       contract.go               # multicoin precompiles

graft/evm/                                       # shared EVM libs (separate go.mod)
├── message/           leafs_request, block_request, code_request, summaries, codec
├── sync/              client, handlers, leaf, code, block, evmstate, engine
├── triedb/            hashdb/, pathdb/           # the two classic trie backends
└── firewood/          triedb, base_trie, account_trie, reconstructed_*  # Firewood integration

vms/evm/acp176, vms/evm/acp226                   # the dynamic-fee/timing reference impls (avalanchego module)
```

---

## 4. The two-layer VM, the extension seam, and the factory

### 4.1 The factory and `WrapVM` (`plugin/factory/factory.go`)

```go
var ID = ids.ID{'e', 'v', 'm'}                          // = constants.EVMID
func (*Factory) New(logging.Logger) (interface{}, error) {
    return atomicvm.WrapVM(&evm.VM{}), nil              // in-process / grafted
}
func NewPluginVM() block.ChainVM {
    return atomicvm.WrapVM(&evm.VM{IsPlugin: true})     // rpcchainvm plugin
}
```

Both paths always wrap the inner `evm.VM` in the outer atomic VM. The only
difference is `IsPlugin` (in-process vs out-of-process; see §4.4).
`atomicvm.WrapVM(vm)` does **nothing but embed** the inner VM into the outer struct
(`atomic/vm/vm.go:108`); the wiring happens in the outer `Initialize`.

### 4.2 The two VM layers

**Outer VM** (`plugin/evm/atomic/vm/vm.go:77`) embeds `extension.InnerVM` and adds
all atomic-cross-chain machinery: `AtomicMempool`, `AtomicTxRepository`,
`AtomicBackend` (the atomic trie), atomic-tx gossipers, `SecpCache`, `Fx`. Because
the inner VM is embedded, every method the outer VM doesn't override
(`ParseBlock`, `GetBlock`, `SetPreference`, `LastAccepted`, `WaitForEvent`,
state-sync methods…) **forwards straight to the inner VM**. It overrides
`Initialize`, `SetState`, `Shutdown`, `CreateHandlers`, `BuildBlock(WithContext)`.

**Inner VM** (`plugin/evm/vm.go:179`) embeds `*chain.State` (the AvalancheGo
snowman block-caching layer, [vm-framework.md](vm-framework.md)) and holds the eth
backend (`*eth.Ethereum`), `*core.BlockChain`, `*txpool.TxPool`, `*miner.Miner`,
the warp backend, the state-sync server/client, and a `*versiondb.Database` over
the chain DB.

### 4.3 The extension mechanism — how subnet-evm reuses Coreth

The seam that makes the inner VM reusable is `extension.Config`
(`plugin/evm/extension/config.go:134`), set via `InnerVM.SetExtensionConfig(...)`
**before** `Initialize`. It bundles:
- `ConsensusCallbacks dummy.ConsensusCallbacks` — `{OnFinalizeAndAssemble,
  OnExtraStateChange}`, the hooks where atomic-tx state changes are injected.
- `SyncSummaryProvider`, `SyncExtender` — state-sync summary customization.
- `BlockExtender` — a factory for per-block hooks (`BlockExtension`:
  `SyntacticVerify`/`SemanticVerify`/`Accept`/`Reject`/`CleanupVerified`).
- `ExtraMempool` (`BuilderMempool`) — an extra mempool the block builder drains.
- `ExtraSyncLeafHandlerConfig` — an extra state-sync leaf handler (the atomic trie).

The atomic wrapper supplies all of these (atomic-tx extraction/verify/accept as a
`BlockExtension`, the atomic mempool as `ExtraMempool`, an atomic-trie leaf handler,
and atomic state-sync). **subnet-evm supplies a different `extension.Config`**,
which is how one `evm.VM` body serves both the C-Chain and arbitrary subnets.

### 4.4 In-process vs plugin (`IsPlugin`)

In-process (default), Coreth runs inside the avalanchego binary. As an rpcchainvm
plugin, it runs as a subprocess over gRPC ([vm-framework.md](vm-framework.md)).
The only construction difference is the `IsPlugin` flag, which routes the logger to
`originalStderr` (captured before `plugin.Serve` repipes stderr,
`plugin/evm/vm.go:171-176, 288-297`) so logs flow through rpcchainvm's stderr pipe.
Everything else is identical.

---

## 5. VM lifecycle

### 5.1 `Initialize` (inner, `plugin/evm/vm.go:261`)

Assumes `extensionConfig` is already set (reads `extensionConfig.Clock`
immediately). Major steps: parse config (`config.GetConfig`) → resolve chain alias
→ set up logger (stderr if plugin) → `initializeMetrics` (registers `eth`,
`firewood`, `sdk` gatherers) → `initializeDBs` (splits the chain DB into
`versiondb` + `metadataDB` + `acceptedBlockDB` + `warpDB`) → `parseGenesis` →
build `ethConfig` from ~40 config fields → **state-scheme guards** (Firewood
requires `SnapshotCache=0`, forbids offline pruning) → build the coreth `network`
→ build the **warp backend** (`warp.NewBackend`, metered LRU sig cache) →
`initializeChain` → register the ACP-118 warp signature handler at
`p2p.SignatureRequestHandlerID` → `initializeStateSync`.

`initializeChain` (`vm.go:521`) builds the geth node + `eth.New(...,
dummy.NewDummyEngine(callbacks, mode, desiredTargetExcess, desiredDelayExcess),
clock)`, sets etherbase = `constants.BlackholeAddr`, sets the tx-pool min fee to
`acp176.MinGasPrice`, starts the eth backend, and calls `initChainState` which
wraps the last-accepted block and builds the `*chain.State` caching layer with
callbacks `GetBlock=vm.getBlock`, `UnmarshalBlock=vm.parseBlock`,
`BuildBlock=vm.buildBlock`.

`initializeStateSync` (`vm.go:582`) registers leaf handlers by scheme (HashScheme →
a `StateTrieNode` handler over a standalone read-only triedb; FirewoodScheme → a
`p2p.FirewoodProofHandlerID` proof handler), adds the extension's atomic-trie leaf
handler, and builds the state-sync `Server` and `Client`.

The **outer `Initialize`** (`atomic/vm/vm.go:113`) builds the atomic extension
structs, calls `SetExtensionConfig` then `InnerVM.Initialize`, then constructs the
atomic mempool, atomic gossip system, bonus-block set (mainnet), `AtomicTxRepository`,
and `AtomicBackend` (the atomic trie).

### 5.2 Bootstrapping states & block building

`SetState` walks `StateSyncing → Bootstrapping → NormalOp`. **Block-building and
gossip goroutines are deliberately deferred until `NormalOp`** (`onNormalOperationsStarted`
→ `initBlockBuilding`, `vm.go:768`): there's no need to gossip the mempool before
the node is at the tip. `onBootstrapStarted` calls `blockChain.InitializeSnapshots()`
(ensures snapshots exist even if state sync was skipped). The outer layer starts
atomic-tx gossip at `NormalOp` and calls `Fx.Bootstrapped()`.

### 5.3 Shutdown

Inner `Shutdown` cancels the network, shuts the state-sync client, **stops RPC
handlers before `eth.Stop()`** (which closes the DB), then `eth.Stop()`. Outer
`Shutdown` cancels atomic goroutines then delegates inward.

---

## 6. Block handling & synchronous execution (`wrapped_block.go`)

`wrappedBlock` (`wrapped_block.go:68`) wraps a geth `*types.Block`, an optional
`extension.BlockExtension`, and the VM; it implements `snowman.Block`,
`block.WithVerifyContext`, and `extension.ExtendedBlock`.

- **`ParseBlock`** RLP-decodes the block and **eagerly `syntacticVerify`s** it.
- **`BuildBlock`/`buildBlockWithContext`** (`vm.go:881`) builds a
  `precompileconfig.PredicateContext`, calls `miner.GenerateBlock`, wraps it, then
  does a **dry-run `verify(..., writes=false)`** — verifying without creating a
  triedb reference to the state root (the engine will re-verify with writes).
- **`verify(predicateContext, writes)`** (`wrapped_block.go:252`) is the
  synchronous-execution core:
  1. `syntacticVerify` (well-formedness: coinbase must be `BlackholeAddr`,
     difficulty 1, nonce 0, no uncles, version 0, blobs disabled, AP-gated base-fee
     /block-gas-cost/ext-data checks, then `extension.SyntacticVerify`).
  2. `semanticVerify` (parent lookup, `VerifyMinDelayExcess`/`VerifyTime`; if
     `bootstrapped`, intrinsic-gas + predicate verification; then
     `extension.SemanticVerify`).
  3. If `vm.State.IsProcessing(b.id)` → return (the engine may call
     `VerifyWithContext` multiple times; **`InsertBlockManual` must run once**).
  4. **`blockChain.InsertBlockManual(ethBlock, writes)`** — *here the block is fully
     executed against the EVM state*: header verify, body validate, run all txs
     (`StateProcessor.Process`), validate the resulting state root, and (if
     `writes`) persist the block and commit state to the trie manager.
- `ShouldVerifyWithContext` returns true iff a tx's access list references a
  predicate precompile (warp), gating proposerVM context (`block.WithVerifyContext`).
- **`Accept`** (`wrapped_block.go:96`): `handlePrecompileAccept` (fires warp
  `Accepter`s **before** the accepted-log emission) → `blockChain.Accept(ethBlock)`
  → `PutLastAcceptedID` → `versiondb.CommitBatch()` → `extension.Accept(vdbBatch)`
  which atomically flushes the atomic shared-memory changes with the block's
  versiondb batch.
- **`Reject`** delegates to `extension.Reject()` (re-issues atomic txs to the
  mempool) then `blockChain.Reject`.

### Block building trigger (`block_builder.go`)

`needToBuild` is true when the eth txpool or the extra (atomic) mempool has pending
txs. `waitForEvent` (the surface behind `vm.WaitForEvent`) blocks on a condition
variable until there are txs and the mempool head matches the chain head
(`pendingPoolUpdate` guards against building on a stale head), then waits out the
ACP-226 minimum block delay (`calculateBlockBuildingDelay`) before returning
`PendingTxs`. The outer `BuildBlockWithContext` drives the atomic mempool's
issued/cancelled/discarded lifecycle based on the build result.

---

## 7. libevm "extras" — how Avalanche fields are injected

Coreth uses libevm's "extras" registration to add Avalanche fields/hooks to
upstream geth types without forking them. `params.ChainConfig`/`params.Rules` are
**type aliases** for the geth types; the Avalanche payload is fetched via
`params.GetExtra(cfg)` / `params.GetRulesExtra(rules)`. Three registration sites
(`plugin/evm/libevm.go`'s `RegisterAllLibEVMExtras`, called once):

- **params** (`params/config_libevm.go`) — registers `extras.ChainConfig` (network
  upgrades + `AvalancheContext` + precompile upgrades) sharing the ChainConfig JSON
  root, plus `RulesExtra` (the active precompile/predicater/accepter maps).
- **customtypes** (`plugin/evm/customtypes/libevm.go`) — registers `HeaderExtra`,
  `BlockBodyExtra`, and the `isMultiCoin` state-account flag.
- **core/evm.go** — registers EVM execution `vm.Hooks`.

### Header / block extra fields (`customtypes/`)

`HeaderExtra` (`header_ext.go:40`) adds to the geth header:

| Field | Activated | Meaning |
|-------|-----------|---------|
| `ExtDataHash` | AP1+ (always present in RLP) | hash of the atomic ExtData |
| `ExtDataGasUsed *big.Int` | AP4+ | gas consumed by atomic txs |
| `BlockGasCost *big.Int` | AP4+ (0 in Granite) | required block fee (in gas) |
| `TimeMilliseconds *uint64` | Granite | millisecond block time |
| `MinDelayExcess *acp226.DelayExcess` | Granite | ACP-226 min-block-delay excess |

`BlockBodyExtra` (`block_ext.go`) adds `Version uint32` + `ExtData *[]byte` (the
atomic-tx blob) and **removes** geth's `Withdrawals`. The `Extra` header field also
carries the dynamic-fee state and predicate results (§9.1).

---

## 8. Network upgrades & chain config (`params/extras/`)

`extras.ChainConfig` embeds `NetworkUpgrades` — all **timestamp-based** `*uint64`
activations mapped from AvalancheGo's upgrade schedule ([node.md](node.md)). The
ordered phases and what each gates:

| Phase (avalanchego release) | Gates |
|---|---|
| **ApricotPhase1** (v1.3) | gas limit → 8M; disables SSTORE gas refunds |
| **ApricotPhase2** (v1.4) | Berlin; native-asset precompiles |
| **ApricotPhase3** (v1.5) | **dynamic fees** + London; base fee becomes non-nil; rolling fee window |
| **ApricotPhase4** (v1.6) | block fee (`BlockGasCost`), `ExtDataGasUsed` header field |
| **ApricotPhase5** (v1.7) | atomic-tx **batching** + per-block atomic gas limit; `TargetGas`=15M, denominator 36 |
| **AP-pre6 / 6 / post6** (v1.8) | deprecate `NativeAssetCall`/`NativeAssetBalance` |
| **Banff** (v1.9) | restrict atomic import/export to **AVAX only** |
| **Cortina** (v1.10) | gas limit → 15M; X-Chain linearization (network-wide) |
| **Durango** (v1.11) | **Avalanche Warp Messaging** + Shanghai; predicates active |
| **Etna** (v1.12) | Cancun (blob txs disallowed); min base fee → 1 gwei |
| **Fortuna** (v1.13) | **ACP-176** dynamic gas target & price (replaces AP3 window) |
| **Granite** (v1.14) | millisecond timestamps, **ACP-226** min block delay, P256Verify, P-Chain epochs |

`SetEthUpgrades` maps these to geth forks; Berlin/London are hardcoded per network
at the AP2/AP3 activation block heights (Mainnet Berlin=1640340, London=3308552).
`IsMergeTODO = true` is passed wherever geth wants an `isMerge` flag — Avalanche has
no PoW→PoS merge, so it's a placeholder.

---

## 9. Dynamic fees (`plugin/evm/customheader/`, `vms/evm/acp176`, `acp226`)

Coreth's fee mechanism has evolved across three regimes; the active one is
upgrade-gated, and the dummy engine **recomputes and requires exact equality** of
the header's base fee, block gas cost, and gas state on every block.

### 9.1 The header `Extra` field layout

```
pre-AP3:   arbitrary (AP1: must be empty)
AP3..pre-Fortuna:  [80-byte rolling fee window] [predicate results...]   (Durango+ adds predicates)
Fortuna+:          [24-byte ACP-176 state]      [predicate results...]
```
`PredicateBytesFromExtra` reads from offset 80 (AP3) or 24 (Fortuna). The dummy
engine **prepends** the fee prefix in `FinalizeAndAssemble` after the miner appends
predicate-result bytes — final order is `[fee-state | predicate-results]`.

### 9.2 Legacy rolling fee window (AP3–Etna, `dynamic_fee_windower.go`)

A 10-slot, 10-second rolling window of consumed gas stored in the header `Extra`.
Base fee adjusts toward a gas target (`ap3.TargetGas`=10M, `ap5.TargetGas`=15M) by
`delta = max(1, |totalGas−target|·parentBaseFee/target/denominator)` (denominator
12 at AP3, 36 at AP5), bounded per phase (Etna floor 1 gwei, AP5 floor 25 gwei,
AP3 range 75–225 gwei). Idle time multiplies the decrease by elapsed windows.

### 9.3 ACP-176 dynamic gas target & price (Fortuna, `vms/evm/acp176`)

A 24-byte state `{capacity, excess, targetExcess}`:
- `Target = MinTargetPerSecond(1M) · e^(targetExcess/TargetConversion)`.
- `MaxCapacity = Target · 10` (this becomes `header.GasLimit` under Fortuna).
- `GasPrice = MinGasPrice(1) · e^(excess/(Target·87))` (87 ≈ 60/ln2 → price doubles
  at most ~every 60 s of sustained 2× load).
- Each block advances capacity/excess by elapsed time and consumes gas; the target
  moves toward `desiredTargetExcess` (config `GasTarget`), bounded `±2^15` per block.

This is the **same exponential kernel** (`gas.CalculatePrice`) that SAE-EVM's
`gastime` uses continuously ([saevm.md](saevm.md) §11); Coreth applies it
block-discretely.

### 9.4 ACP-226 minimum block delay (Granite, `vms/evm/acp226`)

A `DelayExcess` (initial ≈2000 ms) where `Delay = 1ms · e^(excess/2^20)`, moving
`±200` per block toward the configured target. `VerifyTime` enforces millisecond
monotonicity and `actualDelayMS ≥ parentMinDelay`, and the block builder waits this
delay before producing.

### 9.5 Block gas cost / block fee (AP4+, `block_gas_cost.go`)

A required minimum "block fee" denominated in gas, derived from inter-block time
(faster-than-2s blocks raise it, slower lower it; step 50k/200k, capped 1M; **0 in
Granite**). `VerifyBlockFee` requires `Σ tx tips + atomic-tx contribution ≥
requiredBlockGasCost·baseFee` — this is what makes atomic-tx fees subsidize block
production and prices congestion.

---

## 10. EVM execution path (`core/`)

`InsertBlockManual → insertBlock` (`core/blockchain.go:1331`): recover senders
concurrently → `engine.VerifyHeader` → `validator.ValidateBody` → build the
`StateDB` at `parent.Root` (with a concurrent trie prefetcher) →
`StateProcessor.Process` → `validator.ValidateState` (state root must equal
`statedb.IntermediateRoot`) → if `writes`, `writeBlockWithState` (commit-with-snap,
then `stateManager.InsertTrie` **last** so a triedb reference is created only when
Accept/Reject will follow).

`StateProcessor.Process` (`core/state_processor.go`): `ApplyUpgrades` (activates/
deactivates stateful precompiles whose config transitions this block) → EIP-4788
beacon root → per-tx `ApplyMessage` → `engine.Finalize` (atomic-tx state changes +
block-fee verification). `StateTransition` (`core/state_transition.go`):
Durango init-code-size limit, **AP1+ disables SSTORE refunds**, and crucially the
**full base fee is paid to the coinbase, not burned** (`:400`) — an Avalanche
divergence from EIP-1559. After execution it checks `evm.ExecutionInvalidated()`
(precompile delegate-call gating).

**Predicate checks** (`core/predicate_check.go`): for warp, `CheckTxPredicates`
extracts predicates from the tx access list, requires the proposerVM block context,
runs `VerifyPredicate` per predicate, and encodes the pass/fail **bitset** into the
header `Extra`. Storing results in the block lets historical blocks be re-verified
cheaply without re-running verification.

`StateManager` (`core/state_manager.go`) chooses a `TrieWriter` by scheme/mode:
**pruning** (hashdb, default) commits the state root only at `CommitInterval`
(4096) boundaries and dereferences roots older than `StateHistory` via a bounded
"tip buffer", optimistically flushing the dirty cache over a 768-block window;
**archival** (`!Pruning`) and **Firewood** commit every block.

---

## 11. The dummy consensus engine (`consensus/dummy/consensus.go`)

A stub implementing geth's `consensus.Engine` to bridge to Avalanche rules. It
holds the `ConsensusCallbacks` (`OnFinalizeAndAssemble`/`OnExtraStateChange` — where
atomic txs are injected) plus ACP-176/226 "desired excess" guidance from config.

- `Author` = `header.Coinbase`; `CalcDifficulty`/`Prepare` force difficulty 1;
  `VerifyUncles` rejects any uncles.
- `verifyHeaderGasFields` recomputes the expected base fee, block gas cost, and gas
  limit via `customheader.*` and **requires the header to match exactly**.
- `Finalize` runs `OnExtraStateChange` (applies atomic-tx balance changes), re-checks
  block gas cost & `ExtDataGasUsed`, and verifies the block fee.
- `FinalizeAndAssemble` (block production) runs `OnFinalizeAndAssemble`, sets the
  block-gas-cost/ext-data fields, prepends the ACP-176/AP3 fee prefix to `Extra`,
  sets `MinDelayExcess`, commits the state root, and assembles the block with the
  atomic ExtData (recomputing `ExtDataHash`).

---

## 12. Atomic transactions & cross-chain shared memory (`plugin/evm/atomic/`)

Atomic txs bridge the account model to UTXO X/P-chains. They are **not** EVM txs —
they ride in the block's `ExtData` and are applied via the dummy engine's
`OnExtraStateChange`/`OnFinalizeAndAssemble` callbacks.

### 12.1 Tx types (`tx.go`, `import_tx.go`, `export_tx.go`)

- `Tx = {UnsignedAtomicTx, Creds []verify.Verifiable}`; the tx ID is the hash of the
  **signed** bytes. The `UnsignedAtomicTx` interface exposes `InputUTXOs()`,
  `Verify`, `Visit` (visitor for semantic verify), `AtomicOps()` (shared-memory
  Put/Remove requests), and `EVMStateTransfer` (apply balance changes).
- **`UnsignedImportTx`** (foreign-chain → C-Chain, **mints** AVAX):
  `ImportedInputs []*avax.TransferableInput`, `Outs []EVMOutput`. `EVMStateTransfer`
  credits each output (`AddBalance` for AVAX, scaled by `X2CRate`;
  `AddBalanceMultiCoin` for assets). `AtomicOps` returns `RemoveRequests` (consume
  the source UTXOs). UTXOs are removed at **Accept**, not Verify (shared memory
  isn't versiondb-protected).
- **`UnsignedExportTx`** (C-Chain → foreign chain, **burns** AVAX): `Ins []EVMInput`
  (address + nonce + amount), `ExportedOutputs []*avax.TransferableOutput`.
  `EVMStateTransfer` debits each input (checking & incrementing the account
  **nonce** for replay protection) and `AtomicOps` returns `PutRequests` (produce
  destination UTXOs). `InputUTXOs` synthesizes a `nonce+address` pseudo-UTXO ID so
  the same conflict machinery applies.

**AVAX denomination scaling**: `X2CRate = 1e9` converts X/P-chain **nAVAX** (9
decimals) to C-chain **wei** (18 decimals). Multicoin assets are 1:1. Gas/fee:
`BlockFeeContribution` scales the excess burned AVAX (above the dynamic fee) into an
18-decimal block-fee contribution; AP5 charges a fixed 10k intrinsic gas per atomic
tx, capped at a 100k per-block atomic gas limit. **Banff bans non-AVAX atomic
transfers entirely.**

### 12.2 The atomic trie & backend (`atomic/state/`)

The **atomic trie** is a Merkle trie keyed by `[height(8B)][blockchainID(32B)] →
atomic ops`, committed at `CommitInterval` boundaries so state-sync summaries can
reference a recent atomic root. `AtomicBackend` orchestrates the trie + the
`AtomicRepository` (txID→tx and height→txs indexes) + shared memory. At block
`Accept`, `atomicState.Accept(commitBatch)` calls
`sharedMemory.Apply(atomicOps, commitBatch, atomicChangesBatch)` — committing the
EVM state batch, the atomic DB changes, and the shared-memory Put/Remove **in one
atomic operation** ([chains.md](chains.md)).

### 12.3 The atomic mempool (`atomic/txpool/`)

A **separate** mempool from the eth txpool, ordered purely by atomic effective gas
price (a dual min/max heap). Conflict detection is **UTXO/account-input based**
(`utxoSpenders` map), with replace-by-fee on conflicts and lowest-fee eviction at
the size cap. It tracks a block-building lifecycle (Pending → Current → Issued →
Discarded). Bloom-filter-backed for SDK gossip.

### 12.4 Double-spend prevention (layered) & bonus blocks

Conflicts are prevented at five layers: within a tx (sorted/unique inputs), in the
mempool (`utxoSpenders`), within a block (`verifyTxs` rejects overlapping inputs),
across unaccepted "processing" ancestors (`conflicts()` walks the chain checking
`InputUTXOs` overlap), and atomically at the shared-memory commit. **Bonus blocks**
(67 hardcoded mainnet heights, `bonus_blocks.go`) are historical blocks whose atomic
ops were accepted without applying the corresponding shared-memory operations; their
ops are still included in the atomic trie (so roots match history) but
shared-memory application is skipped (`WriteAll` instead of `sharedMemory.Apply`),
and they're excluded from UTXO-presence verification.

### 12.5 Native-asset / multicoin precompiles (`nativeasset/contract.go`)

Pre-Banff "native asset" support: `NativeAssetBalance` (read a multicoin balance)
and `NativeAssetCall` (atomically transfer a multicoin asset then CALL). Multicoin
balances are stored as **EVM contract storage slots** keyed by a normalized asset ID
(bit-0 partitioned from regular storage, `core/extstate`). Deprecated to always-revert
after AP6/Banff.

---

## 13. State storage: trie databases & Firewood

All three backends implement the libevm `triedb.DBOverride` interface
(`Update`/`Commit`/`Reference`/`Dereference`/`Cap`/`Reader`).

| Backend | Keying | Memory mgmt | Historical state | Pruning |
|---------|--------|-------------|------------------|---------|
| **hashdb** (`graft/evm/triedb/hashdb`) | node **hash** | refcounted dirties + flush list | yes, if referenced | by dereference cascade |
| **pathdb** (`graft/evm/triedb/pathdb`) | node **path** | disk layer + ≤128 diff layers | not in this fork | layer flatten/drop (freezer disabled) |
| **Firewood** (`graft/evm/firewood`) | **flat key** (Rust-backed) | managed in Rust; revisions + proposals | via revisions (`Archive`) | internal |

**Firewood** (`firewood/triedb.go`) is a Rust-backed authenticated DB
(`firewood-go-ethhash/ffi`) using Ethereum node hashing. It has no externally
visible trie nodes — state is a flat keyspace (`keccak(addr)` for accounts,
`keccak(addr)||keccak(slot)` for storage), and the trie root is computed by creating
an FFI **proposal** (it can't hash without proposing). Because it can't serve the
classic leaf/proof handler, Firewood uses a dedicated **FFI range-proof** state-sync
path (`p2p.FirewoodProofHandlerID`). It requires snapshots disabled and a chain data
dir, and uses block-hash-keyed updates so identical state roots across different
blocks are distinguished.

---

## 14. State sync (`graft/evm/sync/`, `graft/evm/message/`)

A bootstrapping node downloads a recent state snapshot instead of replaying all
blocks. End to end:

1. **Summary selection** — the server serves a `BlockSyncSummary` (block number,
   hash, state root) at the nearest block ≤ last-accepted divisible by the syncable
   interval. The atomic layer wraps this in a `Summary` that *also* carries the
   committed `AtomicRoot`.
2. **Leaf sync** — the client requests trie leaves over key ranges
   (`LeafsRequest`/`LeafsResponse` with **range proofs**); the server serves them
   **snapshot-first** (reading the flat snapshot under a 75%-deadline budget and
   validating 64-key segments against the trie root, filling gaps from the trie).
   The client verifies every response with `trie.VerifyRangeProof`. A
   `CallbackSyncer` with 8 workers reconstructs the account trie (registering
   storage roots and code hashes), then storage tries (up to 8 concurrent), using a
   `StackTrie` that asserts `Commit() == root` per trie. Large tries are split into
   prefix segments for parallelism; all progress is persisted and resumable.
3. **Code & blocks** — `CodeRequest` (≤5 hashes, verified by keccak) and
   `BlockRequest` (256 recent blocks, needed for the `BLOCKHASH` opcode), each
   chained/verified client-side.
4. **Atomic-trie sync** runs as a registry extender alongside EVM-state sync,
   leaf-syncing the height-keyed atomic trie to `Summary.AtomicRoot`. Its
   `OnFinishBeforeCommit` marks the shared-memory cursor and `OnFinishAfterCommit`
   replays atomic ops to shared memory (crash-safe/resumable via the cursor).
5. **Finalization** — `AcceptSync` validates the synced block, `ResetToStateSyncedBlock`,
   and commits all markers (last-accepted ID, summary key deletion, shared-memory
   cursor) in one versiondb commit.

This plugs into AvalancheGo's `block.StateSyncableVM` via `engine.Client` and the
`sync_adapter.go` chain-context adapter. Firewood substitutes its FFI range-proof
syncer for the leaf syncer.

---

## 15. Warp / Interchain Messaging (`warp/`, `precompile/contracts/warp/`)

Avalanche Warp Messaging (ICM) lets the C-Chain emit BLS-signed messages that other
subnets can verify against the source chain's validator set.

### 15.1 The signing backend (`warp/backend.go`)

The `Backend` signs unsigned-message hashes with the node's BLS key
(`vm.ctx.WarpSigner`) and persists/caches signatures. It signs **only**:
- messages it accepted from its **own** chain's `sendWarpMessage` logs (the warp
  precompile's `Accepter` calls `AddMessage` at block accept),
- configured **off-chain** `AddressedCall` messages, and
- **block-hash** payloads for blocks the node has accepted
  (`verifyBlockMessage → GetAcceptedBlock`).

Arbitrary unknown payloads are **rejected** (the verifier signs nothing it can't
attest to). A BLS-key-change defense: `AddMessage` stores the full unsigned message
(not the signature) so a restarted node with a new key re-signs correctly rather
than serving stale signatures.

### 15.2 The handler, aggregator, and service

- The backend is registered as an **ACP-118 `Verifier`** at
  `p2p.SignatureRequestHandlerID` (`acp118.NewCachedHandler`) so other nodes can
  request this node's signature ([networking.md](networking.md), [primitives.md](primitives.md)).
- For the RPC API, the node builds an `acp118.SignatureAggregator` that collects
  peers' signatures into an aggregate (`warp_getMessageAggregateSignature` /
  `warp_getBlockAggregateSignature`), weighted by the source subnet's validator set
  at a P-Chain height (default quorum 67/100). The **P-Chain exception**: messages
  from the primary network/P-Chain are verified against the local subnet's set
  (P-Chain is always synced).

### 15.3 The warp precompile (`precompile/contracts/warp/`)

A stateful precompile at `0x02..05` with `sendWarpMessage(bytes)` (emits a
`SendWarpMessage` log — picked up by the `Accepter` at accept time),
`getVerifiedWarpMessage(index)` / `getVerifiedWarpBlockHash(index)` (read a
predicate-verified message; valid iff the predicate exists and its failed-bit is
unset), and `getBlockchainID()`. Incoming warp messages ride in transaction
**access lists** as predicates; `VerifyPredicate` parses the message, resolves the
source subnet, fetches the validator set at the proposerVM P-Chain height, and
verifies the aggregate BLS signature meets quorum. Results are cached as a bitset in
the block header (§10).

---

## 16. The stateful precompile framework (`precompile/`)

Precompiles are registered in three reserved address ranges (`0x01../0x02../0x03..`)
via `modules.RegisterModule` (deterministic, sorted by address). A `Module` bundles
a `ConfigKey`, address, a `StatefulPrecompiledContract` (4-byte function-selector
dispatch), and a `Configurator` (activates/deactivates the precompile and its
storage per upgrade). The `RulesExtra` built per-block exposes the active
precompiles, predicaters, and accepters. This is the same framework subnet-evm uses
for custom precompiles; the warp precompile is the C-Chain's main consumer (plus
Granite's `P256Verify`).

---

## 17. Networking & gossip (`network/`, `plugin/evm/eth_gossiper.go`)

Coreth runs **two coexisting request routers** over a single AppHandler, partitioned
by request ID parity (a migration mechanism):
- **Even request IDs** → the legacy coreth `network` handler (state-sync leaf/block/
  code requests, served with a half-deadline buffer and a concurrency semaphore).
- **Odd request IDs** → the AvalancheGo SDK `network/p2p` router.

**All gossip is SDK-handled.** Two SDK gossip systems run push+pull loops (push
~every 100 ms, pull ~every 1 s):
- **Eth tx gossip** at `p2p.TxGossipHandlerID` — a bloom-filter-backed
  `GossipEthTxPool` over the eth txpool; RPC-submitted txs are push-gossiped via the
  `EthPushGossiper`.
- **Atomic tx gossip** at `p2p.AtomicTxGossipHandlerID` — over the atomic mempool.

The legacy `network` also wraps a `SyncedNetworkClient` (synchronous AppRequest with
a buffered-length-1 waiting handler) used by the state-sync client, with
bandwidth-weighted peer prioritization.

---

## 18. APIs & clients (`eth/`, `plugin/evm/client/`)

### 18.1 The eth JSON-RPC backend (`eth/backend.go`, `api_backend.go`)

`CreateHandlers` exposes `/rpc` and `/ws` serving the standard libevm namespaces
(`eth`, `eth-filter`, `net`, `web3`, `debug`/tracing, `txpool`, plus eth-package
`admin`), filtered by `config.EthAPIs()`. Two Avalanche divergences are central:

- **Accepted-as-latest.** `EthAPIBackend` resolves "pending"/"accepted" (and, when
  `allowUnfinalizedQueries` is **off**, "latest") to the last *consensus-accepted*
  block, not the chain head. Queries above the last accepted block return
  `ErrUnfinalizedData` (or empty for `GetTransaction`) unless unfinalized queries are
  explicitly enabled — so the RPC never exposes unverified chain-head state.
- **No re-org / finality window.** There is no fork choice; `chainWithFinalBlock`
  documents a ~2-week finality window (wired only to the disabled blobpool).

Historical/archival state is served via `state_accessor.go`, dispatching by trie
scheme (hashdb replays forward on an ephemeral triedb; pathdb only live state;
Firewood reconstructs from a persisted revision via an FFI `Reconstructed` view).
Many debug APIs are unsupported under Firewood.

### 18.2 Avalanche namespaces & the C-Chain client

The atomic VM layer mounts the **`avax`** namespace (`/avax`): `issueTx`,
`getAtomicTx[Status]`, `getUTXOs` (atomic UTXOs exported to the C-Chain). The
**`admin`** namespace (coreth-specific, distinct from eth-admin) handles profiling
and `setLogLevel`/`getVMConfig`. The `warp` namespace (§15.2) is registered if
enabled. `plugin/evm/client/` is the avalanchego-facing Go client for these
endpoints (`NewCChainClient` targets `/ext/bc/C/avax`).

---

## 19. Component boundaries & relationships

```
        AvalancheGo snow engine + ProposerVM wrapper        [consensus.md, vm-framework.md]
                  │  block.ChainVM (+context, +StateSyncableVM)
                  ▼
        atomic/vm.VM  (WrapVM)  ── extension.Config ──►  per-block atomic hooks
                  │  embeds extension.InnerVM
                  ▼
        plugin/evm.VM  ── *chain.State (snowman caching) ──► wrappedBlock.Verify
                  │                                            = InsertBlockManual
                  ▼                                            (SYNCHRONOUS EVM exec)
        eth.Ethereum ── core.BlockChain ── StateProcessor ── StateDB
                  │            │                                  │
                  │            └─ dummy.DummyEngine (Avalanche header rules + fee verify)
                  │                                               ▼
                  │                                    trie DB: hashdb / pathdb / Firewood
        ┌─────────┼───────────────┬──────────────────┬────────────────────┐
        ▼         ▼               ▼                  ▼                     ▼
   atomic trie  warp backend   eth/avax/warp     SDK + legacy          state sync
   + shared mem (BLS signing,  JSON-RPC          p2p routers           (leaf/proof,
   [chains.md]   ACP-118)      [api.md]          + gossip              atomic-trie)
                [primitives.md]                  [networking.md]       [graft/evm]
```

| Boundary | Mechanism | Where |
|----------|-----------|-------|
| Engine ↔ VM | `block.ChainVM` / `snowman.Block` (sync Verify) | `wrapped_block.go` |
| VM ↔ extension (subnet-evm seam) | `extension.Config` / `BlockExtension` | `extension/config.go` |
| EVM ↔ Avalanche rules | `dummy.DummyEngine` + libevm extras | `consensus/dummy`, `params/` |
| VM ↔ cross-chain | `sharedMemory.Apply(ops, batch, batch)` | `atomic/state/atomic_state.go` |
| VM ↔ other subnets | Warp BLS signing + ACP-118 + predicates | `warp/`, `precompile/contracts/warp` |
| VM ↔ peers | dual router (even=legacy, odd=SDK) + gossip | `network/`, `eth_gossiper.go` |
| VM ↔ tooling | eth/avax/warp/admin JSON-RPC | `eth/`, `plugin/evm/client` |
| Sync ↔ network | leaf/block/code requests + range proofs | `graft/evm/sync` |

---

## 20. Key invariants, concurrency, security, TODOs

**Invariants:**
- Coinbase must be `constants.BlackholeAddr`; difficulty 1; nonce 0; no uncles;
  blobs disabled; blocks non-empty.
- **No re-org at or below the last-accepted block** (finality from Snowman).
- `InsertBlockManual` runs **exactly once** per block (guarded by
  `State.IsProcessing`); the build-time verify uses `writes=false` to avoid leaking
  a triedb reference.
- The dummy engine recomputes base fee / block gas cost / gas state and requires the
  header to match **exactly** (deterministic verification; ACP-176/226 verification
  passes the *claimed* excess so the expected value equals it iff reachable).
- Shared-memory ops are committed **atomically** with the block's versiondb batch;
  warpDB is intentionally **outside** versiondb (signatures needn't be atomic with
  block acceptance).
- The full base fee is **paid to the coinbase, not burned** (Avalanche divergence
  from EIP-1559).
- Atomic-trie roots include bonus-block ops (history matches) but skip their
  shared-memory application.
- State-sync trie reconstruction asserts `StackTrie.Commit() == targetRoot` (and
  Firewood asserts `Firewood.Root() == root`); leaf responses are range-proof-verified
  on both sides.

**Concurrency:** `builderLock` guards the (NormalOp-only) block builder;
`core.BlockChain.chainmu` serializes block insertion; a bounded acceptor queue
(default 64) processes accepted blocks asynchronously; the state-sync client retries
until context expiry with a bandwidth-weighted peer set.

**Security:** the node signs only warp messages it can attest to; unfinalized RPC
queries are gated off by default; `ExtRPCEnabled`/`UnprotectedAllowed` gate sensitive
APIs; request handling drops requests with <100 ms budget and caps concurrent
AppRequests with a semaphore.

**Notable TODOs / status:**
- Several `plugin/evm/atomic/vm` fields are flagged "TODO: unexport".
- `HealthCheck` is a stub (`plugin/evm/health.go:12`).
- pathdb freezer / state-history rollback is fully disabled in this fork.
- Firewood: node iteration/proofs unsupported (hence the FFI range-proof sync path);
  several debug APIs unsupported; the reconstructed-state release relies on GC.
- Coreth and subnet-evm leaf wire formats are to be unified in a future upgrade.
- The even/odd request-ID router split is a transitional migration mechanism.

---

## 21. Configuration & parameters

| Parameter | Default | Meaning | Source |
|-----------|---------|---------|--------|
| VM ID | `'evm'` (`constants.EVMID`) | C-Chain VM registration | `plugin/factory/factory.go:18` |
| `X2CRate` | 1e9 | nAVAX → wei (AVAX only) | `atomic/tx.go:33` |
| `CommitInterval` | 4096 | trie commit cadence (pruning) | `config/default_config.go` |
| `StateSyncCommitInterval` | 16384 | state-sync trie commit cadence | `config/default_config.go` |
| `StateSyncMinBlocks` | 300000 | min height gap to trigger state sync | `config/default_config.go` |
| `AcceptorQueueLimit` | 64 | async accepted-block queue depth | `config/default_config.go` |
| Pruning | on | prune old state (vs archival) | `config/default_config.go` |
| Push / pull gossip | 100 ms / 1 s | tx gossip cadence | `config/default_config.go` |
| `RPCGasCap` / `RPCTxFeeCap` | 50M / 100 AVAX | RPC call limits | `config/default_config.go` |
| ACP-176 `MinTargetPerSecond` | 1M | min gas target/sec (Fortuna) | `vms/evm/acp176` |
| ACP-176 `TargetToMaxCapacity` | 10 | gas limit = target·10 | `vms/evm/acp176` |
| AP3 `TargetGas` / AP5 | 10M / 15M | legacy fee-window gas target | `customheader` |
| AP5 atomic gas limit | 100k | per-block atomic gas ceiling | `atomic` (ap5) |
| Atomic tx intrinsic gas (AP5) | 10k | per-atomic-tx fixed gas | `atomic` (ap5) |
| `WarpQuorumDenominator` / default num | 100 / 67 | warp signature quorum | `precompile/contracts/warp` |
| state schemes | hashdb (default) / pathdb / Firewood | trie backend | `config` |

---

## 22. Cross-references

- [evm.md](evm.md) — C-Chain overview, the Coreth-vs-SAE framing, the graft model.
- [saevm.md](saevm.md) — the experimental async (ACP-194) successor C-Chain VM.
- [vm-framework.md](vm-framework.md) — `block.ChainVM` / `snowman.Block`, the
  ProposerVM/MeterVM/TracedVM wrapper stack, rpcchainvm plugins, the `chain.State`
  caching layer.
- [consensus.md](consensus.md) — the Snowman engine driving Verify/Accept.
- [chains.md](chains.md) — cross-chain atomic shared memory.
- [database.md](database.md) — versiondb, prefixdb, the database stack.
- [primitives.md](primitives.md) — IDs, codec, secp256k1 (atomic-tx sigs), BLS (warp).
- [networking.md](networking.md) — the p2p SDK, gossip, ACP-118 signature aggregation.
- [api.md](api.md) — how `CreateHandlers` plugs the JSON-RPC server into the node API server.
- [node.md](node.md) — node bootstrap, the upgrade schedule that gates the phases.
- [overview.md](overview.md) — top-level architecture.
- ACP-176 (dynamic gas), ACP-194 (SAE), ACP-226 (block delay) — linked inline.
