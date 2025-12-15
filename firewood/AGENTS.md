# Firewood - AI Assistant Guide

This document provides context and guidance for AI assistants working with the Firewood codebase.

## Project Overview

Firewood is an embedded key-value store optimized for storing recent Merkleized
blockchain state with minimal overhead. It's designed for the Avalanche C-Chain
and EVM-compatible blockchains that store state in Merkle tries.

**Key Characteristics:**

- Written in Rust (edition 2024, MSRV 1.91.0)
- Beta-level software with evolving API
- Compaction-less database that directly stores trie nodes on-disk
- Not built on generic KV stores (LevelDB/RocksDB)
- Uses trie structure directly as the index on-disk
- Maintains configurable number of recent revisions in memory and on disk

## Architecture Principles

1. **Direct Trie Storage**: Unlike most state management approaches, Firewood directly uses the trie structure as the index on-disk rather than emulating it on top of a generic database.

2. **Revision Management**: Creates new roots for each revision, tracks deleted nodes in a future-delete log (FDL), and returns space to free lists when revisions expire.

3. **Disk Addressing**: Root address of a node is simply the disk offset within the database file, not based on hashes.

4. **Recoverability**: Guarantees recoverability by not referencing new nodes before they're flushed to disk and carefully managing free lists.

## Workspace Structure

This is a Cargo workspace with the following members:

```text
firewood/             # Core library and main database implementation
├── src/              # Core source code
│   ├── db.rs         # Main database API
│   └── manager.rs    # RevisionManager for managing historical revisions
├── examples/         # Example usage (e.g., insert example)
└── benches/          # Benchmarks

firewood-macros/      # Procedural macros for the project
storage/              # Storage layer implementation
triehash/             # Trie hashing functionality
ffi/                  # Foreign Function Interface (FFI) binding for Golang
├── src/              # Rust FFI bindings (C-compatible API)
├── firewood.go       # Go wrapper around the Firewood `Db` type
├── proposal.go       # Go wrapper around the Firewood `Proposal` type
└── revision.go       # Go wrapper around the Firewood `DbView` type
fwdctl/               # CLI tool for interacting with Firewood databases
benchmark/            # Performance benchmarking suite
├── bootstrap/        # Script for running C-Chain reexecution benchmark on an EC2 instance.
└── setup-scripts/    # Scripts for setting up benchmark environments
```

## Important Terminology

- **Revision**: Historical point-in-time state of the trie
- **View**: Interface to read from a Revision or Proposal
- **Node**: Portion of a trie that can point to other nodes and/or contain Key/Value pairs
- **Hash/Root Hash**: Merkle hash for a node/root node
- **Proposal**: Consists of base Root Hash and Batch, not yet committed
- **Commit**: Operation of applying Proposals to the most recent Revision
- **Batch**: Ordered set of Put/Delete operations

## Feature Flags

### `ethhash`

By default, Firewood uses SHA-256 hashing compatible with merkledb. Enable this feature for Ethereum compatibility:

- Changes hashing from SHA-256 to Keccak-256
- Understands "account" nodes at specific depths with RLP-encoded values
- Computes account trie hash as actual root
- See `firewood/storage/src/hashers/ethhash.rs` for implementation details

### `logging`

Enable for runtime logging. Set `RUST_LOG` environment variable accordingly (uses `env_logger`).

## Common Development Tasks

### FFI

Building and using the FFI library is a multi-step process. To generate the
Firewood Rust FFI bindings:

```bash
cd ffi/src                                              # Go to Rust binding directory
cargo clean                                             # Remove any existing bindings
cargo build --profile maxperf --features ethhash,logger # Generate bindings
```

To then have Golang utilize these new bindings:

```bash
cd ..                   # Go to ffi directory
go tool cgo firewood.go # Generate cgo wrappers
```

### Using the CLI

The `fwdctl` tool provides command-line operations on databases. See `fwdctl/README.md`.

## Coding Conventions and Constraints

For more information on coding conventions and constraints, please refer to [CONTRIBUTING.md](./CONTRIBUTING.md)

## PR Strategy

Before submitting/updating a PR, run the following

```bash
cargo fmt                                                               # Format code
cargo test --workspace --features ethhash,logger --all-targets          # Run tests
cargo clippy --workspace --features ethhash,logger --all-targets        # Linter
cargo doc --no-deps                                                     # Ensure docs build
```

All tests must pass, and there should be no clippy warnings.

### Markdown Linter

If your PR touches any Markdown file, run the following:

```bash
markdownlint-cli2 .
```

If the linter fails, run the following to fix any lint errors:

```bash
markdownlint-cli2 . --fix
```

If you don't have `markdownlint-cli2` available on your system, run the
following to install the linter:

```bash
brew install markdownlint-cli2
```

## Performance Profiles

- **release**: Standard release with debug symbols
- **maxperf**: Panic abort, single codegen unit, fat LTO, no debug symbols

## Dependencies Management

Key dependencies are centrally managed in workspace `Cargo.toml`:

- `firewood`, `firewood-macros`, `firewood-storage`, `firewood-ffi`, `firewood-triehash` (workspace members)
- Common deps: `clap`, `thiserror`, `smallvec`, `sha2`, `log`, etc.
- Test deps: `criterion`, `tempfile`, `rand`, etc.

## Key Files to Know

- `README.md` - Main documentation
- `CONTRIBUTING.md` - Contribution guidelines
- `RELEASE.md` - Release process
- `README.docker.md` - Docker setup
- `CHANGELOG.md` - Version history
- `clippy.toml` - Linting configuration
- `Taskfile.yml` - Task runner recipes
- `cliff.toml` - Changelog generation config

## Notes for AI Assistants

1. **Safety First**: This codebase denies unsafe code. Never suggest unsafe
   blocks without documentation and strong justification. Unsafe code could be
   utilized in the `ffi` crate.

2. **Testing**: Any changes should include appropriate tests. Run `cargo test --release` to verify.

3. **Performance Context**: This is a database designed for blockchain state. Performance matters. Consider allocation patterns and hot paths.

4. **Beta Status**: The API may change. Don't assume stability guarantees.

5. **Feature Flags**: Be aware of `ethhash` feature flag when discussing Ethereum compatibility vs. default merkledb compatibility.

6. **Documentation**: Public APIs should be well-documented. Run `cargo doc --no-deps` to check.

7. **Workspace Awareness**: This is a multi-crate workspace. Changes may affect multiple crates. Check `Cargo.toml` for workspace structure.

## Additional Resources

- [Auto-generated docs](https://ava-labs.github.io/firewood/firewood/)
- [Issue tracker](https://github.com/ava-labs/firewood/issues)
