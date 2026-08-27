# SAE Subnet-EVM Port Status

Living status document for porting the SAE Subnet-EVM spike (PR #5387, branch
`ceyonur/subnetevm-sae-spike-cleanup`, diverged from master at `35f36b0e5c`,
2026-05-12) onto current master. All work happens on
`JonathanOppenheimer/subnetevm-sae-port`. Written for a reader with zero
session context: read this file plus `git status` / `git log` to resume.

Reference documents (read before changing anything):

- [`README.md`](README.md) and [`PORT_CONTEXT.md`](PORT_CONTEXT.md) — the
  original author's design handover for this VM (feature map, state-timing
  invariants, TODOs). Still authoritative for *what* the VM must do; this file
  supersedes them on *how* it attaches to master's SAE core.
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
- [x] 6. Full validation: `task test-unit`, `task test-e2e-warp-sae`, and the
      pre-existing C-Chain e2e suite pass

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

Milestones 0-4 complete on `JonathanOppenheimer/subnetevm-sae-port`. The
merge of `origin/master` (`6c61faba62`) is committed; all four workspace
modules build; `task lint` passes; all `vms/saevm/...` (incl. subnetevm),
`vms/transitionvm` unit tests pass. The full subnetevm feature suite is
green: warp (incl. predicate verification), validators/uptime, tx/deployer
allowlists, nativeminter, rewardmanager (incl. forged-coinbase rejection via
rebuild-hash-mismatch), gaspricemanager (activation transitions,
pinned-config restart persistence — now via headers rather than the removed
xdb artifact — settlement lag, validator-target-gas), feemanager retirement,
state upgrades, and eth extras.

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

Milestone 6 full validation: `task test-unit` (the no-race sweep already
passed repo-wide), `task test-e2e-warp-sae`, and the pre-existing C-Chain
e2e suite.

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
- CI wiring: the spike's `e2e_warp` / `e2e_warp_sae` jobs were re-expressed
  into master's root `ci.yml` (the merge's master-wins policy had dropped
  the additions while keeping their removal from `subnet-evm-ci.yml`).

## Decisions log

- 2026-08-27: Reconciliation plan written (this file). Header-encoded gas
  config chosen over persisted artifacts (D1) — the spike README's own
  recorded alternative — because master removed synchronous-block
  persistence (the artifact bootstrap path) and an error-less header-only
  `GasConfigAfter` shrinks the shared-core delta to: `FinalizeHeader`,
  `CanExecuteTransaction` rules param, `RequiresTransactionAdmissionCheck`,
  and the txgossip `Admitter`. FLAG FOR REVIEW: this deviates from the
  spike's shipped mechanism (not its feature surface); the artifact design
  remains re-implementable if header bytes are ever unacceptable.
