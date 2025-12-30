# Bazel Build System

This document explains how Bazel is used in this repository and the rationale behind key architectural decisions.

## Quick Start

```bash
# Enter nix development shell (provides bazelisk and Go toolchain)
nix develop

# Build avalanchego
task bazel-build

# Run unit tests
task bazel-test

# Regenerate BUILD files after adding/removing Go files
task bazel-generate-gazelle
```

## Architecture Overview

### Why Bazel?

1. **Hermetic builds** - Reproducible builds regardless of host environment
2. **Incremental compilation** - Only rebuild what changed
3. **Parallel execution** - Efficient use of multi-core systems
4. **Caching** - Local and remote build caching
5. **Multi-language** - Single build system for Go, Rust (future), protobuf, etc.

### Toolchain Strategy: Host SDK from Nix Shell

We use `go_sdk.host()` to pick up the Go toolchain from the PATH, which is provided by the Nix development shell (`nix develop`).

**Why this approach?**

| Approach | Pros | Cons |
|----------|------|------|
| **go_sdk.host() (chosen)** | Single source of truth (nix/go/), simple setup, works with nix shell | Requires nix shell, not hermetic outside nix |
| **rules_nixpkgs_go** | Bazel calls Nix directly, fully hermetic | Incompatible with rules_go v0.56+ (missing 'pack' attribute issue) |
| **go_sdk.download()** | Pure Bazel, no nix required | Duplicate Go version config, version drift risk |

The `go_version_check.bzl` module extension validates that the Go version in PATH matches `go.mod` at build time, catching misconfigurations early.

**rules_nixpkgs_go incompatibility**: We attempted to use rules_nixpkgs_go v0.13.0 to have Bazel directly call Nix for toolchains. However, it's incompatible with rules_go v0.56.0+ due to a missing `pack` attribute in the go_toolchain rule. See: https://github.com/tweag/rules_nixpkgs/issues/667

### Version Pinning Rationale

| Tool | Version | Pin Mechanism | Rationale |
|------|---------|---------------|-----------|
| Bazel | 8.0.1 | `.bazelversion` + bazelisk | Latest stable LTS with full bzlmod support |
| Go | 1.24.11 | `nix/go/default.nix` | Project requirement, shared with nix shell |
| rules_go | 0.56.0 | `MODULE.bazel` | **Critical**: Last version supporting Go 1.24.x |
| gazelle | 0.45.0 | `MODULE.bazel` | Compatible with rules_go 0.56.0 |

**Go 1.25 / rules_go constraint**: Go 1.25 removed the `pack` tool from the Go distribution. `rules_go` uses `pack` internally, so:
- `rules_go` v0.56.x is the last version supporting Go 1.24.x
- `rules_go` v0.57.0+ requires Go 1.25+
- **When upgrading to Go 1.25+, also upgrade rules_go**

### Why Bazel 8 (not Bazel 7)?

| Factor | Bazel 7 | Bazel 8 |
|--------|---------|---------|
| bzlmod | Optional | Default |
| LTS status | Older | Current |
| WORKSPACE | Default | Deprecated (still works) |

Bazel 8 is the current LTS with native bzlmod support. We use pure bzlmod (MODULE.bazel) without WORKSPACE files.

## CGO Dependencies

Several dependencies require special handling for CGO compilation in Bazel's sandboxed environment.

### blst (BLS Signatures) - PERFORMANCE CRITICAL

**Problem**: Complex CGO with assembly files that gazelle cannot handle automatically.

**Solution**: Disable gazelle for this module and use a custom BUILD file via patch:
```python
go_deps.gazelle_override(build_file_generation = "off", path = "github.com/supranational/blst")
go_deps.module_override(patches = ["//.bazel/patches:com_github_supranational_blst.patch"], ...)
```

The patch is based on [Prysm's blst.BUILD](https://github.com/prysmaticlabs/prysm/blob/develop/third_party/blst/blst.BUILD).

**Why blst matters**: BLS signatures are used extensively in Avalanche consensus. This is a hot path that requires assembly optimization.

### libevm (secp256k1)

**Problem**: Gazelle-generated BUILD file doesn't include the libsecp256k1 C source files.

**Solution**: Patch to add textual headers for the C sources:
```python
go_deps.module_override(patches = ["//.bazel/patches:com_github_ava_labs_libevm.patch"], ...)
```

### gnark-crypto (BLS12-381 for KZG) - KNOWN ISSUE

**Problem**: Assembly files use cross-package `#include` directives:
```asm
#include "../../../field/asm/element_4w/element_4w_arm64.s"
```
Bazel's sandboxed builds don't allow relative includes across package boundaries.

**Current workaround**: Use `purego` build tag to avoid assembly:
```python
go_deps.gazelle_override(directives = ["gazelle:build_tags purego"], path = "github.com/consensys/gnark-crypto")
```

**Performance impact**: The purego implementation is slower than assembly. gnark-crypto is used for KZG blob commitments (EIP-4844). If performance becomes an issue, a comprehensive patch to fix assembly includes would be needed (~20 packages affected).

**Dependency chain**:
```
avalanchego → libevm → kzg4844 → go-kzg-4844 → gnark-crypto
```

### firewood-go-ethhash FFI

**Problem**: Pre-built static libraries with `-L` paths that don't work in Bazel's sandbox.

**Solution**: Patch to use `cc_import` for proper static library linking.

## Multi-Module Structure

The repository has multiple Go modules with local replacements:

```
go.mod                      # Root module
graft/coreth/go.mod         # replaces github.com/ava-labs/coreth
graft/evm/go.mod            # replaces github.com/ava-labs/evm
graft/subnet-evm/go.mod     # replaces github.com/ava-labs/subnet-evm
```

**How this works in Bazel**:

1. Root `BUILD.bazel` has gazelle resolve directives:
   ```python
   # gazelle:resolve go github.com/ava-labs/avalanchego/graft/coreth //graft/coreth
   ```

2. Each graft module has its own `BUILD.bazel` with gazelle prefix:
   ```python
   # gazelle:prefix github.com/ava-labs/avalanchego/graft/coreth
   ```

3. Dependencies from graft modules not in root go.mod are added to `use_repo()` in MODULE.bazel

## File Structure

```
.bazelversion              # Bazel version pin (used by bazelisk)
.bazelrc                   # Bazel configuration
MODULE.bazel               # bzlmod dependencies and CGO overrides
BUILD.bazel                # Root build file with gazelle targets
.bazel/
  go_version_check.bzl     # Go version validation module extension
  patches/                 # CGO dependency patches
    com_github_supranational_blst.patch
    com_github_ava_labs_libevm.patch
    com_github_ava_labs_firewood_go_ethhash_ffi.patch
graft/*/BUILD.bazel        # Per-module gazelle configuration
scripts/bazel_workspace_status.sh  # Git commit stamping
```

## Taskfile Commands

| Task | Description |
|------|-------------|
| `task bazel-build` | Build avalanchego |
| `task bazel-build-race` | Build with race detection |
| `task bazel-test` | Run all unit tests |
| `task bazel-test-race` | Run tests with race detection |
| `task bazel-clean` | Clean Bazel cache |
| `task bazel-query` | Query Bazel targets |
| `task bazel-generate-gazelle` | Regenerate BUILD files |
| `task bazel-check-generate-gazelle` | Check BUILD files are up-to-date |
| `task bazel-validate` | Full validation with E2E tests |

## Regenerating BUILD Files

After adding/removing Go files or changing imports:

```bash
# Regenerate BUILD.bazel files
task bazel-generate-gazelle
```

Note: Dependencies are read directly from `go.mod` via `go_deps.from_file()`, so there's no separate dependency update step. However, new indirect dependencies from graft modules may need to be added to `use_repo()` in MODULE.bazel.

## Troubleshooting

### "go version mismatch" error

Ensure you're in the nix development shell:
```bash
nix develop
```

### BUILD file changes not detected

Regenerate BUILD files:
```bash
task bazel-generate-gazelle
```

### CGO compilation errors

Check that CGO environment variables are set in `.bazelrc`:
```bash
build --action_env=CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"
build --action_env=CGO_ENABLED=1
```

### gnark-crypto assembly errors

If you see errors like:
```
#include: open /field/asm/element_4w/element_4w_arm64.s: no such file or directory
```

This is the known gnark-crypto issue. The purego workaround should prevent this, but if it's not working, check:
1. `.bazelrc` has `build --@rules_go//go/config:tags=purego`
2. `MODULE.bazel` has the gazelle_override with build_tags directive
3. Try `bazel clean --expunge` to clear cached BUILD files

## Known Limitations

1. **Nix shell required**: Bazel picks up Go from the PATH, so you must run in `nix develop`
2. **No remote execution**: Host toolchains are local-only
3. **gnark-crypto purego**: Using pure Go implementation pending assembly fix
4. **Go 1.24.x constraint**: Must stay on Go 1.24.x until ready to upgrade rules_go

## Future Work

### Rust Support

When Rust is needed, add to MODULE.bazel:
```python
bazel_dep(name = "rules_rust", version = "...")
```

The Rust toolchain can also be provided by the nix shell using similar host SDK approach.

### CI Integration

CI migration is planned as a follow-up. The Bazel tasks in Taskfile provide the foundation for CI jobs.

### gnark-crypto Assembly

If purego performance is insufficient for KZG operations, create a comprehensive patch that either:
1. Uses genrules to preprocess assembly files (expand #includes at build time)
2. Inlines shared assembly content into each consuming package
