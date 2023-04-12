# Firewood: non-archival blockchain key-value store with hyper-fast recent state retrieval.

Firewood is an embedded key-value store, optimized to store blockchain state.
It prioritizes access to latest state, by providing extremely fast reads, but
also provides a limited view into past state. It does not copy-on-write the
Merkle Patricia Trie (MPT) to generate an ever growing forest of tries like EVM,
but instead keeps one latest version of the MPT index on disk and apply
in-place updates to it. This ensures that the database size is small and stable
during the course of running firewood. Firewood was first conceived to provide
a very fast storage layer for qEVM to enable a fast, complete EVM system with
right design choices made totally from scratch, but it also serves as a drop-in
replacement for any EVM-compatible blockchain storage system, and fits for the
general use of a certified key-value store of arbitrary data.

Firewood is a robust database implemented from the ground up to directly store
MPT nodes and user data. Unlike most (if not all) of the solutions in the field,
it is not built on top of a generic KV store such as LevelDB/RocksDB. Like a
B+-tree based store, firewood directly uses the tree structure as the index on
disk. Thus, there is no additional “emulation” of the logical MPT to flatten
out the data structure to feed into the underlying DB that is unaware of the
data being stored.

Firewood provides OS-level crash recovery via a write-ahead log (WAL). The WAL
guarantees atomicity and durability in the database, but also offers
“reversibility”: some portion of the old WAL can be optionally kept around to
allow a fast in-memory rollback to recover some past versions of the entire
store back in memory. While running the store, new changes will also contribute
to the configured window of changes (at batch granularity) to access any past
versions with no additional cost at all.

The on-disk footprint of Firewood is more compact than geth. It provides two
isolated storage space which can be both or selectively used the user. The
account model portion of the storage offers something very similar to StateDB
in geth, which captures the address-“state key” style of two-level access for
an account’s (smart contract’s) state. Therefore, it takes minimal effort to
delegate all state storage from an EVM implementation to firewood. The other
portion of the storage supports generic MPT storage for arbitrary keys and
values. When unused, there is no additional cost.

Firewood is an embedded key-value store, optimized to store blockchain state.
It prioritizes access to latest state, by providing extremely fast reads, but
also provides a limited view into past state. It does not copy-on-write like
the EVM, but instead makes in-place changes to the state tree. This ensures
that the database size is small and stable during the course of running
firewood. Firewood exists to provide a very fast storage layer for [qEVM](https://github.com/ava-labs/qevm) to use in a custom subnet.

## Build
Firewood currently is Linux-only, as it has a dependency on the asynchronous
I/O provided by the Linux kernel (see `libaio`). Unfortunately, Docker is not
able to successfully emulate the syscalls `libaio` relies on, so Linux or a
Linux VM must be used to run firewood. It is encouraged to enhance the project
with I/O supports for other OSes, such as OSX (where `kqueue` needs to be used
for async I/O) and Windows. Please contact us if you're interested in such contribution.

Firewood is written in stable Rust, but relies on the Rust nightly toolchain
for code linting/formatting.

## Run
There are several examples, in the examples directory, that simulate real world
use-cases. Try running them via the command-line, via `cargo run --release
--example simple`.

## Test
```
cargo test --release
```
