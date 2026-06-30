# Bazel Build System

This document explains how Bazel is configured and used in the
avalanchego monorepo.

## Prerequisites

The `bazel` command is provided by [bazelisk](https://github.com/bazelbuild/bazelisk),
which automatically downloads the correct Bazel version from `.bazelversion`. Most
Taskfile targets use `./scripts/nix_run.sh bazelisk ...`, which runs in the repo's
nix dev shell when needed and avoids nesting `nix develop` when already inside it.
In the nix dev shell (`nix develop`), `bazel` and `bazelisk` are both on PATH directly.
For Nix installation and repo dev shell setup, see [CONTRIBUTING.md](../CONTRIBUTING.md#nix).

Some tasks (e.g. `task bazel-check-metadata`) only require tooling that is installed
by default on GitHub Action runners (e.g. `bash`, `git`, `go`, `bazelisk`).  These
tasks can be executed without a nix shell which in CI avoids the cost of nix installation.

## Quick Start

```bash
# Build the main binary
task bazel-build

# Build with optimizations
task bazel-build-opt

# Run unit tests (default Bazel mode: no race, no shuffle)
task bazel-test

# Stronger validation with race detection and shuffle
task bazel-test-race-shuffle

# Update Bazel metadata after changing Go imports or Bazel module deps
task bazel-generate-metadata

# Clean build cache
task bazel-clean
```

## Why Bazel?

1. **Hermetic builds** - Reproducible builds regardless of host environment
2. **Incremental compilation** - Only rebuild what changed
3. **Parallel execution** - Efficient use of multi-core systems
4. **Caching** - Local and remote build caching
5. **Multi-language** - Single build system for Go, Solidity, Rust, protobuf, etc.

## Architecture Overview

### Toolchain Strategy

The build uses `go_sdk.from_file()` to read the Go version from
`go.mod`, ensuring a single source of truth without manual syncing:

```python
go_sdk = use_extension("@io_bazel_rules_go//go:extensions.bzl", "go_sdk")
go_sdk.from_file(go_mod = "//:go.mod")
```

**Approaches considered:**

| Approach | Pros | Cons |
|----------|------|------|
| `go_sdk.from_file()` (chosen) | Reproducible, single source of truth via go.mod, no nix required | None significant |
| `go_sdk.download()` | Explicit version in MODULE.bazel | Go version must be synced manually across go.mod files |
| `go_sdk.host()` | Uses system Go | Requires nix shell, not hermetic outside nix |
| `rules_nixpkgs_go` | Bazel calls Nix directly, fully hermetic | **Incompatible with rules_go v0.57+** (toolchain API mismatch) |

> **Note:** rules_nixpkgs_go v0.13.0 proved incompatible with rules_go
> v0.56.0+. The rules_go toolchain API changed in ways that
> rules_nixpkgs_go doesn't support. See:
> https://github.com/tweag/rules_nixpkgs/issues/667

### Version Pinning

This repo keeps version pins in the checked-in configuration consumed by the
relevant tooling rather than duplicating them in documentation:

- Bazel: `.bazelversion`
- Go: `go.mod` (read by `go_sdk.from_file()`)
- Bazel modules such as `rules_go` and Gazelle: `MODULE.bazel`

When checking or updating a version, use those files as the source of truth.

### Repository tools and external-dependency fetches

Bazel CI uses two separate Gazelle `go_deps` extension instances:

- the main `go_deps` instance reads `go.work` for the workspace modules and the
  external repos they import
- the isolated `tool_go_deps` instance reads `tools/external/go.mod` for
  repo-owned helper tools that CI may need to launch before other Bazel tasks

That split is intentional. The CI setup path needs to fetch the Bazel-owned
`//tools/external:task` bootstrap target and warm external dependency caches
without also depending on whatever local workspace state happens to exist in a
particular checkout.

For the same reason, `MODULE.bazel` intentionally omits `use_repo` bindings for
workspace modules such as `avalanchego` and `graft/*`. Those modules are built
from the local source tree, so binding their generated local-path repos is not
needed for normal builds. Omitting them also keeps broad fetches such as
`bazel fetch //...` from traversing personal workspace state like local
symlinks or repo-adjacent directories while trying to prepare external
repositories for CI.

If future Bazel changes appear to make this split unnecessary, treat that as a
behavioral change to validate rather than a cleanup to apply mechanically. The
important invariant is that Bazel CI can bootstrap repo tools and prefetch the
external dependencies its jobs need without coupling that setup step to
machine-specific workspace state.

### Why Bazel 8?

| Factor | Bazel 7 | Bazel 8 |
|--------|---------|---------|
| bzlmod | Optional | Default |
| LTS status | Older | Current |
| WORKSPACE | Default | Deprecated (still works) |

Bazel 8 is the current LTS with native bzlmod support. bzlmod replaces
the legacy use of WORKSPACE files.

### Multi-Module Structure

The repository contains multiple Go modules with different licenses:

| Module | Path | License | Import Path |
|--------|------|---------|-------------|
| avalanchego | `/` (root) | BSD-3 | `github.com/ava-labs/avalanchego` |
| coreth | `graft/coreth/` | LGPL-3 | `github.com/ava-labs/avalanchego/graft/coreth` |
| evm | `graft/evm/` | - | `github.com/ava-labs/avalanchego/graft/evm` |
| subnet-evm | `graft/subnet-evm/` | LGPL-3 | `github.com/ava-labs/avalanchego/graft/subnet-evm` |

Each module has its own `go.mod` with `replace` directives pointing to
sibling modules. Bazel handles cross-module imports via `go.work` and
gazelle prefix directives.

### Key Configuration Files

| File | Purpose | Safe to Delete? |
|------|---------|-----------------|
| `MODULE.bazel` | Bazel module definition, dependencies, patches | **No** |
| `MODULE.bazel.lock` | Locked module/dependency resolution state | Yes (regenerated) |
| `go.work` | Go workspace aggregating all modules (used by `go_deps`) | **No** |
| `.bazelrc` | Bazel build flags and settings | **No** |
| `.bazelignore` | Directories excluded from Bazel | **No** |
| `.bazelversion` | Bazel version pin (used by bazelisk) | **No** |
| `BUILD.bazel` (root) | Gazelle config: prefix, exclusions, proto disable | **No** |
| `.bazel/patches/*.patch` | Fixes for external dependencies (some generated) | **No** |
| `.bazel/patches/build_files/` | Source BUILD files for generated patches | **No** |
| `scripts/generate_bazel_patches.sh` | Generates `.patch` files from `build_files/` | **No** |
| `.bazel/defs.bzl` | Custom test macros for graft module timeouts | **No** |
| `scripts/bazel_workspace_status.sh` | Git commit stamping for releases | **No** |

### BUILD.bazel Files with Custom Content

Most BUILD.bazel files are auto-generated by Gazelle, but some have
custom content that would be lost if deleted:

| File | Custom Content |
|------|---------------|
| `BUILD.bazel` (root) | Gazelle directives (prefix, exclude, proto) + `gazelle()` rule |
| `main/BUILD.bazel` | `x_defs` for Git commit stamping |
| `graft/coreth/BUILD.bazel` | `gazelle:prefix`, `gazelle:map_kind` for test timeouts |
| `graft/evm/BUILD.bazel` | `gazelle:prefix`, `gazelle:map_kind` for test timeouts |
| `graft/subnet-evm/BUILD.bazel` | `gazelle:prefix`, `gazelle:map_kind` for test timeouts |

**Preserving custom content**: Gazelle directives (`# gazelle:prefix`,
`# gazelle:map_kind`) are automatically preserved. For other custom
content (like `x_defs`), use `# keep` comments to prevent gazelle from
removing them:

```python
x_defs = {
    # keep
    "github.com/ava-labs/avalanchego/version.GitCommit": "{STABLE_GIT_COMMIT}",
},
```

**Rule of thumb**: Use `# keep` only for custom content that Gazelle
would otherwise remove or rewrite.

**Caveat**: Gazelle's `fix` mode does not rename existing function
calls in files with `# keep` comments. This means changes to
`gazelle:map_kind` directives (e.g., renaming a macro) won't propagate
to `# keep` files automatically -- they need a manual find-and-replace.

## Gazelle

[Gazelle](https://github.com/bazelbuild/bazel-gazelle) automatically
generates BUILD.bazel files from Go source code.

### Where Gazelle Comes From

Gazelle is declared as a `bazel_dep` in `MODULE.bazel`. It is **not**
provided by Nix. The recommended entrypoint is `task
bazel-generate-metadata`, which regenerates Bazel metadata for the repo.

### When to Run Gazelle

Run `task bazel-generate-metadata` after:
- Adding new `.go` files
- Changing import statements
- Adding new packages/directories
- Modifying `go.mod` dependencies
- Modifying `MODULE.bazel`

`task bazel-generate-metadata` also refreshes `MODULE.bazel.lock` into the
same state later Bazel module commands expect. `bazel mod tidy` alone does not
always fully refresh `MODULE.bazel.lock`, so a later Bazel command may rewrite
it. Running the lockfile refresh as part of metadata generation makes that
update happen in one predictable place instead of as a later surprise.

### How Gazelle Handles Multiple Modules

A single root-level gazelle run handles all Go modules. Gazelle
discovers `# gazelle:prefix` directives in subdirectories and uses
them for import path resolution, so graft modules get correct
`importpath` values without needing separate gazelle targets.

Each Go module root needs a `BUILD.bazel` with a `gazelle:prefix`
directive:

```python
# graft/coreth/BUILD.bazel
# gazelle:prefix github.com/ava-labs/avalanchego/graft/coreth
```

Dependencies are similarly unified via a single declaration:
```python
go_deps.from_file(go_work = "//:go.work")
```

The `go.work` file aggregates all module dependencies, so external
dependencies from graft modules are resolved automatically.

### Custom Test Macros via `gazelle:map_kind`

The graft modules (coreth, evm, subnet-evm) have longer test timeouts
than the root module. Each graft module's root BUILD.bazel uses a
`gazelle:map_kind` directive to remap `go_test` to the `graft_go_test`
macro (defined in `.bazel/defs.bzl`, Bazel "long" timeout, 900s):

```python
# graft/coreth/BUILD.bazel
# gazelle:prefix github.com/ava-labs/avalanchego/graft/coreth
# gazelle:map_kind go_test graft_go_test //.bazel:defs.bzl
```

This remaps all `go_test` targets in that subtree to use the custom
macro with the longer timeout.

## External Dependency Handling

### Go Dependencies

Go dependencies are resolved via `go_deps.from_file(go_work = ...)`
(see "How Gazelle Handles Multiple Modules" above).

### Patched Dependencies

Some dependencies require patches for Bazel compatibility. Patches are
in `.bazel/patches/` and applied in `MODULE.bazel`.

#### Patching strategies

Ordered from least to most invasive:

1. **Gazelle directives** (`gazelle_override` with directives)
   Guide gazelle's BUILD file generation with directives like
   `gazelle:exclude` to skip problematic directories. Use when
   excluding certain source files is sufficient.

2. **Sandbox relaxation** (`tags = ["no-sandbox"]`)
   Disable sandboxing on specific targets so they can access
   undeclared inputs (e.g., cross-package assembly includes). Use when
   the build tool needs files that can't be declared as Bazel
   dependencies due to tooling limitations. Trade-off: loses
   hermeticity on those targets; incompatible with remote execution.

3. **BUILD file augmentation** (patch on gazelle output)
   Patch the gazelle-generated BUILD files to add missing sources,
   flags, or attributes. Use when gazelle gets most things right but
   misses something specific.

4. **Custom BUILD files** (`build_file_generation = "off"` + patch)
   Replace gazelle-generated BUILD files entirely. Use when the
   dependency has complex build requirements (CGO, assembly,
   non-standard layout) that gazelle can't handle.

**Choosing between augmentation (3) and custom BUILD files (4):**
Prefer augmentation when gazelle produces a correct BUILD file that
only needs minor additions (e.g., an extra `copts` flag or a missing
source file). Switch to custom BUILD files when gazelle gets less than
~70% of the target right — at that point, patching the delta becomes
harder to maintain than owning the whole file. Signs that custom BUILD
files are warranted: `cc_library` targets with platform-specific
`select()`, CGO with assembly, pre-built static libraries via
`cc_import`, or unity-build compilation models.

| Dependency | Issue | Solution |
|------------|-------|----------|
| `ava-labs/libevm` | Missing C sources for secp256k1 | Custom BUILD files + gazelle directive excludes secp256k1 dir |
| `firewood-go-ethhash/ffi` | Pre-built static libraries | Custom BUILD files with `cc_import` |
| `supranational/blst` | Complex CGO with assembly | Custom BUILD files (gazelle disabled) |
| `consensys/gnark-crypto` | Assembly cross-package includes | `no-sandbox` on bls12-381 `fp` and `fr` targets |

#### libevm (secp256k1)

**Problem:** Gazelle-generated BUILD file doesn't include libsecp256k1 C
sources.

**Solution:** Custom BUILD files + gazelle directive excluding the
secp256k1 directory from generation. See `Patch Maintenance` below
for how to edit these.

#### firewood-go-ethhash FFI

**Problem:** Pre-built static libraries with `-L` paths that don't work
in Bazel's sandbox.

**Solution:** Custom BUILD files with `cc_import` for proper static
library linking. Gazelle can't generate these rules for pre-built
libraries. See `Patch Maintenance` below for how to edit these.

#### blst (BLS Signatures)

**Problem:** Complex CGO with assembly files that gazelle cannot handle.

**Solution:** Custom BUILD files replacing gazelle output entirely.
Gazelle is disabled and a patch provides custom BUILD files handling
blst's unity-build compilation model with platform-specific assembly
selection. Compiler flags are derived from the CGO directives in
`blst.go`. See `Patch Maintenance` below for how to edit these.

#### gnark-crypto (BLS12-381 for KZG)

**Problem:** Assembly files use cross-package `#include` directives:
```asm
#include "../../../field/asm/element_4w/element_4w_arm64.s"
```
Bazel's sandboxed builds don't allow relative includes across package
boundaries.

**Solution:** Sandbox relaxation via `tags = ["no-sandbox"]` on the
bls12-381 `fp` and `fr` targets, allowing the Go assembler to resolve
relative includes from the full source tree in the execroot. Only
bls12-381 is patched since it's the only curve in the dependency graph.
This is simpler than duplicating assembly files or providing custom
BUILD files for the entire module.
See [rules_go#3636](https://github.com/bazel-contrib/rules_go/issues/3636)).
```python
go_deps.module_override(
    patches = ["//.bazel/patches:com_github_consensys_gnark_crypto_asm_includes.patch"],
    path = "github.com/consensys/gnark-crypto",
)
```

**Dependency chain:**
`avalanchego → libevm → kzg4844 → go-kzg-4844 → gnark-crypto`

gnark-crypto is used for KZG blob commitments (EIP-4844).

### Protocol Buffers

Proto generation is disabled globally via the root `BUILD.bazel`:

```python
# gazelle:proto disable_global
```

The repository uses pre-generated `.pb.go` files checked into source
control rather than generating them at build time. This avoids proto
toolchain complexity in Bazel while maintaining compatibility with the
existing `go generate` workflow.

Bazel targets that import protobuf runtime packages depend on the Go module
`google.golang.org/protobuf` through `go_deps.from_file(go_work = "//:go.work")`
and refer to it as `@org_golang_google_protobuf//...`. The repo does not rely
on direct `protobuf` / `rules_proto` Bazel module dependencies for proto code
generation.

## Common Tasks

### Building

```bash
# Build main binary
task bazel-build                    # or: bazel build //main:avalanchego

# Build with optimizations
task bazel-build-opt               # or: bazel build --compilation_mode=opt //main:avalanchego

# Build everything
bazel build //...
```

### Testing

This repository exposes multiple test entrypoints, and they do not all use the
same defaults.

- `bazel test //...` and `task bazel-test` use the default Bazel mode:
  race off, shuffle off.
- `scripts/build_test.sh` uses race on, shuffle on.
- The reusable GitHub Actions Bazel workflow runs its unit-test jobs with the
  `race-shuffle` mode.

The default Bazel entrypoints favor cheaper local iteration. The non-Bazel
unit-test path and Bazel CI unit-test jobs use stronger validation modes.

Other behavior still roughly aligns:

- The script passes `-tags test` to `go test`; currently there are no
  `//go:build test` files in this repo, so it has no effect.
- The script excludes several directories via `go list | grep -v ...`;
  Bazel instead relies on `tags = ["manual"]` to keep non-unit tests
  out of `bazel test //...`.

```bash
# Run all unit tests in the default Bazel mode (no race, no shuffle)
task bazel-test                   # or: bazel test //...

# Run tests for a specific package
bazel test //utils/...

# Run specific test functions (target:test_name + filter)
bazel test //utils:set_test --test_filter=TestSet_Add

# Race detection without shuffle
task bazel-test-race              # or: bazel test --config=race //...

# Collect coverage
bazel coverage //...

# Race detection with shuffle enabled
task bazel-test-race-shuffle      # or: bazel test --config=race-shuffle //...

# Run E2E tests (requires built binary)
task bazel-test-e2e
```

#### Test Options

| Public mode | Behavior | Bazel config | Typical use |
|-------------|----------|--------------|-------------|
| default (`bazel-test`) | race off, shuffle off | Bazel default | fastest normal local Bazel iteration |
| `race` | race on, shuffle off | `--config=race` | stronger local validation without order randomization |
| `race-shuffle` | race on, shuffle on | `--config=race-shuffle` | strongest local validation; also used by Bazel CI unit-test jobs |

Examples:
```bash
# Default repo task mode (no shuffle, no race)
task bazel-test                   # or: bazel test //...

# Race detection without shuffle
task bazel-test-race              # or: bazel test --config=race //...

# Race detection with shuffle enabled
task bazel-test-race-shuffle      # or: bazel test --config=race-shuffle //...
```

#### Test Timeouts

Bazel has four timeout categories. `.bazelrc` sets the durations via
`--test_timeout=short,moderate,long,eternal`:

| Category | Duration | Used By |
|----------|----------|---------|
| short | 60s | - |
| moderate | 120s | Root module tests (default for `go_test`) |
| long | 900s | Graft module tests (via `graft_go_test` macro) |
| eternal | 3600s | - |

The `graft_go_test` macro sets `timeout = "long"` so graft tests
get the 900s budget. Root module tests use the default `"moderate"`
category (120s). See Custom Test Macros for how this is wired up.

#### Non-Unit Tests and the `manual` Tag

Tests that are not unit tests (e2e tests, integration tests, load
tests) must have `tags = ["manual"]` in their BUILD.bazel file. This
excludes them from `bazel test //...` which should only run unit
tests.

This roughly mirrors the behavior of `scripts/build_test.sh`, which excludes these directories via grep:
```bash
grep -v tests/e2e | grep -v tests/upgrade | grep -v tests/fixture/bootstrapmonitor/e2e | ...
```

**Tests with `manual` tag:**

| Test | BUILD.bazel Location |
|------|---------------------|
| Main E2E tests | `tests/e2e/BUILD.bazel` |
| Upgrade tests | `tests/upgrade/BUILD.bazel` |
| Bootstrap monitor E2E | `tests/fixture/bootstrapmonitor/e2e/BUILD.bazel` |
| Subnet-EVM warp tests | `graft/subnet-evm/tests/warp/BUILD.bazel` |
| Subnet-EVM load tests | `graft/subnet-evm/tests/load/BUILD.bazel` |
| Coreth warp tests | `graft/coreth/tests/warp/BUILD.bazel` |

**When adding new non-unit tests**, add the manual tag with a `# keep`
comment. The `# keep` is required because gazelle does not manage the
`manual` tag and will strip it when regenerating BUILD files from
scratch. Use `go_test` in your BUILD.bazel -- in graft modules,
`gazelle:map_kind` will automatically rewrite it to `graft_go_test`:
```python
go_test(
    name = "my_e2e_test",
    srcs = ["my_e2e_test.go"],
    tags = ["manual"],  # keep -- not a unit test
    deps = [...],
)
```

### Maintenance

```bash
# Regenerate Bazel metadata
task bazel-generate-metadata

# Update MODULE.bazel use_repo calls
task bazel-mod-tidy               # or: bazel mod tidy

# Refresh Bazel module metadata files
task bazel-sync-module-metadata

# Clean build outputs
task bazel-clean                  # or: bazel clean

# Full cache clean
task bazel-clean-all              # or: bazel clean --expunge

# CI/local: verify Bazel metadata is current
task bazel-check-metadata
```

As part of `bazel-check-metadata`, package-local `BUILD.bazel` files are
expected to define at most one `go_library` rule. Multiple
`go_library` rules in one directory are usually stale metadata left
behind by a package rename or move, where Gazelle added the new rule
without removing the old checked-in one.

This repo prefers linting for that stale-rule pattern rather than
deleting and regenerating all non-curated `BUILD.bazel` files. The lint
is narrower and safer: it fails on the specific suspicious state we want
to prevent, without relying on a maintained list of which BUILD files
are safe to destroy and recreate from scratch.

In CI, the Bazel workflow runs `bazel-check-metadata` before Bazel build
and test jobs. This makes stale metadata fail with a single actionable
error instead of surfacing later as multiple downstream Bazel failures.
This is especially useful for pull requests tested against a moving base
branch, where the metadata included in the PR may be stale relative to
the current merge target.

In GitHub Actions, the Bazel jobs use the local
`./.github/actions/setup-bazel` composite action. It prepares cache
state for the dependencies those jobs are expected to need and sets
`RUN_TASK_PREFER_BAZEL=1`. With that variable set, `run_task.sh` uses
the Bazel-owned `//tools/external:task` target instead of bootstrapping
`task` with `go tool` on runners where Go is already on `PATH`. That
preference is only for CI; local developer use still defaults to the
Go-based task bootstrap.

See [Bazel CI External Dependency
Caching](#bazel-ci-external-dependency-caching) for the motivation,
cache-key design, checked-in list of Bazel CI target patterns used to
prepare the build dependency cache, and enforcement model. This keeps
repo tool bootstrapping and build dependency caching inside Bazel for
the lighter-weight Bazel CI jobs. The E2E Bazel job uses the same cache
setup before its heavier test wrapper.

That check includes the Bazel module metadata files, so lockfile drift
is caught in the metadata phase rather than showing up later as a
surprising working-tree mutation.

## Bazel CI External Dependency Caching

### Why this exists

The intent is similar to `actions/setup-go`: set up dependency caches once
from a small amount of checked-in metadata so later CI jobs can reuse them
instead of downloading the same things again. Bazel does not infer the right
shared CI cache contents from `go.mod` alone, so this repo has to be more
explicit about what it fetches ahead of time.

The primary motivation is CI reliability, not just speed. This repo has seen
GitHub Actions flakes when Bazel jobs had to download external dependencies and
Go module data from the network in each job. Caching as much of that setup
work as possible means fewer repeated network requests during the Bazel
workflow, which reduces exposure to those infrastructure failures.

### What is cached

The Bazel CI cache setup restores and configures two kinds of cached data:

- Bazel `repository_cache`
- shared Gazelle `GOMODCACHE`

The shared `GOMODCACHE` is required because Gazelle `go_repository` otherwise
keeps Go module downloads in each Bazel work area. That means a later job can
still hit the network even after the setup fetch has already run. The action
therefore enables both:

- `GO_REPOSITORY_USE_HOST_MODCACHE=1`
- `GOMODCACHE=...`

### Cache key

The GitHub Actions cache key is:
`bazel-repo-${runner.os}-${runner.arch}-${hashFiles('.bazelversion', 'MODULE.bazel.lock', 'scripts/bazel_ci_dependency_list.sh')}`
with a same-platform restore prefix of
`bazel-repo-${runner.os}-${runner.arch}-`.

That split is intentional:
- `runner.os` and `runner.arch` separate caches by platform
- `.bazelversion` invalidates the cache when the Bazel version changes
- `MODULE.bazel.lock` invalidates the cache when the pinned external
  dependency set changes
- `scripts/bazel_ci_dependency_list.sh` invalidates the cache when the
  checked-in Bazel CI target patterns used by setup change
- the broader same-platform restore key still gives a useful warm start
  because these caches store downloaded dependency data, not per-run
  build outputs

### Checked-in list of Bazel CI target patterns used to prepare the build dependency cache

This setup is similar in spirit to `actions/setup-go`: before the later Bazel
CI jobs run, prepare cache state for the build dependencies they are expected
to need so those jobs do not each discover missing dependencies on their own.

The setup action first restores any previously saved dependency data,
configures Bazel to use it, and fetches the Bazel-owned
`//tools/external:task` bootstrap target before the workflow's first
`./scripts/run_task.sh ...` invocation. In the per-platform `setup` job it is
run with `initial-setup: true`; in that mode it also checks Bazel metadata and
runs `./scripts/run_task.sh bazel-cache-ci-build-dependencies`, which
delegates to `./scripts/cache_bazel_ci_build_dependencies.sh` and uses the
checked-in list in `./scripts/bazel_ci_dependency_list.sh`.

That checked-in list names both:
- the Bazel bootstrap targets needed before the first CI task launch
- the Bazel target patterns whose build dependencies the later CI jobs are
  expected to need

The list should cover what `bazel-ci.yml` actually runs, rather than trying to
fetch everything Bazel could possibly reach. This avoids missing dependencies
needed by the real CI jobs while also avoiding broader fetches that download
unrelated repos and toolchains.

A related design constraint is that this setup path must stay focused on
external dependencies, not local workspace-module discovery. The isolated
`tool_go_deps` extension and the omission of workspace-module `use_repo`
bindings in `MODULE.bazel` are part of the same design: they let the setup job
fetch Bazel-owned repo tools and warm caches for later jobs without making
`bazel fetch` walk machine-specific workspace state.

### Enforcement

The Bazel CI tasks that consume this prepared cache state run through
`./scripts/run_bazel_ci_command.sh`. When `BAZEL_CI_ENFORCE_DEPENDENCY_LIST=1`
is set (as it is in `bazel-ci.yml`), that wrapper extracts the target patterns
from its Bazel invocation and fails if they are not present in
`bazel_ci_dependency_list.sh`.

That keeps the checked-in list aligned with the Bazel CI jobs we actually run.
It makes it harder for a new or changed Bazel CI job to start depending on a
different set of external build dependencies without also updating the list of
target patterns used by setup to prepare the cache.

### Changing this safely

When modifying `setup-bazel`, `run_task.sh`, `run_bazel_ci_command.sh`,
`bazel_ci_dependency_list.sh`, or the Bazel module wiring that supports them,
preserve these invariants:

- CI can launch `task` without assuming a preinstalled repo-specific wrapper
- the `setup` job prepares the dependency state later Bazel CI jobs are
  expected to consume
- the checked-in dependency list matches the Bazel target patterns actually run
  by `bazel-ci.yml`
- cache-prefetch behavior stays focused on external repositories and does not
  start depending on developer-specific workspace state

Validate changes proportionally:

- run `./scripts/test_run_task_launcher.sh` when changing `run_task.sh` or its
  Bazel bootstrap path so the launcher policy and working-directory behavior are
  still covered
- run the affected Bazel tasks through their normal entrypoints (for example
  `task bazel-check-metadata`, `task bazel-cache-ci-build-dependencies`, and
  the relevant `task bazel-test-*` targets) so the checked-in dependency list,
  bootstrap target, and cache-preparation flow still agree
- if you change which Bazel CI commands or target patterns the workflow runs,
  update `scripts/bazel_ci_dependency_list.sh` in the same change rather than
  letting CI discover the mismatch later
- if you change `MODULE.bazel` or `MODULE.bazel.lock`, rerun the normal Bazel
  metadata workflow and confirm the setup path still reaches repo tools and
  external repos without traversing unintended local workspace state

The GitHub Actions Bazel workflow also defines a single aggregate job,
`bazel-required`, that depends on the other jobs in the workflow via
`needs`.  Branch protection can require that one workflow-level job
instead of tracking each underlying Bazel job separately. This reduces
required-check maintenance to the workflow level.

If the `setup` job fails its metadata check in CI, rebase or merge the target
branch, run `task bazel-generate-metadata`, commit the resulting changes, and
rerun CI.

### Apple CommandLineTools

On macOS, `.bazelrc` defaults to using the Apple CommandLineTools
installed under `/Library/Developer/CommandLineTools`. This is the
default location for the tools installed without Xcode, and the
location used by GitHub Actions runners.

For most usage, these defaults should be sufficient. If a machine uses
Xcode or a non-default Apple developer toolchain location, the
defaults can be overridden via `.bazelrc.local` which is optionally
imported by `.bazelrc`. `.bazelrc.local` is intended to be generated
via `task bazel-configure-local`, which runs
`./scripts/generate_bazelrc_local.sh` under the repo's standard task
entrypoint. The script uses `xcode-select -p` and `xcrun --sdk macosx
--show-sdk-path` to determine the host's active Apple developer
directory and macOS SDK and writes those values to `.bazelrc.local`.
The script can also be run directly and is invoked automatically by
direnv.

When invoked by direnv, generation is best-effort: failures are shown
as warnings during shell entry but do not prevent entering the repo.
When run directly, the script exits non-zero on discovery failures so
manual setup problems remain actionable.

## Adding a New Go Module

When adding a new Go module under `graft/`:

1. **Create the module's go.mod and add to go.work**:
   ```
   go work use ./graft/newmodule
   ```

2. **Create the module's root BUILD.bazel** with the gazelle prefix:
   ```python
   # graft/newmodule/BUILD.bazel
   # gazelle:prefix github.com/ava-labs/avalanchego/graft/newmodule
   ```

3. **Generate Bazel metadata**:
   ```bash
   task bazel-generate-metadata
   ```

Dependencies are resolved automatically via `go.work`.

## Troubleshooting

### "no such package" or import errors

Regenerate Bazel metadata:
```bash
task bazel-generate-metadata
```

### Missing external dependency

1. Check if it's in `go.mod` - if not, add it
2. Run `task go-mod-tidy` (this runs `go mod tidy` in all modules,
   syncs the workspace, and runs `bazel mod tidy`)

### CGO compilation errors

Check `.bazelrc` for required CGO flags. CGO is enabled via
`--action_env=CGO_ENABLED=1`.

If you see errors related to CGO dependencies (blst, gnark-crypto,
secp256k1), verify that:
1. Patches in `.bazel/patches/` are applied correctly in `MODULE.bazel`
2. The `gazelle_override` with `build_file_generation = "off"` is set
   for packages with custom BUILD files

### gnark-crypto assembly errors

If you see errors like:
```
#include: open /field/asm/element_4w/element_4w_arm64.s: no such file or directory
```

This indicates the gnark-crypto assembly patch isn't being applied. Check:
1. The patch file exists: `.bazel/patches/com_github_consensys_gnark_crypto_asm_includes.patch`
2. `MODULE.bazel` has the `module_override` applying the patch
3. Try `task bazel-clean-all` to clear cached BUILD files

### Build cache issues

Try a full cache clean:
```bash
task bazel-clean-all
```

If builds still fail after cleaning, check if `MODULE.bazel.lock` needs regenerating:
```bash
rm MODULE.bazel.lock
task bazel-mod-tidy
```

### "duplicate target" errors

Usually means gazelle created a target that conflicts with a
manually-defined one. Check for custom content in the BUILD.bazel
file.

## CGO Configuration

CGO is enabled via environment variables in `.bazelrc`:

```
build --action_env=CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"
build --action_env=CGO_ENABLED=1
```

Dependency-specific CGO issues are handled via patches (see
Patched Dependencies above).

## Version Stamping

The main binary includes Git commit information via `x_defs` in
`main/BUILD.bazel` (see BUILD.bazel Files with Custom Content above
for the `# keep` annotation pattern).

Stamping is enabled via the `release` config in `.bazelrc`:

```bash
bazel build //main:avalanchego                    # Dev build (no stamp, cached)
bazel build --config=release //main:avalanchego   # Release build (stamped)
```

## Known Limitations

1. **gnark-crypto sandbox relaxation** - The `no-sandbox` tags on bls12-381
   targets (see "Sandbox relaxation" strategy above) mean those compilations
   are not hermetically sandboxed. This is acceptable for a pinned dependency
   but incompatible with remote execution.

2. **Go version sync** - Go version must be kept in sync across
   `go.work`, `go.mod` files, and `nix/go/default.nix`. Bazel reads
   the version from `go.mod` via `go_sdk.from_file()`, so
   `MODULE.bazel` doesn't need separate updating. Use
   `task update-go-version -- <version>` to update all files except
   nix (which requires SHA changes). CI enforces consistency via
   `task check-go-version`.

## Future Improvements

The following improvements are planned or under consideration:

### Remote Caching and Execution

Remote caching would enable:
- Cache sharing between CI runs
- Faster builds for new team members
- Cross-machine cache reuse

`go_sdk.download()` enables remote execution since the Go toolchain is
hermetic and reproducible.

Implementation: Add BuildBuddy, Buildkite or similar remote cache service.

### CI Integration

- **Migrate CI to Bazel** - Update GitHub Actions to use Bazel for
  builds/tests where possible
- **Retain non-Bazel coverage** - Keep traditional `go build` CI jobs
  to support third-party consumers who don't use Bazel

### Patch Maintenance

Patches that create new BUILD files (blst, firewood, libevm) are
generated from readable BUILD files in `.bazel/patches/build_files/`.
This avoids hand-maintaining patch hunk line counts, which Bazel's
internal patch parser is strict about (unlike `git apply`).

**To modify a patch:**

1. Edit the BUILD file in `.bazel/patches/build_files/<module>/`
2. Run `task bazel-generate-patches` to regenerate `.patch` files
3. Verify: `./scripts/nix_run.sh bazelisk build @<module>//<target>`
   (or plain `bazelisk build @<module>//<target>` when the host already has the
   required tools available)
4. Commit both the BUILD file and the generated `.patch` file

Patches that modify existing files (e.g., gnark-crypto's `no-sandbox`
tag) are maintained manually and are not generated by this script.

**Periodic review:**
- Check if upstream projects have improved Bazel support
- Test patches against dependency updates
- Consider upstreaming BUILD files where feasible
- Monitor [rules_go#3636](https://github.com/bazel-contrib/rules_go/issues/3636) for
  proper cross-package asm include support (would remove gnark-crypto's sandbox relaxation)

### Test Configuration

Graft modules already have custom test timeouts via `gazelle:map_kind`
(see "Custom Test Macros" section). Note that `gazelle:map_kind`
applies to all `go_test` targets in the subtree, including non-unit
tests. This is harmless today since Bazel only runs unit tests, but
may need revisiting if non-unit tests move to Bazel. Consider further
stratification:
- Integration tests: medium timeout (300s)
- E2E tests: explicit long timeout (currently use `manual` tag)

## References

- [rules_go documentation](https://github.com/bazelbuild/rules_go)
- [Gazelle documentation](https://github.com/bazelbuild/bazel-gazelle)
- [Bazel Go Tutorial](https://bazel.build/start/go)
