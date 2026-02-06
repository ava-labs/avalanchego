# Bazel Build System

This document explains how Bazel is configured and used in the
avalanchego monorepo.

## Quick Start

```bash
# Build the main binary
task bazel-build

# Build with optimizations
task bazel-build-opt

# Run unit tests
task bazel-test

# Update BUILD.bazel files after changing Go imports
task bazel-gazelle-generate

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

The build uses `go_sdk.download()` to download a specific Go version,
ensuring reproducible builds without requiring a Nix shell:

```python
go_sdk = use_extension("@io_bazel_rules_go//go:extensions.bzl", "go_sdk")
go_sdk.download(version = "1.24.12")
```

**Approaches considered:**

| Approach | Pros | Cons |
|----------|------|------|
| `go_sdk.download()` (chosen) | Reproducible, no nix required for builds | Go version must be synced manually across go.mod files |
| `go_sdk.host()` | Single source of truth via nix | Requires nix shell, not hermetic outside nix |
| `rules_nixpkgs_go` | Bazel calls Nix directly, fully hermetic | **Incompatible with rules_go v0.56+** (toolchain API mismatch) |

> **Note:** rules_nixpkgs_go v0.13.0 proved incompatible with rules_go
> v0.56.0+. The rules_go toolchain API changed to require a `pack`
> attribute that rules_nixpkgs_go doesn't provide.  See:
> https://github.com/tweag/rules_nixpkgs/issues/667

### Version Pinning

| Tool | Version | Pin Mechanism | Rationale |
|------|---------|---------------|-----------|
| Bazel | 8.0.1 | `.bazelversion` + bazelisk | Current LTS with native bzlmod support |
| Go | 1.24.12 | `MODULE.bazel` go_sdk.download | Project requirement |
| rules_go | 0.56.0 | `MODULE.bazel` | Last version supporting Go 1.24.x |
| gazelle | 0.45.0 | `MODULE.bazel` | Compatible with rules_go 0.56.0 |

**Go 1.25 upgrade note:** Go 1.25 removed the `pack` tool from the Go
distribution, which rules_go v0.56.x relies on. When upgrading to Go
1.25+, also upgrade rules_go to v0.57.0+ (which no longer depends on
external `pack`).

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

### CGO Dependencies

Several dependencies require special handling for CGO compilation in
Bazel's sandboxed environment.

#### blst (BLS Signatures)

**Problem:** Complex CGO with assembly files that gazelle cannot handle.

**Solution:** Disable gazelle and use a custom BUILD file via patch:
```python
go_deps.gazelle_override(build_file_generation = "off", path = "github.com/supranational/blst")
go_deps.module_override(patches = ["//.bazel/patches:com_github_supranational_blst.patch"], ...)
```

The patch provides custom BUILD files for blst's subdirectories, handling
the unity-build compilation model and cross-platform assembly selection.
Compiler flags are derived from the CGO directives in `blst.go`.
BLS signatures are performance-critical for Avalanche consensus.

#### gnark-crypto (BLS12-381 for KZG)

**Problem:** Assembly files use cross-package `#include` directives:
```asm
#include "../../../field/asm/element_4w/element_4w_arm64.s"
```
Bazel's sandboxed builds don't allow relative includes across package boundaries.

**Solution:** Patch assembly files to convert `.s` includes to `.h` includes,
allowing them to work within Bazel's sandbox:
```python
go_deps.module_override(
    patches = ["//.bazel/patches:com_github_consensys_gnark_crypto_asm_includes.patch"],
    path = "github.com/consensys/gnark-crypto",
)
```

**Dependency chain:**
```
avalanchego → libevm → kzg4844 → go-kzg-4844 → gnark-crypto
```

gnark-crypto is used for KZG blob commitments (EIP-4844).

#### libevm (secp256k1)

**Problem:** Gazelle-generated BUILD file doesn't include libsecp256k1 C sources.

**Solution:** Patch to add textual headers for the C sources and exclude
the directory from gazelle:
```python
go_deps.gazelle_override(directives = ["gazelle:exclude crypto/secp256k1/**"], path = "github.com/ava-labs/libevm")
```

#### firewood-go-ethhash FFI

**Problem:** Pre-built static libraries with `-L` paths that don't work
in Bazel's sandbox.

**Solution:** Patch to use `cc_import` for proper static library linking.

### Key Configuration Files

| File | Purpose | Safe to Delete? |
|------|---------|-----------------|
| `MODULE.bazel` | Bazel module definition, dependencies, patches | **No** |
| `MODULE.bazel.lock` | Locked dependency versions | Yes (regenerated) |
| `.bazelrc` | Bazel build flags and settings | **No** |
| `.bazelignore` | Directories excluded from Bazel (`.direnv`) | **No** |
| `.bazelversion` | Bazel version pin (used by bazelisk) | **No** |
| `BUILD.bazel` (root) | Gazelle config: prefix, exclusions (`tools/`), proto disable | **No** |
| `.bazel/patches/*.patch` | Fixes for external dependencies | **No** |
| `.bazel/defs.bzl` | Custom test macros for graft module timeouts | **No** |
| `scripts/bazel_workspace_status.sh` | Git commit stamping for releases | **No** |

### BUILD.bazel Files with Custom Content

Most BUILD.bazel files are auto-generated by Gazelle, but some have
custom content that would be lost if deleted:

| File | Custom Content |
|------|---------------|
| `BUILD.bazel` (root) | Gazelle directives (`prefix`, `exclude`, `proto`) |
| `main/BUILD.bazel` | `x_defs` for Git commit stamping |
| `graft/coreth/BUILD.bazel` | `gazelle:prefix`, `gazelle:map_kind` for test timeouts |
| `graft/evm/BUILD.bazel` | `gazelle:prefix`, `gazelle:map_kind` for test timeouts |
| `graft/subnet-evm/BUILD.bazel` | `gazelle:prefix`, `gazelle:map_kind` for test timeouts |

**Preserving custom content**: Gazelle directives (`# gazelle:prefix`,
`# gazelle:map_kind`) are automatically preserved. For other custom
content (like `x_defs`), use `# keep` comments to prevent gazelle from
removing them:

```python
x_defs = {  # keep
    "github.com/ava-labs/avalanchego/version.GitCommit": "{STABLE_GIT_COMMIT}",
},
```

**Rule of thumb**: Don't delete BUILD.bazel files that contain gazelle
directives or `# keep` comments.

## Gazelle

[Gazelle](https://github.com/bazelbuild/bazel-gazelle) automatically
generates BUILD.bazel files from Go source code.

### Where Gazelle Comes From

Gazelle is declared as a `bazel_dep` in `MODULE.bazel`. It is **not**
provided by Nix. The recommended entrypoint is `task
bazel-gazelle-generate`, which runs Gazelle for all modules in the repo.

### When to Run Gazelle

Run `task bazel-gazelle-generate` after:
- Adding new `.go` files
- Changing import statements
- Adding new packages/directories
- Modifying `go.mod` dependencies

### How Gazelle Handles Multiple Modules

Each Go module root needs a `BUILD.bazel` with a `gazelle:prefix`
directive:

```python
# graft/coreth/BUILD.bazel
# gazelle:prefix github.com/ava-labs/avalanchego/graft/coreth
```

### Custom Test Macros via `gazelle:map_kind`

The graft modules (coreth, evm, subnet-evm) have longer test timeouts
than the root module. To handle this, custom test macros are defined
in `.bazel/defs.bzl`:

| Macro | Timeout | Used By |
|-------|---------|---------|
| `graft_go_test_900s` | 900s (long) | coreth, subnet-evm |
| `graft_go_test_600s` | 900s (closest to 600s) | evm |

Gazelle automatically uses these macros via `gazelle:map_kind`
directives in each graft module's root BUILD.bazel:

```python
# graft/coreth/BUILD.bazel
# gazelle:prefix github.com/ava-labs/avalanchego/graft/coreth
# gazelle:map_kind go_test graft_go_test_900s //.bazel:defs.bzl
```

This remaps all `go_test` targets in that subtree to use the custom
macro with the longer timeout.

## External Dependency Handling

### Go Dependencies

Go dependencies are managed via the `go_deps` extension in `MODULE.bazel`:

```python
go_deps = use_extension("@bazel_gazelle//:extensions.bzl", "go_deps")
go_deps.from_file(go_work = "//:go.work")
```

The `go.work` file aggregates all module dependencies (root + graft
modules), so dependencies from nested modules are resolved
automatically.

### Patched Dependencies

Some dependencies require patches for Bazel compatibility:

| Dependency | Issue | Solution |
|------------|-------|----------|
| `supranational/blst` | Complex CGO with assembly | Custom BUILD.bazel via patch |
| `ava-labs/libevm` | Missing C sources for secp256k1 | Patch to add sources |
| `consensys/gnark-crypto` | Assembly include paths | Patch asm includes for Bazel sandboxing |
| `firewood-go-ethhash/ffi` | Pre-built static libraries | Custom BUILD.bazel via patch |

Patches are in the `.bazel/patches/` directory and applied in `MODULE.bazel`.

### Protocol Buffers

Proto generation is disabled globally via the root `BUILD.bazel`:

```python
# gazelle:proto disable_global
```

The repository uses pre-generated `.pb.go` files checked into source
control rather than generating them at build time. This avoids proto
toolchain complexity in Bazel while maintaining compatibility with the
existing `go generate` workflow.

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

**When adding new non-unit tests**, add the manual tag:
```python
go_test(
    name = "my_e2e_test",
    srcs = ["my_e2e_test.go"],
    tags = ["manual"],  # Not a unit test
    deps = [...],
)
```

### Maintenance

```bash
# Update BUILD files
task bazel-gazelle-generate                 # or: bazel run //:gazelle

# Format BUILD files
task bazel-fmt                     # or: buildifier -r .

# Update MODULE.bazel use_repo calls
task bazel-mod-tidy               # or: bazel mod tidy

# Clean build outputs
task bazel-clean                  # or: bazel clean

# Full cache clean
task bazel-clean-all              # or: bazel clean --expunge

# Delete generated BUILD files (for clean regeneration)
task bazel-gazelle-delete

# CI: verify BUILD files are up-to-date
task check-bazel-gazelle-generate
```

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

3. **Run gazelle** to generate BUILD.bazel files:
   ```bash
   task bazel-gazelle-generate
   ```

Dependencies are resolved automatically via `go.work`.

## Troubleshooting

### "no such package" or import errors

Run gazelle to regenerate BUILD files:
```bash
task bazel-gazelle-generate
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
build --action_env=CGO_ENABLED=1
build --action_env=CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"
```

Some packages require specific CGO flags (e.g., BLST
cryptography). These are configured in `.bazelrc` or handled via
patches.

## Version Stamping

The main binary includes Git commit information via `x_defs` in
`main/BUILD.bazel`:

```python
go_binary(
    name = "avalanchego",
    embed = [":main_lib"],
    x_defs = {
        "github.com/ava-labs/avalanchego/version.GitCommit": "{STABLE_GIT_COMMIT}",
    },
)
```

Stamping is enabled via the `release` config in `.bazelrc`:

```bash
bazel build //main:avalanchego                    # Dev build (no stamp, cached)
bazel build --config=release //main:avalanchego   # Release build (stamped)
```

## Known Limitations

1. **Go/rules_go version coupling** - Upgrading to Go 1.25+ requires
   also upgrading rules_go to v0.57.0+ (see Version Pinning section)

2. **gnark-crypto assembly patches** - The assembly include patches need
   maintenance when upgrading gnark-crypto versions

3. **Manual Go version sync** - Go version must be kept in sync across:
   - `MODULE.bazel` (`go_sdk.download(version = "...")`)
   - `go.mod` files
   - `nix/go/default.nix` (if using nix shell)

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

External dependency patches in `.bazel/patches/` should be reviewed periodically:
- Check if upstream projects have improved Bazel support
- Test patches against dependency updates
- Consider upstreaming BUILD files where feasible (especially gnark-crypto)

### Test Configuration

Graft modules already have custom test timeouts via `gazelle:map_kind`
(see "Custom Test Macros" section). Consider further stratification:
- Root module unit tests: shorter timeout (60s instead of 120s)
- Integration tests: medium timeout (300s)
- E2E tests: explicit long timeout (currently use `manual` tag)

## References

- [rules_go documentation](https://github.com/bazelbuild/rules_go)
- [Gazelle documentation](https://github.com/bazelbuild/bazel-gazelle)
- [Bazel Go Tutorial](https://bazel.build/start/go)
