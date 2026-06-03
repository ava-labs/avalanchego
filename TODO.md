# SAE VM — Unfinished Work Checklist

A consolidated checklist of all TODOs and unfinished work found under `vms/saevm/`,
ordered roughly by **easiness and dependency** (quick self-contained wins first,
externally-blocked items last). Each item links to a `file:line` reference.

> Scope: a sweep of every `.go` file under `vms/saevm/` plus `docs/`. All
> unfinished work is `TODO`-marked — there are **no** `FIXME`/`XXX`/`HACK`
> comments, no `panic("not implemented")`, and no stubbed-out functions. The
> `proxytime/`, `intmath/`, `cmputils/`, `types/`, and `params/` packages have
> no markers.

---

## Tier 1 — Quick wins (TRIVIAL / EASY, self-contained, unblocked)

- [ ] **Make `SnapshotCacheSizeMB` configurable** — `saedb/tracker.go:42`
  Hardcoded `const = 128`; move onto the `Config` struct. *(TRIVIAL)*
- [ ] **Slim down `saedb.Config` / construct `*triedb.Config` in a method** — `saedb/tracker.go:26`
  Store only minimal primitive fields; build the heavy struct in `NewTracker`. Pairs with the item above. *(EASY)*
- [ ] **Cache `Tx.Bytes()`** — `cchain/tx/tx.go:123`
  Avoid re-marshalling on every call (also speeds up `ID()`). Mind concurrent sharing across txpool/builder. *(EASY)*
- [ ] **Cache `Tx.ID()`** — `cchain/tx/tx.go:109`
  Avoid recomputing `Bytes()`+hash each call. Do together with the `Bytes()` cache. *(EASY)*
- [ ] **Verify `extDataHash` matches the hash of `extData` during parsing** — `cchain/hooks.go:335`
  Build sets the hash but the parse path never validates it — a consistency/security check. *(EASY)*
- [ ] **Avoid DB read to confirm canonicality (2 sites — same task)** — `blocks/access.go:170`, `blocks/access.go:226`
  `ResolveRPCNumberOrHash` / `FromHash` do a `rawdb.ReadCanonicalHash` solely to enforce `RequireCanonical`; derive canonicality from the in-memory consensus view instead. *(EASY)*
- [ ] **Decode the millisecond timestamp in `BlockTime`** — `cchain/hooks.go:131`
  `HeaderExtra.TimeMilliseconds` already exists; combine with seconds. Consumer side of the encode item in Tier 3. *(EASY)*
- [ ] **Binary-search percentile thresholds in `tipPercentiles`** — `gasprice/block_cache.go:81`
  Prefix-sum gas + `slices.BinarySearch` per percentile (O(txs+p) → O(p·log txs)). Perf-only, deferred until large blocks demand it. *(EASY)*
- [ ] **Export `blocks.executionResults` to collapse duplicate DB reads** — `sae/rpc/stateful.go:111`
  `PostExecutionStateRoot` + `ExecutionBaseFee` each read+unmarshal the same record. *(EASY)*
- [ ] **Allow minimal node config via `configBytes`** — `cchain/vm.go:70`
  Currently ignored; define and parse a small operator config (pruning, cache sizes). *(EASY–MEDIUM)*

---

## Tier 2 — Medium, mostly self-contained

- [ ] **Decode ACP-176 target-excess / ACP-183 min-price excess in `GasConfigAfter`** — `cchain/hooks.go:118`
  Currently returns hardcoded gas config; read params from the header. Pairs with the encode items in Tier 3 (land together). *(MEDIUM)*
- [ ] **Skip shared-memory ops for bonus blocks in `Apply`** — `cchain/state/state.go:175`
  Import the known mainnet bonus-block set (Coreth has it) and skip reapplying their ops. *(MEDIUM)*
- [ ] **Store tx height instead of full tx bytes in `writeTx`** — `cchain/state/state.go:273`
  Fetch the tx from the block on read (`GetTx`) instead of double-writing. Changes on-disk index format + API read path. *(MEDIUM)*
- [ ] **Non-copying iteration in `Pending.Iter`** — `cchain/txpool/txpool.go:316`
  Currently snapshots+sorts a full copy of the pool per call; provide a lazy sorted view. *(MEDIUM)*
- [ ] **Parallelize import transfer/signature verification** — `cchain/tx/import.go:179`
  Sequential `verifyCredentials`; parallelize carefully around the shared `sigCache` dedup. Perf-only. *(MEDIUM)*
- [ ] **Parallelize export signature verification** — `cchain/tx/export.go:184`
  Same shared-`sigCache` challenge as the import item; do them together. Perf-only. *(MEDIUM)*
- [ ] **Fix `WaitForEvent` busy loop on `PendingTxs`** — `cchain/vm.go:161`
  Txpool only clears after execution, so pending txs can re-signal while their block is still processing. Coordinate txpool state with in-flight blocks. *(MEDIUM)*
- [ ] **Replace LRU `blockCache` with a ring buffer (if contention observed)** — `gasprice/block_cache.go:96`
  Heights are monotonic, so a fixed ring keyed by height avoids LRU overhead. Speculative/perf-only. *(MEDIUM)*
- [ ] **Invert `gastime.New` to take `startingPrice` instead of `startingExcess`** — `gastime/gastime.go:47`
  Callers can't reason about raw ACP-176 excess; derive it from a human-meaningful price. Requires inverting the exp price formula (touches `intmath`) + updating all callers. *(MEDIUM)*
- [ ] **Re-confirm the “header hash never read” assumption on libevm upgrades** — `sae/rpc/stateful.go:100`
  `StateAndHeaderByNumberOrHash` mutates Root/BaseFee on the returned header; add a guard/regression test so a libevm bump can't silently break it. *(MEDIUM)*
- [ ] **Centralize the settled-height race guard in `readHash`/`readByHash`** — `sae/blocks.go:208`
  Move the ad-hoc `settled.Height() < num` check into the helper that already holds the `syncMap` lock. *(MEDIUM)*
- [ ] **Decouple `txToSummary` from libevm's internal format** — `sae/rpc_test.go:328`
  Test duplicates libevm's tx-summary string format; expose it upstream or stop depending on the exact format. *(EASY/MEDIUM, test-only)*

---

## Tier 3 — Cross-package / design work (often producer↔consumer pairs)

These header/block-encoding items are the **producer** side of several Tier 1/2
consumer items; encode + decode should land together and depend on ACP field
layouts being finalized.

- [ ] **Encode ACP-176 target excess in the header (`BuildHeader`)** — `cchain/hooks.go:193` *(MEDIUM)*
- [ ] **Encode ACP-183 min-price excess in the header (`BuildHeader`)** — `cchain/hooks.go:194` *(MEDIUM)*
- [ ] **Enforce minimum block time in `BuildHeader`** — `cchain/hooks.go:195`
  Relates to the `WaitForEvent` min-delay item below. *(EASY–MEDIUM)*
- [ ] **Encode the millisecond timestamp (`BuildHeader`)** — `cchain/hooks.go:214`
  Producer side of the `BlockTime` decode (Tier 1); needs ms-precision clock plumbed in. *(EASY)*
- [ ] **Encode the ACP-226 min-delay excess (`BuildHeader`)** — `cchain/hooks.go:216`
  Track delay excess across blocks. *(MEDIUM)*
- [ ] **Wait for the minimum block delay (ACP-226) in `WaitForEvent`** — `cchain/vm.go:166`
  Currently returns immediately on pending txs. Pairs with the min-block-time + min-delay-excess items. *(MEDIUM)*
- [ ] **Encode `settled` in the block (`BuildBlock`) + decode in `SettledBy`** — `cchain/hooks.go:333`, `cchain/hooks.go:126`
  Producer/consumer pair; define the settled-block encoding in the block format. *(MEDIUM)*
- [ ] **Push-gossip cross-chain txs in `IssueTx`** — `cchain/api.go:224`
  Wire an AppSender/gossiper (via `txgossip`) into the service. *(MEDIUM)*
- [ ] **Replace raw `core.Genesis` with Coreth's genesis format** — `cchain/vm.go:80`
  Affects test fixtures (`vm_test.go:88`) too. *(MEDIUM)*
- [ ] **Unify the hook clock and the VM config clock** — `sae/vm_test.go:266`
  A single time source instead of keeping `hooks.Now` and `vmConfig.Now` in sync (footgun). Production change in `Config`/`hook`. *(MEDIUM)*
- [ ] **Early-terminate end-of-block-ops loop on insufficient block space** — `sae/block_builder.go:280`
  Have `hook.PointsG.PotentialEndOfBlockOps` signal remaining space; cross-package interface change (Coreth/Subnet-EVM implementors). *(MEDIUM)*
- [ ] **Skip exhausted accounts in `TransactionsByPriority`** — `txgossip/priority.go:26`
  Let the block builder signal an invalid low-nonce tx so higher-nonce txs from that sender are skipped. Needs a builder↔priority feedback channel. *(MEDIUM)*
- [ ] **Finer-grained settlement determination via minimum gas consumption** — `blocks/settlement.go:304`
  Compute a per-block lower-bound gas (`intrinsicGasSum`) to prove settlement without waiting on actual execution. Coupled with the next item. *(MEDIUM)*
- [ ] **Propagate interim execution-time to queued blocks** — `saexec/execution.go:211`
  Only worthwhile if `LastToSettleAt` regularly returns `false` (execution blocking consensus). Profiling-gated; same theme as the settlement item above. *(MEDIUM)*
- [ ] **Consider exporting `txToOp` and `Apply`** — `worstcase/state.go:179`
  API-shape decision; note `ApplyTx` also does validation that `Apply` doesn't, so a naive export drops guarantees. *(EASY, design call)*

---

## Tier 4 — Hard / consensus-critical / large numerical work

- [ ] **Enforce EOA-only issuance at actual execution time** — `worstcase/state.go:205`
  Worst-case path only best-effort rejects non-EOA senders; real enforcement must live in `saexec.Executor` and handle accounts that become non-EOA (EIP-7702 set-code / AA). Depends on `saexec` + SAE/ACP-194 execution semantics. *(HARD)*
- [ ] **Defer `T * K` into the exponential to avoid precision loss** — `gastime/gastime.go:138`
  `excessScalingFactor()` saturates at `MaxUint64`; move the multiply into `calculatePrice` so extreme inputs never round. Held to the stricter `lint-saevm`/gosec G115 bar. *(HARD)*
- [ ] **Refactor execution to a first-class read-only/trace mode** — `sae/rpc/stateful.go:33`
  Replace the fragile `noEndOfBlockOps` method-suppression hack; broad refactor of the execution pipeline + `hook.Points` interface. *(HARD)*

### Warp support (large feature — these are blocked until Warp lands)

- [ ] **Persist produced warp messages from receipts (`AfterExecutingBlock`)** — `cchain/hooks.go:177` *(HARD)*
- [ ] **Encode warp predicate results in the header (`BuildBlock`)** — `cchain/hooks.go:331` *(HARD)*
- [ ] **Provide meaningful block contexts in tests once Warp lands** — `cchain/vm_test.go:302` *(EASY, blocked on Warp)*

---

## Tier 5 — Blocked on external dependencies / decisions

### Blocked on libevm / geth upstream

- [ ] **Delete `GetTd` once libevm drops it from `ethapi.Backend`** — `sae/rpc/static.go:40`
  Currently returns zero (no total difficulty under PoS). *(TRIVIAL, blocked)*
- [ ] **Re-enable `TestSubscriptions`** — `sae/rpc_test.go:142`
  Skipped due to an async pending-tx subscription race; fixed by geth PR #33990 once pulled into libevm. *(TRIVIAL, blocked)*
- [ ] **Re-enable `TestWorstCase`** — `sae/worstcase_test.go:121`
  Skipped due to a `legacypool` nonce-update race on block-execution events; needs the underlying (likely upstream) pool fix. *(HARD, blocked)*
- [ ] **Efficient `txpool.TxPool` iterator** — `txgossip/txgossip.go:105`
  `txSet.Iterate` calls `Content()` which materializes all pending+queued maps; add a streaming iterator upstream in libevm. *(MEDIUM, external)*
- [ ] **WebSocket CPU limiting / max request duration** — `sae/http.go:24`
  Coreth/subnet-evm wrap the ws handler with DoS protections; either upstream them into libevm or formally decide they're unneeded. Security-relevant. *(MEDIUM–HARD)*

### Blocked on the state-sync feature (not yet implemented)

- [ ] **Bloom indexer checkpoint for state-synced nodes** — `sae/rpc/rpc.go:104`
  Must call `core.ChainIndexer.AddCheckpoint` with the first available block, else log indexing breaks on synced nodes. *(MEDIUM, blocked)*
- [ ] **Relocate `SettledGasTime` into state-sync logic** — `hook/hook.go:220`
  Placement note; move once state sync exists. *(EASY, blocked)*

### Blocked on product / protocol / operational decisions

- [ ] **Allow `MinPrice == 0` for static pricing (fee-less networks)?** — `gastime/config.go:58`
  One-line relaxation of `Validate()`, but blocked on a design decision (strevm issue #266); must verify downstream price math handles a true zero. *(TRIVIAL, blocked)*
- [ ] **Enforce a maximum per-tx gas amount in `Txpool.Add`?** — `cchain/txpool/txpool.go:191`
  Open policy question; trivial to implement once a limit is chosen. *(EASY, decision)*
- [ ] **Reduce Coreth's `commitInterval` to 1 before the SAE transition** — `cchain/state/state.go:72`
  Migration prerequisite (issue #5375); operational/Coreth-config dependency, not a change in this file. *(MEDIUM, external)*

### Investigation only

- [ ] **Diagnose `TestAcceptBlock` flake (`go test` vs Bazel discrepancy)** — `sae/accept_block_test.go:24`
  Skipped under `go test`, enabled under Bazel; determine whether it's a real GC-finalizer/VM bug or a test flaw, then unify. *(MEDIUM, investigation)*

---

## Documented limitations (not code TODOs — context for the above)

From `vms/saevm/docs/invariants.md` — known, intentionally-not-yet-enforced invariants:

- Side effects of acceptance are **not realized atomically** (settlement-then-acceptance ordering is pragmatic, not atomic).
- Broadcast-before-poll is only guaranteed by `WaitUntilExecuted()` / `WaitUntilSettled()`; other indicators lack it.
- No guarantee on the ordering of atomic updates, nor that the set updates atomically.
- No cross-class ordering guarantees for same-block side effects (e.g. `ChainEvent` vs `ChainHeadEvent`).
- If code and `invariants.md` disagree, the code is considered buggy until proven otherwise.

`vms/saevm/README.md`: no Go API-stability guarantees — package is under active development.
