# SAE Subnet-EVM Port Status

Living status document for porting the SAE Subnet-EVM spike (PR #5387, branch
`ceyonur/subnetevm-sae-spike-cleanup`, diverged from master at `35f36b0e5c`,
2026-05-12) onto current master. All work happens on
`JonathanOppenheimer/subnetevm-sae-port`. Written for a reader with zero
session context: read this file plus `git status` / `git log` to resume.

Reference documents (read before changing anything):

- [`README.md`](README.md) — the design handover for this VM (feature map,
  state-timing invariants, TODOs), updated during the port to the shipped
  design. Authoritative for *what* the VM must do; this file supersedes it on
  *how* the port got there. (`PORT_CONTEXT.md` is now just a pointer here.)
- `vms/saevm/hook/hook.go` and `vms/saevm/cchain/hooks.go` on master — the
  current hook interface and its reference implementation.

## Milestones

- [x] 0. Reconciliation plan written (this file)
- [x] 1. Merge of `origin/master` committed, conflicts resolved per policy
      (build may be broken)
- [x] 2. Workspace compiles (`go build ./...` in all 4 modules), `task lint`
      passes
- [x] 3. cchain green: all `vms/saevm/...` (minus subnetevm) +
      `vms/transitionvm` unit tests pass
- [x] 4. subnetevm green: all `vms/saevm/subnetevm/...` unit tests pass
- [x] 5. Duplication audit done + hoists landed (see "Duplication audit"
      below); tests still green
- [ ] 6. Full validation: the gates passed before the post-implementation
      review below. Revalidation of that review's final tree is pending.

## Merge resolution policy (decided up front)

Single `git merge origin/master` (no rebase of the 553 spike commits).

- **Master wins wholesale**: `vms/saevm/{sae,hook,blocks,saexec,txgossip,gasprice,gastime,worstcase,params,types,saetest,saedb,adaptor,cmputils}`
  and the packages master added since (`proxytime`, `firewood`, `network`,
  `statesync`, `docs`), `vms/saevm/cchain/**` (master restructured it:
  `hook/` subpackage flattened into `hooks.go`, warp reshaped, `dynamic/`
  added — spike-era files master doesn't have get deleted),
  `vms/transitionvm/**`, `graft/coreth/**`, `node/node.go`,
  `.github/workflows/ci.yml`, `tests/e2e/c/**`. go.mod/go.sum/MODULE.bazel/
  lock files: take master's and regenerate.
- **Branch wins**: `vms/saevm/subnetevm/**`, the spike's `graft/subnet-evm`
  feature work (gaspricemanager precompile, feemanager retirement,
  customtypes SAE header extras, params/extras changes),
  `scripts/build_subnet_evm_sae.sh`, Taskfile targets
  (`build-subnet-evm-sae`, `test-e2e-warp-sae`, `test-e2e-warp-sae-ci`),
  spike's `tests/e2e` + `tests/fixture` additions. All of it then gets fixed
  against master's core.
- **Deleted in favor of master equivalents**: `vms/saevm/intmath` (master has
  `utils/math/intmath`).
- Spike edits to master-owned files are *requirements to re-express*, not
  diffs to preserve. The re-expression plan is the next section.

## Hook reconciliation plan (the load-bearing decisions)

Both sides changed `hook.Points` since the merge base. **Master's interface
is the base**; the spike's needs are re-expressed as the minimal extensions
below. Master's relevant shape: `SettledBy(*types.Header) Settled` (quartet:
Height/GasUnix/GasNumerator/Excess, stamped into header extras by
`BuildBlock`), `VerifyBlockSyntax`, the
`StartExecutingBlock`/`FinishExecutingBlock`/`AfterExecutingBlock` split,
error-less `GasConfigAfter`, `Transaction.Size()`. Master also *removed*
`MarkSynchronous` — synchronous blocks persist nothing and derive execution
results from their headers (`synchronousExecutionResults`).

### D1. Gas-price-manager runtime: header-encoded gas config (NOT persisted artifacts)

The spike shipped ACP-224 as a persisted "hook artifact": `ExecutionArtifact`
projected gaspricemanager storage into opaque bytes at execution time, SAE
persisted them in the execution-results row (canoto field 5), and an
error-returning `GasConfigAfter` loaded them keyed by `SettledHeight`. The
spike's README records the alternative that was considered: *"encode the
effective gas-pricing configuration in the Subnet-EVM header. That would make
recovery and rebuilds self-contained at the header level."*

**Decision: use the header-encoding alternative.** Master's evolution flipped
the trade-off:

- Master deleted `MarkSynchronous` and no longer persists execution results
  for synchronous blocks, which was the artifact design's bootstrap path for
  genesis-enabled gaspricemanager. Re-adding artifact persistence would mean
  re-adding synchronous-block persistence against master's explicit design.
- Master's `GasConfigAfter` is error-less and header-only; the artifact
  design needs an error return (fallible DB load) threaded through 4 call
  sites, plus a `hookArtifact` canoto field, a `MarkExecuted` parameter, and
  `saexec` plumbing. Header encoding needs none of it: `GasConfigAfter`
  keeps master's exact signature.
- Recovery/rebuild become self-contained at the header level (the reason the
  alternative was recorded in the first place).

Mechanism:

- `graft/subnet-evm` customtypes `HeaderExtra` gains an optional SAE
  gas-config group carrying the *derived* values the artifact used to carry:
  `ValidatorTargetGas bool`, `TargetGas gas.Gas`, plus the
  `gastime.GasPriceConfig` triple (`TargetToExcessScaling`, `MinPrice`,
  `StaticPricing`). Stamped by `FinalizeHeader` (see D2) from the
  settled-state read of gaspricemanager storage, gated on
  `IsPrecompileEnabled(gaspricemanager, settled.Time)` — the same state view
  and gate the artifact producer used. Absent group = precompile not enabled
  at settled time.
- `Points.GasConfigAfter(h)` (master signature, no error):
  1. group present → `effective(headerTarget)` exactly as the spike's
     `gasConfigArtifact.effective`: `ValidatorTargetGas=true` → target from
     header `TargetExcess`; false → target pinned by `TargetGas`.
  2. group absent, `h.Number == 0`, and gaspricemanager is genesis-enabled →
     derive the same values from the chain config's
     `InitialGasPriceConfig` (mirroring what
     `gaspricemanager.Configure` writes at activation). This covers
     `synchronousGasTime(genesis)` and is deterministic. (cchain uses the
     same `h.Number.Sign() == 0` special-casing idiom in
     `targetExponent`.)
  3. otherwise → ACP-176 defaults (`gastime.DefaultGasPriceConfig()`,
     target from `TargetExcess`).
- Consensus safety: the group is verified by SAE's rebuild-and-compare
  (`VerifyBlock` hash equality) — a rebuilder re-derives the group from its
  own settled-state read, so a forged group fails verification. Worst-case
  and actual execution read the same header group, so they cannot diverge.
- Async headers never need the genesis fallback: a block settling genesis
  reads genesis state at build time and stamps the group itself.
- Known limitation (documented, out of port scope like state sync): a
  *transition* chain whose gaspricemanager storage was mutated before the
  SAE transition would fall to branch 3 for legacy headers. Legacy chains
  cannot have gaspricemanager storage today, so this only matters if the
  legacy plugin adopts the precompile before transition support lands.

Dropped spike core changes as a result: `Points.ExecutionArtifact`,
`Points.GasConfigAt`, error-returning `GasConfigAfter`, `hookArtifact`
canoto field + `MarkExecuted` param + `blocks.HookArtifact` +
`Block.HookArtifact`, `saexec` artifact plumbing, xdb capture inside
subnetevm `Points`, and the spike's `MarkSynchronous` triedb/state changes
(obsolete — the function no longer exists). `subnetevm/hook/artifact.go`
becomes a header-extras codec instead of a canoto artifact.

### D2. `BlockBuilder.FinalizeHeader` — the one builder extension

Problem: `worstcase.State.FinishBlock` calls `hooks.GasConfigAfter(hdr)` on
the in-progress header *before* master's `BuildBlock` runs, so anything
`GasConfigAfter` reads must be on the header earlier than master stamps its
settled marker. cchain doesn't hit this (its gas config comes from
`BuildHeader`-stamped fields); subnetevm does (its gas config comes from a
settled-state read that is only possible once `lastSettled` is known — after
`BuildHeader`, which runs before `lastToSettle` can be computed).

**Decision**: add to `hook.BlockBuilder`:

```go
// FinalizeHeader populates header fields that depend on the settled block,
// after lastSettled has been determined and BEFORE the worst-case
// projection consumes the header. settledState is rooted at settled's
// post-execution state; implementations SHOULD only read contract storage
// from it.
FinalizeHeader(hdr *types.Header, settled *types.Header, settledState libevm.StateReader) error
```

- Called in `sae.buildWithTxs` right after `hdr.Root = lastSettled.
  PostExecutionStateRoot()`, before `state.StartBlock(hdr)`. The state
  reader is a fresh `b.exec.StateDB(lastSettled.PostExecutionStateRoot())`
  open (cheap — shared cache underneath), keeping the reader semantically
  "pure settled state" rather than exposing the worst-case StateDB. This
  also drops the spike's `worstcase.State.StateDB()` accessor.
- subnetevm implementation stamps (a) the D1 gas-config group and (b)
  `header.Coinbase` via the spike's `resolveCoinbase` (rewardmanager
  precedence rules, gated on `settled.Time`; rebuilders keep overriding with
  the received block's coinbase for operator-choice branches, unchanged from
  the spike). Storage reads are unaffected by worst-case balance/nonce
  projections, so the values equal what the spike read at `BuildBlock` time.
- cchain + hookstest: no-op `return nil`.
- `BuildBlock` keeps **master's exact signature** (`settled hook.Settled`
  last param); subnetevm stamps the settled quartet there, mirroring
  cchain's idiom. The spike's extra `BuildBlock` params (worst-case state,
  settled header) are dropped — FinalizeHeader covers both needs.

### D3. Settled marker: adopt master's quartet

Spike carried only `SettledHeight` in subnet-evm header extras and a
`Points.SettledHeight` hook. Master requires
`SettledBy(*types.Header) Settled` with all four fields populated ("state
sync and recovery will not function correctly" otherwise).

**Decision**: `graft/subnet-evm` customtypes gain
`SettledGasUnix/SettledGasNumerator/SettledExcess *uint64` alongside the
existing `SettledHeight`, mirroring master's coreth customtypes; subnetevm
`Points.SettledBy` mirrors cchain's (zero value when any field is nil). Wire
format change is fine — this VM has never shipped.

### D4. `CanExecuteTransaction` gains an explicit `rules` first parameter

Master: `CanExecuteTransaction(from, to, state)`. The txallowlist check must
gate its storage read on `rules.IsPrecompileEnabled(...)` computed **for the
same state view it is handed** (worst-case: last-settled; admitter:
last-executed); master's signature cannot convey which rules pair with the
state.

**Decision** (as the spike): signature becomes
`CanExecuteTransaction(rules params.Rules, from common.Address, to *common.Address, state libevm.StateReader) error`.
`worstcase.State` re-captures the settled header (spike change) and computes
settled rules in `ApplyTx`. cchain ignores the parameter; hookstest's fn
field updated. Note this intentionally un-mirrors libevm's
`RulesAllowlistHooks` shape (libevm keys rules via the receiver; `Points` is
one long-lived value so rules must be explicit).

### D5. `Points.RequiresTransactionAdmissionCheck(rules) bool`

Cheap rules-only gate so mempool ingress skips sender recovery and state
opens when no admission-relevant precompile is active (this is what keeps
the admitter free for cchain, which returns `false`; over-reporting safe).
Kept exactly as the spike defined it.

### D6. Mempool admitter (txallowlist ingress)

`vms/saevm/sae/admitter.go` (+test) is kept and adapted: `txgossip` regains
the `Admitter` interface and `NewSet` gains the `admitter` param **in
addition to** master's new `exec` param; `sae.NewVM` wires
`newAdmitter(vm.exec, hooks, chainConfig)`. Reads last-executed state
(fresher than worst-case's last-settled — operators see role changes at
ingress sooner), per-head cache, fresh StateDB per call.

### D7. Hook mapping for the rest (no interface changes)

| Spike | Master home |
| --- | --- |
| `BeforeExecutingBlock(rules, parent, statedb, block)` (precompile/state upgrade activation) | `StartExecutingBlock(rules, statedb, parent, block)` — same semantics, master already added `parent` |
| `AfterExecutingBlock(statedb, block, receipts)` (warp precompile-accept) | master's `AfterExecutingBlock(block, receipts)` — canonical-only is correct for warp storage; the statedb param was unused |
| — | `FinishExecutingBlock`: no-op for subnetevm (no coreth-style ExtData transfers) |
| — | `VerifyBlockSyntax`: new minimal subnetevm implementation (stateless syntactic invariants of subnet-evm SAE blocks; exact checks decided during M4 against what `blocks.Parse` doesn't already cover) |
| `Tx` (inert placeholder) | gains `Size() uint64 { return 0 }` for master's `Transaction` interface |

### D8. Spike additions master already upstreamed (use master's)

`VM.SubscribeChainHeadEvent`, `VM.LastExecutedState`, `saerpc.Provider.
Server()` all exist on master. Re-add only what's genuinely missing (e.g.
`VM.RPCServer()` accessor if absent, `sae.ErrHashMismatch` export used by
subnetevm's forged-header tests).

## Duplication audit (milestone 5) — findings and outcomes

A systematic file-level comparison of `vms/saevm/cchain/**` vs
`vms/saevm/subnetevm/**` (line-intersection assisted). Bar for hoisting:
literal/near-literal copies, or differences reducing cleanly to an extension
point.

### Hoisted

- **Warp → one shared `vms/saevm/warp`** (the confirmed case). `Storage`
  (master cchain's shape + an `AddMessage` method matching subnet-evm's
  `precompileconfig.WarpMessageWriter`), `Verifier` (with an
  `AddressedCallVerifier` extension point — subnet-evm's validator-uptime
  attestation handling plugs in; its `UptimeVerifier` + `warp/messages` stay
  chain-specific), the concurrent block-predicate engine
  (`VerifyBlockPredicates`, parameterized by a per-chain closure — subnetevm
  gained concurrency, resolving a spike TODO), `ParseOffChainMessages`
  (operator warp-message config parsing, previously copied verbatim with its
  test), and `RegisterHandler` (ACP-118 wiring, cache size 512, previously
  copied). Chain packages keep genuine glue only: cchain `FromReceipts` +
  `VerifyBlock` closure; subnetevm `PredicateBytes` closure +
  `HandlePrecompileAccept` + uptime verifier.
- **`sae.VM.IsAcceptedBlock`**: the GetBlock → GetBlockIDAtHeight →
  compare canonicality check was duplicated as cchain's `warpBackend` and
  subnetevm's `blockClient`; both adapters deleted, `*sae.VM` satisfies the
  shared warp `Backend` directly.
- **`hook.NewSettled` / `Settled.AsPointers`**: the settled-marker
  read/write over the four `*uint64` header-extra fields was byte-identical
  in both chains (the two `HeaderExtra` types differ, so the quartet of
  pointers is the abstraction, not the extra struct).
- **`hook.BlockTimeFrom`**: both chains hand-rolled the identical
  seconds-authoritative + millisecond-refinement rule. Hoisting also fixed a
  live inconsistency: subnetevm's `WaitForEvent` pacing used the
  ms-authoritative `customtypes.BlockTime` while its hooks used the
  seconds-authoritative rule.
- **`hook.NewBlockDBExecutionResults`**: identical blockdb-backed
  `ExecutionResultsDB` implementations; the `Points` method remains as the
  injection seam.
- **`types.NewChainEthDB`**: the verbatim-copied "ethdb"-prefix +
  rpcchainvm-compaction comment block.
- **`subnetevm/hook/acp176` now delegates to shared `vms/evm/acp176`**: the
  local math (Target, UpdateTargetExcess, DesiredTargetExcess, constants)
  was proven value-identical to the shared state machine; only the
  header-friendly `TargetExcess` value type remains local.

### Copy-and-tweak residue fixed along the way

- subnetevm `Initialize` now rolls back (Shutdown) on failure and `Shutdown`
  is idempotent, matching cchain's robustness.
- subnetevm's redundant `preference` tracking (an override of SetPreference
  plus an atomic pointer) deleted in favor of `sae.VM.GetPreference`.
- Dead code deleted: subnetevm's empty prometheus registry, and the
  never-called `HasLastSync`/`WriteLastSync` (`ReadLastSync` kept with a
  TODO — transition support will reintroduce a writer).

### Kept separate (and why)

- **Genesis handling**: cchain rebuilds its chain config from scratch
  (hardcoded C-Chain history) and owns block/state writing; subnetevm honors
  the operator's genesis JSON and delegates to the graft's
  `core.SetupGenesisBlock`. Two coherent designs; ~0 shared lines.
- **Operator config**: field sets barely overlap and parse policy is
  deliberately opposite (cchain tolerates unknown fields; subnetevm rejects
  them so legacy-only knobs fail loudly).
- **VM scaffolding**: cchain's two-phase Initialize/finishInitialize exists
  for state sync (which subnetevm doesn't have); WaitForEvent's tx-race is
  C-Chain-specific; Shutdown plumbing differs on context propagation.
- **state/ packages**: cchain's is the atomic-request trie; subnetevm's is a
  one-key lastSync accessor. Only the name is shared.
- **factory vs plugin**: in-process factory vs rpcchainvm plugin runner.
- **api/metrics/log glue**: zero functional overlap.

### Deferred follow-ups (coupled, cross-graft; not required for this port)

- Unify the exponent types: `cchain/dynamic.DelayExponent` duplicates shared
  `vms/evm/acp226.DelayExcess` verbatim, `cchain/dynamic.TargetExponent` and
  `subnetevm/hook/acp176.TargetExcess` are the same value under different
  names. Both graft `customtypes` packages import the harness-local types
  for header fields, so the move touches both grafts — sequenced separately.
- A `hook.Points.MinBlockDelayAfter` extension would let the ACP-226
  block-separation gate and build pacing share one implementation (3 call
  sites per chain today, in different units).

## Current state

ALL MILESTONES COMPLETE on `JonathanOppenheimer/subnetevm-sae-port`. The
merge of `origin/master` (`6c61faba62`) is committed; all four workspace
modules build; `task lint` passes; all `vms/saevm/...` (incl. subnetevm),
`vms/transitionvm` unit tests pass. The full subnetevm feature suite is
green: warp (incl. predicate verification), validators/uptime, tx/deployer
allowlists, nativeminter, rewardmanager (incl. forged-coinbase rejection via
rebuild-hash-mismatch), gaspricemanager (activation transitions,
pinned-config restart persistence — now via headers rather than the removed
xdb artifact — settlement lag, validator-target-gas), feemanager retirement,
state upgrades, and eth extras. See "Milestone 6 validation record" for the
repo-wide and e2e results.

Notable adaptations beyond the plan:

- The warp predicate test's tx gas limit rose from 200k to 1M: master's
  subnet-evm charges warp signature-verification gas as intrinsic gas
  (`RulesExtra.AccessListGas` with predicaters), enforced identically in
  worst-case admission and actual execution. Mirrors the legacy plugin's
  warp tests.
- `sae.ErrHashMismatch` exported (spike re-add) for forged-header tests.
- `loggingtest.New` (master's hoisting of the spike's
  `saetest.NewTBLogger`) is used throughout.
- The subnet-evm customtypes golden RLP/hash pin was updated for the new
  optional tail fields (legacy headers encode unchanged — nil optional
  fields are omitted).

### Next action

All milestones are complete, including the post-review systemic dedup/idiom
audit (W1-W27; see that section). The branch stays local until Jonathan
reviews (no push, no PR — per the working agreement). Review focus:

1. The D1 decision (header-encoded gas config replacing the spike's
   persisted-artifact design) — see the Decisions log and the milestone-6
   adversarial-review record.
2. The six defaulted owner decisions listed at the end of the systemic-audit
   section (W19 x2, W20 header-bytes change, W21, W7, W12).
3. The public golden-hash update in
   `graft/subnet-evm/plugin/evm/customtypes/header_ext_test.go` (new
   optional header tail fields; legacy encodings unchanged).
4. The deferred follow-ups at the end of the duplication audit (exponent
   type unification across cchain/dynamic and vms/evm/acp226; ActiveRules
   wire-type move; uptimetracker loop hoist).

## Milestone 6 validation record

- `task test-unit` equivalent (race-enabled `scripts/build_test.sh`,
  repo-wide): PASS (exit 0).
- `graft/subnet-evm` unit scope (its CI exclusion of `tests/**` applied):
  PASS. `graft/coreth` and `graft/evm` module tests: PASS (coreth's
  `tests/warp` TestE2E needs a live network and is excluded from unit scope
  on master too).
- `task test-e2e-warp-sae`: PASS 5/5 specs after a fixture fix — the
  SubnetA→SubnetA combination fetched a latest-state nonce for a key whose
  nonce the earlier combinations had advanced, racing SAE's async execution;
  the initial send now uses the pool-aware `PendingNonceAt` like the
  delivery steps already did.
- Pre-existing C-Chain e2e suite (`tests/e2e` with
  `--ginkgo.label-filter=c`): PASS 4/4 specs (ProposerVM API, Interchain
  Workflow, Dynamic Fees, ProposerVM Epoch).
- Bonus: `task test-e2e-warp` (the same suite against the LEGACY subnet-evm
  plugin): PASS 5/5, confirming the shared warp e2e suite serves both
  plugins.
- An adversarial consensus-safety review of the header-encoded gas-config
  design (worst-case/actual divergence, forged headers, activation windows,
  genesis determinism, restart recovery, float determinism, ingestion
  coverage) found no refutation; its non-blocking hardening suggestions were
  applied (genesisGasConfig gates on IsPrecompileEnabled; RLP
  present-and-zero decoding documented on the syntax checks; genesis-read
  constants documented as consensus-critical).
- CI wiring: the spike's `e2e_warp` / `e2e_warp_sae` jobs were re-expressed
  into master's root `ci.yml` (the merge's master-wins policy had dropped
  the additions while keeping their removal from `subnet-evm-ci.yml`).

## Systemic dedup/idiom audit (post-review follow-up)

Jonathan's review of the first "complete" state found the dedup shallow
("countless examples … this feels patchy"). A 5-lens audit workflow (88
agents: duplication, dead-code, idiom-vs-cchain, staleness, API-surface
lenses, each finding adversarially verified; 77 confirmed findings) produced
a 28-item work plan (W1–W28). Implemented in phases, one commit per phase:

- Phase A (`58c381335f`): W1 hooks flattened into package-root `hooks.go`
  (hook/ subpackage deleted; unexported `hooks`/`builder` types, cchain
  layout); W2 local acp176 copy deleted in favor of shared
  `vms/evm/acp176.State` (+ `TargetExcess *gas.Gas` in customtypes).
- Phases B+C (`9edcc16f39`): W3 warp/messages byte-copy deleted (imports
  graft's); W4–W9 config.go rewritten (unexported config, saeConfig
  assembly, parse-time RPC/DB verification, defaults from
  saedb/legacypool constants); W10–W15 vm.go lifecycle aligned with cchain
  (closeMu/onClose, idempotent Shutdown, rollback-before-lock Initialize,
  metrics gauge, single config log, genesis parsing extracted to
  genesis.go); W16 cchain uses shared `types.NewChainEthDB`; W17 plugin
  runner deleted (stdlib flag + direct registration); W18 validators
  `Manager` renamed `Uptime` (Tracker() dropped); W19 genesis decisions
  (see flags below).
- Phase D (`eff867b5ff`): W20 `VerifyBlock` returns `BlockResults`
  (PredicatersExist short-circuit dropped — see flags); W21 ACP-226
  `earliestBlockTime`/`waitUntil` WaitForEvent pacing (nil MinDelayExcess =>
  no minimum); W22 `acp226.DelayExcess.DelayDuration()` added and shared.
- Phase E (`17a33533ff`): W23 warptest hoisted to `vms/saevm/warp/warptest`
  (cchain + subnetevm + corethgen SUTs consume it; hand-rolled BLS plumbing
  deleted); W24 shared predicate engine + verifier extension dispatch get
  direct unit tests, subnetevm warp tests trimmed to glue deltas; W25
  goleak-verified TestMains matching cchain.
- Phase F (this commit): W26 stale-comment sweep (StartExecutingBlock
  naming, header-encoded gas-config narratives, satisfied TODOs deleted);
  W27 README rewritten to the shipped header-encoded design (dead links and
  resolved TODO rows removed), PORT_CONTEXT reduced to a pointer, package
  doc added, `config.md` added.
- W28 (delete PORT_STATUS/PORT_CONTEXT, fold into README/PR description) is
  deliberately deferred: these files are the working agreement's resume
  anchor until Jonathan reviews.

Owner decisions taken by default during the audit (FLAG FOR REVIEW):

1. W19: the commented-out `NetworkUpgradeOverrides` block was deleted (a
   one-line TODO remains in genesis.go) rather than implemented.
2. W19: `EmptyFeeConfig` is substituted with `DefaultFeeConfig` only when
   the legacy feeManager precompile is configured (narrow fix; SAE
   otherwise treats legacy FeeConfig as inert).
3. W20: the `PredicatersExist` short-circuit was DROPPED, so blocks with no
   predicate-carrying txs now stamp empty predicate-result bytes into
   `header.Extra` (header-bytes change; acceptable pre-release, flagged
   because it changes hashes vs the spike).
4. W21: a parent header with nil `MinDelayExcess` yields no minimum
   build-wait (consensus-matching; builder-side only).
5. W7: no `validators-api-enabled` config gate was added (the API is always
   served, matching the spike; legacy plugin has a gate).
6. W12: peer-visible warp AppError strings were left verbatim (e.g. "failed
   to parse addressed call message") to avoid changing wire-observable
   text; internal error wraps were reworded to repo style.

## PR-ready summary

**What this branch does**: revives the SAE Subnet-EVM L1 VM spike (PR #5387)
against current master. `vms/saevm/subnetevm` is a working SAE-based
subnet-evm VM (out-of-process rpcchainvm plugin) sharing master's SAE core
with the C-Chain wrapper; the C-Chain VM's behavior is unchanged except
where both wrappers were fixed together (journal disable, shared hoists).

**Definitive validation** (single sweep on the final tree `a717578482`):
`task lint` ALL SUCCESS; repo-wide race unit tests 230 packages, 0 failures;
`task test-e2e-warp-sae` 5/5 specs; C-Chain e2e (`--ginkgo.label-filter=c`)
4/4 specs; repo root left clean by the runs.

**Load-bearing design decision**: ACP-224 runtime gas config is encoded in
the header (GasConfig* extras group stamped by `FinalizeHeader` from settled
state, read back by `GasConfigAfter`) instead of the spike's persisted
per-block artifacts — see D1 and the adversarial-review record. Hook-surface
delta against master's SAE core is minimal: `BlockBuilder.FinalizeHeader`,
a `rules` param on `CanExecuteTransaction`,
`RequiresTransactionAdmissionCheck`, and the txgossip `Admitter` extension
point; subnet-evm's validator-uptime verification extends the shared warp
verifier via `AddressedCallVerifier`.

**Dedup outcome**: exactly one shared SAE warp implementation
(`vms/saevm/warp` + `warptest`), shared acp176/acp226 types, cchain-idiom
parity across config/lifecycle/metrics/tests — see the duplication audit and
the systemic-audit sections for the full hoist/keep record.

**Reviewer attention** (decisions taken by default, flagged): the six items
at the end of the systemic-audit section; the reward-routing semantic change
(post-London coinbase gets tip only — see README "Breaking Change"); the
customtypes golden-hash update (new optional header tail fields); the
e2e suite's two SAE-consistency accommodations (`PendingNonceAt`, log-filter
retry) and the possible VM-side follow-up of serving `eth_getLogs` from the
executor cache.

## Final validation record (post-audit tree)

After the audit phases and doc refresh, the four gates were re-run
sequentially (first on `6405bc7320`, then definitively on `a717578482` —
all four PASS in that single final sweep):

- `task lint`: PASS (ALL SUCCESS). The first run of this sweep found 7 real
  issues in the port (forbidigo `require.Error`, gci ordering x3,
  gofmt/gofumpt in warp/warp.go, unparam in verifier_test) — fixed in
  `bdf649c8be`, including exporting `saedb.ErrZeroCommitInterval` so the
  parse-time rejection is asserted by identity.
- Repo-wide race unit sweep (`scripts/build_test.sh`): PASS, 230 packages,
  0 failures.
- `task test-e2e-warp-sae`: PASS 5/5. The first run flaked on
  SubnetA→SubnetA at "expected a SendWarpMessage event": under SAE,
  `eth_getTransactionReceipt` is served from the executor's in-memory cache
  (`immediateReceipts`) the moment execution finishes, while `eth_getLogs`
  reads the durably-indexed logs, which land slightly later — so
  WaitMined-then-FilterLogs races by design. Fixed in `6405bc7320` by
  polling the filter (suite default timeout) while keeping the
  exactly-one-event assertion, mirroring the suite's existing
  `PendingNonceAt` accommodation. A VM-side alternative (serving `GetLogs`
  from the executor cache) is a shared-core semantics change, recorded as a
  possible follow-up, not taken here.
- Pre-existing C-Chain e2e (`--ginkgo.label-filter=c`): PASS 4/4.

The sweep also caught the SAE wrappers journaling the legacypool to the
node process's working directory when `local-txs-enabled=true` (observed as
a stray `transactions.rlp` at the repo root during the warp-sae e2e — the
fixture enables local txs). Both legacy plugins disable the journal
(`TxPool.Journal = ""`); the subnetevm AND cchain saeConfig now do the
same.

## Decisions log

### 2026-09-02: Post-implementation correctness and simplification review

The review found and fixed correctness gaps that the earlier final sweep did
not cover:

- `b832799273` restores Subnet-EVM RPC, batch-limit, pending-resolution, and
  database-cache defaults.
- `751eeb9347` exposes the SAE header extension fields through Subnet-EVM RPC.
- `fcd4ad6660` applies Subnet-EVM network-upgrade overrides.
- `291606e2b6` enforces the transaction allowlist after Helicon in both block
  execution and txpool admission. The SAE execution path still bypasses the
  legacy internal hook so the check runs exactly once.
- `5f5663c207` replaces floating-point gas scaling with deterministic integer
  rational arithmetic.
- `2d8669841e` rejects SAE-only header fields in the legacy Subnet-EVM VM.
- `c07cf5bcf7` evaluates upgrade compatibility at the accepted head on restart
  and deletes the obsolete `lastSync` database record.

The follow-up duplication audit also found three smaller sharing wins:

- `30f4051e7b` hoists the identical C-Chain and Subnet-EVM minimum-delay metric
  into `vms/saevm/sae`.
- `2c79514a00` deletes C-Chain's private ACP-176 and ACP-226 math and uses the
  shared implementations in `vms/evm`. The graft changes are limited to the
  header type and genesis plumbing consumed by SAE; the legacy wire field name
  and `uint64` encoding remain unchanged.
- `4361a93058` deletes one-call configuration wrappers and invokes the shared
  warp message parser directly. The execution-results injection seam remains
  because SAE recovery tests use it.

The review also restores `tests/load` exactly to `origin/master` and removes
the unreferenced spike-only `spam-c-chain` example; neither is used by the SAE
warp suite.

Exact next action: regenerate/check Bazel metadata, run lint and all four
module builds, then run the repo-wide unit, SAE warp e2e, and C-Chain e2e gates
on one final tree. Record the results here and check milestone 6 only if every
gate passes.

- 2026-08-27: Reconciliation plan written (this file). Header-encoded gas
  config chosen over persisted artifacts (D1) — the spike README's own
  recorded alternative — because master removed synchronous-block
  persistence (the artifact bootstrap path) and an error-less header-only
  `GasConfigAfter` shrinks the shared-core delta to: `FinalizeHeader`,
  `CanExecuteTransaction` rules param, `RequiresTransactionAdmissionCheck`,
  and the txgossip `Admitter`. FLAG FOR REVIEW: this deviates from the
  spike's shipped mechanism (not its feature surface); the artifact design
  remains re-implementable if header bytes are ever unacceptable.
