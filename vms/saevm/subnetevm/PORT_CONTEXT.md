# Subnet-EVM SAE Port Context

This file merges the repo-relevant context from the external planning notes:

- `/Users/ceyhun.onur/.cursor/plans/subnetevm_sae_vm_port_6ee040b4.plan.md`
- `/Users/ceyhun.onur/.cursor/plans/acp224_plumbing_afterblock_8293d192.plan.md`

Use this as AI handover context. The current source code and
[`README.md`](README.md) are the source of truth. Some original plan text used
the pre-reorg path `vms/subnetevm`; in this repo state that maps to
`vms/saevm/subnetevm`.

## Port Goal

Create an SAE-based Subnet-EVM VM as the counterpart to the legacy
[`graft/subnet-evm`](../../../graft/subnet-evm) plugin. The VM starts from the
SAE/Coreth wrapper shape, removes C-Chain/Coreth-only behavior, and wires the
Subnet-EVM feature surface through SAE hooks.

The core sequencing was:

1. Copy the Coreth/C-Chain SAE scaffold.
2. Remove atomic transactions, shared memory, native-asset legacy plumbing,
   ExtData, and `/avax` RPC.
3. Swap to `graft/subnet-evm/params(/extras)` and Subnet-EVM custom header
   extras.
4. Adapt the SAE hook and block builder.
5. Add Subnet-EVM features incrementally: warp, validators API, state upgrades,
   allowlists, native minter, reward manager, gas price manager, and RPC extras.
6. Retire legacy fee controls and document remaining TODOs.

## Scope Decisions

### In Scope

- Subnet-EVM chain config and fork timeline:
  `SubnetEVM`, `Durango`, `Etna`, `Granite`, `Helicon`.
- Subnet-EVM custom header extras, extended with SAE fields:
  `SettledHeight` and `TargetExcess`.
- Warp precompile and predicate support.
- `validators.getCurrentValidators`.
- `eth_getActiveRulesAt`.
- `txallowlist`, `deployerallowlist`, `nativeminter`, `rewardmanager`,
  `gaspricemanager`.
- `GenesisPrecompiles`, `PrecompileUpgrades`, and `StateUpgrades`.
- Standalone rpcchainvm plugin entrypoint.

### Out of Scope / Deferred

- Standalone per-chain database support.
- Full legacy operator config compatibility.
- `networkUpgradeOverrides` support.
- `eth_feeConfig` for the new `gaspricemanager` shape.
- Legacy `feemanager` reintroduction.
- Post-Helicon cleanup of legacy fee compatibility.
- State-sync support in this wrapper.
- Parity testdata corpus; coverage is per-feature SUT tests plus upstream
  `graft/subnet-evm` package tests.

## Header Extras

The port reuses `graft/subnet-evm/plugin/evm/customtypes` rather than creating a
new local header package. This keeps the Subnet-EVM wire shape while adding SAE
fields.

Final relevant fields:

- `BlockGasCost`: kept for layout parity, always stamped to zero under SAE.
- `TimeMilliseconds`: existing Subnet-EVM field.
- `MinDelayExcess`: ACP-226.
- `SettledHeight`: SAE execution-result lookup.
- `TargetExcess`: ACP-176 gas target vote state.

Decision: keep `BlockGasCost` inert instead of deleting it. ACP-226 owns block
delay and ACP-176 / `gaspricemanager` own gas pricing.

## Upgrade and Genesis Handling

`GenesisPrecompiles` and `upgradeBytes` use the shared
`extras.ChainConfig` machinery:

- `extras.ChainConfig.UnmarshalJSON` parses inline genesis precompile configs
  into `GenesisPrecompiles`.
- `vms/saevm/subnetevm/vm.go::parseGenesis` unmarshals `UpgradeConfig` from
  `upgradeBytes`.
- `core.SetupGenesisBlock` / `Genesis.toBlock` applies genesis precompile state
  via `ApplyPrecompileActivations`.
- `hook.Points.BeforeExecutingBlock` applies timestamped `PrecompileUpgrades`
  and `StateUpgrades` for `(parent.Time, block.Time]`.

Important timing split: activations mutate the child block's post-execution
state. Any value committed into a header, worst-case prediction, or admission
decision must be read from a state view that the builder and verifier can both
reproduce.

## System-Configuration State Reads

Several precompiles act as system configuration:

- `txallowlist`: transaction admission.
- `rewardmanager`: fee recipient / `header.Coinbase`.
- `gaspricemanager`: gas target and base-fee config.

SAE has distinct build, execute, and settle phases, so every read chooses an
explicit state view:

- `txallowlist` mempool/RPC ingress reads last-executed state via
  `vms/saevm/sae/admitter.go`. This gives operators fast ingress behavior after
  role changes.
- `txallowlist` worst-case admission reads last-settled state because that is
  the state available to the build path.
- `rewardmanager` checks activation at `settled.Time` and reads the state passed
  to `BuildBlock`, which corresponds to the build-time settled view.
- `gaspricemanager` projects state into a persisted hook artifact at execution
  time, then later reads that artifact by `SettledHeight`.

The expected trade-off is a temporary disagreement between last-executed and
last-settled paths around activation windows. Tests intentionally assert this
instead of hiding it.

## Allowlist Design

`txallowlist` is block-validity-sensitive: libevm transaction preflight errors
can invalidate execution. SAE therefore owns the post-Helicon tx sender check in
both mempool admission and worst-case admission.

`deployerallowlist` stays in libevm's `CanCreateContract` hook. That hook runs
inside EVM frames and produces a failed receipt rather than a block-invalidating
error. SAE does not add a separate deploy admission check because a top-level
`tx.To() == nil` check would miss nested `CREATE` / `CREATE2`.

## Native Minter

`nativeminter` requires no SAE hook. The upstream precompile mutates the active
`StateDB` inline inside the EVM call, so worst-case and actual execution observe
the same state transition.

## Reward Manager

Reward routing is implemented by stamping `header.Coinbase`.

Rules mirror legacy Subnet-EVM:

- If `rewardmanager` is not active at the settled timestamp, use the local
  configured fee recipient when chain-level `AllowFeeRecipients` is true;
  otherwise burn.
- If `rewardmanager` is active and its state allows fee recipients, use the
  local configured fee recipient.
- Otherwise use the stored reward address, including the zero address if that is
  what legacy state semantics produce.

Determinism issue: in fee-recipient-allowed branches, the builder's local config
can differ across nodes. Rebuilders therefore use the received block's
`header.Coinbase` for those branches so `VerifyBlock` can rebuild the exact
received header. Deterministic branches still reject incorrect coinbase values
via hash mismatch.

## ACP-224 / `gaspricemanager` Runtime

Original ACP-224 plan: extend `hook.Points.GasConfigAfter` to accept a settled
`libevm.StateReader` rooted at `h.Root`, then read `gaspricemanager` storage in
the hook at every call site.

That approach was straightforward but had a recovery concern: historical replay
and recovery paths would need to reopen many roots, and earlier prototypes
exposed root-lifetime issues around `saedb.Tracker`.

Final shipped design:

- `hook.Points.ExecutionArtifact` reads `gaspricemanager` storage from the
  block's post-execution state and encodes the effective gas config into an
  opaque artifact.
- SAE persists the artifact with execution results.
- `hook.Points.GasConfigAfter` loads the artifact by `SettledHeight`.
- If the artifact is empty, the hook falls back to ACP-176 defaults.
- `ValidatorTargetGas=true` means target gas comes from header `TargetExcess`.
- `ValidatorTargetGas=false` means target gas is pinned by precompile storage.

This avoids historical state opens during recovery while keeping worst-case and
actual execution deterministic.

Important caveat: `Block.MarkSynchronous` still opens state directly before the
production `saedb.Tracker` exists. This works for always-SAE genesis state, but
transition chains may materialize old synchronous blocks whose state has been
pruned. The high-priority follow-up is to route `MarkSynchronous` through the
production tracker once init ordering allows it.

Alternative considered: add a `GasConfigNeedsState(h) bool` hook gate so chains
without `gaspricemanager` avoid state reads. Estimated savings were small, and
the artifact design made this unnecessary for the recovery problem. Revisit only
if profiling identifies the artifact path as hot.

## Legacy Fee Controls

Legacy `feeManager` is retired at Helicon using shared helpers under
`graft/subnet-evm/precompile/contracts/feemanager/retirement`.

Cases:

- A stale genesis `feeManager` entry at or after Helicon is normalized out when
  chain genesis predates Helicon. Existing chains cannot change immutable
  genesis bytes.
- Any `feeManager` `PrecompileUpgrades` entry at or after Helicon is rejected.
  Operators can remove those upgrade bytes before restart.
- If `feeManager` is live before Helicon, parsing injects a synthetic disable at
  Helicon so its storage is wiped at transition.

Legacy `FeeConfig` remains for legacy startup, but SAE treats the zero value as
inert and skips validation because ACP-176 and `gaspricemanager` own runtime gas
pricing here.

Post-Helicon cleanup should remove the retirement helpers, test pinning,
`IsHelicon` precompile-config interface tail, and legacy `FeeConfig`
deprecation scaffolding after Helicon is permanently active on all supported
networks.

## Validators API

The legacy validators API is re-hosted under `vms/saevm/subnetevm/api`.

The API is kept behind small interfaces to avoid import cycles. `validators`
manager owns the uptime tracker, connection events, initial sync, and periodic
sync. Tests drive deterministic uptime through the VM's mockable clock.

## RPC Extras

The SAE wrapper serves `eth_getActiveRulesAt`, using the same wire shape as the
legacy modern endpoint.

Intentionally not served:

- `eth_getActivePrecompilesAt`: deprecated upstream.
- `eth_feeConfig`: deferred until a `gaspricemanager`-shaped live answer is
  needed by tooling.

## Plugin

The plugin entrypoint mirrors the legacy Subnet-EVM plugin runner:

- Register Subnet-EVM libevm extras first.
- Build the SAE VM.
- Serve through `rpcchainvm`.
- Support `--version` as an operator sanity check.

## Prioritized Follow-Ups

High:

- Route `MarkSynchronous` through production `saedb.Tracker`.
- Add standalone per-chain database support if this VM needs legacy operator DB
  isolation.

Medium:

- Plumb the legacy operator config surface into `config.go` where fields have
  SAE equivalents.
- Decide whether to support `networkUpgradeOverrides`.
- Add `eth_feeConfig` for the new `gaspricemanager` shape if tooling requires
  it.
- Do post-Helicon cleanup of legacy fee controls.
- Revisit SAE tracker/root lifetime if future hooks need historical state.

Low:

- Cache admitter state if profiling shows txallowlist admission is hot.
- Clean up copied-header TODOs in `saexec`.
- Add JSON log formatting parity if operators need it.
- Parallelize warp predicate calculation if it becomes a bottleneck.

## Verification Commands

Focused commands used during this port:

```sh
go build ./vms/saevm/subnetevm/... ./vms/saevm/saexec/... ./vms/saevm/worstcase/... ./vms/saevm/blocks/... ./vms/saevm/hook/... ./graft/subnet-evm/precompile/contracts/gaspricemanager/... ./graft/subnet-evm/precompile/contracts/feemanager/...
go test ./vms/saevm/subnetevm/... ./vms/saevm/saexec/... ./vms/saevm/worstcase/... ./vms/saevm/blocks/... ./vms/saevm/hook/... ./graft/subnet-evm/precompile/contracts/gaspricemanager/...
```

Plugin smoke test:

```sh
go build -o /tmp/subnetevm-sae-plugin ./vms/saevm/subnetevm/plugin/
/tmp/subnetevm-sae-plugin --version
```

## Source Plan Notes

The parent plan still contains useful historical details, but it is not the
source of truth. Notable differences from the current repo state:

- Old path `vms/subnetevm` now lives at `vms/saevm/subnetevm`.
- Some early alternatives were superseded by implementation decisions captured
  in `README.md`.
- The ACP-224 plan describes the original state-reader design; the final code
  uses persisted hook artifacts instead.

