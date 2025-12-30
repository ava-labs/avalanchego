# Bazel 8 Migration Status

## Summary

This document tracks the progress of migrating avalanchego to Bazel 8 with bzlmod. The goal is to enable local Bazel builds/tests while maintaining the existing `go build` workflow.

## Completed Work

### Infrastructure Files Created

1. **`.bazelversion`** - Pins Bazel to 8.0.1 (used by bazelisk)

2. **`.bazelrc`** - Bazel configuration:
   - Race detector disabled by default, opt-in via `--config=race`
   - CGO settings for BLST portable build
   - Disk cache enabled at `~/.cache/bazel-disk-cache`
   - purego build tag for gnark-crypto (workaround - see Known Issues)
   - Release stamping configuration

3. **`MODULE.bazel`** - bzlmod dependencies:
   - rules_go v0.56.0 (last version supporting Go 1.24.x - Go 1.25 removed `pack` tool)
   - gazelle v0.45.0
   - go_sdk.host() to use Go from nix shell PATH
   - go_version_check module extension for validation
   - CGO dependency overrides (blst, libevm, firewood-ffi, gnark-crypto)
   - use_repo with all direct and indirect dependencies from graft modules

4. **`BUILD.bazel`** (root) - Gazelle configuration:
   - Module prefix: `github.com/ava-labs/avalanchego`
   - Excludes for .git, build, .direnv
   - Resolve directives for graft module local replacements
   - Resolve directives for proto/pb packages

5. **`graft/*/BUILD.bazel`** - Per-module gazelle prefixes:
   - `graft/coreth/BUILD.bazel`
   - `graft/evm/BUILD.bazel`
   - `graft/subnet-evm/BUILD.bazel`

6. **`.bazel/go_version_check.bzl`** - Module extension that validates Go version matches go.mod

7. **`.bazel/patches/`** - CGO dependency patches:
   - `com_github_supranational_blst.patch` - Custom BUILD for blst assembly (BLS signatures)
   - `com_github_ava_labs_libevm.patch` - secp256k1 textual headers
   - `com_github_ava_labs_firewood_go_ethhash_ffi.patch` - Pre-built static library linking

8. **`scripts/bazel_workspace_status.sh`** - Git commit stamping for release builds

9. **`Taskfile.yml`** - Added bazel-* tasks:
   - `bazel-build`, `bazel-build-race`
   - `bazel-test`, `bazel-test-race`
   - `bazel-clean`, `bazel-query`
   - `bazel-generate-gazelle`
   - `bazel-check-generate-gazelle`
   - `bazel-validate`

10. **`flake.nix`** - Added bazelisk to packages

11. **`BAZEL.md`** - Documentation (needs expansion - see below)

### Gazelle Run

Gazelle was run successfully and generated BUILD.bazel files throughout the codebase:
```bash
nix develop --command bazelisk run //:gazelle
```

## Known Issues

### 1. gnark-crypto Assembly Include Problem (BLOCKING BUILD)

**Status**: Build fails due to gnark-crypto assembly

**Error**:
```
external/gazelle++go_deps+com_github_consensys_gnark_crypto/ecc/bls12-381/fr/element_arm64.s:10:
#include: open /field/asm/element_4w/element_4w_arm64.s: no such file or directory
```

**Root Cause**: gnark-crypto's assembly files use cross-package `#include` directives:
```asm
#include "../../../field/asm/element_4w/element_4w_arm64.s"
```
Bazel's sandboxed builds don't allow relative includes across package boundaries.

**Dependency Chain**:
```
avalanchego/firewood/syncer
  → libevm/core/types
    → libevm/crypto/kzg4844
      → go-kzg-4844
        → gnark-crypto/ecc/bls12-381 (BLS12-381 curve for KZG commitments)
```

**Current Workaround Attempt**:
- `.bazelrc` has `build --@rules_go//go/config:tags=purego`
- `MODULE.bazel` has `go_deps.gazelle_override` with `gazelle:build_tags purego`
- These don't seem to be working - the assembly files are still being compiled

**Potential Solutions** (in order of complexity):

1. **Fix purego tag propagation** - Investigate why the purego build tag isn't excluding the assembly files. The .s files have `//go:build !purego` constraints.

2. **Use `build_file_generation = "off"` for gnark-crypto** - Disable gazelle entirely and provide custom BUILD files via patch that either:
   - Use genrules to preprocess assembly (expand #includes)
   - Inline the shared assembly content into each consuming package

3. **Create comprehensive gnark-crypto patch** - ~20 packages need patching to handle assembly correctly

**Performance Note**: gnark-crypto is used for KZG blob commitments (EIP-4844). The purego implementation is slower but functional. blst (BLS signatures) is more performance-critical and already has working assembly via patch.

### 2. Indirect Dependencies Warning

Bazel reports some repos as "indirect" in use_repo. This is a warning, not an error:
```
Fix the use_repo calls by running 'bazel mod tidy'.
```

## Files Modified from Original Repo

- `flake.nix` - Added bazelisk
- `Taskfile.yml` - Added bazel-* tasks

## Files Added

```
.bazelversion
.bazelrc
MODULE.bazel
BUILD.bazel (root - manual)
BAZEL.md
BAZEL_MIGRATION_STATUS.md (this file)
.bazel/go_version_check.bzl
.bazel/patches/com_github_supranational_blst.patch
.bazel/patches/com_github_ava_labs_libevm.patch
.bazel/patches/com_github_ava_labs_firewood_go_ethhash_ffi.patch
graft/coreth/BUILD.bazel (manual gazelle config)
graft/evm/BUILD.bazel (manual gazelle config)
graft/subnet-evm/BUILD.bazel (manual gazelle config)
scripts/bazel_workspace_status.sh
+ Many BUILD.bazel files generated by gazelle
```

## Next Steps

### Immediate (Fix Build)

1. **Debug gnark-crypto purego tag** - Figure out why `--@rules_go//go/config:tags=purego` isn't working:
   ```bash
   # Check if tag is being passed
   nix develop --command bazelisk build //main:main --sandbox_debug
   ```

2. **Alternative: Disable gnark-crypto gazelle** - If purego doesn't work:
   ```python
   # In MODULE.bazel
   go_deps.gazelle_override(
       build_file_generation = "off",
       path = "github.com/consensys/gnark-crypto",
   )
   go_deps.module_override(
       patches = ["//.bazel/patches:com_github_consensys_gnark_crypto.patch"],
       path = "github.com/consensys/gnark-crypto",
   )
   ```
   Then create a comprehensive patch.

### Validation (After Build Works)

1. **Build validation**:
   ```bash
   nix develop --command bazelisk build //main:main
   ./bazel-bin/main/main_/main --version
   ```

2. **Unit test validation**:
   ```bash
   nix develop --command bazelisk test //...
   ```

3. **E2E validation**:
   ```bash
   task bazel-build
   task build-xsvm
   AVALANCHEGO_PATH=$(pwd)/bazel-bin/main/main_/main task test-e2e-existing-ci
   ```

### Documentation

1. **Update BAZEL.md** with:
   - More detailed rationale for design decisions
   - Troubleshooting section for gnark-crypto
   - Performance implications of purego

### Future Work

1. **CI Integration** - Add GitHub Actions workflow for Bazel builds
2. **gnark-crypto assembly** - If purego performance is insufficient, create proper assembly patch
3. **Rust support** - When needed, add rules_rust

## Reference: Existing Bazel Repo

The `../avalanchego_maru-bazel/` directory contains a previous attempt at Bazel migration. Key findings:
- Also used purego for gnark-crypto (same issue)
- Used rules_go 0.59.0 (we use 0.56.0 for Go 1.24.x compatibility)
- Patches were copied from there

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Go toolchain | go_sdk.host() | rules_nixpkgs_go incompatible with rules_go (missing pack attribute) |
| Go version enforcement | go_version_check.bzl | Validates Go version matches go.mod at build time |
| rules_go version | 0.56.0 | Last version supporting Go 1.24.x (pack tool removed in Go 1.25) |
| gnark-crypto | purego tag (attempting) | Assembly #include paths incompatible with Bazel sandbox |
| blst | Custom BUILD patch | Assembly works, based on Prysm's blst.BUILD |
