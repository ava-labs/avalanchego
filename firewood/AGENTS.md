# Firewood - AI Assistant Guide

This document provides context and guidance for AI assistants working with the Firewood codebase.

## Project Overview

Firewood is an embedded key-value store optimized for storing recent Merkleized
blockchain state with minimal overhead. It's designed for the Avalanche C-Chain
and EVM-compatible blockchains that store state in Merkle tries.

**Key Characteristics:**

- Written in Rust (edition 2024, MSRV 1.94.0 — but see
  [Toolchain Floor for `--all-features`](#toolchain-floor-for---all-features))
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
- **View**: Read-only interface into a Revision, Proposal, or Reconstructed state
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

Any tagged enums added to the FFI api where the union body contains a pointer must be defined as `#[repr(C, usize)]` so that the enum tag forces the C struct to have pointer alignment.

### Using the CLI

The `fwdctl` tool provides command-line operations on databases. See `fwdctl/README.md`.

## Coding Conventions and Constraints

For more information on coding conventions and constraints, please refer to [CONTRIBUTING.md](./CONTRIBUTING.md)

## Commit and PR Title Convention

Commit messages and PR titles **must** follow the
[Conventional Commits](https://www.conventionalcommits.org/) specification.
This is enforced by CI on every pull request (see `.github/workflows/pr-title.yaml`).

Allowed types: `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`,
`refactor`, `style`, `test`.

A scope is optional. Examples:

```text
feat: add new revision API
fix(storage): correct off-by-one in free list
chore(deps): bump lz4_flex from 0.11.5 to 0.11.6
```

When writing commit message bodies, consider following the structure from the
PR template (`.github/pull_request_template.md`):

- **Why this should be merged** — motivation and context
- **How this works** — brief description of the approach
- **How this was tested** — what you ran to verify correctness
- **Breaking Changes** — note any breaking changes to `firewood`,
  `firewood-storage`, `firewood-ffi`, `firewood-go`, or `fwdctl`

Not every commit needs all sections — use your judgement based on the scope
of the change.

## PR Strategy

Before submitting or updating a PR, run the local pre-push checks:

```bash
just prepush
```

This runs `just lint` followed by `just test`. These recipes use the same
lower-level `ci-*` recipes as GitHub Actions, keeping the local and CI commands
in sync. Run either phase separately while iterating:

```bash
just lint
just test
```

If `just` is not installed, use `./scripts/run-just.sh prepush`. The wrapper
uses `just` when available, falls back to Nix when available, and otherwise
prints installation instructions.

The complete set of Rust profile names and Cargo arguments is defined in
`scripts/run-rust-ci.sh`. Every profile there is portable to macOS, so the Just
recipes and CI run the same set. The Justfile and CI workflows should pass
profile names instead of duplicating Cargo feature and profile arguments. When
changing a CI Rust matrix, update the shared script and both callers as needed.

All tests must pass, and there should be no clippy warnings.

### Toolchain Floor for `--all-features`

The workspace declares `rust-version = "1.94.0"`, and that is accurate for the
default build, `--no-default-features`, and `--features ethhash,logger`.
`--all-features` needs **1.94.1**.

`--all-features` turns on `fwdctl`'s `launch` feature, which pulls in the AWS SDK
(`aws-config`, `aws-sdk-ec2`, `aws-sdk-ssm`, `aws-sdk-sts`, and their
`aws-smithy-*` dependencies). Every one of those crates declares
`rust-version = "1.94.1"`. Cargo refuses the build before compiling anything:

```text
error: rustc 1.94.0 is not supported by the following packages:
  aws-config@1.9.0 requires rustc 1.94.1
  ...
```

This is unrelated to the platform gating described under
[Linux-only Checks](#linux-only-checks) — it applies equally on macOS and Linux.

On exactly 1.94.0, either use a newer toolchain for `debug-all-features` (any
1.94.1+ release works, and CI is well ahead of the floor), or downgrade the AWS
crates as the error suggests. The workspace `rust-version` is deliberately left at
1.94.0 so the floor reflects what the shipped library crates need rather than what
an optional `fwdctl` feature needs; bumping it to 1.94.1 is the alternative if the
split proves confusing in practice.

### Slow Tests

`just test` runs every portable Rust test profile with nextest's `ci` profile,
so tests prefixed with `test_slow_` are included automatically. Targeted or
default-profile test commands are useful during development, but do not replace
`just test` before pushing.

### Linux-only Checks

The local `lint`, `test`, and `prepush` recipes are designed to run on macOS.
They intentionally omit CI checks that require Linux: the differential fuzz jobs,
which use Linux-specific resource limits and tooling. GitHub Actions remains
authoritative for those.

`--all-features` is *not* in that category. The `io-uring` feature is accepted on
every platform but only takes effect on Linux, where `storage/build.rs` sets the
`cfg(io_uring)` alias that gates the ring backend; elsewhere the feature is inert
and the standard I/O path is used. So `debug-all-features` runs in `just lint`
and `just test` like any other profile. Note this means the ring code itself is
compiled only by the Linux CI jobs — a macOS-green `--all-features` run does not
prove `storage/src/linear/io_uring.rs` builds.

Do not add Linux-only commands to the macOS-compatible aggregate recipes.

Two further CI checks have no local aggregate equivalent: the license-header
check (a GitHub Action) and the `examples` job. The examples can be run
manually with `just ci-rust benchmark-example <profile>` and
`just ci-rust insert-example <profile>`.

### Markdown Linter

`just lint` and `just prepush` run the Markdown checks used by CI. To run only
the repository-wide Markdown check, use:

```bash
just ci-lint-markdown
```

If the linter fails, run the following to fix any lint errors:

```bash
just fix-markdown
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
- `CHANGELOG.md` - Version history
- `.devcontainer/` - Development container configuration
- `clippy.toml` - Linting configuration
- `justfile` - Just command runner recipes
- `cliff.toml` - Changelog generation config

## Notes for AI Assistants

1. **Safety First**: This codebase denies unsafe code. Never suggest unsafe
   blocks without documentation and strong justification. Unsafe code could be
   utilized in the `ffi` crate.

2. **Testing**: Any changes should include appropriate tests. Run targeted tests
   while iterating and `./scripts/run-just.sh prepush` before handoff.

3. **Performance Context**: This is a database designed for blockchain state. Performance matters. Consider allocation patterns and hot paths.

4. **Beta Status**: The API may change. Don't assume stability guarantees.

5. **Feature Flags**: Be aware of `ethhash` feature flag when discussing Ethereum compatibility vs. default merkledb compatibility.

6. **Documentation**: Public APIs should be well-documented. The documentation
   check is included in `./scripts/run-just.sh lint`; run `./scripts/run-just.sh ci-docs` to invoke it
   separately.

7. **Workspace Awareness**: This is a multi-crate workspace. Changes may affect multiple crates. Check `Cargo.toml` for workspace structure.

## Code Review Guidelines

See [`CODE_REVIEW.md`](./CODE_REVIEW.md) for the complete set of code review checks.

## Additional Resources

- [Auto-generated docs](https://ava-labs.github.io/firewood/rustdoc/firewood/)
- [Issue tracker](https://github.com/ava-labs/firewood/issues)
