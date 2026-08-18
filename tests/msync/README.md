# MerkleSync E2E

## Overview

This package provides a standalone end-to-end validation harness for C-Chain state sync bootstrap.
It defaults to the hashdb sync path (`--state-scheme=hash`) and can also run Firewood/MerkleSync
with `--state-scheme=firewood`.

It is intentionally separate from the shared `tests/e2e` suite. The goal is to provide targeted,
reviewable coverage for MerkleSync-specific bootstrap behavior without coupling this slower,
specialized flow into the general e2e matrix.

Run it locally with:

```bash
task tests:merkle-sync:e2e -- --min-bootstrap-height=300 --target-bytes=10485760
```

CI runs one job per state scheme:

```bash
./scripts/run_task.sh tests:merkle-sync:e2e-ci           # hash scheme (the default)
./scripts/run_task.sh tests:merkle-sync:e2e-ci-firewood  # firewood scheme
```

The CI entrypoints differ from the local `e2e` task in that they use a race-built Bazel avalanchego
binary. They are wired up as the `e2e-merkle-sync-hash` and `e2e-merkle-sync-firewood` jobs in
`.github/workflows/bazel-ci.yml` so a regression in either sync path is attributable on its own.

## What this validates

The harness is meant to prove more than "a new node became healthy."

A successful run should demonstrate all of the following:
- C-Chain was configured with the requested `state-scheme`, as reported by the VM itself
- the bootstrap node selected state sync rather than skipping it, and synced the expected summary
- the selected scheme's sync path actually ran (Firewood/MerkleSync, or the hashdb leaf-sync path)
- code sync was exercised
- heuristic block-sync/backfill activity was exercised
- post-bootstrap RPC checks confirm expected code, storage, and balance state

## Workload shape

The generated workload is intentionally mixed so the final synced state is nontrivial:
- deploys contracts
- performs trie/storage writes
- modifies existing storage
- issues plain transfers
- issues a C→X export so the atomic trie is non-empty

Current limitation:
- the C→X export currently improves realism/coverage only; this harness does not yet make
  dedicated post-bootstrap atomic-trie assertions

By default, the harness uses both:
- a minimum block-height threshold
- a measured data-growth threshold

The size threshold can be disabled by setting `--target-bytes=0`.

This helps ensure the bootstrap target contains enough history and state to make MerkleSync
validation meaningful.

The size threshold measures growth of the shared state paths:
- `db/`
- `chainData/`

It does not measure the total node directory size.

## Topology and lifecycle

The harness uses a two-phase tmpnet lifecycle:

1. start one validator for state generation
2. generate workload until thresholds are reached
3. stop that validator
4. copy only shared state (`db/`, `chainData/`) to the second validator
5. start the final serving validator topology
6. force enough post-restart blocks to cross a fresh summary boundary and record the refreshed summary height
7. start a fresh ephemeral bootstrap node and validate bootstrap

Important notes:
- the initial generation phase temporarily disables sybil protection on the single generation node so
  a one-node tmpnet can produce the required state and history
- before bootstrap validation, the final serving topology is restarted without that override so the
  bootstrap node validates against the intended serving network shape
- only shared execution/state data is copied between validators
- node-specific identity/runtime files are intentionally not copied

## Evidence sources

Bootstrap correctness is validated from multiple sources.

### Post-bootstrap RPC checks
The harness verifies:
- deployed contract bytecode exists
- selected storage values match expected final state
- transfer recipient balance matches the generated workload

### Metrics
The harness checks metrics showing:
- bootstrap-node metrics provide state-request activity for the selected scheme
  (`avalanche_evm_sync_firewood_sync_requests_made` for `firewood`,
  `avalanche_evm_eth_sync_state_trie_leaves_requested` for `hash`)
- validator metrics provide serving-side evidence, including code sync requests and block-request activity
- for `hash`, validator metrics must additionally show served leafs requests
  (`avalanche_evm_eth_leafs_request_count`); Firewood range proofs are served over a dedicated p2p
  handler instead, so no equivalent serving-side metric is asserted for `firewood`

### Health API
Sync-path evidence comes from the bootstrap node's `/ext/health`, where the C-Chain VM reports its
own state under `checks.C.message.engine.vm`:

```json
{
  "state": "normalOp",
  "stateScheme": "firewood",
  "stateSync": {
    "status": "completed",
    "summaryHeight": 320,
    "summaryHash": "0x..."
  }
}
```

The harness asserts that:
- `state` is `normalOp`, so the node finished the whole bootstrap path
- `stateScheme` is the scheme the run requested, which is what proves the sync exercised that scheme's
  storage backend
- `stateSync.status` is `completed`, which simultaneously rules out the `disabled`, `skipped` and
  `failed` outcomes that would otherwise leave the harness validating a plain bootstrap
- `stateSync.summaryHeight` is the refreshed summary height the harness forced before starting the
  bootstrap node, which proves *which* summary was synced

This replaces the earlier approach of grepping `logs/C.log` for syncer names and phrases like
"state sync started". The VM is the authority on its own state, so the assertions no longer depend on
log wording, log levels, or log-line ordering. `logs/C.log` remains the place to look when
diagnosing a failure.

The producer of these fields is `Health` in `vms/saevm/sae/health.go`, which reports `state` and
`stateScheme` and carries its own `TODO(#5513)` for the `stateSync` object. The harness re-declares
the JSON shape it expects rather than importing that type, so a rename fails this test loudly.

Two caveats on the shape above, both tracked by #5513:
- `stateSync` is never reported by the SAE C-Chain, so the harness does not assert it in SAE mode
- the equivalent coreth producers do not exist on this branch (`graft/coreth/plugin/evm/health.go` is
  a `nil, nil` stub, and there is no `engine.SyncStatus` in `graft/evm/sync/engine/client.go`), so a
  coreth run cannot satisfy these assertions at all — see the SAE C-Chain mode section

### Reported durations

The harness polls the bootstrap node's health while it bootstraps and reports two durations:

- `stateSyncDuration` — from the first health check that reported `stateSync.status = syncing` to the
  first one that reported a finished status. This is the sync itself, excluding node startup and the
  bootstrapping that follows it.
- `bootstrapDuration` — from starting the node to it first reporting healthy. This covers everything.

Precision notes:
- the VM does not time its own sync, so `stateSyncDuration` is *sampled*: the harness uses the node's
  own health check timestamps, and each observation records a transition that happened somewhere in
  the preceding check interval
- the bootstrap node is therefore configured with `--health-check-frequency=500ms` instead of the
  tmpnet default of 2s, so the reported duration is accurate to within roughly half a second
- `bootstrapDuration` is measured directly by the harness and is exact
- a sync fast enough to start and finish inside a single check interval is never observed in
  progress; the harness logs a warning instead of a duration, and the correctness assertions above
  are unaffected

## Configuration knobs

Useful flags include:
- `--target-bytes` (set to `0` to disable the size threshold)
- `--min-bootstrap-height`
- `--batch-size`
- `--writes-per-tx`
- `--load-write-slots`
- `--load-modify-slots`
- `--state-sync-min-blocks`
- `--state-sync-commit-interval`
- `--state-scheme`
- `--activate-latest-after`

These exist to help local iteration and future fixture generation.

State-scheme note:
- `--state-scheme` accepts `hash` (default) or `firewood`, and applies to the serving nodes and the
  bootstrap node alike
- scheme-specific chain configuration lives in one place (`newStateSchemeConfig` in `main.go`), which
  also carries the scheme's expected bootstrap evidence; `firewood` additionally requires
  `snapshot-cache: 0` and an unset `populate-missing-tries`, while `hash` keeps the default snapshot
  cache so serving nodes can answer leafs requests from their snapshots
- run the Firewood/MerkleSync variant with:
  `task tests:merkle-sync:e2e -- --state-scheme=firewood`

Upgrade-schedule note — this selects the C-Chain implementation under test:
- unlike the shared `tests/e2e` suite (which leaves the latest upgrade unscheduled by default), this
  harness defaults `--activate-latest-after=0` so the latest upgrade is active from genesis and the
  synced state reflects the newest format
- the flag remains overridable: `<0` leaves the latest upgrade unscheduled, `0` activates it from
  genesis, and `>0` schedules it that duration after network start
- the latest upgrade is **Helicon**, which transitions the C-Chain from coreth to the SAE VM
  (`vms/saevm/cchain`). The transition happens at the first block at or after `heliconTime`, so
  `--activate-latest-after=0` means the SAE C-Chain serves the chain from height 0 and coreth is
  shut down before the harness issues its first transaction

### SAE C-Chain mode (the default)

With Helicon scheduled (`--activate-latest-after >= 0`) the harness runs in SAE mode, which it logs
at startup as `saeCChain: true`. **The SAE C-Chain does not implement state sync** — see the
`TODO(#5513)` in `vms/saevm/cchain/sync.go`, the unimplemented `AcceptSummary` in
`vms/saevm/statesync/acceptor.go`, and the commented-out `state-sync-enabled` config field in
`vms/saevm/cchain/config.go`. `StateSyncEnabled()` returns `false`, so no configuration can make a
sync happen.

In SAE mode the harness therefore validates **bootstrap and post-bootstrap state only**, and says so
in a startup warning. Specifically it drops:
- the state sync status, summary-hash and summary-height health assertions (SAE's health details
  carry no `stateSync` object; see the matching `TODO(#5513)` in `vms/saevm/sae/health.go`)
- the scheme-specific sync metrics and the code/block serving metrics, which are coreth metrics
- the `state-sync-*` chain config keys, which the SAE C-Chain does not accept
- the summary-boundary assertion in the post-restart phase, which still runs to prove the restarted
  topology builds blocks

What it still proves: the requested state scheme is in use (`stateScheme` in the VM health details),
a fresh node bootstraps to `normalOp` against the serving topology, and the post-bootstrap RPC checks
match the generated workload.

A negative value leaves Helicon unscheduled and keeps the chain on coreth:

```bash
task tests:merkle-sync:e2e -- --activate-latest-after=-1s
```

**That path currently fails**, for a reason that predates SAE mode: `HealthCheck` in
`graft/coreth/plugin/evm/health.go` returns `nil, nil` ("TODO perform actual health check"), so a
coreth C-Chain reports `"vm": null` in its health details and every health assertion below is
unsatisfiable. `stateScheme` and `engine.SyncStatus` do not exist in coreth or
`graft/evm/sync/engine/client.go` on this branch either — only `vms/saevm/sae/health.go` implements
these details. A coreth run reaches the evidence step (workload, bootstrap and the post-bootstrap RPC
checks all pass, and the sync metrics are satisfied) and then fails in
`validateMerkleSyncEvidence`. Fixing it needs a real coreth `HealthCheck`, not a change to this
harness.

Two SAE behaviours the harness has to accommodate, both handled for coreth too:
- **explicit gas limits.** The SAE C-Chain never sets `rpc.Config.GasCap`, so libevm's estimator
  falls back to `MaxUint64/2` as its ceiling and `eth_estimateGas` can return a limit the mempool
  rejects with `exceeds block gas limit`. Every transaction the harness issues therefore carries an
  explicit gas limit sized to its work rather than an estimate; `requireGasLimitsFitBlock` checks the
  largest against the chain's own block gas limit before the workload starts. SAE also charges at
  least `ceil(gasLimit/params.Lambda)`, so over-provisioning a limit costs real block space.
- **asynchronous execution.** SAE executes blocks asynchronously and streams receipts per
  transaction, while the RPC resolves `latest` to the last *fully executed* block. A receipt
  therefore does not imply the deployed code is readable at `latest`, so the harness polls for it
  (`awaitDeployed`) instead of using `bind.WaitDeployed`.

Important tmpnet-scale tuning note:
- `--state-sync-min-blocks` is intentionally lowered so the bootstrap node chooses state sync rather
  than skipping it on a small tmpnet run. It has no effect in SAE mode
- `--state-sync-commit-interval` is part of making refreshed summary-boundary evidence observable
  and repeatable during the restarted serving phase. In SAE mode it only sets `commit-interval` and
  the number of blocks forced after the serving restart

Maintenance note:
- this harness relies on the VM health details described above and on specific metric names
- the health assertions are a structured proof of which summary was synced and how the sync ended;
  the metric assertions remain heuristic evidence that code sync and block backfill were exercised
- if tmpnet, the health details, or C-Chain sync metrics change, the evidence checks in this harness
  may need to be updated even when the underlying bootstrap behavior is still correct

## Current limitations

This harness currently generates state from scratch during the run.
That is slower than an archive-backed restore flow, but it provides a trustworthy baseline for
validation.

## Related files

- `tests/msync/main/main.go`
- `tests/msync/main/BUILD.bazel`
- `tests/msync/Taskfile.yml`
- `tests/Taskfile.yml`
- `Taskfile.yml`
- `.github/workflows/bazel-ci.yml`
- `.review-briefs/merkle-sync-e2e.md`

Implementation note:
- `plans/msync-e2e.md` contains implementation scaffolding/background and is not the durable
  maintenance documentation for this harness
