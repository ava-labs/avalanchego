# Bazel Build System

This document explains how Bazel is configured and used in the
avalanchego monorepo.

## Table of contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Why Bazel?](#why-bazel)
- [Architecture Overview](#architecture-overview)
  - [Toolchain Strategy](#toolchain-strategy)
  - [Go dependency preparation](#go-dependency-preparation)
  - [Version Pinning](#version-pinning)
  - [External-dependency fetches](#external-dependency-fetches)
  - [Why Bazel 8?](#why-bazel-8)
  - [Multi-Module Structure](#multi-module-structure)
  - [Key Configuration Files](#key-configuration-files)
  - [BUILD.bazel Files with Custom Content](#buildbazel-files-with-custom-content)
- [Gazelle](#gazelle)
  - [Where Gazelle Comes From](#where-gazelle-comes-from)
  - [When to Run Gazelle](#when-to-run-gazelle)
  - [How Gazelle Handles Multiple Modules](#how-gazelle-handles-multiple-modules)
  - [Custom Test Macros via `gazelle:map_kind`](#custom-test-macros-via-gazellemap_kind)
- [External Dependency Handling](#external-dependency-handling)
  - [Go Dependencies](#go-dependencies)
  - [Patched Dependencies](#patched-dependencies)
    - [Patching strategies](#patching-strategies)
    - [libevm (secp256k1)](#libevm-secp256k1)
    - [firewood-go-ethhash FFI](#firewood-go-ethhash-ffi)
    - [blst (BLS Signatures)](#blst-bls-signatures)
    - [gnark-crypto (BLS12-381 for KZG)](#gnark-crypto-bls12-381-for-kzg)
  - [Protocol Buffers](#protocol-buffers)
- [Common Tasks](#common-tasks)
  - [Building](#building)
  - [Testing](#testing)
    - [E2E and monitored test runners](#e2e-and-monitored-test-runners)
    - [Go Test Selection](#go-test-selection)
    - [Test Options](#test-options)
    - [Test Timeouts](#test-timeouts)
    - [Non-Unit Tests and the `manual` Tag](#non-unit-tests-and-the-manual-tag)
  - [Maintenance](#maintenance)
- [Bazel CI External Dependency Caching](#bazel-ci-external-dependency-caching)
  - [Why this exists](#why-this-exists)
  - [Test platforms and cache policy](#test-platforms-and-cache-policy)
  - [Why the remote cache uses gRPC](#why-the-remote-cache-uses-grpc)
  - [What is cached](#what-is-cached)
  - [Cache key](#cache-key)
  - [Checked-in Bazel dependency list](#checked-in-bazel-dependency-list)
  - [Enforcement](#enforcement)
  - [Changing this safely](#changing-this-safely)
  - [Apple CommandLineTools](#apple-commandlinetools)
  - [The macOS C compiler](#the-macos-c-compiler)
- [Adding a New Go Module](#adding-a-new-go-module)
- [Troubleshooting](#troubleshooting)
  - ["no such package" or import errors](#no-such-package-or-import-errors)
  - [Missing external dependency](#missing-external-dependency)
  - [CGO compilation errors](#cgo-compilation-errors)
  - [gnark-crypto assembly errors](#gnark-crypto-assembly-errors)
  - [Build cache issues](#build-cache-issues)
  - ["duplicate target" errors](#duplicate-target-errors)
- [CGO Configuration](#cgo-configuration)
- [Version Stamping](#version-stamping)
- [Known Limitations](#known-limitations)
- [Future Improvements](#future-improvements)
  - [Remote Execution](#remote-execution)
  - [CI Integration](#ci-integration)
  - [Solidity builds and binding generation](#solidity-builds-and-binding-generation)
  - [Patch Maintenance](#patch-maintenance)
  - [Test Configuration](#test-configuration)
- [References](#references)

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

# Run unit tests
task bazel-test-unit

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

[`MODULE.bazel`](../MODULE.bazel) uses `go_sdk.from_file()` to select a managed
Go toolchain from `go.mod`. Bazel uses this toolchain to compile Go targets.
These builds do not require a separately installed Go compiler. Commands outside
Bazel can still require Go on `PATH`.

**Approaches considered:**

| Approach | Pros | Cons |
|----------|------|------|
| `go_sdk.from_file()` (chosen) | Reproducible, single source of truth via go.mod, no nix required | None significant |
| `go_sdk.download()` | Explicit version in MODULE.bazel | Go version must be synced manually across go.mod files |
| `go_sdk.host()` | Uses system Go | Requires Go on `PATH`; depends on host setup |
| `rules_nixpkgs_go` | Bazel calls Nix directly, fully hermetic | **Incompatible with rules_go v0.57+** (toolchain API mismatch) |

> **Note:** rules_nixpkgs_go v0.13.0 proved incompatible with rules_go
> v0.56.0+. The rules_go toolchain API changed in ways that
> rules_nixpkgs_go doesn't support. See:
> https://github.com/tweag/rules_nixpkgs/issues/667

### Go dependency preparation

Bazel builds need both a toolchain and dependency sources:

1. Gazelle's `go_deps` extension reads the Go workspace metadata.
2. Gazelle's `go_repository` rules use a Go module cache to obtain dependency
   modules and prepare external source repositories.
3. Bazel compiles those repositories with its managed Go toolchain.

[`setup-bazel`](../.github/actions/setup-bazel/action.yml) configures Gazelle's
shared cache at `$HOME/.cache/bazel-go-repository-modcache`. Its [configuration
helper](../.github/actions/setup-bazel/configure-bazelrc.sh) sets `GOMODCACHE`
through `--repo_env` and enables `GO_REPOSITORY_USE_HOST_MODCACHE=1`. In this
Gazelle setting, `HOST_MODCACHE` refers to the module cache available to
Gazelle's repository rules, not to a host Go compiler.  Without these settings,
Gazelle keeps module downloads in each Bazel work area.  The CI path is not a
default for all local Bazel commands.

See [Go toolchains and dependency caches](./go-toolchains-and-caches.md) for
cache boundaries, preparation order, and the limits of download restrictions.

### Version Pinning

This repo keeps version pins in the checked-in configuration consumed by the
relevant tooling rather than duplicating them in documentation:

- Bazel: `.bazelversion`
- Go: `go.mod` (read by `go_sdk.from_file()`)
- Bazel modules such as `rules_go` and Gazelle: `MODULE.bazel`

When checking or updating a version, use those files as the source of truth.

### External-dependency fetches

Bazel CI uses the Gazelle `go_deps` extension to read `go.work` for workspace
modules and the external repositories they import.

`MODULE.bazel` intentionally omits `use_repo` bindings for
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

Use the Bazel unit-test tasks to select Go test rules and exclude manual tests.

```bash
# Run all cacheable Go unit tests
task bazel-test-unit

# Run all cacheable Go unit tests with race detection
task bazel-test-unit-race

# Run all Go unit tests with race detection and shuffle
task bazel-test-unit-race-shuffle

# Run a specific test target
bazel test //utils:set_test --test_filter=TestSet_Add

# Collect coverage
bazel coverage //...
```

#### E2E and monitored test runners

```bash
# Run all E2E tests with Bazel-built runtime dependencies
task bazel-test-e2e

# Run E2E tests with race detection
task bazel-test-e2e-race

# Run E2E tests serially with race detection
task bazel-test-e2e-race-serial

# Run E2E tests serially with Bazel-built runtime dependencies
task bazel-test-e2e-serial

# Run only the C-Chain ProposerVM API test
task bazel-test-e2e-smoke

# Run the existing-network E2E test
task bazel-test-e2e-existing-network

# Run the activation E2E tests
task bazel-test-e2e-post-latest
task bazel-test-e2e-schedule-latest

# Run the load, warp, and release-upgrade tests
task bazel-test-load-subnet-evm
task bazel-test-warp-subnet-evm
task bazel-test-load
task bazel-test-load-ci # Runs with the 30-second CI load duration
task bazel-test-upgrade
```

These tasks run the following test suites. The CI job name for each suite
follows its task name in parentheses.

- **Default E2E suite** (`bazel-test-e2e` and its race, serial, and smoke
  variants): runs the `tests/e2e` suite. It leaves the latest protocol upgrade
  unscheduled.
- **Existing-network test** (`bazel-test-e2e-existing-network`, job
  `e2e-existing-network`): runs an XSVM test twice against one network. The
  second run restarts and reuses the network. This test checks the
  network-reuse tooling.
- **Activation tests** (`bazel-test-e2e-post-latest` and
  `bazel-test-e2e-schedule-latest`, jobs `e2e-post-latest` and
  `e2e-schedule-latest`): run the default E2E suite with a different
  activation time for the latest protocol upgrade. That upgrade is active at
  genesis, or it becomes active four minutes after network start.
- **Load tests** (`bazel-test-load` and `bazel-test-load-ci`, job `load`):
  run the AvalancheGo load test.
- **Subnet-EVM load and warp tests** (`bazel-test-load-subnet-evm` and
  `bazel-test-warp-subnet-evm`, jobs `load-subnet-evm` and `warp-subnet-evm`):
  run the Subnet-EVM Ginkgo suites.
- **Release-upgrade test** (`bazel-test-upgrade`, job `upgrade`): upgrades a
  network from a published AvalancheGo release to the Bazel-built binary.

All suites except the default E2E suite are *monitored tests*. This name
identifies only the CI group that runs them. Each test in the group runs
through the
[`run-monitored-tmpnet-cmd` action](../.github/actions/run-monitored-tmpnet-cmd/action.yml),
which collects metrics and logs. The default E2E job also uses this action,
but it runs as a separate job.

`task bazel-test-e2e` runs `bazel run //tests/e2e:e2e_runner`. The
[`e2e_runner` target](../tests/e2e/BUILD.bazel) uses Bazel's Go toolchain to build
avalanchego, Ginkgo, the E2E test binary, and XSVM.
The [Bazel runner](../scripts/tests.e2e.bazel.sh) uses the
[shared E2E script](../scripts/tests.e2e.sh) to execute these prebuilt binaries.
Bazel can cache the binary builds. E2E test execution does not use cached test
results.

Bazel manages Go compilation for the load tests, but not Solidity compilation
or binding generation. Both `bazel-test-load` and `bazel-test-load-ci`
regenerate Solidity bindings before Bazel builds the load-test binary, so local
contract edits reach the test. The trade-off is that generation runs on every
task invocation, even when Bazel reuses all compiled outputs. Keep generation in
both tasks until Bazel manages the
[Solidity build and binding-generation pipeline](#solidity-builds-and-binding-generation).

Generation uses `solc` and Go outside Bazel through the Nix shell. Gazelle's
cache does not supply the generator's dependencies. CI therefore prepares the
separate Go module cache for the `load` jobs in both the `monitored` and
`monitored-race` groups.

The Subnet-EVM load and warp tasks use `GINKGO_LABEL_FILTER` to select tests. For
example, `GINKGO_LABEL_FILTER='!load'` excludes the load simulation. An explicit
`--ginkgo.label-filter` task argument overrides this environment variable.

The release-upgrade test uses the [upgrade script](../scripts/tests.upgrade.sh),
which downloads a published AvalancheGo release to upgrade from. The script
downloads that release on every run, even when Bazel reuses all compiled outputs.
This task requires access to GitHub release downloads; Bazel's caches do not
make it runnable offline. Without an argument, it uses the script's default
compatible release. To select a different compatible release, pass its version
without the `v` prefix:

```bash
task bazel-test-upgrade -- 1.15.1
```

Scheduled Bazel CI runs the default E2E suite serially on every scheduled
platform. It enables race detection only on Ubuntu 24.04 AMD64, the same
platform as the scheduled race unit-test run.

The `monitored` group runs all monitored tests without race detection. It runs
on pull requests, merge groups, pushes to `master` and `dev`, and tag pushes.

The `monitored-race` group runs in scheduled CI on Ubuntu 24.04 AMD64. It runs
the activation tests, both load tests, and the Subnet-EVM warp test with race
detection. The activation tests run serially to limit resource use.

Scheduled CI does not run the existing-network or release-upgrade tests. Race
detection would not add coverage for either test:

- The existing-network test checks the network-reuse tooling.
- The release-upgrade test restarts nodes over existing databases, waits for
  them to become healthy, and checks that a new node can bootstrap. The
  scheduled race E2E runs already cover those operations. Every tmpnet network
  start restarts its bootstrap node over an existing database, and the E2E
  suite checks bootstrapping. The only difference is that the release-upgrade
  test reads state that a published release wrote.

Add the release-upgrade test to the `monitored-race` group if AvalancheGo adds
code that runs only for state from an earlier release, such as a database
migration.

All scheduled Bazel jobs disable the remote cache.

The Subnet-EVM load simulator must complete 50,000 transactions. Its timeout is
five minutes without race detection and thirty minutes with race detection.
Race instrumentation can make that workload exceed five minutes on CI runners.
The race task increases only the time limit; it does not reduce the workload.
`SIMULATOR_TIMEOUT` supplies the limit to the simulator script. The CI job timeout
separately limits setup, builds, execution, and cleanup.

Run the additional scheduled configurations locally with:

```bash
task bazel-test-e2e-post-latest-race-serial
task bazel-test-e2e-schedule-latest-race-serial
task bazel-test-load-ci-race
task bazel-test-load-subnet-evm-race
task bazel-test-warp-subnet-evm-race
```

These tests use Bazel-built binaries so that CI can reuse compiled outputs. A
job that uses `go build` compiles AvalancheGo, plugins, and test binaries again
each time it runs. Bazel's remote cache shares individual outputs across jobs
and runs. To make these jobs faster, first look for ways to increase cache
reuse. For how this differs from a GitHub Actions cache archive, see
[Archive caches and content-addressable caches](./ci.md#archive-caches-and-content-addressable-caches).

[CI runs the default E2E suite and monitored tests](../.github/workflows/bazel.yml)
through the same tasks used locally. The Go workflow also runs the default E2E
suite with race detection to test artifacts built with `go build`. This follows the
[CI entrypoint conventions](./ci.md#principles) and avoids a separate artifact
lifecycle.

PR and scheduled CI select different monitored test groups. Disabled groups
still appear as skipped checks. The goal is to give those checks readable names,
not to eliminate skipped checks.

GitHub skips disabled jobs before it expands their matrices. A name such as
`${{ matrix.name }}` can then appear literally in the checks list. For this
reason, put the `if` condition on a calling job with a fixed name. Keep matrix
execution inside the called workflow.

The `monitored` and `monitored-race` jobs both call
[`bazel-monitored.yml`](../.github/workflows/bazel-monitored.yml).
Their fixed names keep skipped checks readable. When enabled, each group shows
a separate job for each test suite. Both groups share the setup, monitoring,
and execution steps.

Test selection stays in the callers so that each caller defines its test policy.
Each matrix entry specifies a suite name and task. It can also specify a runner
choice and network owner. The task selects the test configuration, including
race detection.

Reusable workflow inputs cannot accept lists or objects. Callers therefore pass
matrix data as a JSON string. The called workflow converts it with `fromJSON()`.
JSON is a consequence of passing matrices between workflows, not a requirement
for readable skipped-check names.

Defining YAML matrices in the callers would avoid JSON, but would lose readable
names for skipped jobs. Keeping both matrices inside the called workflow would
also avoid JSON and preserve readable names. However, it would move test
selection away from the callers.

The Bazel entrypoint uses `bazel run`, not `bazel test`. It keeps network data
available for inspection after a failure. Network data defaults to `$HOME/.tmpnet`.
Any sandboxed execution must preserve access to this data after a failure.

The XSVM plugin link lives under `build/bazel-e2e/plugins` to keep each
checkout's plugin state separate. This also avoids overwriting plugins from
non-Bazel builds.

#### Go Test Selection

`scripts/run_bazel_go_tests.sh` queries Bazel for `go_test` rules. It excludes
tests tagged `manual`. The unit-test tasks use this script for these scopes:

| Scope | Rules selected |
|-------|----------------|
| `all` | All non-manual Go test rules |
| `smoke` | The selected Go smoke test rule |

This selection prevents Go test flags from reaching non-Go tests such as
`gazelle_test`. Do not replace these tasks with `bazel test //...`.

#### Test Options

| Option | Default | Toggle with |
|--------|---------|-------------|
| Race detection | OFF | `--config=race` (enable) |
| Shuffle | OFF | `--config=race-shuffle` (enable) |

The `bazel-test-unit-race` task enables race detection and reuses cached test
results. The `bazel-test-unit-race-shuffle` task also shuffles tests and disables
result caching.

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
excludes them from wildcard commands such as `bazel test //...` and
`bazel build //...`; those commands should not select non-unit-test work.

Tags do not propagate through dependencies. Targets that exist only to build
or run a non-unit test, such as a test-binary `genrule` or an E2E `sh_binary`
runner, must also have `tags = ["manual"]`. Explicit commands, such as
`bazel run //tests/e2e:e2e_runner`, still select manual targets.

The Go unit-test script excludes equivalent directories during package
selection. See [`scripts/tests.unit.sh`](../scripts/tests.unit.sh).

**Tests with `manual` tag:**

| Test | BUILD.bazel Location |
|------|---------------------|
| Main E2E tests | `tests/e2e/BUILD.bazel` |
| Upgrade tests | `tests/upgrade/BUILD.bazel` |
| Bootstrap monitor E2E | `tests/fixture/bootstrapmonitor/e2e/BUILD.bazel` |
| Subnet-EVM warp tests | `graft/subnet-evm/tests/warp/BUILD.bazel` |
| Subnet-EVM load tests | `graft/subnet-evm/tests/load/BUILD.bazel` |

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

In GitHub Actions, Bazel jobs use the local `./.github/actions/setup-bazel`
composite action. The `lint-avalanchego` job also uses this action before it runs
`bazelisk mod tidy`. The action applies the shared runner disk guard described in
[CI disk space](./ci-disk-space.md) before it restores and prepares the cache. It
then restores the pinned Task binary with `setup-task`. Local developer use still
uses the Go-based Task bootstrap when Task is not on `PATH`.

See [Bazel CI External Dependency
Caching](#bazel-ci-external-dependency-caching) for the motivation,
cache-key design, checked-in Bazel dependency list, and [how CI checks the
list](#enforcement).

The metadata check includes the Bazel module metadata files, so lockfile drift
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

### Test platforms and cache policy

Bazel CI uses a small pre-merge platform set. GitHub-hosted runners can fail for
reasons outside the repository. A smaller job set reduces that risk.

Non-scheduled Bazel CI runs these jobs:

- Linux pre-merge CI runs the full cacheable unit-test suite, the default E2E
  suite, and the monitored tests.
- macOS pre-merge CI runs one cacheable unit-test smoke target and one focused
  E2E smoke test.

Pull requests and merge groups use the same Linux unit-test task,
`bazel-test-unit-race`. Both enable race detection and reuse test results from
the remote cache.

Pre-merge E2E and monitored tests run without race detection. For the reason,
see [Race detection and output caching](./ci.md#race-detection-and-output-caching).

See [E2E and monitored test runners](#e2e-and-monitored-test-runners) for build
and execution details.
See [E2E runner capacity](./ci.md#use-beefier-runners-for-faster-e2e-jobs) for the
shared runner-sizing policy.

The macOS smoke E2E job does not collect metrics or logs. Monitoring installs
Nix to provide Prometheus and Promtail. Nix installation adds too much time to
pre-merge macOS CI. The scheduled full E2E job collects metrics and logs. Both
jobs upload tmpnet data to help diagnose failures.

Previously, Bazel CI divided the full unit-test suite among three
component-specific jobs. These jobs ran in parallel to keep the pre-merge
runtime acceptable. Pre-merge tests now reuse results from the remote cache.
Only tests without reusable results need to run. This makes race detection
practical without separate jobs. Reconsider separate jobs if cache reuse declines
or one job makes the pre-merge runtime unacceptable.

Each setup job checks Bazel metadata. On `master`, a cache miss makes each setup
job prepare the CI dependency list. The job then disables downloads, checks
metadata, and saves the cache. Setup jobs can duplicate this work when they
share a key. On other refs, each Bazel-consuming job prepares its own non-exact
restore before it disables repository and module downloads. Those jobs do not
save the cache.

The [daily scheduled workflow](../.github/workflows/bazel-scheduled.yml) runs
full unit-test and E2E suites across its platform matrix. It
includes a race-detection and shuffled-order unit-test run. That run disables
test-result caching so it executes rather than reusing a cached random result.

The scheduled workflow also disables the remote cache. This provides daily
validation that does not depend on remote action or test results.

The remote cache stores results from the cacheable pre-merge unit tests when
CI provides the remote-cache URL and authorization header. This policy applies
only to Bazel CI. Go module version CI remains unchanged because it checks
compatibility for downstream consumers.

When you change the CI test set, update
`./scripts/bazel_ci_dependency_list.sh`. The list must include every target
pattern that `run_bazel_ci_command.sh` runs in CI.

### Why the remote cache uses gRPC

CI uses Bazel's gRPC remote-cache protocol instead of its HTTP protocol. This
choice limits the effect of a slow or degraded network path. It is not a claim
that gRPC is faster than HTTP in normal conditions.

For an HTTP remote cache, Bazel applies
[`--remote_timeout`](https://bazel.build/reference/command-line-reference#flag--remote_timeout)
as an inactivity timeout. Each received byte resets the timeout. A large
download can therefore continue for hours if the cache sends data very slowly.
[Bazel issue #11782](https://github.com/bazelbuild/bazel/issues/11782) describes
this difference between the HTTP and gRPC timeout behavior.

For a gRPC remote cache, Bazel applies `--remote_timeout` as a deadline for each
remote procedure call (RPC). Slow progress does not extend this deadline. Bazel
then uses
[`--remote_retries`](https://bazel.build/reference/command-line-reference#flag--remote_retries)
to limit the retries after the first attempt.

The CI setup currently sets a 60-second deadline and three retries. Thus, one
failed RPC can use approximately 240 seconds:

```text
60 seconds × (1 initial attempt + 3 retries) = 240 seconds
```

This value is an estimate, not a wall-clock limit for the Bazel command. A
command can make multiple RPCs. Retry delays and other build work also add time.
Use command and job timeouts to set a limit for the complete CI operation.

CI job timeouts bound the complete operation. The timeout values are configured
in `.github/workflows/bazel.yml` and
`.github/workflows/bazel-smoke.yml`. Scheduled jobs configure their longer
limits in `.github/workflows/bazel-scheduled.yml`. They run broader tests
without the remote cache. Bazel's test timeouts still limit each test process.
The job-level limits also cover loading, analysis, builds, downloads, retries,
and test setup.

Tests with Bazel 8.8.0 confirmed the expected behavior. An HTTP cache download
continued beyond 120 seconds when a proxy limited traffic to 1 KiB/s. The test
used `--remote_timeout=60s`, and the download continued to receive data. The
same limit caused a gRPC cache read to fail after its deadline. Direct TLS did
not change the gRPC deadline behavior.

This policy accepts a bounded cache failure instead of a CI job that makes very
slow progress for hours. In the gRPC test, Bazel reported `Missing digest` and
failed the build. It did not fall back to local execution. Retries can recover
from a temporary failure, but retry recovery under this throttling scenario has
not been confirmed.

Preserve these requirements when you change the remote-cache client settings:

- Use the `grpcs://` scheme for the CI cache URL.
- Set `--remote_timeout` and `--remote_retries` explicitly.
- Treat the timeout as a limit for each RPC, not for the complete build.
- Keep a separate timeout for the complete command or CI job.

Reconsider the transport only if Bazel changes the HTTP timeout behavior or if
the gRPC failure behavior no longer meets CI requirements. Test any change with
a throttled cache read. Confirm that useful but slow traffic cannot keep the CI
operation active without a limit.

This repository configures only the Bazel client. Cache-server and proxy
configuration are outside this repository's scope.

### What is cached

The Bazel CI cache setup configures four types of cached data:

- Bazel repository cache at `$HOME/.cache/bazel-repository-cache`, selected
  with `--repository_cache`
- shared Gazelle module cache; see [Go dependency preparation](#go-dependency-preparation)
- the Bazel binary in a versioned `~/.cache/bazelisk` subdirectory
- Bazel remote action and test-result data

GitHub Actions restores the repository cache, `GOMODCACHE`, and the Bazelisk
cache directory on each runner. The first two caches contain downloaded external
dependencies. `setup-bazel` sets `BAZELISK_HOME` to a subdirectory named after
`.bazelversion`. That subdirectory contains the selected Bazel release. Before it
saves a cache entry, the action removes the other Bazelisk version subdirectories.
This prevents old Bazel releases from accumulating in new cache entries. On
`master`, the setup job prepares and saves a cache entry for later jobs on the same
platform. On other refs, later Bazel-consuming jobs prepare their own non-exact
restores but do not save them.

The `lint-avalanchego` job restores this cache before it runs `bazelisk mod tidy`.
This prevents the command from downloading Bazel or cached external tools when the
cache contains them. The job cannot depend on the Bazel workflow setup job because
`needs` does not cross workflows. The Bazel workflow keeps the only cache-writing
setup job.

The shared Gazelle module cache lets later jobs reuse prepared module inputs
instead of downloading them in a new Bazel work area. See
[Go dependency preparation](#go-dependency-preparation) for the cache path and
the meaning of `GO_REPOSITORY_USE_HOST_MODCACHE`.

The remote cache is separate from the GitHub Actions caches. Bazel reads it and
writes to it during configured pre-merge builds and tests. It stores action
outputs and cacheable test results. The scheduled workflow does not read from or
write to the remote cache.

### Cache key

[`setup-bazel`](../.github/actions/setup-bazel/action.yml) constructs the
GitHub Actions cache key with a platform-specific `bazel-repo-` prefix. It also
uses that prefix for same-platform warm restores.

An exact cache hit must contain all external dependencies that CI commands
need. This includes dependencies for CI target patterns and commands without
target patterns. The key must include every input that Gazelle reads through
`go_deps`. `MODULE.bazel.lock` does not replace the Go workspace inputs.

That split is intentional:
- `runner.os` and `runner.arch` separate caches by platform
- `.bazelversion` invalidates the cache when the Bazel version changes, so
  Bazelisk downloads the selected Bazel release again
- `MODULE.bazel` and `MODULE.bazel.lock` invalidate the cache when Bazel module
  resolution or module-extension configuration changes
- `go.work`, `go.work.sum`, and the `go.mod` and `go.sum` files for each
  workspace module invalidate the cache when Gazelle's `go_deps` input graph
  changes. `tools/external/go.mod` is not a workspace module and is not an
  input to `go_deps`.
- `scripts/bazel_ci_dependency_list.sh` invalidates the cache when the
  checked-in Bazel dependency list changes
- the broader same-platform restore key still gives a useful warm start for
  unchanged external dependencies and tools, such as Buildozer
- the GitHub Actions cache does not store Bazel build outputs

The remote cache does not use the GitHub Actions cache key. Bazel computes its
remote keys from action inputs and build configuration. Platform and race
configuration differences therefore produce different keys. CI configures this
cache only when all these conditions are true:

- the workflow enables the remote cache
- CI provides the remote-cache URL
- CI provides the authorization header

### Checked-in Bazel dependency list

This setup has the same goal as `actions/setup-go`: prepare dependency input
before a Bazel job runs. Later commands must not discover a missing repository
through the network.

The setup action restores Task and cached Bazel inputs. It then configures
Bazel to use those inputs. The per-platform `setup` job runs
[`setup-bazel`](../.github/actions/setup-bazel/action.yml) with
`is-setup-job: true`.

On `master`, a cache miss in that job runs
`./scripts/run_task.sh bazel-cache-ci-build-dependencies`. That task calls
`./scripts/cache_bazel_ci_build_dependencies.sh`. The script fetches the
checked-in dependency list. It also runs `bazel mod deps` to prepare the module
graph. The action then disables downloads and checks Bazel metadata. It saves
the cache only after that check succeeds.

On other refs, a non-exact restore also prepares dependencies. These jobs do
not save a cache entry. The setup job disables downloads and checks metadata.
Later Bazel-consuming jobs also disable downloads after preparation. The action
sets `--repository_disable_download` and `GOPROXY=off` for repository rules.
`--repository_disable_download` blocks Bazel repository downloads.
`GOPROXY=off` prevents Gazelle's Go commands from downloading missing modules.
Those commands can still read cached modules. A missing module causes a failure
instead of a download. These controls do not block all network access; see
[download restrictions](./go-toolchains-and-caches.md#download-restrictions).

The checked-in list has two parts:

- `bazel_ci_target_patterns` lists target patterns for the Bazel CI workflows.
  `run_bazel_ci_command.sh` checks these patterns before it runs a CI command.
- `bazel_ci_additional_dependency_targets` lists external tools for metadata
  checks and commands without target patterns. The metadata formatter needs
  `@buildifier_prebuilt//:buildifier`. `bazel mod tidy` needs the generated
  Buildozer target in this list.

`//...` covers workspace targets, not targets in external repositories. Fetch
Buildifier explicitly so the metadata check can run with repository downloads
disabled. `bazel mod deps` prepares the module graph but does not download the
Buildifier binary.

Do not add every Bazel target to the list. Add each CI target pattern and each
command-only dependency that CI needs. This keeps unrelated repositories and
toolchains out of the cache.

A related design constraint is that this setup path must stay focused on
external dependencies, not local workspace-module discovery. The omission of
workspace-module `use_repo` bindings in `MODULE.bazel` keeps `bazel fetch`
from walking machine-specific workspace state.

### Enforcement

Bazel CI build and test commands with target patterns use
`./scripts/run_bazel_ci_command.sh`. The Go test helper gives its source target
patterns to this wrapper. The wrapper checks that the patterns are present in
`bazel_ci_target_patterns` when CI enables enforcement.

That keeps the target-pattern list aligned with the Bazel CI jobs that run.
It makes it harder for a new or changed Bazel CI job to use an undeclared build
dependency.

Commands without target patterns, such as `bazel mod tidy` and `bazel mod deps`,
do not use this wrapper. The metadata formatter also runs Buildifier directly.
The wrapper does not check `bazel_ci_additional_dependency_targets`. Add
command-only dependencies to that list manually. Validate those commands with repository downloads disabled after
cache preparation.

### Changing this safely

When modifying `setup-bazel`, `run_task.sh`, `run_bazel_ci_command.sh`,
`bazel_ci_dependency_list.sh`, or the Bazel module wiring that supports them,
preserve these invariants:

- CI can launch `task` without assuming a preinstalled repo-specific wrapper
- jobs that also prepare dependencies for Go tasks outside Bazel run `setup-bazel`
  before `setup-go-for-project` or `install-nix` disables `GOPROXY`; see
  [CI dependency provisioning](./ci.md#provision-ci-job-dependencies)
- on `master`, every `setup` job requests a cache save after it checks metadata;
  see [CI cache policy](./ci.md#ci-cache-policy) for cache preparation and
  concurrent-save behavior
- on other refs, each setup job checks metadata, and each Bazel-consuming job
  prepares a non-exact restore locally before repository downloads are disabled
- GitHub Actions dependency-cache writes remain restricted to
  `github.ref == 'refs/heads/master'`; see
  [CI cache policy](./ci.md#ci-cache-policy) for the rationale
- the checked-in dependency list includes each target pattern and command-only
  dependency that Bazel CI uses
- cache-prefetch behavior stays focused on external repositories and does not
  start depending on developer-specific workspace state
- when enabled, remote caching requires a `grpcs://` cache URL and the
  authorization header
- the daily scheduled workflow disables remote caching
- setup does not print the remote-cache authorization header

Validate changes proportionally:

- run each affected Bazel task through its normal entrypoint
- include `task bazel-check-metadata` and `task bazel-cache-ci-build-dependencies`
  when these tasks are relevant
- run the relevant `task bazel-test-unit-*` and `task bazel-test-e2e-*` targets
- confirm that the dependency list and cache preparation agree
- when you change cache setup or action order, test these cases on a fresh runner:
  - an exact restore
  - a non-exact restore
  - a cache miss
- confirm that Bazel preparation populates Gazelle's separate module cache before
  Go setup disables `GOPROXY`
- confirm that the workload succeeds with repository downloads disabled.
  An exact hit alone does not test this ordering constraint
- if you change a Bazel CI command or target pattern, update
  `scripts/bazel_ci_dependency_list.sh` in the same change rather than letting
  CI discover a missing dependency later
- if you change `MODULE.bazel` or `MODULE.bazel.lock`, rerun the normal Bazel
  metadata workflow and confirm the setup path still reaches repo tools and
  external repos without traversing unintended local workspace state

The merge-group Bazel entrypoint defines one aggregate job for each platform:

- `linux-amd64 / bazel-required` depends on the full reusable-workflow call.
- `darwin-arm64 / bazel-required` depends on the smoke reusable-workflow call.

Branch protection can require these aggregate jobs instead of tracking each
underlying Bazel job separately. Their `name` values preserve the check names
that the nested reusable workflows previously emitted. Update branch protection
before changing these names.

The pull-request entrypoint defines the same checks without `needs` and skips
them immediately. This lets a pull request join the merge queue before all CI
finishes. See [Required checks and the merge queue](./ci.md#required-checks-and-the-merge-queue)
for the admission policy and its trade-offs. See [Required-check validation in
GitHub](./ci.md#required-check-validation-in-github) for checks that local lint cannot perform.

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

Both entrypoints run inside the nix dev shell, whose `DEVELOPER_DIR`
and `SDKROOT` would make `xcode-select` and `xcrun` report nix's SDK.
The script clears them and calls those binaries by absolute path, so a
hand-exported `DEVELOPER_DIR` is ignored — use `sudo xcode-select -s`.

When invoked by direnv, generation is best-effort: failures are shown
as warnings during shell entry but do not prevent entering the repo.
When run directly, the script exits non-zero on discovery failures so
manual setup problems remain actionable.

### The macOS C compiler

`.bazelrc` pins `--repo_env=CC=/usr/bin/clang`. Bazel resolves the
compiler from `CC` in the client environment, which in the nix dev
shell is nix's clang wrapper; that wrapper takes its sysroot from
`NIX_CFLAGS_COMPILE`, which Bazel strips from action environments,
failing the cgo stdlib build on a missing `resolv.h`. `DEVELOPER_DIR`
and `SDKROOT` do not help — they select an SDK, not a compiler.

## Adding a New Go Module

When adding a new Go module under `graft/`:

1. **Create the module's go.mod and add to go.work**:
   ```
   go work use ./graft/newmodule
   ```
   Add the module's `go.mod` and `go.sum` to the Bazel dependency-cache key in
   [`.github/actions/setup-bazel/action.yml`](../.github/actions/setup-bazel/action.yml).
   The key intentionally excludes non-workspace modules such as
   `tools/external` that are not inputs to Gazelle's `go_deps` extension.

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

### Remote Execution

CI uses remote caching. Remote execution remains a possible improvement.
`go_sdk.download()` provides the hermetic Go toolchain that remote execution
requires.

### CI Integration

The intended long-term strategy is to run tests with Bazel-built artifacts,
except for the non-Bazel unit and E2E jobs. Retain those jobs to support
third-party consumers who do not use Bazel. Checks and lint are outside this
migration.

### Solidity builds and binding generation

The intended end state is for Bazel to manage the complete load-contract build:

```text
Solidity sources → pinned solc → ABI and bytecode → pinned abigen → Go bindings → load-test binary
```

This would make Solidity sources and generator tools explicit build inputs.
Contract edits would invalidate the affected actions, while unchanged generation
could reuse cached outputs. Bazel support for this pipeline is a future goal;
the load tasks still require generation outside Bazel.

Remove task-level binding generation only after the Bazel dependency chain
replaces it. Validate that a Solidity edit updates the bytecode embedded in the
load-test binary. Also check that an unchanged build reuses generation outputs
and that both load tasks work without a separate generation command.

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

Graft modules use `gazelle:map_kind` to apply custom test timeouts (see [Custom Test
Macros](#custom-test-macros-via-gazellemap_kind)). These settings apply to all
`go_test` targets in the subtree, including non-unit tests.

The [E2E and monitored test runners](#e2e-and-monitored-test-runners) build their
artifacts with Bazel and execute them through `bazel run`. They do not use
Bazel's test-result cache or `--test_timeout` setting. CI job timeouts limit
these runs. When changing a runner, check its execution limits separately from
its `go_test` timeout attribute.

If more non-unit suites move to `bazel test`, consider separate timeout settings
for unit, integration, and E2E tests. Keep non-unit targets tagged `manual` so
wildcard commands do not select them.

## References

- [rules_go documentation](https://github.com/bazelbuild/rules_go)
- [Gazelle documentation](https://github.com/bazelbuild/bazel-gazelle)
- [Bazel Go Tutorial](https://bazel.build/start/go)
