# tmpnet import/export

## Overview
This PR adds generic tmpnet archive export/import support for restartable local tmpnet network
fixtures whose persistent nodes are process-backed.

The new functionality lives in `tests/fixture/tmpnet` and `tests/fixture/tmpnet/tmpnetctl`, not in
the MerkleSync harness. Export produces an archive of a stopped, process-backed tmpnet network with
persistent nodes, and import materializes that archive as a fresh tmpnet network instance with a new
UUID and a new network directory.

This is the tmpnet-side infrastructure needed for later archive-backed test flows, but the
implementation here is intentionally generic and independently testable.

## Why now
The MerkleSync follow-up work wants to restore a pre-generated tmpnet validator network instead of
regenerating state from scratch on every run.

That optimization should not be implemented as msync-specific file copying. tmpnet itself needs a
first-class notion of exporting a restartable fixture and importing it as a fresh network.

## What reviewers should know
A few invariants matter for review:

- **Only non-ephemeral nodes are archived.** Ephemeral nodes are disposable test machinery and must
  not become part of a reusable fixture, and export requires at least one non-ephemeral node.
- **Archived persistent nodes are currently process-backed only.** The implementation copies local
  node filesystem state for non-ephemeral nodes and rejects unsupported persistent runtimes such as
  kube-backed nodes. Ephemeral nodes are excluded rather than validated for archiveability.
- **Runtime configuration is not archived.** Imports must bind the archive to locally supplied
  runtime settings, so archives are portable fixtures rather than self-contained execution bundles.
- **Import creates a fresh tmpnet identity.** The imported network gets a new tmpnet UUID and a
  newly materialized network path.
- **Imported persistent nodes retain their node identities.** Fresh tmpnet identity applies to the
  network UUID/path, not to the archived node staking material.
- **Import does not preserve source data-dir paths.** Archived explicit `--data-dir` values are
  cleared so imported nodes use freshly derived tmpnet-managed paths under the new network
  directory.
- **This is Go-level tmpnet logic.** Import is not implemented as ad hoc text rewriting of files in
  a tarball.
- **The archive is restart-oriented, not a raw copy of every artifact.** Persistent node state
  needed to restart is included; transient runtime artifacts such as process metadata, logs, and
  metrics snapshots are excluded.
- **Validation is e2e.** Correctness depends on a real `avalanchego` binary being able to restart
  from imported state, so the substantive validation lives in a Ginkgo e2e suite, not only in unit
  tests.

## Scope
In scope:
- add tmpnet archive export/import helpers in `tests/fixture/tmpnet/archive.go`
- add `tmpnetctl export-network` and `tmpnetctl import-network`
- add a Taskfile entrypoint (`test-e2e-tmpnet`) for the new Bazel tmpnet archive e2e test
- add a dedicated Bazel CI job that invokes that task
- add Bazel-wired Ginkgo e2e coverage under `tests/fixture/tmpnet/e2e`
- keep the existing tmpnet unit test coverage for serialization round-tripping and archive-boundary
  copy behavior
- add durable tmpnet README usage/invariant documentation for archive import/export

Out of scope:
- msync harness integration
- remote/shared fixture publishing
- archive format versioning or compatibility guarantees beyond this PR’s immediate use

## Approach
### Export
Export reads an existing stopped tmpnet network from disk, filters out ephemeral nodes, stages the
restartable network contents, and writes them to a `tar.gz` archive.

The staged contents include:
- network config
- genesis
- subnets dir, if present
- each non-ephemeral node directory, excluding transient runtime artifacts

The staged node copy intentionally excludes:
- `process.json`
- `logs/`
- `metrics/`

Export also strips runtime configuration from the archived fixture so the archive can be rebound to
local runtime settings at import time.

Export rejects a running network rather than attempting a live snapshot of mutable node state.

### Import
Import untars the archive to a temporary directory, reads the tmpnet network using normal tmpnet
parsing, then re-materializes it through tmpnet’s own network creation path.

During import:
- the network UUID is replaced with a fresh UUID
- the network directory is re-created via tmpnet’s normal network creation logic
- locally supplied runtime config is bound to the imported network
- node `DataDir` values are re-derived for the new network path
- imported nodes have runtime state cleared before materialization
- subnets are re-written into the new network directory
- archived persistent node contents are copied into the newly created node directories

This preserves tmpnet’s normal network/object relationships rather than treating import as a tarball
patching exercise.

### CLI
`tmpnetctl` grows two commands:
- `export-network --network-dir ... --archive-path ...`
- `import-network --archive-path ...` plus local runtime flags/env to bind the imported fixture to
  the current host

These commands use the existing tmpnet logger instead of printing structured status with
`fmt.Fprintf`, except for the pre-existing command-line cases that still intentionally print to
stdout/stderr. `import-network` binds local runtime config and materializes a fresh stopped network
on disk; starting a freshly imported network is currently exposed via the Go API (`Bootstrap`), not
via a dedicated `tmpnetctl` command.

### E2E coverage
The new e2e suite uses the same general pattern as other tmpnet/e2e tests:
- Ginkgo
- Bazel `go_test`
- real `avalanchego` binary
- `tests/fixture/e2e` helpers for lifecycle/context

The test starts a private one-node network, drives real C-Chain activity, adds an ephemeral node,
stops the network, exports it, imports it, and verifies that the imported network restarts and
still serves the expected chain state, including the transferred recipient balance.

That test is meant to prove the important behavioral contract: import/export is not just
structurally valid on disk, it produces a real restartable tmpnet network.

## Validation
Validated with Bazel:

- `bazel test //tests/fixture/tmpnet:tmpnet_test //tests/fixture/tmpnet/e2e:e2e_test \
  --test_output=errors --test_env=AVALANCHEGO_PATH=/home/maru/src/av-tmpnet-import-export/bin/avalanchego`
- `task test-e2e-tmpnet`

What the e2e test specifically verifies:
- export excludes ephemeral nodes
- import produces a new UUID
- import produces a new materialized network dir
- imported network can bootstrap successfully with a real `avalanchego`
- imported node preserves previously produced chain state, including the transferred recipient
  balance, and continues serving RPC requests after restart

What the unit tests additionally verify:
- archive-boundary copying excludes `process.json`, `logs/`, and `metrics/`
- export rejects a running network
- archiveability helper validation rejects unsupported or non-archiveable networks
  (for example, process-backed persistent nodes only and at least one non-ephemeral node required)
- import clears explicit archived data-dir settings so imported nodes use fresh tmpnet-managed
  paths and copied persistent state lands under the newly derived path
- subnet configuration round-trips through export/import

## Review focus
Reviewers should spend the most time on:

1. **Archive boundaries in `tests/fixture/tmpnet/archive.go`**
   - are we including the right persistent artifacts?
   - are we excluding the right transient artifacts?
   - is runtime config correctly stripped from the archive and rebound locally on import?
   - is the import path correctly re-materializing tmpnet objects rather than smuggling old
     path/identity assumptions forward?

2. **Fresh identity behavior**
   - new UUID on import
   - new network path on import
   - no accidental reuse of transient runtime state
   - persistent node identities preserved while runtime binding is refreshed locally

3. **E2E coverage shape in `tests/fixture/tmpnet/e2e/e2e_test.go`**
   - does the test prove restartability in a realistic way?
   - does it cover the primary mixed persistent+ephemeral export case clearly, while leaving
     archiveability edge cases to focused unit tests?

4. **CLI/task/CI integration**
   - command UX in `tests/fixture/tmpnet/tmpnetctl/main.go`
   - new task wiring in `Taskfile.yml`
   - new job wiring in `.github/workflows/bazel-ci.yml`
   - logger usage and consistency with existing tmpnet/tmpnetctl structure

## Follow-ups
Likely follow-up work after this PR:
- use tmpnet import/export from the MerkleSync harness
- consider whether tmpnet should gain a dedicated CLI command to start a freshly imported network
- decide whether the Go-level tmpnet archive API needs more durable usage examples beyond the
  current README notes
- consider whether this feature eventually needs stronger archive format/versioning conventions if
  it becomes a broader workflow

## References
- `tests/fixture/e2e/*`
- `tests/e2e/e2e_test.go`
- `plans/msync-e2e.md` (historical background; not authoritative for the final archive shape)
