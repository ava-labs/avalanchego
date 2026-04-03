# Merkle Sync end-to-end validation plan

## Goal

Create a standalone test job that:
1. starts an isolated tmpnet network,
2. generates enough chain/state data to meaningfully exercise merkle sync bootstrap,
3. starts a fresh node against that populated network, and
4. validates that the new node can bootstrap successfully.

This should **not** be added to the shared `tests/e2e` suite. It should instead follow the pattern of other standalone test executables such as `tests/load/main`, with its own task and CI job.

## Constraints and existing building blocks

- `tests/fixture/tmpnet` already provides the temporary network lifecycle we need.
- `tests/fixture/e2e` already contains reusable startup/bootstrap logic, especially:
  - private network startup patterns
  - `CheckBootstrapIsPossible(...)` for adding a fresh node and validating bootstrap
- `tests/load` already shows how to generate C-Chain traffic at sustained volume.
- Initial work should focus on **generate data from scratch**, even if slow.
- A later phase can optimize by restoring from a published archive containing:
  - node database/state
  - network configuration
  - genesis and any other required fixture inputs

## Phase 1: generate-from-scratch standalone test

### Desired outcome
A standalone package/executable that:
- provisions a tmpnet network,
- waits for it to become healthy,
- generates a target amount of C-Chain data,
- verifies a new node can bootstrap from that state.

### Implementation outline
1. **Create a new standalone test package**
   - Add a new package for merkle sync validation, separate from `tests/e2e`.
   - Add a `main` entrypoint, similar in style to `tests/load/main`.

2. **Start an isolated tmpnet network**
   - Reuse the tmpnet/e2e startup path rather than inventing a new network bootstrap flow.
   - Keep the network isolated from other tests and responsible for its own lifecycle.

3. **Generate load**
   - Reuse the existing load-test approach where practical.
   - Prefer simple, high-volume C-Chain traffic rather than introducing new custom transaction patterns unless necessary.
   - Keep the generation logic parameterized so the target can be adjusted easily.

4. **Define the initial threshold**
   - Do not hardcode a guessed transaction count as the definition of "10 MB worth of blocks".
   - Instead, make the initial implementation stop based on an observable target, such as:
     - on-disk data size growth for the node/network, or
     - a configurable transaction/block target used only as an approximation while measuring the resulting data footprint.
   - Emit enough logging/metrics to understand how much data was produced and how long it took.

5. **Validate bootstrap**
   - Reuse the existing bootstrap helper to add a new node and verify it becomes healthy.
   - Confirm the existing validator nodes remain healthy afterward.

6. **Add task wiring**
   - Add a dedicated task to build/run this standalone test.
   - Ensure it can be invoked locally with the same tmpnet-style flags used by existing standalone tests.

7. **Add CI wiring**
   - Add a dedicated CI job that invokes the new task.
   - Keep this job isolated from normal e2e execution due to variable runtime.

## Phase 2: fixture/archive-based bootstrap validation

### Desired outcome
Reduce runtime by separating "state generation" from "bootstrap validation".

### Implementation outline
1. Define the archive contents needed to recreate the populated network state.
2. Add tooling or documentation for producing that archive from Phase 1.
3. Add a mode that restores the archived state instead of generating it from scratch.
4. Reuse the same bootstrap-validation step against restored state.
5. Keep the generate-from-scratch mode available for refreshing the fixture.

## Research findings: what actually exercises MerkleSync

### Critical distinction: Firewood is required

A standalone bootstrap test only exercises the MerkleDB/Firewood sync path when C-Chain runs with:
- `state-scheme = firewood`

Otherwise bootstrap exercises the older EVM state sync implementation rather than the Merkle sync path under:
- `database/merkle/sync/*`
- `database/merkle/firewood/syncer/*`
- `graft/evm/sync/evmstate/firewood_syncer.go`

Key runtime-selection reference:
- `graft/evm/sync/engine/client.go`

### Bootstrap success alone is a false-positive signal

Even with Firewood enabled, a node may still skip state sync and succeed via ordinary bootstrap.

In `graft/evm/sync/engine/client.go`, state sync is skipped when the last accepted height is too close to the summary height relative to `state-sync-min-blocks`.

Implication:
A meaningful MerkleSync e2e must require evidence that:
1. Firewood was enabled,
2. state sync was selected rather than skipped, and
3. the Firewood/Merkle syncer actually ran.

### Which sync behaviors matter for a fresh-node bootstrap?

For a fresh bootstrap node, the important path is primarily:
- state summary selection,
- range-proof-based Merkle sync,
- code extraction/fetch from proofs,
- recent block backfill,
- accept synced block and continue bootstrap.

For this scenario, change-proof coverage is **not** the primary target.
The Firewood adapter reports insufficient history for change proofs and the generic server falls back to range proofs.

### Block backfill matters too

The sync registry includes a block syncer, and recent block backfill uses a window of 256 blocks.

Implication:
A meaningful test should drive the target height above 256 so the backfill path is exercised, not just trie sync.

### Code sync must be exercised

The Firewood EVM state syncer extracts code hashes from committed range proofs and enqueues them into the code syncer.

Implication:
A workload containing only EOAs / plain transfers is insufficient.
The final synced state must include contract accounts with bytecode.

## Minimal workload composition for a meaningful MerkleSync test

### Required configuration preconditions
1. **Enable Firewood**
   - Use `state-scheme = firewood`
2. **Force state sync to actually run**
   - Lower `state-sync-min-blocks` enough for a tmpnet-scale test
3. **Make summaries available quickly**
   - Use a practical local `state-sync-commit-interval`

### Required state/history composition
1. **Deploy at least 2 distinct contracts**
   Example candidates:
   - `tests/load/contracts/TrieStressTest.sol`
   - `tests/load/contracts/LoadSimulator.sol`

   Purpose:
   - exercise contract-code sync
   - ensure more than one contract/account leaf exists
   - avoid a single-contract-only trie shape

2. **Create many new storage slots**
   Example candidates:
   - repeated `TrieStressTest.writeValues(...)`
   - repeated `LoadSimulator.write(...)`

   Purpose:
   - make trie sync nontrivial
   - force multiple range-proof rounds rather than a tiny one-shot sync

3. **Modify existing storage slots**
   Example candidate:
   - `LoadSimulator.modify(...)`

   Purpose:
   - avoid a purely append-only final trie shape
   - ensure final state is not degenerate

4. **Include some plain EOA transfers**
   Purpose:
   - ensure account trie is not purely contract-centric

5. **Pad block height above both thresholds**
   - above the lowered `state-sync-min-blocks`
   - above 256 blocks

   Purpose:
   - guarantee real state sync is chosen
   - guarantee recent-block backfill is exercised

## Better success criteria than "10 MB and bootstrap succeeded"

`10 MB` is only a weak proxy. It mixes block storage, trie state, code storage, indices, snapshots, and other artifacts. Different workloads can produce similar disk growth while exercising very different sync behavior.

A stronger MerkleSync test should require evidence of most or all of the following:

1. **Firewood path selected**
   - Firewood config enabled
   - logs/metrics indicate the Firewood sync path ran

2. **State sync not skipped**
   - no "too close, skipping state sync" path
   - positive evidence state sync started

3. **Merkle/range-proof sync actually iterated**
   - more than one proof request / commit round

4. **Code sync exercised**
   - at least one code hash fetched
   - preferably more than one distinct bytecode present

5. **Block backfill exercised**
   - target height > 256

6. **Post-bootstrap state correctness checks**
   Validate via RPC, not only node health:
   - deployed contracts exist
   - selected storage values match expectations
   - balances / token balances match expectations

## Current implementation status

A standalone harness now exists at:
- `tests/msync/main/main.go`

Current state:
- isolated tmpnet startup works
- bootstrap helper reuse works
- write-heavy state generation works
- on-disk growth measurement (`db/` + `chainData/`) works

Current limitation:
- the harness currently uses disk growth as the stopping condition and does **not yet** guarantee that the Firewood Merkle sync path, code sync, and block backfill paths are all exercised.

## Open questions to resolve during implementation

1. What exact local chain config values should be used for:
   - `state-scheme = firewood`
   - `state-sync-min-blocks`
   - `state-sync-commit-interval`

2. What is the best observable evidence in logs/metrics that confirms:
   - state sync started,
   - Firewood syncer ran,
   - range-proof sync performed multiple rounds,
   - code sync executed,
   - 256-block backfill executed?

3. How much of the recommended workload composition should be encoded into the first generate-from-scratch version versus deferred to an archived-state fixture?

## Simplest high-value next step

Refactor the standalone harness so that it validates MerkleSync-specific preconditions before worrying about data volume:
1. run C-Chain with Firewood,
2. lower state-sync thresholds so state sync is chosen,
3. ensure summaries are produced quickly,
4. generate a mixed workload containing:
   - at least two contracts,
   - insert-heavy writes,
   - modify-heavy writes,
   - some EOA transfers,
5. drive height beyond 256 blocks,
6. add checks that prove state sync was used and post-bootstrap state is correct.

That will convert the current bootstrap smoke test into a meaningful MerkleSync validation.
