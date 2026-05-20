# MerkleSync E2E

## Overview

This PR adds a standalone end-to-end MerkleSync validation harness for C-Chain with Firewood and
wires it into CI as its own job.

The harness creates an isolated tmpnet network, generates enough state and history to force a
meaningful state-sync bootstrap, starts a fresh bootstrap node, and validates both that the node
bootstraps successfully and that the bootstrap actually exercised the Firewood/MerkleSync path.

This is intentionally **not** part of the shared `tests/e2e` suite. It follows the standalone
pattern used by other executables such as `tests/load/main`, so reviewers should evaluate it as
new dedicated coverage rather than an expansion of the shared e2e matrix.

## Why now

Bootstrap success by itself is too weak a signal for MerkleSync coverage.
A useful e2e test here needs to distinguish ordinary bootstrap from real Firewood-backed state
sync, and also show that code sync and recent block backfill were exercised. This PR establishes
that baseline before follow-up work optimizes it via archive/import based fixture reuse.

## What reviewers should know

The key review invariant is that this PR is trying to prove **meaningful MerkleSync bootstrap**, not
just “a new node became healthy.”

Review the diff with these constraints in mind:

1. **Firewood must be enabled**
   - The bootstrap being validated here is specifically the Firewood/MerkleSync path.

2. **State sync must be chosen, not skipped**
   - The harness lowers `state-sync-min-blocks` for tmpnet-scale testing and checks bootstrap logs
     to ensure state sync was started rather than skipped.

3. **The workload must exercise more than plain account transfers**
   - Contract deployment and storage writes/modifications are included so code sync and a
     nontrivial trie/state shape are exercised.

4. **Recent block backfill must be exercised**
   - The harness drives height above 256 and checks explicit block-sync evidence.

5. **The bootstrap node is ephemeral and disposable**
   - Validation evidence must come from that bootstrap node during the run, but its state is not
     part of the persistent serving topology.

6. **This PR establishes the slow but trustworthy baseline**
   - Follow-up optimization via archive/import is intentionally separate work.

## Scope

This PR:
- adds a standalone executable at `tests/msync/main/main.go`
- configures C-Chain to use Firewood for the serving and bootstrap nodes
- generates mixed C-Chain workload including:
  - contract deployment
  - trie writes
  - storage writes and modifications
  - plain transfers
  - a C→X export to make the atomic trie non-empty
- uses a two-phase tmpnet lifecycle with a post-restart summary refresh before bootstrapping a
  fresh ephemeral node
- validates post-bootstrap correctness over RPC
- validates MerkleSync-specific evidence via metrics and bootstrap-node `logs/C.log`
- adds a standalone task entrypoint in `tests/msync/Taskfile.yml`, surfaced via the existing Taskfile include chain
- wires the standalone job into `.github/workflows/bazel-ci.yml`

This PR does not:
- add archive/export-import support to tmpnet
- optimize the test by restoring pre-generated state
- make fixture generation optional yet
- move this coverage into the shared `tests/e2e` suite

## Acceptance criteria

Review this PR as correct only if the harness demonstrates all of the following:

1. The bootstrap node runs C-Chain with Firewood enabled.
2. The bootstrap node selects state sync rather than skipping it.
3. The serving network advances post-restart and the bootstrap logs contain separate accepted-summary evidence plus at least one syncer log line at the refreshed summary height. This is an inferred summary-boundary check, not a direct proof that the accepted summary was logged with that height.
4. Post-bootstrap RPC checks confirm expected contract code, storage values, and transfer balance.
5. Metrics/log evidence shows Firewood proof requests, code sync activity, and heuristic block-sync/backfill activity.
   Reviewers should verify the source split is sensible: bootstrap-node metrics/logs prove state-sync
   selection and bootstrap-path behavior, while validator metrics prove code/block serving activity.
6. The harness remains a standalone executable/job rather than coupling this coverage into the
   shared `tests/e2e` suite.

## Approach

Reviewers should keep these implementation invariants in mind rather than treating the harness as a
simple "node became healthy" test:

- The workload must leave behind contract code, nontrivial storage state, plain transfer effects,
  and enough history to require recent block backfill.
- The serving topology is intentional: one generation node seeds shared `db/`/`chainData/`, then
  a two-node serving network is restarted before a fresh ephemeral bootstrap node is added.
- The single-node generation phase temporarily disables sybil protection so tmpnet can generate the
  required state and history before bootstrap is validated against the final serving topology.
- The copied-state boundary is intentionally limited to shared execution/state data (`db/`,
  `chainData/`). Reviewers should verify this preserves the generated C-Chain state while avoiding
  accidental reuse of node-specific identity, staking, or runtime metadata.
- After restart, the harness forces additional blocks so the bootstrap logs include at least one
  syncer log line from a refreshed post-restart summary boundary rather than only pre-restart
  activity. This is heuristic observability evidence rather than a fully structured proof of which
  summary the bootstrap node accepted.
- The C→X export is included as a realism/coverage improvement so the atomic trie is non-empty;
  this PR does not yet make atomic-trie-specific post-bootstrap assertions.
- Validation is intentionally stronger than health checks:
  - RPC checks confirm contract code, storage values, and balances after bootstrap
  - metrics confirm Firewood proof requests and validator-served code/block requests
  - bootstrap-node `logs/C.log` is checked for state-sync selection, syncer activity, sync
    completion, refreshed summary-height evidence elsewhere in the bootstrap flow, and absence of
    the “skipping state sync” path
  - some assertions rely on specific metric/log identifiers; reviewers should judge whether those
    identifiers are stable enough for CI across expected implementation churn rather than merely
    present in one run

## Validation

This PR was validated with:
- `./scripts/run_task.sh tests:merkle-sync:e2e -- --min-bootstrap-height=300 --target-bytes=10485760`
- `./scripts/run_task.sh lint-action`
- `./scripts/nix_run.sh go test ./tests/msync/main`

From one successful representative run:
- workload generation reached height 303
- post-restart summary refresh produced height 304
- observed growth in the generation node's measured shared-state paths was about 10 MB
- wall-clock runtime was about 10m32s

Reviewers should evaluate validation in two layers:
1. whether the harness really forces the intended bootstrap mode
2. whether the resulting assertions are strong enough to catch false positives

Key false positives to guard against:
- successful bootstrap via ordinary bootstrap rather than state sync
- state sync being selected without Firewood/MerkleSync activity
- post-bootstrap state checks passing even though code sync or block-sync activity never ran
- bootstrap logs lacking refreshed post-restart summary-boundary evidence alongside accepted-summary evidence

## Review focus

Focus review on:

1. **Bootstrap semantics in `tests/msync/main/main.go`**
   - Does the harness genuinely force Firewood-backed state sync?
   - Are the workload composition and thresholds strong enough?
   - Are the evidence checks tied to stable semantics rather than overly incidental log phrasing?

2. **Workload-to-evidence mapping**
   - deployed contracts → bytecode must be retrievable after bootstrap
   - storage writes/modifications → expected storage values must survive bootstrap
   - transfers → recipient balance must match the generated workload
   - height >256 → block-sync evidence must show block-request activity consistent with recent block backfill

3. **Two-phase tmpnet lifecycle correctness**
   - Is the stop/copy/restart flow sound?
   - Is copying only `db/` and `chainData/` the right shared-state boundary?
   - Does the fresh-summary step clearly produce refreshed post-restart summary-boundary evidence in
     the bootstrap logs, consistent with the current heuristic check?

4. **CI job shape in `.github/workflows/bazel-ci.yml`**
   - Is the job appropriately isolated from shared e2e?
   - Does it use the expected monitored tmpnet conventions (`run-monitored-tmpnet-cmd`, dedicated
     `artifact_prefix`, and `filter_by_owner`)?
   - Does it keep this slower harness separate from the shared `tests/e2e` suite?

5. **Task and ownership boundaries**
   - Is this correctly implemented as a standalone job rather than folded into `tests/e2e`?
   - Is the current PR keeping optimization/archive work out of scope?

## Follow-ups

Planned follow-ups are intentionally separate from this PR:
- add tmpnet archive export/import for non-ephemeral networks
- add archive-backed mode to the msync harness using that tmpnet capability
- decide whether any MerkleSync iteration evidence should be strengthened further (for example,
  requiring a multi-round proof metric threshold rather than only `> 0`)

## References

Useful files for review:
- `tests/msync/main/main.go`
- `tests/msync/README.md`
- `tests/msync/Taskfile.yml`
- `tests/Taskfile.yml`
- `Taskfile.yml`
- `tests/msync/main/BUILD.bazel` (standard standalone executable Bazel wiring; low review priority)
- `.github/workflows/bazel-ci.yml`
- `plans/msync-e2e.md`

Long-lived documentation note:
- This review brief is meant to supplement the code and the durable test documentation in
  `tests/msync/README.md`, not replace them.
- Stable maintenance/usage guidance for the harness should live with the code in
  `tests/msync/README.md`.
- The detailed implementation narrative and future work partitioning belong in
  `plans/msync-e2e.md`, not in this review brief.
- If tmpnet archive/import becomes a stable workflow, its durable user/developer guidance should
  live with tmpnet/tmpnetctl documentation rather than in a PR review brief.
