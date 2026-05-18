# Subnet-EVM SAE VM (`subnetevm`)

`subnetevm` is the SAE-based Subnet-EVM VM. It is a chain-specific harness
around [`vms/saevm`](../) that reuses the legacy Subnet-EVM packages under
[`graft/subnet-evm`](../../../graft/subnet-evm) for chain config, header
extensions, precompiles, warp, and upgrade semantics.

This README is both an onboarding note and a design handover for follow-up
work. It summarizes the decisions made during the port, then points to the
implementation and tests for exact behavior.

## Layout

| Area | Files |
| --- | --- |
| VM initialization, config, handlers | [`vm.go`](vm.go), [`config.go`](config.go) |
| SAE hook and block building | [`hook/points.go`](hook/points.go), [`hook/block_builder.go`](hook/block_builder.go) |
| Subnet-EVM JSON-RPC extras | [`api/eth_extras.go`](api/eth_extras.go), [`api/client/client.go`](api/client/client.go) |
| Validators API and uptime | [`api/validators.go`](api/validators.go), [`validators/manager.go`](validators/manager.go) |
| Warp support | [`warp/`](warp/) |
| Out-of-process plugin | [`plugin/`](plugin/) |
| End-to-end feature tests | [`vm_*_test.go`](./) |

## Porting Model

The port starts from the SAE C-Chain wrapper shape, but it is not a Coreth VM
with Subnet-EVM precompiles sprinkled on top. The first commits deliberately
strip Coreth-only behavior: atomic transactions, shared memory, native-asset
legacy plumbing, ExtData fields, and the `/avax` namespace. After that, the VM
switches to the Subnet-EVM config, rules, custom header extras, precompile
registry, and fork timeline.

SAE changes the timing model. Legacy Subnet-EVM usually executes a block and
observes state synchronously. This VM builds against last-settled state,
executes asynchronously, and later settles execution results. Any feature that
reads chain state while building, admitting, or pricing a block must be explicit
about which state view it uses. The tests intentionally encode those timing
boundaries rather than hiding them behind compatibility shims.

The package is expected to run as an out-of-process rpcchainvm plugin.

## Supported Surface

| Feature | Primary code | Coverage |
| --- | --- | --- |
| Subnet-EVM chain config, upgrades, and genesis precompiles | [`vm.go`](vm.go), [`config.go`](config.go), [`graft/subnet-evm/params/extras`](../../../graft/subnet-evm/params/extras) | [`vm_txallowlist_test.go`](vm_txallowlist_test.go), [`vm_deployerallowlist_test.go`](vm_deployerallowlist_test.go), [`vm_feemanager_test.go`](vm_feemanager_test.go), [`vm_gaspricemanager_test.go`](vm_gaspricemanager_test.go) |
| Subnet-EVM header extras plus SAE fields | [`graft/subnet-evm/plugin/evm/customtypes`](../../../graft/subnet-evm/plugin/evm/customtypes), [`hook/block_builder.go`](hook/block_builder.go) | [`graft/subnet-evm/plugin/evm/customtypes`](../../../graft/subnet-evm/plugin/evm/customtypes), [`hook/block_builder_test.go`](hook/block_builder_test.go) |
| Warp precompile, predicates, and ACP-118 signing | [`warp/`](warp/), [`hook/block_builder.go`](hook/block_builder.go) | [`vm_warp_test.go`](vm_warp_test.go), [`warp/*_test.go`](warp/) |
| `validators.getCurrentValidators` | [`api/validators.go`](api/validators.go), [`validators/manager.go`](validators/manager.go) | [`vm_validators_test.go`](vm_validators_test.go) |
| `eth_getActiveRulesAt` | [`api/eth_extras.go`](api/eth_extras.go) | [`vm_eth_extras_test.go`](vm_eth_extras_test.go) |
| `txallowlist` and `deployerallowlist` | [`hook/points.go`](hook/points.go), [`vms/saevm/sae/admitter.go`](../sae/admitter.go), [`graft/subnet-evm/precompile/contracts`](../../../graft/subnet-evm/precompile/contracts) | [`vm_txallowlist_test.go`](vm_txallowlist_test.go), [`vm_deployerallowlist_test.go`](vm_deployerallowlist_test.go), [`../sae/admitter_test.go`](../sae/admitter_test.go) |
| `nativeminter` | upstream precompile package | [`vm_nativeminter_test.go`](vm_nativeminter_test.go) |
| `rewardmanager` fee routing | [`hook/block_builder.go`](hook/block_builder.go), [`config.go`](config.go) | [`vm_rewardmanager_test.go`](vm_rewardmanager_test.go), [`hook/block_builder_test.go`](hook/block_builder_test.go) |
| State upgrades | [`hook/points.go`](hook/points.go) | [`vm_test.go`](vm_test.go) |
| `gaspricemanager` precompile and runtime gas config | [`hook/points.go`](hook/points.go), [`hook/artifact.go`](hook/artifact.go), [`../blocks/execution.go`](../blocks/execution.go), [`../saexec/execution.go`](../saexec/execution.go), [`../worstcase/state.go`](../worstcase/state.go) | [`vm_gaspricemanager_test.go`](vm_gaspricemanager_test.go), [`hook/block_builder_test.go`](hook/block_builder_test.go) |
| Legacy `feeManager` retirement and legacy `FeeConfig` deprecation | [`graft/subnet-evm/precompile/contracts/feemanager/retirement`](../../../graft/subnet-evm/precompile/contracts/feemanager/retirement), [`vm.go`](vm.go), [`../../../graft/subnet-evm/plugin/evm/vm.go`](../../../graft/subnet-evm/plugin/evm/vm.go) | [`vm_feemanager_test.go`](vm_feemanager_test.go), [`../../../graft/subnet-evm/plugin/evm/feemanager_retirement_test.go`](../../../graft/subnet-evm/plugin/evm/feemanager_retirement_test.go) |

## Differences From `cchain`

[`vms/saevm/cchain`](../cchain) is the SAE C-Chain wrapper. `subnetevm`
shares the same SAE core but has a different feature surface.

| Area | `cchain` | `subnetevm` |
| --- | --- | --- |
| Chain config | `graft/coreth/params(/extras)` | `graft/subnet-evm/params(/extras)` |
| Fork timeline | C-Chain/Coreth forks | Subnet-EVM/Durango/Etna/Granite/Helicon timeline |
| Atomic txs and shared memory | Supported via C-Chain-specific txs and `/avax` | Removed |
| Native asset legacy precompile | Present in the Coreth lineage | Removed |
| Header extras | Coreth customtypes plus SAE fields | Subnet-EVM customtypes plus SAE fields |
| Warp | Same SAE warp shape | Same shape, plus Subnet-EVM-specific validator uptime message verification |
| Stateful Precompiles | Warp | Subnet-EVM allowlists, nativeminter, rewardmanager, gaspricemanager |
| Validators API | Not exposed by `cchain` | `/validators` gorilla-rpc namespace |
| Extra `eth_*` methods | C-Chain baseline | `eth_getActiveRulesAt` |
| Plugin shape | In-tree C-Chain VM | Standalone rpcchainvm plugin entrypoint |
| Gas pricing | ACP-176 | ACP-176 plus `gaspricemanager` runtime artifact path |

## Differences From `graft/subnet-evm`

This VM intentionally reuses the legacy Subnet-EVM packages for chain rules,
precompiles, and wire shapes where they still apply. The differences below are
the parts a reviewer should not expect to match line-for-line.

| Area | `graft/subnet-evm` plugin | SAE `subnetevm` |
| --- | --- | --- |
| Execution model | Synchronous block execution in the legacy EVM pipeline | SAE build, execute, and settle pipeline with Tau-lag-aware reads |
| State timing | Most feature checks observe the current parent/execution state | Admission and runtime hooks may intentionally read last-executed, last-settled, or persisted hook artifacts depending on the consensus boundary |
| Package boundary | Full legacy VM under [`graft/subnet-evm/plugin/evm`](../../../graft/subnet-evm/plugin/evm) | Chain wrapper under this package, generic mechanics in [`vms/saevm`](../) |
| Config surface | Broad legacy operator config, including standalone DB and legacy fee knobs | Narrow SAE config in [`config.go`](config.go); unsupported legacy knobs are deferred rather than silently reinterpreted |
| Database mode | Supports standalone per-chain database mode | Uses the AvalancheGo-provided database; standalone DB is deferred |
| State sync | Legacy plugin state-sync paths remain in `graft/subnet-evm` | Not ported in this wrapper |
| Network upgrade overrides | Legacy supports `networkUpgradeOverrides` | Not a port target yet; see the TODO in [`vm.go`](vm.go) |
| Legacy `feeManager` | Retired at Helicon through shared `graft/subnet-evm` retirement helpers | Same retirement helpers are called during SAE genesis parsing |
| Gas price manager | Precompile package and registry live under `graft/subnet-evm` | Runtime base-fee path reads the precompile through SAE hook artifacts |
| `txallowlist` admission | Legacy libevm hook path | RPC/mempool ingress uses [`../sae/admitter.go`](../sae/admitter.go); worst-case uses [`hook/points.go`](hook/points.go) |
| `deployerallowlist` | libevm `CanCreateContract` frame-local revert | Same enforcement layer; SAE does not add a separate admission check |
| RPC extras | Legacy exposes older Subnet-EVM extras, including deprecated surfaces | This VM serves `eth_getActiveRulesAt`; `eth_getActivePrecompilesAt` and `eth_feeConfig` are intentionally not served |
| Validators API | Legacy `/validators` service | Re-hosted under [`api/validators.go`](api/validators.go) with SAE uptime tracking |
| Plugin loading | Legacy Subnet-EVM plugin binary | Standalone SAE plugin under [`plugin/`](plugin/) |
| Test strategy | Legacy package has broad historical VM coverage | SAE wrapper keeps focused per-feature SUT tests and relies on `graft/subnet-evm` package tests for precompile internals |

The largest practical difference is state timing. For example, a role mutation
in block N is visible to latest-state RPC immediately after execution, but
last-settled reads will not observe it until the mutation settles. Tests that
look at allowlist roles, reward routing, and gas manager storage usually assert
both views.

Another intentional difference is operator config breadth. Legacy Subnet-EVM
has years of operational knobs, some of which are not meaningful in SAE or were
not part of this port. The SAE wrapper accepts the subset it can implement
cleanly and leaves the rest as explicit TODOs instead of silently accepting
unsupported behavior.

## Design Decisions and Alternatives

### Genesis and Upgrade Config

Genesis precompiles and `upgradeBytes` are parsed through the shared
`extras.ChainConfig` machinery, so the same upstream precompile configs drive
both the legacy plugin and this wrapper. The SAE hook applies timestamped
`PrecompileUpgrades` and `StateUpgrades` in the `(parent.Time, block.Time]`
window before EVM transactions execute.

This is intentionally separate from the read paths below. Activation mutates
the child block's post-execution state. Any value that affects block building,
admission, or header verification must still read from the state view that the
builder and verifier can both reproduce.

### Header Extras

The port reuses Subnet-EVM's `customtypes` instead of inventing a local SAE
header package. SAE fields are appended to the existing header-extra shape:
`SettledHeight` records which execution result row to consult, and
`TargetExcess` carries ACP-176's gas-target vote state. `BlockGasCost` remains
in the header for layout compatibility but is always stamped to zero. ACP-226
and ACP-176 own block delay and gas pricing under SAE.

### Settled-State System Configuration Reads

Several Subnet-EVM precompiles act as system configuration rather than ordinary
contract state: `txallowlist` controls admission, `rewardmanager` controls fee
routing, and `gaspricemanager` controls the gas clock. SAE must choose the state
view for each read deliberately because build, execute, and settle are separate
phases.

Reads that affect block building use settled-state timing: reward routing checks
whether `rewardmanager` is active at `settled.Time`, tx allowlist worst-case
admission reads the last-settled state, and gas pricing reads the hook artifact
keyed by `SettledHeight`. Inbound mempool verification is the exception: it
reads last-executed state through the admitter so operator role changes are
visible at ingress sooner.

The invariant is that any value committed into a block header or worst-case
prediction must be derived from the same state view that verification can
reproduce. If a precompile activates at time `T`, a builder cannot assume its
storage exists until a block at or after `T` has executed and reached the
settled view used by that path.

### Allowlist Enforcement

`txallowlist` is block-validity-sensitive because libevm's transaction-level
preflight error can invalidate execution. SAE therefore owns the post-Helicon
sender check in admission and worst-case paths. `deployerallowlist` is different:
contract creation failures are frame-local EVM errors, so unauthorized deploys
are mined with failed receipts. SAE keeps libevm as the single authority for
deployer checks.

Alternative considered: add a SAE-side `tx.To() == nil` for `CanDeploy`
check. It was rejected because it would not cover nested creates and would
leave two partially-overlapping enforcement paths.

### Native Minter

`nativeminter` is the simpler stateful-precompile case. It mutates the active
`StateDB` inline through the upstream implementation, so no SAE hook is needed
and worst-case and actual execution observe the same state transition.

### Reward Routing

Reward routing is resolved by stamping `header.Coinbase`. The implementation
mirrors legacy Subnet-EVM's precedence but reads from the state view available
to SAE at build time. Rebuild/verification is deterministic because rebuilder
instances use the received header's coinbase where legacy rules allow the
operator to choose a recipient.

### Gas Price Manager Runtime

The first ACP-224 design passed a settled `StateDB` into `GasConfigAfter` so
each call site could read `gaspricemanager` storage at `h.Root`. That was simple
for normal execution but created an awkward recovery surface: historical replay
and recovery would need to reopen many roots. The shipped design persists a
hook artifact when a block executes and loads it by `SettledHeight` later.

This is not fully closed until the `MarkSynchronous` state-availability TODO is
resolved. `MarkSynchronous` currently opens state directly before the production
`saedb.Tracker` exists; that is fine for always-SAE genesis state, but transition
chains can materialize old synchronous blocks whose state may no longer be
available. See the `Synchronous-block state opener` TODO below.

Alternative considered: add a `GasConfigNeedsState` gate so chains without
`gaspricemanager` avoid the state read. The estimated hot-path saving was small,
and the artifact design made the gate unnecessary for the recovery concern. If
profiling later shows the artifact path is still too expensive, the gate can be
added as an interface extension.

### Legacy Fee Controls

The legacy `feeManager` precompile is retired at Helicon using helpers shared
by the legacy plugin and this VM. That keeps both binaries on the same
post-Helicon state transition.

The retirement path has three cases. A stale genesis `feeManager` entry at or
after Helicon is normalized out when the chain genesis predates Helicon, because
existing chains cannot change immutable genesis bytes. Any `feeManager`
`PrecompileUpgrades` entry at or after Helicon is rejected instead; operators
can remove those upgrade bytes before restart. If `feeManager` is live before
Helicon, the parser injects a synthetic disable at Helicon so its storage is
wiped at the transition. Legacy `FeeConfig` remains for legacy startup, but SAE
treats the zero value as inert and skips validation because ACP-176 and
`gaspricemanager` own runtime gas pricing here.

### Plugin and Factory

The standalone plugin entrypoint mirrors the legacy plugin runner and registers
Subnet-EVM libevm extras process-wide before serving.


## TODOs and Deferred Work

| Priority | Todo | Status |
| --- | --- | --- |
| High | Synchronous-block state opener | [`../blocks/settlement.go`](../blocks/settlement.go) opens state directly while `saedb.Tracker` does not exist yet in init ordering. Route `MarkSynchronous` through the production tracker once SAE can construct it earlier. This matters for recovery/transition chains where old synchronous roots may have been pruned. |
| High | Standalone per-chain database support | Skipped for this port. Legacy Subnet-EVM supports per-chain DB engines and paths; SAE currently uses the AvalancheGo-provided DB. Revisit only if operator isolation or per-chain engine selection becomes required. |
| Medium | Full legacy operator config compatibility | Plumb the legacy `graft/subnet-evm/plugin/evm/config` surface into this VM's [`config.go`](config.go) where fields have SAE equivalents. This is needed before claiming full operator-config compatibility with existing Subnet-EVM deployments. |
| Medium | `networkUpgradeOverrides` support | Not a port target yet. The legacy plugin can apply them, but this VM deliberately keeps a support-decision TODO in [`vm.go`](vm.go) because accepting override timestamps would need fresh tests across SAE build, execute, and settle timing. |
| Medium | `eth_feeConfig` for `gaspricemanager` | Deferred. If tooling needs live fee-config answers, add an `eth_feeConfig`-shaped method to [`api/eth_extras.go`](api/eth_extras.go), but serialize the new `GasPriceConfig` shape rather than reviving legacy `FeeConfig`. |
| Medium | Post-Helicon cleanup | Once Helicon is permanently active on all supported networks, remove the shared `feeManager` retirement compatibility, the `IsHelicon` precompile-config interface tail, test fixture pinning, and legacy `FeeConfig` deprecation scaffolding. |
| Medium | Recovery-sensitive future hooks | If a future hook needs historical state again, revisit SAE tracker/root lifetime before adding per-block state opens. The gas-pricing artifact path was chosen specifically to avoid that recovery issue. |
| Low | Admitter state cache | Deferred performance work in [`../sae/admitter.go`](../sae/admitter.go). The current per-call state open is correct and bounded; cache only if profiling shows inbound allowlist admission is hot. |
| Low | SAE header-copy cleanup | [`../saexec/execution.go`](../saexec/execution.go) has two TODOs around passing copied headers to `GasConfigAfter` / `ExecutionArtifact`. Clean this up if the hook API grows a clearer immutable-header contract. |
| Low | Log JSON format support | [`vm.go`](vm.go) wires `log-level` into libevm and the AvalancheGo logger, but JSON log formatting is still a TODO. Add it only if operator config needs parity with legacy logging behavior. |
| Low | Warp predicate concurrency | [`warp/predicates.go`](warp/predicates.go) computes tx predicates serially. Parallelize only if predicate-heavy blocks show this as a measurable bottleneck. |

## Commit Map

Final signed feature commits, mapped to the parent-plan todo ids:

| Commit | Title | Todo ids |
| --- | --- | --- |
| `0075f0e2bd` | `sae/subnetevm: copy coreth VM scaffold` | `bootstrap-copy` |
| `991b5bfc07` | `sae/subnetevm: remove coreth-only plumbing` | `remove-atomic`, `remove-nativeasset-avax` |
| `4147c5d953` | `sae/subnetevm: switch to subnet-evm config and hooks` | `swap-chainconfig`, `header-extras`, `hook-rewrite` |
| `e76fdd74dd` | `sae/subnetevm: add standalone plugin entrypoint` | `factory-plugin` |
| `8f863e4765` | `sae/subnetevm: wire warp support` | `wire-warp` |
| `e6f72123bc` | `sae/subnetevm: serve validators API` | `validators-api` |
| `28f4dd8d7f` | `sae/subnetevm: apply state upgrades at activation` | `state-upgrades` |
| `457df4c38e` | `sae/subnetevm: wire allowlist precompiles` | `wire-deployer-tx-allowlist` |
| `a94e61e2c6` | `sae/subnetevm: wire native minter precompile` | `wire-nativeminter` |
| `ca41b2de1c` | `sae/subnetevm: route fees through reward manager` | `wire-rewardmanager` |
| `6ca592dede` | `sae/subnetevm: clean up legacy fee controls` | `force-disable-legacy-feemanager-at-helicon`, `force-disable-legacy-feemanager-at-helicon-tests`, `defer-relax-feeconfig-validations-under-sae` |
| `29e2cc20e2` | `sae/subnetevm: register gas price manager precompile` | `deferred-acp224feemanager` |
| `29b8480a26` | `sae/subnetevm: drive gas pricing from gas price manager` | `deferred-gaspricemanager-runtime`, `deferred-validatortargetgas-override`, `deferred-sae-recovery-settledroot-tracking` |
| `51ef742c0d` | `sae/subnetevm: add subnet-evm eth extras API` | `rpc-active-precompiles-rules` |
| `d45ac0b0b1` | `sae/subnetevm: document unsupported network upgrade overrides` | `tests-audit-legacy-subnetevm-vm-test` follow-up |
| `61348a6ab4` | `sae/subnetevm: polish validators warp and reward tests` | follow-up test/doc polish |

Generated build metadata is intentionally omitted from the feature map.
