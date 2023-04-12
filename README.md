# Firewood: non-archival blockchain key-value store with hyper-fast recent state retrieval.

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
data being stored.

Firewood provides OS-level crash recovery via a write-ahead log (WAL). The WAL
guarantees atomicity and durability in the database, but also offers
“reversibility”: some portion of the old WAL can be optionally kept around to
allow a fast in-memory rollback to recover some past versions of the entire
store back in memory. While running the store, new changes will also contribute
to the configured window of changes (at batch granularity) to access any past
versions with no additional cost at all.

Firewood provides two isolated storage spaces which can be both or selectively
used the user. The account model portion of the storage offers something very similar
to StateDB in geth, which captures the address-“state key” style of two-level access for
an account’s (smart contract’s) state. Therefore, it takes minimal effort to
delegate all state storage from an EVM implementation to firewood. The other
portion of the storage supports generic trie storage for arbitrary keys and
values. When unused, there is no additional cost.

## License
firewood is licensed by the Ecosystem License. For more information, see the
[LICENSE file](./LICENSE.md).

## Roadmap
### Green Milestone
This milestone will focus on additional code cleanup, including supporting
concurrent access to a specific revision, as well as cleaning up the basic
reader and writer interfaces to have consistent read/write semantics.
- [ ] Concurrent readers of pinned revisions while allowing additional batches
to commit, to support parallel reads for the past consistent states. The revisions
are uniquely identified by root hashes.
- [ ] Pin a reader to a specific revision, so that future commits or other
operations do not see any changes.
- [ ] Be able to read-your-write in a batch that is not committed. Uncommitted
changes will not be shown to any other concurrent readers.

### Seasoned milestone
This milestone will add support for proposals, including proposed future
branches, with a cache to make committing these branches efficiently.
- [ ] Be able to propose a batch against the existing committed revision, or
propose a batch against any existing proposed revision.
- [ ] Be able to quickly commit a batch that has been proposed. Note that this
invalidates all other proposals that are not children of the committed proposed batch.

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

To integrate firewood into a custom VM or other project, see the [firewood-connection](./firewood-connection/README.md) for a straightforward way to use firewood via custom message-passing.

## CLI
Firewood comes with a CLI tool called `fwdctl` that enables one to create and interact with a local instance of a firewood database. For more information, see the [fwdctl README](fwdctl/README.md).

## Test
```
cargo test --release
```
