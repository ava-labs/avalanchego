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

For the current CI benchmark findings around remote caching and impacted-target
execution, see [docs/bazel-remote-cache-benchmark.md](./bazel-remote-cache-benchmark.md).


```bash
# Build the main binary
task bazel-build

# Build with optimizations
task bazel-build-opt

# Run unit tests
task bazel-test

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

| Tool | Version | Pin Mechanism | Rationale |
|------|---------|---------------|-----------|
| Bazel | 8.0.1 | `.bazelversion` + bazelisk | Current LTS with native bzlmod support |
| Go | 1.25.10 | `go.mod` via go_sdk.from_file | Single source of truth |
| rules_go | 0.57.0 | `MODULE.bazel` | Go 1.25+ support (compiles `pack` from source) |
| gazelle | 0.45.0 | `MODULE.bazel` | Compatible with rules_go 0.57.0 |

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

By default, `bazel test` matches `scripts/build_test.sh` behavior,
with a few exceptions:

- The script passes `-tags test` to `go test`; currently there are no
  `//go:build test` files in this repo, so it has no effect.
- The script excludes several directories via `go list | grep -v ...`;
  Bazel instead relies on `tags = ["manual"]` to keep non-unit tests
  out of `bazel test //...`.

```bash
# Run all unit tests (shuffle enabled, race on)
task bazel-test                    # or: bazel test //...

# Run tests for a specific package
bazel test //utils/...

# Run specific test functions (target:test_name + filter)
bazel test //utils:set_test --test_filter=TestSet_Add

# Fast local iteration (no race, no shuffle)
task bazel-test-fast               # or: bazel test --config=fast //...

# Collect coverage
bazel coverage //...

# Run E2E tests (requires built binary)
task bazel-test-e2e
```

#### Test Options

| Option | Default | Toggle with |
|--------|---------|-------------|
| Race detection | ON | `--config=norace` (disable) |
| Shuffle | ON | `--config=noshuffle` (disable) |
| Fast mode | - | `--config=fast` (no shuffle, no race) |

Examples:
```bash
# Disable race detection
bazel test --config=norace //...

# Disable shuffle only
bazel test --config=noshuffle //...

# Fast mode (no shuffle, no race)
bazel test --config=fast //...
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

### Diff-Aware Test Selection with Impacted Targets

An impacted-target tool compares two revisions of a Bazel workspace and reports
the set of **impacted targets**: Bazel targets whose definition or transitive
inputs are affected by the change between the two revisions. `bazel-diff` is
one candidate implementation of this model.

#### Intended architecture: diff-range-driven selective execution

Selective Bazel test execution should be treated as a layer **above** the
ordinary Bazel test targets, not as a replacement for them.

- The ordinary Bazel targets remain the source of truth for what can be built
  and tested.
- A higher-level selective-testing layer determines which subset of those test
  targets is relevant for a particular git diff.
- Local developers can still run the full Bazel target sets directly when they
  want exhaustive validation.
- CI and local workflows can use the selective-testing layer to reduce work when
  only part of the graph is affected.

The intended user-facing interface for this layer is **`targeted-bazel`**. It
accepts normal Bazel-style invocations and only changes behavior when diff-aware
selection is enabled. In other words:

- without a diff input, `targeted-bazel ...` should behave like plain `bazel ...`
- with a diff input, `targeted-bazel --diff <range> ...` should narrow the
  requested Bazel target set to the impacted subset selected by policy

This keeps the selective layer close to ordinary Bazel usage instead of forcing
callers to learn a separate partition-specific command shape.

`targeted-bazel` is the intended command surface. The lower-level impacted-target
machinery exists to support that behavior and may evolve, but task entrypoints
and user-facing documentation should center `targeted-bazel` rather than the
implementation details behind it.

The core abstraction for this layer is a **git diff range**.

Examples:

- `origin/master..HEAD` - committed changes on the current branch
- `origin/master..` - committed changes plus the current working tree
- `HEAD^..` - the previous revision through the current working tree

Open-ended ranges are interpreted as comparing the left-hand revision against
`HEAD` plus any staged and unstaged working-tree changes.

The intended responsibility split is:

- **Outside Bazel:** choose the git diff range to analyze.
  - Examples: CI may choose a branch-delta range for a rebased PR branch, while
    a developer may choose `origin/master..` locally.
  - This is workflow policy, not impacted-target computation.
- **Inside Bazel:** given a chosen diff range, compute impacted labels, filter
  them to the relevant partition, and optionally execute the selected tests.

The main reason to move more of this mechanism into Bazel is not that manifest
computation is expensive or that caching the manifest is especially important.
The value is that Bazel gives the mechanism a more explicit structure:

- named inputs
- a named derived artifact
- a clear consumer of that artifact

In other words, the selective-testing layer should be understandable as:

```text
diff range + current workspace + selector machinery
  -> selected-test manifest
  -> test execution
```

This makes the interface explicit instead of leaving it as workflow glue hidden
inside scripts. The important derived artifact is the **selected-test
manifest**. If selector machinery changes, the manifest must be recomputed. If
that recomputation produces the same manifest, the required test execution does
not change. If it produces a different manifest, the selected tests change.

This separation keeps the contract clear:

- the caller decides **what diff range** should be analyzed
- the Bazel-owned selective-testing layer decides **which Bazel targets** that
  diff range impacts
- the ordinary Bazel test targets remain directly runnable without selective
  filtering

Example output is a newline-delimited list of Bazel labels:

```text
//utils:go_default_library
//utils:go_default_test
//network/p2p:go_default_library
```

This is different from plain `bazel test //...` behavior:

- Bazel executes targets you ask it to build or test.
- An impacted-target tool helps decide **which** targets are relevant for a given git diff.
- Repo scripts/tasks can then filter the impacted target set down to the test
  targets or CI partition they care about.

For Go in particular, the useful granularity is usually the Bazel target, not an
individual `TestXxx` function. A change to a package's production code may
impact that package's library target, its test target, and downstream test
targets that depend on it. A change to only `*_test.go` files should usually
impact that package's test target without necessarily impacting downstream
consumers of the package's library target.

#### Local-first workflow

Diff-aware selection must be reproducible locally, but there are two useful
comparison modes and they answer slightly different questions:

1. **Local branch-delta mode** compares the branch's current checkout against
   its fork-point with the base branch (usually `git merge-base origin/master
   HEAD`). This answers: "what changed on this branch since it diverged?"
2. **Pull-request merged-result mode** compares the current base-branch tip
   against the synthetic merge commit that GitHub creates for `pull_request`
   workflows. This answers: "what changes when this PR is merged into the
   current base branch right now?"

The intended local workflow is therefore:

1. choose the comparison mode that matches the question you are asking
2. choose a git diff range for that mode (for local branch-delta work this is
   usually the branch fork-point through the current checkout or working tree)
3. run the normal Bazel command through `targeted-bazel`
4. let the selector narrow that requested target set when a diff is provided
5. otherwise let the command pass through unchanged as ordinary `bazel`

For example:

```bash
# Plain bazel-style execution
 targeted-bazel test //graft/coreth/... //graft/evm/...

# Diff-aware execution against the same requested Bazel scope
 targeted-bazel --diff origin/master.. test //graft/coreth/... //graft/evm/...
```

In the current rollout, the same task entrypoints can be used in both places:

- **Locally:** if `BAZEL_IMPACTED_BASE_SHA` and `BAZEL_IMPACTED_DIFF_RANGE` are
  unset, `targeted-bazel` behaves like ordinary `bazel`.
- **In CI:** those env vars can be set to enable selective execution against a
  chosen diff range without changing the task shape.

This keeps local validation and CI behavior aligned without pretending that
branch-delta and merged-result comparisons are interchangeable.

#### Relationship to CI partitioning

The repository keeps separate Bazel CI jobs (`unit-main`, `unit-coreth`,
`unit-subnet-evm`, `e2e`) for wall-clock and resource reasons. Diff-aware test
selection is meant to reduce unnecessary work within that partitioned model,
not replace it with a single monolithic job. In practice this means a CI job may
use diff information to skip itself entirely or to run only the impacted test
targets within its partition.

#### Relationship to caching

Diff-aware selection and caching solve different problems:

- An impacted-target tool minimizes **scope** by telling us which targets are impacted by a
  change.
- Bazel's local and remote caches minimize **re-execution** by reusing results
  for selected targets whose inputs have already been validated.

Local development already benefits from a persistent on-disk cache. CI can gain
similar benefits from a remote cache, which becomes more important as the repo's
build graph grows to include more expensive toolchains and dependencies.

#### Trust boundary: in-graph vs out-of-graph inputs

Affected-target analysis is only trustworthy to the extent that the relevant
inputs are visible to Bazel's dependency graph.

- **In-graph inputs** are candidates for precise affected-target analysis.
- **Out-of-graph inputs** are not safe to rely on for selective CI skipping.

For this repo, examples of likely in-graph inputs include:

- `*.go` and `*_test.go`
- `go.mod` and `go.work`
- `MODULE.bazel`
- `BUILD.bazel` and `.bzl`
- declared external dependencies
- Bazel-managed toolchains and platform/configuration inputs

Examples of likely out-of-graph inputs unless explicitly modeled include:

- GitHub-hosted runner image drift
- undeclared environment variables
- user/system bazelrc files
- host-installed tools that are not provided by Bazel or the repo's pinned nix
  environment
- host OS / Xcode / CommandLineTools changes unless they are treated as explicit
  CI environment identity inputs

The operating rule is conservative: if an important input is outside the graph,
CI should run more, not less. False positives (running extra tests) are
acceptable. False negatives (skipping tests that should have run) are not.

#### Environment identity and CI trust

Selective CI decisions depend not only on source inputs, but also on the
identity of the execution environment used to validate them.

For Linux CI, this may be represented by a pinned Docker image digest and/or a
pinned nix environment identity. For macOS CI, this may be represented by the
full macOS version together with relevant Apple toolchain identity (for example
Xcode or CommandLineTools version) and the runner class/label.

When that environment identity changes, prior selective-CI assumptions are not
automatically trusted. The conservative response is to re-establish a baseline
with broad/full CI before relying on selective skipping again for that
environment.

#### Authoritative baselines

A useful mental model is to treat successful base-branch runs as the
authoritative source of affected-target metadata for an immutable revision and a
specific execution environment identity.

PR runs may consume previously generated metadata only when the relevant inputs
match, including:

- base commit SHA
- Bazel version
- affected-target tool version
- execution environment identity

If any of those differ, recomputation or conservative fallback is required.
This is especially relevant after CI environment changes, where the first
successful authoritative run in the new environment establishes a new trusted
baseline.

#### Current behavior

- Pull-request runs for the three Bazel unit partitions (`unit-main`,
  `unit-coreth`, `unit-subnet-evm`) perform a **cheap selector step first
  inside each unit job** before paying for the repo's heavier Bazel task path.
- The job-facing selector boundary is intentionally a **single entry point per
  partition**. In current CI that selector is `select-bazel-tests`, and it is
  responsible for:
  - impacted-target computation
  - partition-local filtering to non-manual `go_test` targets
  - deciding whether the job should run in `full`, `skip`, or `selective` mode
- On GitHub-hosted runners, that selector step currently relies on:
  - `actions/setup-go` for host Go
  - preinstalled `bazelisk`
  - Bazel's configured JDK (queried via `bazel info java-home` when host `java`
    is not on `PATH`)
  - direct download/caching of the `bazel-diff` jar
- If a partition-local manifest is empty, that unit job exits successfully
  without running the heavier Bazel task.
- If the manifest is non-empty, the unit job currently falls through to the
  existing selective `targeted-bazel` task.
- If no trusted diff is available, or if selector computation fails, CI fails
  open to the full partition task rather than risking an unsafe skip.
- On `pull_request` workflows, GitHub checks out a synthetic merge commit by
  default. In that mode, impacted-target selection compares the current base
  branch tip (`github.event.pull_request.base.sha`) against that synthetic
  merge result. This intentionally answers "what is impacted by merging this
  PR into the current base branch?" rather than only "what changed on the PR
  branch since it forked?"
- Merge queue and other non-PR flows use the full partition task.
- Full `master` / postsubmit validation remains authoritative.
- The Bazel `e2e` job does not use impacted-target selection.
- Impacted-target tooling is not used as the sole detector of runner-image or
  other out-of-graph environment drift.

#### Workflow boundary for selective unit jobs

The current workflow intentionally does **not** compute impacted targets in a
separate upstream job and share them to the unit partitions via artifacts.
That shape was explored and is a plausible future optimization boundary, but it
is not the current default for one reason: at current CI economics, the fixed
per-job setup costs are too large relative to the computation being shared.

The important comparison is not just:

- "compute impacted targets once" versus
- "compute impacted targets three times"

It is:

- saved repeated impacted-target computation versus
- added cost from an extra job boundary, checkout, lightweight host setup,
  dependency serialization, and artifact upload/download handoff.

In practice, the deciding factor was that the fixed setup cost of a separate
compute job was of similar magnitude to the impacted-target computation it was
trying to amortize. Under those conditions, introducing an upstream
`compute-impacted-targets` job added complexity and dependency latency without a
clear wall-clock win.

That is why the current integration boundary is a **single selector step per
unit job** rather than a shared impacted-target artifact lifecycle. This should
not be interpreted as claiming that impacted-target computation and
partition-local filtering are conceptually inseparable. They are distinct
operations, and it is still reasonable to separate them internally for
benchmarking, diagnostics, or implementation clarity. The point is only that
**the workflow-level API** for the unit jobs is intentionally unified.

Future maintainers should reconsider a separate shared compute job only if the
assumptions above change materially. In particular, revisiting that design is
reasonable if one or more of the following become true:

- fixed per-job setup costs (job startup, checkout, host setup) fall
  substantially
- impacted-target computation becomes substantially more expensive
- multiple workflow consumers genuinely need the same computed target set
- artifact sharing becomes cheaper than repeated per-job computation
- measurements show a clear wall-clock or cost win from a shared compute job

The selector path used by CI should also remain locally reproducible. CI-specific
plumbing should stay thin, and core selector behavior should continue to live in
repo code, scripts, or tools rather than only inside GitHub Actions-only
abstractions.

Validation for this path is intentionally split across layers:

- logic/unit coverage for `tools/impactedtests`
- a dedicated Bazel integration test/job for the selector integration path
- exclusion of `tools/impactedtests` from the legacy `go test ./...` unit sweep
- a Bazel `manual` tag on the integration target so broad Bazel test sweeps do
  not pull it in accidentally

#### Conservative rollout policy

Affected-target tooling should be introduced gradually:

1. local observability first (`print` impacted targets)
2. validate narrow repo-specific scenarios
3. apply to one CI partition
4. use selective execution for pull-request runs first
5. keep merge queue and `master`/postsubmit on full partition runs while confidence is being established
6. fail open to broader execution when uncertainty comes from selective-test computation (for example impacted-target calculation or partition query failure)
7. expand scope only after confidence is earned

The near-term goal is not perfect selective CI for every Bazel change. The goal
is a conservative, understandable rollout that reduces unnecessary work without
weakening merge safety.

Current selector timings on a fast local machine are on the order of ~9-12s per
invocation. On GitHub-hosted runners, after moving selection ahead of Nix, the
empty-manifest Bazel unit jobs are roughly:

- Linux: ~1 minute
- Darwin: ~2 minutes

That is a large improvement over the previous multi-minute Nix/bootstrap path,
but it is not "free" yet. The remaining cost is mostly selector work itself
(`go run`, Bazel startup/query work, and `bazel-diff` hash generation), not Nix.

#### What is and is not worth optimizing next

The biggest win already landed was: **do not pay Nix/bootstrap for empty PR unit
jobs**.

For the current GitHub-hosted runner setup, the following are usually **not**
the most interesting optimization targets:

- manually installing Bazelisk (it is already preinstalled)
- manually installing Java (it is already preinstalled)
- swapping `go run` for downloaded selector binaries without evidence that Go
  startup is the dominant cost
- Bazelizing the selector tools purely as a latency optimization; that may
  actually be counterproductive for a pre-Bazel decision step

The more promising next optimizations are:

- reuse authoritative base-branch selector artifacts or base hashes instead of
  recomputing both sides on every PR job
- cache or pre-provision `bazel-diff`
- reuse the already-computed partition manifests on the non-empty path instead
  of selecting twice
- look for further selector cost reductions only after measuring the current
  per-job selector path

#### PR minimization vs authoritative merge validation

A useful interim policy is:

- **Pull-request runs** may use impacted-target selection to reduce latency and cost.
- **Merge queue and `master` / postsubmit runs** continue to run the full partition.

This preserves an important safety property: no change is merged and then left
without a full run in the authoritative CI environment. It also limits the risk
from environment drift or other out-of-graph inputs. If a runner image,
toolchain, or other CI dependency changes in a way that the affected-target tool
does not model, a full postsubmit run on `master` will still detect the breakage
promptly.

Under this policy, the affected-target mechanism is responsible only for being a
useful, conservative selector for PR-side Bazel-graph changes. It is not solely
responsible for guaranteeing long-term branch health in the presence of CI
environment drift; full postsubmit runs retain that role.

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

In GitHub Actions, the `check-metadata`, `unit-main`, `unit-coreth`, and
`unit-subnet-evm` jobs run their repo-defined tasks directly via
`./scripts/run_task.sh ...` without installing Go or Nix first. Those
jobs use the local `./.github/actions/setup-bazel-repository-cache`
composite action to restore a GitHub-backed Bazel repository cache, then
rely on `run_task.sh` falling back to the Bazel-owned `//tools/external:task`
target when `task` is not already on `PATH`. The per-platform
`check-metadata` job is the designated cache writer and follows the metadata
check with `bazel fetch //...` so downstream shards can restore a cache that
already contains the full external dependency set for that platform. This
keeps repo-tool bootstrapping and dependency caching inside Bazel for the
lightweight Bazel CI shards. The E2E Bazel job uses the same repository-cache
setup before its heavier test wrapper.

That check includes the Bazel module metadata files, so lockfile drift is
caught in the metadata phase rather than showing up later as a surprising
working-tree mutation.

### Remote Cache Benchmark

The repo also includes `task bazel-benchmark-remote-cache`, a CI-style local
benchmark for separating two distinct cache layers that are easy to conflate:

- **Repository cache** (`bazel fetch --all`) for external dependency materialization
- **Remote action cache** (`bazel-remote`) for reusing target build/test results

The benchmark intentionally measures them in separate phases:

1. **Setup**: run one measured setup command, currently `bazel fetch --all`,
   into an otherwise empty temporary `repository_cache`
2. **Per-command benchmark**: for each configured Bazel command, run fresh
   output-base measurements against that populated `repository_cache`

The benchmark is also intended to answer a second question: whether
realistic cache-server latency changes the tradeoff between HTTP and gRPC
remote caching enough that gRPC becomes necessary.

To keep that question separate from transport guesswork, representative
cache latency is measured before the benchmark from a public AWS us-east-1
regional endpoint. The current approximation uses the repo-local
`//tools/measure-http-latency` tool in `--mode=warm-h2`, which reuses a
single connection and measures request-write to first-response-byte latency.
That is a better no-server proxy for persistent gRPC behavior than cold
TTFB, because it avoids repeatedly charging TCP/TLS setup costs that a warm
channel would not pay.

The harness now also validates that input against a local proxied path
before the measured Bazel runs begin: it starts `bazel-remote`, starts
`toxiproxy`, applies the measured latency to a proxied HTTP endpoint, and
checks that the observed proxied warm reused-connection latency is within
`BAZEL_REMOTE_CACHE_LATENCY_TOLERANCE_MS` of the measured target. This
validation is skipped when `BAZEL_REMOTE_CACHE_LATENCY_MS` is set, because
override mode is intended for fast local iteration rather than provenance.

The benchmarked HTTP cache traffic itself now also flows through that
latency-injecting proxy path for the cold and warm remote-cache runs, so the
current HTTP results include the modeled representative cache distance. The
next iteration is to add the parallel gRPC comparison under the same latency
model.

This is still an approximation of runner-to-us-east-1 network distance,
not a claim that the public endpoint exactly matches the latency
characteristics of a future EKS-hosted `bazel-remote`. It should be treated
as a better steady-state transport proxy than cold-connect TTFB, not as a
perfect simulation of a deployed cache service. Measuring against a real
deployed cache service is the next planned refinement.

For fast local iteration, the benchmark also supports an explicit latency
override via `BAZEL_REMOTE_CACHE_LATENCY_MS`. When that override is set,
the benchmark skips live measurement and any future proxy-path validation.

The intended cached comparison for each configured command is:
- no remote cache
- HTTP remote cache, cold
- HTTP remote cache, warm
- gRPC remote cache, cold
- gRPC remote cache, warm

Fresh `--output_base` values are important because the benchmark is trying to
model a CI worker that can reuse downloaded external dependencies while still
starting each measured target command without local action/output state from a
prior invocation.

For local iteration, the generalized harness now reuses setup-side dependency
state by default across runs via a persistent cache root under
`~/.cache/av-bazel-remote-cache-benchmark/` (or `$XDG_CACHE_HOME` when set).
That persistence applies only to `repository_cache` and the shared Gazelle
`GOMODCACHE`, not to the benchmarked remote-cache contents. Set
`BAZEL_REMOTE_CACHE_FRESH_SETUP_CACHE=1` to force a fully fresh setup-cache
run.

The current generalized task intentionally avoids `fetch --all` because that
can pull in irrelevant transitive repos and toolchains unrelated to the
benchmarked jobs. Instead, its setup phase hard-codes the current union of
benchmarked target patterns:

- `bazel fetch //main:avalanchego //... -- -//graft/...`

TODO: keep that setup fetch union aligned with the benchmark command list until
this exploratory harness either stabilizes enough to derive it mechanically or
is replaced.

The current generalized task benchmarks:

- `bazel build //main:avalanchego`
- `bazel build --config=race //main:avalanchego`
- `bazel test //... -- -//graft/...`

The fast `task bazel-benchmark-remote-cache-ids-test` smoke target remains
useful for quick harness validation. It now reuses the same generalized
benchmark script with a narrower `fetch //ids:ids_test` + `test //ids:ids_test`
configuration and extra warm-log assertions proving cached test-result reuse.

Initial local validation on one developer workstation produced:

- setup `fetch --all`: `46.663s`
- `build //main:avalanchego`
  - no-cache: `57.330s`
  - cold-remote-cache: `60.668s`
  - warm-remote-cache: `46.075s`
- `build --config=race //main:avalanchego`
  - no-cache: `56.862s`
  - cold-remote-cache: `61.052s`
  - warm-remote-cache: `29.980s`

Those numbers are host-specific and should not be treated as stable targets;
what matters is the shape of the comparison. The benchmark now also includes
`bazel test //... -- -//graft/...` so the same CI-style remote-cache comparison
can answer whether unchanged main-module unit-test work is avoidable across
fresh-output-base runs. That is the relevant signal when deciding how much value
remote caching provides before introducing more targeted change-detection
optimizations.

The Bazel CI workflow therefore includes a non-required observational benchmark
job on the Linux amd64 and macOS arm64 runners. It uploads the full benchmark
log as an artifact so changes can be compared across branches and runner
configurations.

For the planned Firewood-from-source comparison, keep the experiment matrix
explicit and compare like-for-like runs on the same CI runner class:

- baseline branch vs Firewood-from-source branch
- no remote cache vs cold remote cache vs warm remote cache
- normal build vs race build vs main-module unit tests
- identical benchmark task and `fetch --all` setup phase

That makes the resulting benchmark logs actionable when deciding whether added
build work (for example, building Firewood from source) increases the payoff of
remote caching enough to justify CI complexity, and whether HTTP remains good
enough under realistic cache latency or whether gRPC provides enough additional
benefit to justify requiring it.

The GitHub Actions Bazel workflow also defines a single aggregate job,
`bazel-required`, that depends on the other jobs in the workflow via
`needs`.  Branch protection can require that one workflow-level job
instead of tracking each underlying Bazel job separately. This reduces
required-check maintenance to the workflow level.

If `check-metadata` fails in CI, rebase or merge the target branch, run
`task bazel-generate-metadata`, commit the resulting changes, and rerun CI.

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
