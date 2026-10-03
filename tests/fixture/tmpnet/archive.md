# tmpnet network archives

Tmpnet network archives save a stopped local process-backed network so a later
run can restore its chain state without rebuilding it. The archive API is
implemented in [`archive.go`](archive.go). For command and Go API examples, see
[archive import/export in the tmpnet README](README.md#archive-importexport).

## Table of contents

- [Purpose](#purpose)
- [Using an archive](#using-an-archive)
- [Archive model](#archive-model)
- [Constraints and invariants](#constraints-and-invariants)
- [Maintaining the archive format](#maintaining-the-archive-format)
- [Validation](#validation)
- [Extension boundaries](#extension-boundaries)

## Purpose

Archive support is tmpnet infrastructure, not an application-specific file-copy
mechanism. It allows archive-backed test flows, such as pre-generated validator
networks, to restore a normal tmpnet `Network` with its usual lifecycle and
configuration behavior.

Import re-materializes a fresh network instead of using paths or runtime state
from the source network. This keeps an archive portable between local execution
environments while preserving the state needed to serve the archived chains.

## Using an archive

Export requires a stopped network. Import requires a local process runtime
configuration, including an AvalancheGo binary compatible with the archive.
Import creates the network on disk but does not start it. Start the returned
network with `Network.Bootstrap`.

`tmpnetctl export-network` and `tmpnetctl import-network` expose the same
operations. The [tmpnet README](README.md#archive-importexport) describes their
flags and the Go API.

## Archive model

An archive is a `tar.gz` staging directory with:

- the tmpnet network configuration, genesis, and subnet configuration;
- one tmpnet node configuration directory for each persistent node; and
- a shared `state/` directory containing one persistent node's `db/` and
  `chainData/` directories.

Per-node configuration preserves each persistent node's identity and flags.
The archive deliberately saves shared database and chain data once. Import
creates each node's new tmpnet-managed data directory, restores its own
configuration, and copies that shared state into every persistent node.

The imported network receives a new tmpnet UUID and network directory. Its
persistent nodes retain their node IDs. Explicit archived `--data-dir` values
are removed so imported nodes do not reuse source paths. Runtime configuration
is not archived; import binds the network to the supplied local process runtime.

## Constraints and invariants

- Only non-ephemeral nodes are archived. Export fails when there are no
  persistent nodes. Ephemeral nodes are test machinery and do not become part
  of a reusable fixture.
- Export supports only process-backed persistent nodes. Kube-backed nodes do
  not have a local filesystem layout that this format can restore. Import also
  requires a process runtime.
- Export fails if any node is running. A live directory copy is not a safe
  snapshot of mutable chain state.
- The archive excludes process metadata, logs, and metrics. They describe the
  source execution, not the restored network.
- The implementation treats `db/` and `chainData/` as shared state. If a new
  persistent node artifact is identity-bound, do not add it to `state/`; retain
  it in that node's configuration or define a separate per-node archive entry.

## Maintaining the archive format

`archive.json` records the archive format version and database version. Export
requires every persistent node's AvalancheGo binary to report the same database
version through `--version-json`. Import rejects a missing or unsupported format
version and rejects a database-version mismatch with the supplied binary.

The format version is a strict compatibility boundary. Increment it whenever an
archive layout or interpretation change is not backward compatible, and update
import behavior and tests in the same change. Do not silently accept an older
layout: an incompatible archive can otherwise appear to import successfully and
fail only when nodes start.

The single shared-state copy is an intentional size and restore-time trade-off.
Changing it to preserve a full state directory per node, or changing which
artifacts are shared, requires a multi-node restart test. The safety property is
that every imported persistent node starts and serves the archived chain state,
not that the archive is structurally well formed.

## Validation

Unit tests in [`network_test.go`](network_test.go) cover archive boundaries,
format and database-version validation, data-directory rematerialization, and
subnet round trips.

Run the archive e2e test with:

```bash
task test-e2e-tmpnet
```

The e2e test starts two persistent nodes, creates C-Chain state, adds an
excluded ephemeral node, exports after stopping the network, imports it, and
boots both restored persistent nodes. It verifies the fresh network identity,
preserved node identities, and availability of the archived chain state from
each restored node. The reusable Bazel CI workflow runs this task as a required
`tmpnet-e2e` job.

## Extension boundaries

Archive support intentionally does not provide remote fixture publication,
shared fixture storage, or a CLI command that starts an imported network. The
next consumers should use the tmpnet API rather than reimplement archive
extraction or source-directory rewriting.
