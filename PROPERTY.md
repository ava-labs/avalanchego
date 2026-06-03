# Differential Property Testing: SAE VM vs Coreth

A design for a property-based test harness that asserts the SAE C-Chain VM
(`vms/saevm/cchain`) and coreth (`graft/coreth`) produce **equivalent transaction
receipts and post-execution state** for the same sequence of transactions —
*despite* radically different block-processing machinery (streaming asynchronous
execution + gas-as-time pricing vs. synchronous insertion + EIP-1559 windowed
base fee).

---

## 1. Why this property is interesting (and non-trivial)

The two systems share the **same EVM**. Both ultimately call libevm's
`core.ApplyTransaction` to mutate state and produce a receipt:

- SAE: `vms/saevm/saexec/execution.go:194-207` calls `core.ApplyTransaction(config, chainCtx, &coinbase, &gasPool, stateDB, header, tx, &blockGasConsumed, vm.Config{})`.
- Coreth: `graft/coreth/core/state_processor.go:121-169` (`applyTransaction`) calls the same libevm entry point.

Both modules depend on the *same pinned* `github.com/ava-labs/libevm`
(`go.mod` — `v1.13.15-0.20260430...`). Neither reimplements the interpreter,
state trie, or receipt construction. So the EVM-level outcome of a transaction
is a **pure function of `(pre-state, tx, block header)`**.

Everything that differs lives *around* that call:

| Concern | Coreth | SAE |
|---|---|---|
| Execution timing | synchronous: `InsertChain` executes before accept | asynchronous: `AcceptBlock` enqueues; a background goroutine executes later (`sae/consensus.go:41-123`, `saexec/execution.go:62-98`) |
| When receipts exist | at insert time | after `block.WaitUntilExecuted(ctx)` resolves (`blocks/execution.go`) |
| Base fee | EIP-1559 windowed, function of *parent block gas vs target* (`plugin/evm/customheader/base_fee.go`) | gas-as-time: `baseFee = e^(excess/K)`, function of *wall-clock time advanced + gas consumed* (`gastime/gastime.go:129-149`, `gastime/acp176.go:44-67`) |
| Gas limit | ~fixed per network | dynamic worst-case bound per block (`sae/block_builder.go:241`) |
| Extra block ops | none | cross-chain import/export "end-of-block ops" (`saexec/execution.go:236-249`) |

**The thesis under test:** *the surrounding machinery should not perturb EVM
execution.* If we hold the EVM-relevant inputs equal, the receipt fields that
come out of `core.ApplyTransaction` must be byte-identical. The fields the two
systems compute *themselves* (base fee, effective gas price, block hash) are
allowed to differ — and the harness must precisely separate those two classes.

This is exactly the kind of invariant that property testing is built for: it is
universally quantified over transaction sequences, the two implementations are
genuinely independent code paths, and a single counterexample is a real bug.

---

## 2. The equivalence relation

A receipt has three classes of field. Classification is grounded in
`saexec/execution.go:220-227` (the only place SAE overwrites receipt fields after
`core.ApplyTransaction`).

### Class A — MUST be identical (pure EVM output)
Produced inside `core.ApplyTransaction`, depend only on `(pre-state, tx, header)`:

- `Status` (1 success / 0 revert)
- `GasUsed`
- `Logs` (and each log's `Address`, `Topics`, `Data`, `Index`)
- `ContractAddress` (for CREATE)
- `PostState` (pre-Byzantium root bytes; typically empty under test config)
- `Type` (mirrors `tx.Type()`)

**Post-execution world state** is also Class A and is the strongest single
check: the libevm state root after applying the same txs to the same pre-state
must be identical. SAE exposes it via `PostExecutionStateRoot` (`blocks/execution.go:205`);
coreth via `statedb.IntermediateRoot(...)` / `header.Root`.

### Class B — identical *iff* inputs are normalized
Depend on tx ordering and on the base fee:

- `CumulativeGasUsed` — identical iff the **same txs are included in the same order**.
- `TransactionIndex` — same condition.
- `EffectiveGasPrice` = `baseFee + min(GasTipCap, GasFeeCap - baseFee)`
  (identical formula on both sides: `saexec/execution.go:226-227`). Identical
  **iff `baseFee` is equal** between the two systems for the block.

### Class C — expected to differ (do NOT compare for equality)
- `BlockHash`, `BlockNumber` (different block structure / numbering)
- `Bloom` derived per block (matches iff logs+order match — treat as derived from Class A)
- Block header fields: timestamp encoding, gas limit, extra data, base fee.

### Normalization strategy to promote Class B → comparable

The harness runs in a **normalized mode** that removes the two legitimate
divergence sources so the comparison can be tight:

1. **Pin the base fee on both sides to the same constant.**
   - SAE: set `GasPriceConfig.StaticPricing = true` with a fixed `MinPrice`
     (`gastime/config.go:24-26`; honored at `gastime/gastime.go:66-68,157-159`).
     This makes `excess` never accumulate, so `baseFee == MinPrice` forever.
     Inject via the hook returned by `cchain/hooks.go:117-123` (`GasConfigAfter`)
     — the harness uses a test hooks build (or `hookstest` stub) that returns
     `StaticPricing: true`.
   - Coreth: use the `dummy` consensus engine in a fee-skipping mode
     (`dummy.NewETHFaker()` / `Mode{ModeSkipBlockFee: true}`,
     `consensus/dummy/consensus.go:68-98`) and a chain config / fork whose base
     fee resolves to the same constant, or post-process by overriding the header
     base fee before re-deriving `EffectiveGasPrice`. Practically: derive
     `EffectiveGasPrice` on both sides from a **canonical base fee** the harness
     chooses, rather than each VM's native base fee.
2. **Force identical transaction inclusion + order.** Rather than rely on each
   VM's mempool ordering (SAE filters by base fee and worst-case gas in
   `sae/block_builder.go:245-277`; coreth sorts by effective gas price), the
   harness *constructs the block's tx list itself* and feeds the **same ordered
   list** to both. See §5.
3. **Exclude cross-chain end-of-block ops** in the baseline property (pure-EVM
   txs only), so SAE's `EndOfBlockOps` set is empty and cannot perturb state.
   A later phase (§9) adds them back as a separate, SAE-only property.

With (1)–(3), Class A **and** Class B must match exactly. Class C is ignored.

### The oracle, precisely

```
EquivReceipts(rs_sae, rs_cor) :=
    len(rs_sae) == len(rs_cor)  ∧
    ∀i. classA(rs_sae[i]) == classA(rs_cor[i])          // status,gasUsed,logs,contractAddr,type,postState
      ∧ rs_sae[i].CumulativeGasUsed == rs_cor[i].CumulativeGasUsed
      ∧ rs_sae[i].TransactionIndex  == rs_cor[i].TransactionIndex
      ∧ canonicalEGP(rs_sae[i])     == canonicalEGP(rs_cor[i])   // recomputed from canonical baseFee

EquivState(sae, cor) := postExecStateRoot(sae) == postExecStateRoot(cor)

EquivReceiptRoot := DeriveSha(receipts_sae) == DeriveSha(receipts_cor)   // after normalizing block-scoped fields
```

`EquivState` is the load-bearing assertion: equal state roots imply the *entire*
world state agrees, which is far stronger than comparing receipts field by field.

---

## 3. Harness architecture

```
                 ┌──────────────────────────────┐
                 │   Property runner (rapid)     │
                 │   rapid.Check(t, prop)        │
                 └───────────────┬──────────────┘
                                 │ Scenario{genesis, []Block{ordered txs}}
              ┌──────────────────┴───────────────────┐
              ▼                                       ▼
   ┌─────────────────────┐                 ┌─────────────────────┐
   │   SAE driver        │                 │   Coreth driver     │
   │ (cchain SUT, async) │                 │ (vmtest VM, sync)   │
   │ issue → build →     │                 │ AddRemotesSync →    │
   │ accept → WaitUntil  │                 │ BuildBlock →        │
   │ Executed → receipts │                 │ Verify → Accept →   │
   │                     │                 │ GetReceiptsByHash   │
   └──────────┬──────────┘                 └──────────┬──────────┘
              │ ReceiptsByBlock, stateRoot            │ ReceiptsByBlock, stateRoot
              └──────────────────┬────────────────────┘
                                 ▼
                       ┌───────────────────┐
                       │  Comparison oracle │  EquivReceipts ∧ EquivState
                       │  (§2). On mismatch:│
                       │  dump scenario +   │
                       │  per-field diff    │
                       └───────────────────┘
```

The two drivers implement a common interface so the property is written once:

```go
// A VM that can be fed a deterministic, pre-ordered block and report results.
type DiffVM interface {
    // Genesis state, same alloc + chain config on both sides.
    Init(t testing.TB, genesis *core.Genesis, baseFee *big.Int)

    // Apply exactly these txs, in this order, as one block on top of the tip.
    // Blocks until receipts are available (SAE: WaitUntilExecuted).
    ApplyBlock(t testing.TB, txs types.Transactions) BlockResult

    Shutdown(t testing.TB)
}

type BlockResult struct {
    Receipts       types.Receipts // normalized: block-scoped fields zeroed
    PostStateRoot  common.Hash
    IncludedTxs    []common.Hash  // to assert both included the same set/order
}
```

- **SAE driver** wraps the `SUT` pattern from `vms/saevm/cchain/vm_test.go:63-158`
  (`newSUT`, `runConsensusLoop`, `WaitUntilExecuted`), reading receipts either via
  `ethclient.TransactionReceipt` or directly from the executor's recent-receipt
  cache (`saexec/receipts.go:48-73`) / `sae/rpc/receipts.go`.
- **Coreth driver** wraps `graft/coreth/plugin/evm/vmtest` (`SetupTestVM`,
  `IssueTxsAndSetPreference`/`IssueTxsAndBuild`, `block.Accept`) and reads
  `vm.blockChain.GetReceiptsByHash(...)` (`core/blockchain_reader.go:175-194`).

Both must be configured with the **same `core.Genesis`** (same `ChainConfig`,
same `Alloc`, same `GasLimit`) and the **same fork/rules**, so the libevm `Rules`
fed into `ApplyTransaction` are identical.

### Two driver fidelity levels

1. **Full-VM mode (highest fidelity, default).** Drive both real VMs end-to-end
   as above. Exercises SAE's async execution, persistence, and RPC paths.
2. **Direct-executor mode (fast, for shrinking/large sweeps).** Compare SAE's
   `saexec.Execute` (`saexec/execution.go:149-278`) against coreth's
   `StateProcessor.Process` (`core/state_processor.go:71-119`) on a shared
   in-memory libevm `state.StateDB`, same header. Strips away consensus/RPC and
   isolates the EVM-output equivalence. Run thousands of cases cheaply, then
   replay any counterexample in full-VM mode.

---

## 4. Reusing existing test infrastructure

Nothing here is greenfield — the building blocks exist:

- **Property framework:** [`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid)
  — a modern, generics-based Go property-testing library. It is **not yet a
  dependency**; add it to the root `go.mod` `require` block (and re-run
  `go-mod-tidy` + bazel sync per CLAUDE.md). Rapid is preferred over gopter here
  for three reasons that matter to this harness:
  - **Imperative generators with automatic, type-safe shrinking** — no
    hand-written shrinkers. A failing `Scenario` is minimized for free.
  - **Native `go test -fuzz` bridge** via `rapid.MakeFuzz`, so the same property
    doubles as a fuzz target (precedent for go-native fuzzing in the repo:
    `vms/saevm/cchain/tx/txtest/fuzzer_test.go`).
  - **Built-in stateful / state-machine testing via `t.Repeat`** — a natural fit
    for SAE's *streaming* multi-block model (see §5 "Stateful mode").

  Core API used: `rapid.Check(t, func(t *rapid.T){...})`, generators
  (`rapid.IntRange`, `rapid.SampledFrom`, `rapid.SliceOfN`, `rapid.Custom`,
  `rapid.Bool`, …) materialized with `.Draw(t, "label")`, `rapid.MakeCheck` for
  subtests, and `rapid.MakeFuzz` for the fuzz entry. Reproduction/control flags:
  `-rapid.checks=N`, `-rapid.seed=N`, `-rapid.steps=N` (and `RAPID_*` env vars).
- **Deterministic wallets / signed txs:** `vms/saevm/saetest/wallet.go`
  (`NewUNSAFEWallet`, `NewUNSAFEKeyChain`, `SetNonceAndSign`,
  `MaxAllocFor(addrs...)`) — deterministic keys via
  `ethtest.UNSAFEDeterministicPrivateKey`, reproducible by seed.
- **Block / chain builders:** `vms/saevm/blocks/blockstest` (`NewEthBlock`,
  `NewBlock`, `NewGenesis`, `ChainBuilder`).
- **Chain config:** `saetest.ChainConfig()` (= `params.MergedTestChainConfig`),
  `types.LatestSigner(config)`.
- **Coreth genesis + funded accounts:** `graft/coreth/plugin/evm/vmtest`
  (`SetupTestVM`, `GenesisJSON`, `TestKeys`, `TestEthAddrs`,
  `IssueTxsAndSetPreference`).
- **Hook stubs (for StaticPricing / fixed gas target):**
  `vms/saevm/hook/hookstest/stub.go` (`NewStub`, `WithGasPriceConfig`, `WithNow`).

The harness should generate **one** set of accounts/keys and one genesis alloc,
then build both genesis representations (coreth `core.Genesis` JSON and SAE
genesis) from that single source of truth, so allocations are bit-identical.

---

## 5. The generator (property input)

The unit of generation is a **Scenario**: a genesis plus a list of blocks, each
block being an *ordered* list of signed transactions.

```go
type Scenario struct {
    Accounts   int                  // funded EOAs (deterministic keys)
    BaseFee    *big.Int             // canonical pinned base fee
    Blocks     [][]TxSpec           // ordered txs per block
}

type TxSpec struct {
    From     int            // account index (signer)
    Kind     TxKind         // Transfer | Create | Call | Revert | OOG | SelfDestruct
    To       int            // account index, or -1 for CREATE
    Value    *big.Int
    GasLimit uint64
    Tip,Cap  *big.Int       // for dynamic-fee txs; legacy uses GasPrice
    Data     []byte         // bytecode for CREATE; calldata for CALL
    TxType   uint8          // 0 legacy, 1 access-list, 2 dynamic-fee
}
```

A `Scenario` is produced by a `rapid.Custom` generator that draws each field by
label; rapid threads `*rapid.T` and shrinks every drawn value automatically:

```go
func scenarioGen() *rapid.Generator[Scenario] {
    return rapid.Custom(func(t *rapid.T) Scenario {
        accts  := rapid.IntRange(2, 8).Draw(t, "accounts")
        nBlock := rapid.IntRange(1, 6).Draw(t, "blocks")
        blocks := make([][]TxSpec, nBlock)
        for b := range blocks {
            n := rapid.IntRange(0, 12).Draw(t, "txsInBlock")
            txs := make([]TxSpec, n)
            for i := range txs {
                txs[i] = TxSpec{
                    From:     rapid.IntRange(0, accts-1).Draw(t, "from"),
                    Kind:     rapid.SampledFrom(allTxKinds).Draw(t, "kind"),
                    To:       rapid.IntRange(-1, accts-1).Draw(t, "to"),
                    TxType:   rapid.SampledFrom([]uint8{0, 1, 2}).Draw(t, "txType"),
                    GasLimit: rapid.Uint64Range(21_000, 5_000_000).Draw(t, "gas"),
                    // Value/Tip/Cap/Data drawn per Kind...
                }
            }
            blocks[b] = txs
        }
        return Scenario{Accounts: accts, BaseFee: canonicalBaseFee, Blocks: blocks}
    })
}
```

Generation rules:

- **Nonces are assigned by the harness**, not generated — keep a per-account
  nonce counter while materializing `TxSpec → *types.Transaction` so txs are
  valid and deterministically ordered (mirrors `wallet.SetNonceAndSign`).
- **Sign with the shared signer** `types.LatestSigner(config)` and the
  deterministic keys.
- **Funding:** start from `MaxAllocFor(addrs...)` so balance underflow is rare
  early; let the generator *occasionally* exceed balance to exercise the failure
  path (both VMs must agree it fails identically).
- **Bias toward interesting EVM behavior**, since uniform random calldata mostly
  reverts trivially:
  - simple value transfers (21000 gas),
  - contract creation with small known bytecodes (e.g., a counter, a `LOG`
    emitter, a `REVERT`-on-condition, an out-of-gas loop, a `SELFDESTRUCT`),
  - calls into previously-created contracts (track created addresses across
    blocks so later blocks can call them — this is what makes multi-block
    scenarios valuable),
  - mixed tx types (legacy / access-list / dynamic-fee) to cover receipt `Type`
    and gas-accounting branches.
- **Multi-block** scenarios are essential: they exercise SAE's settlement /
  cross-block state carry-over and the async execution pipeline, and they catch
  divergences that only appear once state has accumulated.

**Determinism & reproducibility:** rapid drives all randomness from its own seed,
so a failing run is fully reproducible with `-rapid.seed=<N> -rapid.checks=1`
(rapid prints the seed and the minimized draws on failure). No manual seed
plumbing is needed; just make `TxSpec → *types.Transaction` materialization (nonce
assignment, signing) a pure function of the drawn `Scenario` so replay is
deterministic. **Shrinking is automatic** — rapid minimizes the drawn values
(fewer blocks, fewer txs per block, smaller values/gas, simpler tx kinds) with no
custom shrinker code. Persist the minimized `Scenario` as a pinned regression
test (§8).

### Stateful mode (optional, models SAE streaming)

Multi-block scenarios can instead be expressed as a rapid **state machine** via
`t.Repeat`, which is a more faithful model of SAE's streaming pipeline than a
flat list of blocks. Actions:

- `"buildBlock"` — draw an ordered tx list, apply it to *both* VMs, stash the
  expected receipts in the model;
- `"readReceipt"` — pick a previously-issued tx hash and assert both VMs return
  equivalent receipts (exercises SAE's async availability window);
- `""` (rapid's invariant action) — assert the two post-execution state roots are
  equal after every step.

```go
rapid.Check(t, func(t *rapid.T) {
    sae, cor := newSAEDriver(t), newCorethDriver(t)
    model := newModel()
    t.Repeat(map[string]func(*rapid.T){
        "buildBlock":  func(t *rapid.T) { /* draw txs; apply to both; record */ },
        "readReceipt": func(t *rapid.T) { /* compare a stored tx's receipts */ },
        "":            func(t *rapid.T) { requireEqualStateRoot(t, sae, cor) },
    })
})
```

---

## 6. The properties

Each property is a `rapid.Check` body that draws a `Scenario` and runs the
oracle:

```go
func TestReceiptEquivalence(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        s := scenarioGen().Draw(t, "scenario")
        sae, cor := newSAEDriver(t, s), newCorethDriver(t, s)
        for _, blk := range materialize(s) {            // []types.Transactions
            rs := sae.ApplyBlock(t, blk)
            rc := cor.ApplyBlock(t, blk)
            requireEquivReceipts(t, rs, rc)             // §2 oracle
            require.Equal(t, rc.PostStateRoot, rs.PostStateRoot)
        }
    })
}
```

### P1 — Receipt & state equivalence (core property)
For every scenario, applying the identical ordered tx list to both VMs yields
`EquivReceipts ∧ EquivState ∧ EquivReceiptRoot` (§2) for every block.

### P2 — Inclusion agreement
Both VMs include the same transactions in the same order (`IncludedTxs` equal).
This is *given* in full-fidelity mode because the harness supplies the ordered
list directly; assert it to catch a VM silently dropping/reordering a tx
(e.g., SAE's gas-limit-based truncation in `block_builder.go:245-277`).

### P3 — Failure-mode agreement
A tx that fails (revert, out-of-gas, bad nonce, insufficient funds) fails the
**same way** on both: same `Status`, same `GasUsed`, and crucially the same
*effect on subsequent txs* (nonce not consumed on rejection vs. consumed on
revert). Compare the post-block state root, not just the receipt.

### P4 — Determinism / replay (SAE-specific, catches async bugs)
Running the same scenario through SAE twice (fresh VM each time) yields identical
receipts and state root. Also: after `WaitUntilExecuted`, re-reading receipts
from disk (`rawdb`) equals the in-flight cache value
(`saexec/receipts.go`). This guards the async-execution / persistence path
that has no analogue in coreth.

### P5 — Receipt root consistency
For each block, `types.DeriveSha(receipts, trie.NewStackTrie(nil))` computed from
SAE's receipts equals the value coreth stores in `header.ReceiptHash`
(`core/block_validator.go:123-145`), after normalizing block-scoped fields.

---

## 7. Handling the legitimate divergences (don't get false positives)

These are the traps that will produce spurious failures unless explicitly
controlled. Each must be neutralized in normalized mode:

1. **Base fee.** Pin via SAE `StaticPricing` + coreth fee-skip engine, OR
   recompute `EffectiveGasPrice` on both sides from one canonical base fee before
   comparing. (§2.) Without this, every dynamic-fee tx's `EffectiveGasPrice`
   diverges and the property is meaningless.
2. **Block hash / number / timestamp.** Zero these out (Class C) before any
   receipt comparison. SAE encodes time differently (`cchain/hooks.go:130-133`,
   `TimeMilliseconds` TODO) and numbers blocks within its own chain.
3. **Bloom / receipt root.** Recompute from normalized receipts; do not trust the
   stored per-block value, which embeds block-scoped fields.
4. **Cross-chain end-of-block ops.** Excluded in the baseline (pure-EVM txs).
   They mutate SAE state with no coreth equivalent (`saexec/execution.go:236-249`).
5. **Gas limit drift.** SAE's dynamic worst-case gas limit
   (`sae/block_builder.go:241`) can differ from coreth's. Because the harness
   supplies the tx list directly and uses a generous fixed limit on both, this
   only matters if a generated block's total gas exceeds a limit — generate
   within a shared cap to avoid asymmetric truncation, and let P2 catch it if it
   happens.
6. **Coinbase / fee recipient.** Ensure both use the same coinbase so any
   fee-credit state delta matches (affects `EquivState`). With `StaticPricing`
   and equal base fee, fee credits are equal; verify the coinbase address is
   identical in both headers.
7. **Fork/rules.** Both must run the *same* libevm `Rules` (same fork
   activation). Pick one fork for the baseline (e.g., the latest merged test
   config) and parametrize later.

---

## 8. Failure reporting & shrinking

Rapid already prints the **seed**, the **minimized draws**, and the failing
assertion. The harness augments that with domain detail:

- the `-rapid.seed=<N>` line to reproduce (rapid emits it automatically),
- the full minimized `Scenario` JSON (log it in the property body on failure),
- the **block index** and **tx index** of the first divergence,
- a **per-field diff** of the two receipts (which Class-A/B field differs),
- both **post-execution state roots**, and if they differ, a dump of the
  differing accounts/storage (iterate both `state.StateDB`s, diff balances /
  nonces / code / storage),
- the canonical base fee used.

Promote every shrunk counterexample into a deterministic table-test under
`vms/saevm/cchain/...` so regressions are pinned without re-running the fuzzer.

---

## 9. Phasing

1. **Phase 0 — Direct-executor harness (fastest path to signal).** Shared
   in-memory `state.StateDB` + fixed header; run `saexec.Execute` vs
   `StateProcessor.Process` on the same ordered txs; assert `EquivReceipts ∧
   EquivState`. No consensus, no RPC, no async. Validates the equivalence
   relation and the normalization knobs cheaply.
2. **Phase 1 — Generator + rapid.** Add the `scenarioGen` `rapid.Custom`
   generator, biased tx kinds, multi-block. Run P1–P3, P5 via `rapid.Check` in
   direct-executor mode (shrinking comes for free).
3. **Phase 2 — Full-VM drivers.** Wrap `cchain` `SUT` and coreth `vmtest`; run
   the same scenarios end-to-end (build → accept → execute → read receipts).
   Adds P4 (async determinism / disk-vs-cache).
4. **Phase 3 — Re-enable divergence sources as separate properties.** Dynamic
   base fee (assert only Class A + state root still match while `EffectiveGasPrice`
   is allowed to differ by the documented formula), cross-chain end-of-block ops
   (SAE-only invariants), multiple forks.
5. **Phase 4 — Native `go test -fuzz` corpus + CI.** Wrap the same property body
   in `f.Fuzz(rapid.MakeFuzz(prop))` so `FuzzReceiptEquivalence` reuses the rapid
   generator (rapid drives draws from the fuzzer's input bytes). Wire into a
   non-blocking CI job (this is a stricter-bar `vms/saevm` package — see
   `docs/invariants.md` and the `lint-saevm` note in CLAUDE.md).

---

## 10. Proposed file layout

```
vms/saevm/cchain/difftest/            # new package, _test.go only
  scenario.go        # Scenario, TxSpec, scenarioGen (rapid.Custom), materialize()
  scenario_gen_test.go
  diffvm.go          # DiffVM interface, BlockResult, normalize()
  driver_sae.go      # SAE driver (wraps cchain SUT / saexec.Execute)
  driver_coreth.go   # coreth driver (wraps plugin/evm vmtest / StateProcessor)
  oracle.go          # EquivReceipts, EquivState, per-field diff + reporting
  property_test.go   # rapid.Check: P1..P5 (+ t.Repeat stateful variant)
  fuzz_test.go       # FuzzReceiptEquivalence via rapid.MakeFuzz
  regressions_test.go# pinned minimized counterexamples
```

Keep it under `vms/saevm` so it inherits the stricter `lint-saevm` (gosec G115)
bar. Tests need `-tags test` (CLAUDE.md). Both modules are in the `go.work`
workspace, so a test in the root module can import `graft/coreth/...` directly.

---

## 11. Risks & open questions

- **Base-fee pinning fidelity.** Forcing `StaticPricing` on SAE and a fee-skip
  engine on coreth tests the EVM under an *unnatural* fee regime. Mitigate by
  also running Phase-3 (dynamic fees, Class-A-only comparison) so we don't only
  ever test the pinned case.
- **State-root comparability.** Both use libevm tries, but confirm both commit
  with the same `IsEIP158`/scheme and that SAE's `PostExecutionStateRoot`
  corresponds to the same commit point coreth uses (`statedb.Commit` vs
  `IntermediateRoot`). If trie schemes differ (hash vs Firewood,
  `plugin/evm/vmtest` exposes both), force the same scheme on both.
- **Coreth header construction.** Coreth's `dummy` engine
  (`FinalizeAndAssemble`, `consensus/dummy/consensus.go:292-361`) injects fee
  state into `header.Extra` and may set `BlockGasCost`; ensure these don't change
  the EVM `Rules` or the gas charged. The fee-skip mode avoids the *validation*
  but the header still carries a base fee — recompute `EffectiveGasPrice` from a
  canonical value to be safe.
- **Async timing flakiness.** Full-VM SAE runs depend on `WaitUntilExecuted`;
  always block on it before reading and never assert on wall-clock timing.
- **Tx inclusion asymmetry.** If either VM declines a tx the other accepts
  (gas-limit truncation, mempool policy), P2 fires. Decide whether that is a bug
  or an expected policy difference *per tx kind* and encode the decision.
- **Determinism of `core.ApplyTransaction` across the two call sites.** They
  pass `vm.Config{}` — confirm both pass an empty/identical `vm.Config` (tracer
  off, no extra hooks) so no debug path perturbs gas.

---

## 12. One-paragraph summary

Both VMs run the *same* libevm EVM via `core.ApplyTransaction`; only the
consensus, timing, fee, and gas-limit machinery around it differ. The harness
generates random multi-block sequences of signed transactions, feeds the
**identical ordered list** to both a SAE driver (`cchain` SUT, async
build→accept→`WaitUntilExecuted`) and a coreth driver (`plugin/evm` vmtest,
build→accept→`GetReceiptsByHash`), after **normalizing** the two legitimate
divergence sources (base fee pinned via SAE `StaticPricing` + coreth fee-skip
engine; tx ordering fixed by construction). It then asserts that the pure-EVM
receipt fields (`Status`, `GasUsed`, `Logs`, `ContractAddress`, `Type`,
`CumulativeGasUsed`, `TransactionIndex`, canonical `EffectiveGasPrice`) and —
most importantly — the **post-execution state root** are identical, while
ignoring block-scoped fields (`BlockHash`, number, timestamp). The property is
driven by `pgregory.net/rapid`, which automatically shrinks any counterexample to
a minimal scenario (then pinned as a regression test) and doubles as a
`go test -fuzz` target via `rapid.MakeFuzz`.
```
