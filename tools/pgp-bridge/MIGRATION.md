# Migration of `pgp-bridge` into `avalanchego/tools/pgp-bridge`

Date: 2026-07-06

This records how `github.com/ava-labs/pgp-bridge` (a Cobra-based Go CLI that
produces OpenPGP signatures backed by AWS KMS keys) was moved into the
avalanchego monorepo with its git history preserved.

## History import (built-in subtree merge, aligned with graft)

```
git subtree add --prefix=tools/pgp-bridge <pgp-bridge source> main
```

Non-squash, so all 8 upstream commits are preserved. The merge commit carries
the same trailers `graft/` has: `git-subtree-dir`, `git-subtree-mainline`,
`git-subtree-split`, with the split sha recording the exact upstream commit
imported (`0b71173`). `git blame tools/pgp-bridge/...` attributes lines to the
original commits; `git log --follow` across the move boundary is unreliable
because the rename is introduced by a merge (same tradeoff as `graft/`).

## Integration: standalone module, not built by Bazel

pgp-bridge is a standalone CLI that nothing else in the repo imports. It is kept
as its own Go module and deliberately excluded from Bazel:

- **Not in `go.work`.** Adding it there routes it through
  `MODULE.bazel`'s `go_deps.from_file(go_work = ...)`, which requires Bazel
  BUILD files for every workspace member. Keeping it out also stops it from
  perturbing workspace dependency resolution (adding it to the workspace raised
  shared versions and churned `graft/*` go.mod/go.sum and `go.work.sum`).
- **`tools/pgp-bridge` in `.bazelignore`**, and the root `# gazelle:exclude
  tools` directive already keeps gazelle (and `gazelle_test`) from generating or
  requiring BUILD files under `tools/`. Bazel never sees the module.
- **No `MODULE.bazel` entry and no BUILD files.**

### Why not a Bazel target

Making pgp-bridge a Bazel-built module was attempted and rejected:

- The workspace (`go.work`) route builds under Bazel but forces the workspace
  MVS to include pgp-bridge's dependencies, churning unrelated `graft/*`
  go.mod/go.sum.
- The isolated `go_deps` extension route (mirroring `tool_go_deps` for
  `tools/external`) does not work for a *buildable* module: gazelle generates
  BUILD files with canonical repo names (`@com_github_spf13_cobra`), and
  `bazel mod tidy` then forces the isolated extension to export those same
  canonical names, which collide with the main `go_deps` use_repo
  (`The repo name 'com_github_spf13_cobra' is already being used`). The isolated
  extension is designed for hand-referenced tool binaries, not gazelle-built
  local packages.

Bazel build/test integration is therefore a follow-up (see below).

## Build and test

As a standalone module (workspace disabled so `go` does not pick up the repo
`go.work`):

```
cd tools/pgp-bridge
GOWORK=off go build ./...
GOWORK=off go test ./...
```

`go mod tidy` for the module is wired into the repo `go-mod-tidy` task with
`GOWORK=off`, mirroring `tools/external`.

## Module adaptations

- Module path renamed to `github.com/ava-labs/avalanchego/tools/pgp-bridge`,
  internal imports rewritten.
- `go` directive bumped from `1.25` to `1.25.10` to match the repo.
- avalanchego BSD-3 license headers added to all `.go` files (the upstream
  LICENSE was already BSD-3, Copyright Ava Labs), applied with the repo's
  `go-license` tool so the lint `license_header` check passes.

## Follow-ups (not part of this change)

- Add a `go test` CI job (or Bazel integration) so pgp-bridge's tests run in CI.
- Integrate the CLI into the release/packaging signing workflow if desired.
- Archive the source `ava-labs/pgp-bridge` repository.
