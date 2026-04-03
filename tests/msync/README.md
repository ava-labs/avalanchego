# MerkleSync E2E

## Overview

This package provides a standalone end-to-end validation harness for C-Chain MerkleSync bootstrap
with Firewood.

It is intentionally separate from the shared `tests/e2e` suite. The goal is to provide targeted,
reviewable coverage for MerkleSync-specific bootstrap behavior without coupling this slower,
specialized flow into the general e2e matrix.

Run it locally with:

```bash
task tests:merkle-sync:e2e -- --min-bootstrap-height=300 --target-bytes=10485760
```

CI uses:

```bash
./scripts/run_task.sh tests:merkle-sync:e2e-ci
```

The CI entrypoint differs in that it uses a race-built Bazel avalanchego binary.

## What this validates

The harness is meant to prove more than "a new node became healthy."

A successful run should demonstrate all of the following:
- C-Chain was configured with `state-scheme = firewood`
- the bootstrap node selected state sync rather than skipping it
- the Firewood/MerkleSync path actually ran
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
- bootstrap-node metrics provide Firewood proof-request activity
- validator metrics provide serving-side evidence, including code sync requests and block-request activity

### Logs
MerkleSync-specific evidence is taken from the bootstrap node's:
- `logs/C.log`

Important note:
- use `logs/C.log`, not `logs/main.log`, for bootstrap-path evidence

The harness looks for evidence that:
- Firewood was enabled
- state sync started
- a state summary was accepted
- the bootstrap logs include accepted-summary evidence and at least one syncer log line at the refreshed summary height
- this refreshed-summary check is inferred from bootstrap observability rather than directly proving which summary was accepted
- the accepted-summary evidence and refreshed-height syncer evidence are checked separately, not as a single structured "accepted summary at exact refreshed height" proof
- Firewood, Code, and Block syncers ran
- syncers completed successfully
- heuristic block-sync/backfill activity occurred, inferred from validator-served block requests plus bootstrap syncer logs
- the "skipping state sync" path was not taken

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

These exist to help local iteration and future fixture generation.

Important tmpnet-scale tuning note:
- `--state-sync-min-blocks` is intentionally lowered so the bootstrap node chooses state sync rather
  than skipping it on a small tmpnet run
- `--state-sync-commit-interval` is part of making refreshed summary-boundary evidence observable
  and repeatable during the restarted serving phase

Maintenance note:
- this harness relies on specific metric names and bootstrap-log identifiers in `logs/C.log`
- some MerkleSync assertions are intentionally heuristic and observability-dependent rather than a
  fully structured proof of bootstrap internals
- if tmpnet or C-Chain sync observability changes, the evidence checks in this harness may need to
  be updated even when the underlying bootstrap behavior is still correct

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
