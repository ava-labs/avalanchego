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
| VM initialization, handlers, lifecycle | [`vm.go`](vm.go), [`genesis.go`](genesis.go), [`metrics.go`](metrics.go) |
| Operator config | [`config.go`](config.go), [`config.md`](config.md) |
| SAE hooks and block building | [`hooks.go`](hooks.go), [`gas_config.go`](gas_config.go) |
| Subnet-EVM JSON-RPC extras | [`api/eth_extras.go`](api/eth_extras.go), [`api/client/client.go`](api/client/client.go) |
| Validators API and uptime | [`api/validators.go`](api/validators.go), [`validators/manager.go`](validators/manager.go) |
| Warp glue over the shared [`vms/saevm/warp`](../warp) | [`warp/`](warp/) |
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
| Subnet-EVM header extras plus SAE fields | [`graft/subnet-evm/plugin/evm/customtypes`](../../../graft/subnet-evm/plugin/evm/customtypes), [`hooks.go`](hooks.go) | [`graft/subnet-evm/plugin/evm/customtypes`](../../../graft/subnet-evm/plugin/evm/customtypes), [`hooks_test.go`](hooks_test.go) |
| Warp precompile, predicates, and ACP-118 signing | [`warp/`](warp/), [`hooks.go`](hooks.go) | [`vm_warp_test.go`](vm_warp_test.go), [`warp/*_test.go`](warp/) |
| `validators.getCurrentValidators` | [`api/validators.go`](api/validators.go), [`validators/manager.go`](validators/manager.go) | [`vm_validators_test.go`](vm_validators_test.go) |
| `eth_getActiveRulesAt` | [`api/eth_extras.go`](api/eth_extras.go) | [`vm_eth_extras_test.go`](vm_eth_extras_test.go) |
| `txallowlist` and `deployerallowlist` | [`hooks.go`](hooks.go), [`vms/saevm/sae/admitter.go`](../sae/admitter.go), [`graft/subnet-evm/precompile/contracts`](../../../graft/subnet-evm/precompile/contracts) | [`vm_txallowlist_test.go`](vm_txallowlist_test.go), [`vm_deployerallowlist_test.go`](vm_deployerallowlist_test.go), [`../sae/admitter_test.go`](../sae/admitter_test.go) |
| `nativeminter` | upstream precompile package | [`vm_nativeminter_test.go`](vm_nativeminter_test.go) |
| `rewardmanager` fee routing | [`hooks.go`](hooks.go), [`config.go`](config.go) | [`vm_rewardmanager_test.go`](vm_rewardmanager_test.go), [`hooks_test.go`](hooks_test.go) |
| State upgrades | [`hooks.go`](hooks.go) | [`vm_test.go`](vm_test.go) |
| `gaspricemanager` precompile and runtime gas config | [`hooks.go`](hooks.go), [`gas_config.go`](gas_config.go) | [`vm_gaspricemanager_test.go`](vm_gaspricemanager_test.go), [`hooks_test.go`](hooks_test.go) |
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
| Gas pricing | ACP-176 | ACP-176 plus `gaspricemanager` runtime config carried in the header |

## Differences From `graft/subnet-evm`

This VM intentionally reuses the legacy Subnet-EVM packages for chain rules,
precompiles, and wire shapes where they still apply. The differences below are
the parts a reviewer should not expect to match line-for-line.

| Area | `graft/subnet-evm` plugin | SAE `subnetevm` |
| --- | --- | --- |
| Execution model | Synchronous block execution in the legacy EVM pipeline | SAE build, execute, and settle pipeline with Tau-lag-aware reads |
| State timing | Most feature checks observe the current parent/execution state | Admission and runtime hooks may intentionally read last-executed state, last-settled state, or values stamped into the parent header, depending on the consensus boundary |
| Package boundary | Full legacy VM under [`graft/subnet-evm/plugin/evm`](../../../graft/subnet-evm/plugin/evm) | Chain wrapper under this package, generic mechanics in [`vms/saevm`](../) |
| Config surface | Broad legacy operator config, including standalone DB and legacy fee knobs | Narrow SAE config in [`config.go`](config.go); unsupported legacy knobs are deferred rather than silently reinterpreted |
| Database mode | Supports standalone per-chain database mode | Uses the AvalancheGo-provided database; standalone DB is deferred |
| State sync | Legacy plugin state-sync paths remain in `graft/subnet-evm` | Not ported in this wrapper |
| Network upgrade overrides | Supports `networkUpgradeOverrides` | Supported from upgrade bytes and applied before chain-config validation and Ethereum-fork alignment |
| Legacy `feeManager` | Retired at Helicon through shared `graft/subnet-evm` retirement helpers | Same retirement helpers are called during SAE genesis parsing |
| Gas price manager | Precompile package and registry live under `graft/subnet-evm` | Runtime base-fee path reads the precompile config from the parent header (stamped from settled state at build time) |
| `txallowlist` admission | Legacy libevm hook path | RPC/mempool ingress uses [`../sae/admitter.go`](../sae/admitter.go); worst-case uses [`hooks.go`](hooks.go) |
| `deployerallowlist` | libevm `CanCreateContract` frame-local revert | Same enforcement layer; SAE does not add a separate admission check |
| RPC extras | Legacy exposes older Subnet-EVM extras, including deprecated surfaces | This VM serves `eth_getActiveRulesAt`; `eth_getActivePrecompilesAt` and `eth_feeConfig` are intentionally not served |
| Validators API | Legacy `/validators` service with an operator enablement gate | Always served under [`api/validators.go`](api/validators.go) with SAE uptime tracking |
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

ACP-118 verification intentionally uses the shared SAE verifier's ordering and
error-code space: parse failures use code 2 and uptime verification failures
use code 4, rather than the legacy verifier's 1/2 codes. The Subnet-EVM-specific
message encodings remain unchanged.

## Design Decisions and Alternatives

### Genesis and Upgrade Config

Genesis precompiles and `upgradeBytes` are parsed through the shared
`extras.ChainConfig` machinery, so the same upstream precompile configs drive
both the legacy plugin and this wrapper. The SAE hook applies timestamped
`PrecompileUpgrades` and `StateUpgrades` in the `(parent.Time, block.Time]`
window before EVM transactions execute.

`networkUpgradeOverrides` from upgrade bytes are applied before chain-config
validation and Ethereum-fork alignment. On restart, compatibility is checked
against the canonical last-accepted block read from the chain database,
falling back to genesis only when the database is empty.

This is intentionally separate from the read paths below. Activation mutates
the child block's post-execution state. Any value that affects block building,
admission, or header verification must still read from the state view that the
builder and verifier can both reproduce.

### Header Extras

The port reuses Subnet-EVM's `customtypes` instead of inventing a local SAE
header package. SAE fields are appended to the existing header-extra shape:
the `Settled*` quartet records which execution results the block settles,
`TargetExcess` carries ACP-176's gas-target vote state, and the `GasConfig*`
triple carries the effective ACP-224 price configuration (see the gas price
manager section below). When the precompile pins the gas target,
`FinalizeHeader` writes its derived ACP-176 exponent into `TargetExcess`; when
validators control the target, that field continues its bounded per-block
evolution. `BlockGasCost` remains in the header for layout compatibility but
is always stamped to zero. ACP-226 and ACP-176 own block delay and gas pricing
under SAE.

SAE-only fields are built and rebuilt only at or after the effective Helicon
timestamp. They are optional tail fields, so nil values preserve legacy RLP
encodings; the legacy VM rejects headers that populate them. Standard block
RPC responses expose these fields. When the first SAE block follows a parent
without `MinDelayExponent`, no ACP-226 minimum delay applies, but SAE still
enforces monotonic block time. Blocks always carry the canonical predicate
results encoding, including an encoded empty result when no transaction has a
predicate.

[`vms/evm/dynamic`](../../evm/dynamic) is the single owner of scalar delay,
target, and price exponent logic. [`vms/evm/acp176`](../../evm/acp176) retains
the composite gas-time state and delegates scalar target operations to it.

### Settled-State System Configuration Reads

Several Subnet-EVM precompiles act as system configuration rather than ordinary
contract state: `txallowlist` controls admission, `rewardmanager` controls fee
routing, and `gaspricemanager` controls the gas clock. SAE must choose the state
view for each read deliberately because build, execute, and settle are separate
phases.

Reads that affect block building use settled-state timing: reward routing checks
whether `rewardmanager` is active at `settled.Time`, tx allowlist worst-case
admission reads the last-settled state, and gas pricing reads the GasConfig*
group stamped into the parent header (itself derived from settled state when
the stamping block was built). Inbound mempool verification is the exception: it
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

**Breaking Change**: The routed amount follows libevm semantics: after London, `header.Coinbase`
receives only `gasUsed * effectiveTip`, and the base-fee component is burned.
Legacy `graft/subnet-evm` credited `gasUsed * effectiveGasPrice` to coinbase,
so this is an intentional semantic difference in the SAE port.

### Gas Price Manager Runtime

The `gaspricemanager` precompile stores the effective ACP-224 gas-pricing
configuration in contract storage, but blocks are built and verified before
their own execution results exist. The shipped design encodes the effective
configuration into the header itself: when a builder finalizes a header
([`hooks.go`](hooks.go) `FinalizeHeader`), it reads `gaspricemanager` storage
from the settled state and stamps the `GasConfig*` header-extra group
([`gas_config.go`](gas_config.go)). `GasConfigAfter` then recovers the gas
clock for any block from its parent header alone: header group first, genesis
precompile config as the fallback for blocks built before the first stamped
header, ACP-176 defaults otherwise. An activated precompile whose storage
reads back zero-valued is an error, not a silent fallback, so corrupt storage
cannot cause divergence.

The genesis fallback converts configured `TargetGas` through
`DesiredTargetExcess`, using the same canonical target representation as
stamped headers.

This makes recovery, historical replay, and rebuild-verification
self-contained at the header level: no per-block side artifacts are persisted
and no historical state roots need to be reopened.

Alternative considered (and shipped first in the spike): persist a hook
artifact per executed block and load it by `SettledHeight`. It was replaced
because it created a recovery surface (artifacts must exist for every replayed
block) and an extra consensus-adjacent database dependency, where the header
already travels with the block.

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
wiped at the transition. Legacy `FeeConfig` is otherwise inert and unvalidated
under SAE. If a pre-Helicon `feeManager` is configured without
`initialFeeConfig`, parsing substitutes `DefaultFeeConfig` because that
compatibility activation still reads it.

### Plugin and Factory

The standalone plugin entrypoint mirrors the legacy plugin runner and registers
Subnet-EVM libevm extras process-wide before serving.


## TODOs and Deferred Work

| Priority | Todo | Status |
| --- | --- | --- |
| High | Standalone per-chain database support | Skipped for this port. Legacy Subnet-EVM supports per-chain DB engines and paths; SAE currently uses the AvalancheGo-provided DB. Revisit only if operator isolation or per-chain engine selection becomes required. |
| Medium | Full legacy operator config compatibility | Plumb the legacy `graft/subnet-evm/plugin/evm/config` surface into this VM's [`config.go`](config.go) where fields have SAE equivalents. This is needed before claiming full operator-config compatibility with existing Subnet-EVM deployments. |
| Medium | `eth_feeConfig` for `gaspricemanager` | Deferred. If tooling needs live fee-config answers, add an `eth_feeConfig`-shaped method to [`api/eth_extras.go`](api/eth_extras.go), but serialize the new `GasPriceConfig` shape rather than reviving legacy `FeeConfig`. |
| Medium | Legacy-to-SAE transition with active `gaspricemanager` state | Unsupported until transition code materializes the inherited configuration in the first SAE header, because legacy headers have no `GasConfig*` group. |
| Medium | Post-Helicon cleanup | Once Helicon is permanently active on all supported networks, remove the shared `feeManager` retirement compatibility, the `IsHelicon` precompile-config interface tail, test fixture pinning, and legacy `FeeConfig` deprecation scaffolding. |
| Medium | Recovery-sensitive future hooks | If a future hook needs historical state again, revisit SAE tracker/root lifetime before adding per-block state opens. The header-encoded gas config was chosen specifically to avoid that recovery issue. |
| Low | Admitter state cache | Deferred performance work in [`../sae/admitter.go`](../sae/admitter.go). The current per-call state open is correct and bounded; cache only if profiling shows inbound allowlist admission is hot. |
| Low | Log JSON format support | [`vm.go`](vm.go) wires `log-level` into libevm and the AvalancheGo logger, but JSON log formatting is still a TODO. Add it only if operator config needs parity with legacy logging behavior. |
