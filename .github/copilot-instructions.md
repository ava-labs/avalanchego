# Firewood — GitHub Copilot Instructions

Firewood is an embedded key-value store for Merkleized blockchain state, written in Rust
with a Go FFI layer (`ffi/`). It uses the trie structure directly as the storage index
rather than layering on top of a generic KV store.

When assisting with code in this repository, apply the guidelines below in addition to
your standard capabilities.

## Code Review

See [`CODE_REVIEW.md`](../CODE_REVIEW.md) for the complete set of code review checks.

## Cargo Feature Matrix

The following feature combinations must all pass `cargo clippy` and `cargo nextest`
before a PR is ready, on either platform. The `io-uring` feature is portable but
inert off Linux, so `--all-features` builds everywhere — only Linux actually
compiles the ring backend.

| Feature flags               | macOS | Linux |
| --------------------------- | ----- | ----- |
| (none)                      | ✓     | ✓     |
| `--no-default-features`     | ✓     | ✓     |
| `--features ethhash,logger` | ✓     | ✓     |
| `--all-features`            | ✓     | ✓     |

`--all-features` requires rustc 1.94.1 or newer — above the 1.94.0 workspace MSRV
— because it enables `fwdctl`'s `launch` feature, whose AWS SDK dependencies
declare that floor. The other three rows build on 1.94.0.

## Commit and PR Conventions

Commit messages and PR titles must follow
[Conventional Commits](https://www.conventionalcommits.org/). See
`.github/workflows/pr-title.yaml` for the authoritative list of allowed types.

## Additional Context

See `AGENTS.md` for full project context, architecture principles, and the complete
code review guidelines.
