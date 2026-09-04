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

### Sync lifecycle metrics
Sync-path evidence comes from the bootstrap node's `/ext/metrics`, where the SAE C-Chain's summary
handler (`vms/saevm/statesync`) reports the sync's lifecycle as gauges, labeled with the chain:

```text
avalanche_evm_transition_statesync_in_progress{chain="C"}         0
avalanche_evm_transition_statesync_summary_height{chain="C"}      320
avalanche_evm_transition_statesync_started_timestamp{chain="C"}   1.7e+09
avalanche_evm_transition_statesync_finished_timestamp{chain="C"}  1.7e+09
avalanche_evm_transition_statesync_failed{chain="C"}              0
```

The harness asserts that:
- the node reports healthy, so it finished the whole bootstrap path
- `started_timestamp` and `finished_timestamp` are set, `in_progress` is 0 and `failed` is 0, which
  simultaneously rules out the disabled, never-offered, declined and failed outcomes that would
  otherwise leave the harness validating a plain bootstrap
- `summary_height` is at or above the refreshed summary height the harness forced before starting the
  bootstrap node, which proves *which* summary was synced
- the scheme-specific request metrics (see above) prove the sync exercised the requested scheme's
  storage backend

This replaces the earlier approaches of grepping `logs/C.log` for syncer names and of decoding the
VM's health details: the metrics API is uniform across chains and pollable at any frequency, and the
VM is still the authority on its own state. `logs/C.log` remains the place to look when diagnosing a
failure.

The producer of these gauges is `lifecycleMetrics` in `vms/saevm/statesync/metrics.go`, recorded by
the C-Chain summary handler (`vms/saevm/cchain/statesync`) around the sync it launches. The harness
re-declares the metric names it expects rather than importing them, so a rename fails this test
loudly.

Two caveats:
- the lifecycle is asserted whenever the SAE C-Chain can sync the requested scheme, which excludes
  SAE with `--state-scheme=firewood` — see the SAE C-Chain mode section
- coreth registers no equivalent metrics, so a coreth run asserts the request metrics and the
  bootstrapped state only — see the SAE C-Chain mode section

### Reported durations

The harness reports two durations:

- `stateSyncDuration` — the difference between the sync's own `started_timestamp` and
  `finished_timestamp` gauges. This is the sync itself, excluding node startup and the bootstrapping
  that follows it, timed by the VM and therefore exact.
- `bootstrapDuration` — from starting the node to it first reporting healthy, measured directly by
  the harness. This covers everything.

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
- the SAE C-Chain cannot state sync `firewood` yet, so that combination bootstraps from genesis and
  drops the sync assertions; see the SAE C-Chain mode section

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
at startup as `saeCChain: true`. The SAE C-Chain implements state sync in `vms/saevm/cchain/statesync`,
over the SAE handler in `vms/saevm/statesync`, and reports its lifecycle as metrics, so an SAE run
asserts the sync itself. Two differences from a coreth run remain:

- **Firewood is not syncable yet.** The SAE syncer builds a `hashdb` client unconditionally, so the
  SAE C-Chain can only sync the `hash` scheme; see the `TODO(alarso16)` about Firewood in
  `vms/saevm/statesync/server.go`. An SAE run with `--state-scheme=firewood` therefore bootstraps
  from genesis instead of syncing.
- **The sync request metrics have SAE names.** The coreth names configured by `newStateSchemeConfig`
  (`avalanche_evm_eth_sync_state_trie_leaves_requested`, `avalanche_evm_eth_leafs_request_count`,
  ...) are substituted at startup for the SAE C-Chain's, registered by `vms/saevm/statesync` under
  its `statesync` namespace and prefixed with `evm_transition` by `vms/transitionvm`.

The derived flag `stateSyncSupported`, logged at startup next to `saeCChain`, captures whether the
C-Chain can sync the requested scheme; it is false for SAE with `--state-scheme=firewood`, in which
case the bootstrap node is not configured to sync and the sync is not asserted.

When `stateSyncSupported` is false the harness validates **bootstrap and post-bootstrap state only**,
and says so in a startup warning. Specifically it drops:
- the sync lifecycle and request-metric assertions
- the summary-boundary assertion in the post-restart phase, which still runs to prove the restarted
  topology builds blocks
- `state-sync-enabled` on the bootstrap node, so it bootstraps from genesis

What it still proves in that case: a fresh node bootstraps to health against the serving topology,
and the post-bootstrap RPC checks match the generated workload.

Independently of the above, an SAE run always drops the `state-sync-min-blocks` and
`state-sync-commit-interval` chain config keys, which the SAE C-Chain does not accept: it always
offers to sync what it is given, and takes its summary heights from `commit-interval`.

A negative value leaves Helicon unscheduled and keeps the chain on coreth:

```bash
task tests:merkle-sync:e2e -- --activate-latest-after=-1s
```

On that path coreth registers no sync lifecycle metrics, so the harness asserts the requesting- and
serving-side request metrics and the bootstrapped state only; the lifecycle assertions and the sync
duration are SAE-specific.

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
- this harness relies on the specific metric names described above
- the lifecycle assertions are a structured proof of which summary was synced and how the sync
  ended; the request-metric assertions remain heuristic evidence that code sync and block backfill
  were exercised
- if tmpnet or the C-Chain sync metrics change, the evidence checks in this harness may need to be
  updated even when the underlying bootstrap behavior is still correct

### Mid-chain transition mode

A positive `--activate-latest-after` starts the chain on coreth and schedules
Helicon after network start, so the run exercises the coreth-to-SAE
transition itself. The harness then validates **two** bootstrap scenarios
against the transitioned network:

1. a fresh node, which must transition eagerly during `Initialize` (its
   genesis predates the transition time) and state sync via the SAE C-Chain;
2. a node seeded with pre-transition shared state and no transition marker —
   captured from the generation node during the coreth era — started with
   `state-sync-enabled: true`, which must also transition eagerly and sync a
   summary above its pre-transition head instead of resuming execution. This
   node is pinned to state sync **exclusively from the first scenario's node**,
   which stays up to serve it, so the final scenario additionally proves a node
   that initialized via state sync can serve a full state sync.

The pinning needs two layers, because summaries and sync data travel over
different planes:

- the `state-sync-ids`/`state-sync-ips` **node flags** replace the snowman
  syncer's summary beacons (`snow/engine/snowman/syncer/config.go`), so the
  summary frontier and its acceptance vote come from the state-synced node
  alone; a non-validator works because the node manually tracks the given IP
- the `state-sync-ids` **C-Chain config key** restricts the data plane: the SAE
  C-Chain limits its sync `PeerTracker` to the listed peers
  (`vms/saevm/cchain/config.go`), and coreth pins its sync client the same way,
  so the leafs, code, and block requests hit the state-synced node alone

The validators stay up on their executed state throughout: they no longer serve
any part of the second sync, but the post-sync snowman bootstrapping and
consensus still need them. Sync chain: the generation node executes → the fresh
node state syncs from the validators → the partial node state syncs from the
fresh node only.

Phase order: start the generation node with Helicon at now+Δ → issue
transfers so pre-transition blocks exist → stop the node, copy `db/` +
`chainData/` aside as the partial seed, restart it → force blocks until the
C-Chain registers the SAE VM's sync lifecycle metrics (coreth registers none), issuing
the forcing transfers over HTTP and not waiting on their receipts, since
transitionvm's API drain cannot protect a long-lived WebSocket connection
across the transition → run the normal workload, serving restart, and
summary refresh → validate both bootstrap scenarios, the second syncing
solely from the first scenario's still-running node.

Δ must cover node startup plus the coreth-era transfers and seed capture;
the harness fails with "increase --activate-latest-after" if the node
transitions before the seed is captured. `--activate-latest-after=90s` is a
comfortable local value:

```bash
task tests:merkle-sync:e2e -- --activate-latest-after=90s
```

With `--state-scheme=firewood` the SAE C-Chain cannot sync, so the
partial-bootstrap scenario is skipped along with the usual sync assertions.

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
