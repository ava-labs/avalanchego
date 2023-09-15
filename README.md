# Firewood: non-archival blockchain key-value store with hyper-fast recent state retrieval.

![Github Actions](https://github.com/ava-labs/firewood/actions/workflows/ci.yaml/badge.svg)
[![Ecosystem license](https://img.shields.io/badge/License-Ecosystem-blue.svg)](./LICENSE.md)

> :warning: firewood is alpha-level software and is not ready for production
> use. Do not use firewood to store production data. See the
> [license](./LICENSE.md) for more information regarding firewood usage.

Firewood is an embedded key-value store, optimized to store blockchain state.
It prioritizes access to latest state, by providing extremely fast reads, but
also provides a limited view into past state. It does not copy-on-write the
state trie to generate an ever growing forest of tries like other databases,
but instead keeps one latest version of the trie index on disk and apply
in-place updates to it. This ensures that the database size is small and stable
during the course of running firewood. Firewood was first conceived to provide
a very fast storage layer for the EVM but could be used on any blockchain that
requires authenticated state.

Firewood is a robust database implemented from the ground up to directly store
trie nodes and user data. Unlike most (if not all) of the solutions in the field,
it is not built on top of a generic KV store such as LevelDB/RocksDB. Like a
B+-tree based store, firewood directly uses the tree structure as the index on
disk. Thus, there is no additional “emulation” of the logical trie to flatten
out the data structure to feed into the underlying DB that is unaware of the
data being stored. It provides generic trie storage for arbitrary keys and
values.

Firewood provides OS-level crash recovery via a write-ahead log (WAL). The WAL
guarantees atomicity and durability in the database, but also offers
“reversibility”: some portion of the old WAL can be optionally kept around to
allow a fast in-memory rollback to recover some past versions of the entire
store back in memory. While running the store, new changes will also contribute
to the configured window of changes (at batch granularity) to access any past
versions with no additional cost at all.

## License
firewood is licensed by the Ecosystem License. For more information, see the
[LICENSE file](./LICENSE.md).

## Vendored Crates
The following crates are vendored in this repository to allow for making
modifications without requiring upstream approval:
* [`growth-ring`](https://github.com/Determinant/growth-ring)
* [`libaio-futures`](https://github.com/Determinant/libaio-futures)
* [`shale`](https://github.com/Determinant/shale)

These crates will either be heavily modified or removed prior to the production
launch of firewood. If they are retained, all changes made will be shared
upstream.

## Termimology

* `Revision` - A historical point-in-time state/version of the trie. This
   represents the entire trie, including all `Key`/`Value`s at that point
   in time, and all `Node`s.
* `View` - This is the interface to read from a `Revision` or a `Proposal`.
* `Node` - A node is a portion of a trie. A trie consists of nodes that are linked
  together. Nodes can point to other nodes and/or contain `Key`/`Value` pairs.
* `Hash` - In this context, this refers to the merkle hash for a specific node.
* `Root Hash` - The hash of the root node for a specific revision.
* `Key` - Represents an individual byte array used to index into a trie. A `Key`
  usually has a specific `Value`.
* `Value` - Represents a byte array for the value of a specific `Key`. Values can
  contain 0-N bytes. In particular, a zero-length `Value` is valid.
* `Key Proof` - A proof that a `Key` exists within a specific revision of a trie.
  This includes the hash for the node containing the `Key` as well as all parents.
* `Range Proof` - A proof that consists of two `Key Proof`s, one for the start of
  the range, and one for the end of the range, as well as a list of all `Key`/`Value`
  pairs in between the two. A `Range Proof` can be validated independently of an
  actual database by constructing a trie from the `Key`/`Value`s provided.
* `Change Proof` - A proof that consists of a set of all changes between two
  revisions.
* `Put` - An operation for a `Key`/`Value` pair. A put means "create if it doesn't
  exist, or update it if it does. A put operation is how you add a `Value` for a
  specific `Key`.
* `Delete` - A operation indicating that a `Key` that should be removed from the trie.
* `Batch Operation` - An operation of either `Put` or `Delete`.
* `Batch` - An ordered set of `Batch Operation`s.
* `Proposal` - A proposal consists of a base `Root Hash` and a `Batch`, but is not
  yet committed to the trie. In firewood's most recent API, a `Proposal` is required
  to `Commit`.
* `Commit` - The operation of applying one or more `Proposal`s to the most recent
  `Revision`.


## Roadmap
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
branches, with a cache to make committing these branches efficiently.
- [x] Be able to support multiple proposed revisions against latest committed
version.
- [x] Be able to propose a batch against the existing committed revision, or
propose a batch against any existing proposed revision.
- [x] Commit a batch that has been proposed will invalidate all other proposals
that are not children of the committed proposed batch.
- [x] Be able to quickly commit a batch that has been proposed.

### Dried milestone
The focus of this milestone will be to support synchronization to other
instances to replicate the state. A synchronization library should also
be developed for this milestone.
- [ ] Support replicating the full state with corresponding range proofs that
verify the correctness of the data.
- [ ] Support replicating the delta state from the last sync point with
corresponding range proofs that verify the correctness of the data.
- [ ] Enforce limits on the size of the range proof as well as keys to make
  synchronization easier for clients.
- [ ] MerkleDB root hash in parity for seamless transition between MerkleDB
and firewood.
- [ ] Add metric reporting
- [ ] Refactor `Shale` to be more idiomatic

## Build
Firewood currently is Linux-only, as it has a dependency on the asynchronous
I/O provided by the Linux kernel (see `libaio`). Unfortunately, Docker is not
able to successfully emulate the syscalls `libaio` relies on, so Linux or a
Linux VM must be used to run firewood. It is encouraged to enhance the project
with I/O supports for other OSes, such as OSX (where `kqueue` needs to be used
for async I/O) and Windows. Please contact us if you're interested in such contribution.

## Run
There are several examples, in the examples directory, that simulate real world
use-cases. Try running them via the command-line, via `cargo run --release
--example simple`.

## Release
See the [release documentation](./RELEASE.md) for detailed information on how to release firewood. 

## CLI
Firewood comes with a CLI tool called `fwdctl` that enables one to create and interact with a local instance of a firewood database. For more information, see the [fwdctl README](fwdctl/README.md).

## Test
```
cargo test --release
```
