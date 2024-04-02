# Firewood: Compaction-Less Database Optimized for Efficiently Storing Recent Merkleized Blockchain State

![Github Actions](https://github.com/ava-labs/firewood/actions/workflows/ci.yaml/badge.svg?branch=main)
[![Ecosystem license](https://img.shields.io/badge/License-Ecosystem-blue.svg)](./LICENSE.md)

> :warning: Firewood is alpha-level software and is not ready for production
> use. The Firewood API and on-disk state representation may change with
> little to no warning.

Firewood is an embedded key-value store, optimized to store recent Merkleized blockchain
state with minimal overhead. Firewood is implemented from the ground up to directly
store trie nodes on-disk. Unlike most state management approaches in the field,
it is not built on top of a generic KV store such as LevelDB/RocksDB. Firewood, like a
B+-tree based database, directly uses the trie structure as the index on-disk. Thus,
there is no additional “emulation” of the logical trie to flatten out the data structure
to feed into the underlying database that is unaware of the data being stored. The convenient
byproduct of this approach is that iteration is still fast (for serving state sync queries)
but compaction is not required to maintain the index. Firewood was first conceived to provide
a very fast storage layer for the EVM but could be used on any blockchain that
requires an authenticated state.

Firewood only attempts to store the latest state on-disk and will actively clean up
unused data when state diffs are committed. To avoid reference counting trie nodes,
Firewood does not copy-on-write (COW) the state trie and instead keeps
the latest version of the trie index on disk and applies in-place updates to it.
Firewood keeps some configurable number of previous states in memory to power
state sync (which may occur at a few roots behind the current state).

Firewood provides OS-level crash recovery via a write-ahead log (WAL). The WAL
guarantees atomicity and durability in the database, but also offers
“reversibility”: some portion of the old WAL can be optionally kept around to
allow a fast in-memory rollback to recover some past versions of the entire
store back in memory. While running the store, new changes will also contribute
to the configured window of changes (at batch granularity) to access any past
versions with no additional cost at all.

## Architecture Diagram

![architecture diagram](./docs/assets/architecture.svg)

## Terminology

- `Revision` - A historical point-in-time state/version of the trie. This
  represents the entire trie, including all `Key`/`Value`s at that point
  in time, and all `Node`s.
- `View` - This is the interface to read from a `Revision` or a `Proposal`.
- `Node` - A node is a portion of a trie. A trie consists of nodes that are linked
  together. Nodes can point to other nodes and/or contain `Key`/`Value` pairs.
- `Hash` - In this context, this refers to the merkle hash for a specific node.
- `Root Hash` - The hash of the root node for a specific revision.
- `Key` - Represents an individual byte array used to index into a trie. A `Key`
  usually has a specific `Value`.
- `Value` - Represents a byte array for the value of a specific `Key`. Values can
  contain 0-N bytes. In particular, a zero-length `Value` is valid.
- `Key Proof` - A proof that a `Key` exists within a specific revision of a trie.
  This includes the hash for the node containing the `Key` as well as all parents.
- `Range Proof` - A proof that consists of two `Key Proof`s, one for the start of
  the range, and one for the end of the range, as well as a list of all `Key`/`Value`
  pairs in between the two. A `Range Proof` can be validated independently of an
  actual database by constructing a trie from the `Key`/`Value`s provided.
- `Change Proof` - A proof that consists of a set of all changes between two
  revisions.
- `Put` - An operation for a `Key`/`Value` pair. A put means "create if it doesn't
  exist, or update it if it does. A put operation is how you add a `Value` for a
  specific `Key`.
- `Delete` - An operation indicating that a `Key` should be removed from the trie.
- `Batch Operation` - An operation of either `Put` or `Delete`.
- `Batch` - An ordered set of `Batch Operation`s.
- `Proposal` - A proposal consists of a base `Root Hash` and a `Batch`, but is not
  yet committed to the trie. In Firewood's most recent API, a `Proposal` is required
  to `Commit`.
- `Commit` - The operation of applying one or more `Proposal`s to the most recent
  `Revision`.

## Roadmap

**LEGEND**

- [ ] Not started
- [ ] :runner: In progress
- [x] Complete

### Green Milestone

This milestone will focus on additional code cleanup, including supporting
concurrent access to a specific revision, as well as cleaning up the basic
reader and writer interfaces to have consistent read/write semantics.

- [x] Concurrent readers of pinned revisions while allowing additional batches
      to commit, to support parallel reads for the past consistent states. The revisions
      are uniquely identified by root hashes.
- [x] Pin a reader to a specific revision, so that future commits or other
      operations do not see any changes.
- [x] Be able to read-your-write in a batch that is not committed. Uncommitted
      changes will not be shown to any other concurrent readers.
- [x] Add some metrics framework to support timings and volume for future milestones
      To support this, a new method Db::metrics() returns an object that can be serialized
      into prometheus metrics or json (it implements [serde::Serialize])

### Seasoned milestone

This milestone will add support for proposals, including proposed future
branches, with a cache to make committing these branches efficient.

- [x] Be able to support multiple proposed revisions against the latest committed
      version.
- [x] Be able to propose a batch against the existing committed revision, or
      propose a batch against any existing proposed revision.
- [x] Committing a batch that has been proposed will invalidate all other proposals
      that are not children of the committed proposed batch.
- [x] Be able to quickly commit a batch that has been proposed.
- [x] Remove RLP encoding

### Dried milestone

The focus of this milestone will be to support synchronization to other
instances to replicate the state. A synchronization library should also
be developed for this milestone.

- [x] Migrate to a fully async interface
- [x] Pluggable encoding for nodes, for optional compatibility with MerkleDB
- [ ] :runner: MerkleDB root hash in parity for a seamless transition between MerkleDB
      and Firewood.
- [ ] :runner: Support replicating the full state with corresponding range proofs that
      verify the correctness of the data.
- [ ] Pluggable IO subsystem (tokio\_uring, monoio, etc)
- [ ] Add metric reporting
- [ ] Enforce limits on the size of the range proof as well as keys to make
      synchronization easier for clients.
- [ ] Add support for Ava Labs generic test tool via grpc client
- [ ] Support replicating the delta state from the last sync point with
      corresponding change proofs that verify the correctness of the data.
- [ ] Refactor `Shale` to be more idiomatic, consider rearchitecting it

## Build

Firewood currently is Linux-only, as it has a dependency on the asynchronous
I/O provided by the Linux kernel (see `libaio`).

## Run

There are several examples, in the examples directory, that simulate real world
use-cases. Try running them via the command-line, via `cargo run --release
--example simple`.

## Logging

If you want logging, enable the `logging` feature flag, and then set RUST\_LOG accordingly.
See the documentation for [env\_logger](https://docs.rs/env_logger/latest/env_logger/) for specifics.
We currently have very few logging statements, but this is useful for print-style debugging.

## Release

See the [release documentation](./RELEASE.md) for detailed information on how to release Firewood.

## CLI

Firewood comes with a CLI tool called `fwdctl` that enables one to create and interact with a local instance of a Firewood database. For more information, see the [fwdctl README](fwdctl/README.md).

## Test

```
cargo test --release
```

## License

Firewood is licensed by the Ecosystem License. For more information, see the
[LICENSE file](./LICENSE.md).
