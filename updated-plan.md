# Bazel Integration Execution Plan for avalanchego

## Background Context

### Repository Overview
- **Repository**: avalanchego - Avalanche network node implementation
- **Branch**: maru/bazel
- **Main module**: `github.com/ava-labs/avalanchego` (BSD-3 license) at repository root
- **Coreth module**: `github.com/ava-labs/avalanchego/graft/coreth` (LGPL-3 license) at `graft/coreth/`
- **Go version**: 1.24.9 (security-critical, must be exact)

### Why Bazel?
The repository is evolving into a multi-language monorepo:
1. Currently: Two Go modules (avalanchego + coreth) with circular dependencies
2. Coming: Another Go module
3. Future: Rust dependency (Firewood)

Bazel enables:
- Targeted CI/builds (don't rebuild the world for every change)
- Multi-language support (Go now, Rust later)
- Shared toolchain management via rules_nixpkgs

### Current Build System
- **Nix flake** (`flake.nix`) provides development environment
- **Task runner** (`Taskfile.yml`) orchestrates builds via shell scripts
- **Go SDK** defined in `nix/go/default.nix` with explicit version and SHA256s
- **No existing Bazel** - this plan creates it from scratch

### Key Files to Reference
- `flake.nix` - Nix development environment (lines 37-77 have package list)
- `nix/go/default.nix` - Custom Go 1.24.9 derivation with SHA256 checksums
- `nix/go/flake.nix` - Flake wrapper for Go derivation
- `go.mod` - Main module definition with replace directive for coreth
- `graft/coreth/go.mod` - Coreth module with reverse replace directive
- `Taskfile.yml` - Task definitions
- `scripts/build.sh` - Current build script (CGO flags, ldflags)

---

## Execution Strategy

### Approach
- **Minimal first**: Build just avalanchego binary before expanding
- **Incremental**: Validate each step, commit on success
- **Single source of truth**: Go SDK from Nix via rules_nixpkgs (not duplicated)
- **Exclude coreth**: Defer circular dependency handling

### Go SDK Strategy
Use `rules_nixpkgs_go` to source Go SDK from existing Nix derivation:
- Avoids duplicating Go version/checksums in Bazel config
- Future-proof for Rust (rules_nixpkgs_rust uses same pattern)
- Fallback to explicit SDK registration if rules_nixpkgs proves problematic

---

## Step 1: Add Bazel to Nix Environment

### Files to Modify
**`flake.nix`** - Add to the packages list (around line 37-77):

```nix
# After "go-task" line, add:
bazel_7                                    # Bazel build system
buildifier                                 # Bazel file formatter
```

### Full Context
Current packages section looks like:
```nix
packages = with pkgs; [
  git
  go-task
  # ... (add bazel_7 and buildifier here)
```

### Validation Commands
```bash
# Test Bazel is available
nix develop --command bazel version
# Should output: Build label: 7.x.x

# Test buildifier is available
nix develop --command buildifier --version
# Should output version info
```

### Commit
Message: `Add Bazel and buildifier to nix development environment`

---

## Step 2: Modify Nix Go Derivation for rules_go Compatibility

### Why This Is Needed
`nixpkgs_go_configure` from rules_nixpkgs expects the Go SDK to have a `ROOT` marker file at the package root. This is how rules_go identifies the SDK root directory.

### Files to Modify
**`nix/go/default.nix`** - Add ROOT marker in installPhase (around line 49-56):

Current:
```nix
installPhase = ''
  mkdir -p $out
  tar xzf $src -C $out --strip-components=1 --no-same-owner --no-same-permissions
  chmod +x $out/bin/go
'';
```

Change to:
```nix
installPhase = ''
  mkdir -p $out
  tar xzf $src -C $out --strip-components=1 --no-same-owner --no-same-permissions
  chmod +x $out/bin/go
  # ROOT marker required by rules_go/rules_nixpkgs for SDK identification
  touch $out/ROOT
'';
```

### Validation Commands
```bash
# Build the Go derivation
nix build ./nix/go

# Check ROOT file exists
ls $(nix build ./nix/go --print-out-paths 2>/dev/null)/ROOT
# Should show: /nix/store/.../ROOT

# Verify Go still works
$(nix build ./nix/go --print-out-paths 2>/dev/null)/bin/go version
# Should show: go version go1.24.9 ...
```

### Commit
Message: `Add ROOT marker to Go derivation for Bazel compatibility`

---

## Step 3: Initialize Bazel Workspace with bzlmod + rules_nixpkgs

### Files to Create

#### **`MODULE.bazel`** (new file at repository root)
```python
"""Bazel module definition for avalanchego."""

module(
    name = "avalanchego",
    version = "0.0.0",
)

# Core dependencies from Bazel Central Registry
bazel_dep(name = "bazel_skylib", version = "1.7.1")
bazel_dep(name = "platforms", version = "0.0.10")
bazel_dep(name = "rules_go", version = "0.50.1")
bazel_dep(name = "gazelle", version = "0.39.1")

# rules_nixpkgs for Nix integration
# Not in BCR, so we use git_override to fetch from GitHub
bazel_dep(name = "rules_nixpkgs_core", version = "0.13.0")
bazel_dep(name = "rules_nixpkgs_go", version = "0.13.0")

git_override(
    module_name = "rules_nixpkgs_core",
    remote = "https://github.com/tweag/rules_nixpkgs.git",
    # TODO: Pin to specific commit after initial testing
    # Find latest: git ls-remote https://github.com/tweag/rules_nixpkgs.git HEAD
    commit = "REPLACE_WITH_ACTUAL_COMMIT_SHA",
    strip_prefix = "core",
)

git_override(
    module_name = "rules_nixpkgs_go",
    remote = "https://github.com/tweag/rules_nixpkgs.git",
    commit = "REPLACE_WITH_ACTUAL_COMMIT_SHA",  # Same commit as above
    strip_prefix = "toolchains/go",
)

# Configure Go SDK from Nix derivation
# This ensures Bazel uses the exact same Go version as nix develop
nixpkgs_go = use_extension(
    "@rules_nixpkgs_go//:extensions.bzl",
    "nixpkgs_go",
)
nixpkgs_go.toolchain(
    nix_file = "//nix/go:bazel.nix",
    nix_file_deps = ["//nix/go:default.nix"],
)
use_repo(nixpkgs_go, "go_toolchains")

register_toolchains("@go_toolchains//:all")

# Go dependencies from go.mod
go_deps = use_extension("@gazelle//:extensions.bzl", "go_deps")
go_deps.from_file(go_mod = "//:go.mod")
use_repo(
    go_deps,
    # Will be populated by gazelle
)
```

**Important**: Before creating this file, get the actual commit SHA:
```bash
git ls-remote https://github.com/tweag/rules_nixpkgs.git HEAD
# Use the commit SHA in both git_override blocks
```

#### **`nix/go/bazel.nix`** (new file)
```nix
# Bazel wrapper for Go SDK
# Called by rules_nixpkgs_go to build the Go toolchain
#
# This file re-uses the same Go derivation as nix develop,
# ensuring version consistency between Nix and Bazel environments.
{ pkgs ? import <nixpkgs> {} }:
import ./default.nix { inherit pkgs; }
```

#### **`nix/go/BUILD.bazel`** (new file)
```python
# Make nix files visible to Bazel
exports_files([
    "bazel.nix",
    "default.nix",
])
```

#### **`.bazelrc`** (new file at repository root)
```
# Bazel configuration for avalanchego

# Enable bzlmod (default in Bazel 7+, explicit for clarity)
common --enable_bzlmod

# Build settings
build --incompatible_enable_cc_toolchain_resolution

# CGO flags for BLST cryptography library
# Must match scripts/build.sh: CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"
build --action_env=CGO_CFLAGS=-O2 -D__BLST_PORTABLE__
build --action_env=CGO_ENABLED=1

# Test settings
test --test_output=errors

# Performance
build --jobs=auto

# Nix integration - allow network for nix-build
# This may need adjustment based on your Bazel sandbox configuration
build --sandbox_add_mount_pair=/nix

# Version injection (added in Step 6)
# build --workspace_status_command=tools/bazel/workspace_status.sh
```

#### **`.bazelignore`** (new file at repository root)
```
# Directories to exclude from Bazel's file watching
.git
node_modules
.avalanchego
build
.direnv
result
```

### Validation Commands
```bash
# Verify Bazel recognizes the workspace
nix develop --command bazel version
# Should show version without errors about missing workspace

# Verify module resolution (may take time on first run)
nix develop --command bazel mod deps
# Should show dependency tree including rules_go, gazelle, rules_nixpkgs
```

### Commit
Message: `Initialize Bazel workspace with rules_nixpkgs Go integration`

---

## Step 4: Configure Gazelle and Generate Root BUILD

### Files to Create

#### **`BUILD.bazel`** (new file at repository root)
```python
load("@gazelle//:def.bzl", "gazelle")

# Gazelle configuration
# gazelle:prefix github.com/ava-labs/avalanchego
# gazelle:exclude graft/coreth
# gazelle:exclude .git
# gazelle:exclude build
# gazelle:exclude .direnv

gazelle(name = "gazelle")

# Target to update external Go dependencies
gazelle(
    name = "gazelle-update-repos",
    args = [
        "-from_file=go.mod",
        "-to_macro=deps.bzl%go_dependencies",
        "-prune",
    ],
    command = "update-repos",
)
```

### Why Exclude graft/coreth
The main module and coreth have circular `replace` directives:
- `go.mod`: `replace github.com/ava-labs/avalanchego/graft/coreth => ./graft/coreth`
- `graft/coreth/go.mod`: `replace github.com/ava-labs/avalanchego => ../../`

Handling this in Bazel requires careful configuration. For the minimal-first approach, we exclude coreth and focus on building avalanchego without coreth dependencies.

### Validation Commands
```bash
# Run gazelle to generate BUILD files
nix develop --command bazel run //:gazelle

# Check that BUILD.bazel files were created
find . -name "BUILD.bazel" -not -path "./.git/*" | head -20
# Should show BUILD.bazel files in various directories

# Verify no BUILD files in graft/coreth
ls graft/coreth/BUILD.bazel 2>/dev/null
# Should not exist (excluded)
```

### Commit
Message: `Add root BUILD.bazel with Gazelle configuration`

---

## Step 5: Generate BUILD Files and Fix Issues

### Actions
1. Run Gazelle to generate BUILD files throughout the codebase
2. Attempt to build and fix issues iteratively

### Common Issues and Fixes

#### Issue: Missing CGO flags for specific packages
Some packages (especially crypto-related) may need explicit CGO configuration.

Solution: Add to affected BUILD.bazel files:
```python
go_library(
    name = "...",
    # ... existing config ...
    cgo = True,
    copts = ["-O2", "-D__BLST_PORTABLE__"],
)
```

#### Issue: Replace directives not handled
Bazel doesn't automatically handle go.mod replace directives.

Solution: For external dependencies, add to MODULE.bazel:
```python
go_deps.module(
    path = "github.com/some/dependency",
    sum = "h1:...",
    version = "v1.2.3",
    replace = "github.com/fork/dependency",
)
```

#### Issue: Test files with special requirements
Some test files may need to be excluded from the initial build.

Solution: Add gazelle directives:
```python
# gazelle:exclude *_test.go
```

### Validation Commands
```bash
# Attempt build (will likely fail initially)
nix develop --command bazel build //cmd/avalanchego:all

# If errors, fix and retry iteratively
# Check specific error messages and adjust BUILD files

# Once individual fixes are applied, regenerate with gazelle
nix develop --command bazel run //:gazelle
```

### Commit
Message: `Generate initial BUILD files with Gazelle`

---

## Step 6: Build avalanchego Binary with Version Injection

### Files to Create/Modify

#### **`tools/bazel/workspace_status.sh`** (new file)
```bash
#!/bin/bash
# Workspace status script for Bazel
# Provides variables that can be used in x_defs for version injection
#
# Usage: build --workspace_status_command=tools/bazel/workspace_status.sh
# Access in BUILD: x_defs = {"...version.GitCommit": "{STABLE_GIT_COMMIT}"}

set -euo pipefail

# Git commit hash - matches scripts/build.sh behavior
echo "STABLE_GIT_COMMIT $(git rev-parse HEAD 2>/dev/null || echo 'unknown')"

# Optional: Add more status variables as needed
# echo "STABLE_BUILD_TIME $(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

Make executable:
```bash
chmod +x tools/bazel/workspace_status.sh
mkdir -p tools/bazel
```

#### **`.bazelrc`** - Add workspace status command
Uncomment/add this line:
```
build --workspace_status_command=tools/bazel/workspace_status.sh
```

#### **`cmd/avalanchego/BUILD.bazel`** - Modify generated file
After Gazelle generates this file, modify the `go_binary` target:

```python
go_binary(
    name = "avalanchego",
    embed = [":avalanchego_lib"],
    visibility = ["//visibility:public"],
    # Version injection - matches scripts/build.sh ldflags
    x_defs = {
        "github.com/ava-labs/avalanchego/version.GitCommit": "{STABLE_GIT_COMMIT}",
    },
)
```

### Validation Commands
```bash
# Build the binary
nix develop --command bazel build //cmd/avalanchego

# Find and run the binary
BINARY=$(nix develop --command bazel cquery --output=files //cmd/avalanchego 2>/dev/null)
$BINARY --version

# Verify git commit is embedded
$BINARY --version | grep -i commit
# Should show the current git commit hash
```

### Commit
Message: `Configure avalanchego binary build with version injection`

---

## Step 7: Add Task Wrapper

### Files to Modify

#### **`Taskfile.yml`** - Add Bazel tasks
Add after existing build tasks:

```yaml
  build-bazel:
    desc: Builds avalanchego using Bazel
    cmds:
      - nix develop --command bazel build //cmd/avalanchego
    sources:
      - "**/*.go"
      - "**/*.bazel"
      - MODULE.bazel
      - .bazelrc

  build-bazel-all:
    desc: Builds all Bazel targets
    cmds:
      - nix develop --command bazel build //...

  gazelle:
    desc: Updates BUILD.bazel files using Gazelle
    cmds:
      - nix develop --command bazel run //:gazelle

  check-gazelle:
    desc: Verifies BUILD.bazel files are up-to-date
    cmds:
      - nix develop --command bazel run //:gazelle
      - task: check-clean-branch
```

### Validation Commands
```bash
# Test the new task
task build-bazel
# Should build successfully

# Verify original build still works
task build
# Should build successfully (no regression)

# Compare binary sizes (should be similar, ±10%)
ls -la build/avalanchego
ls -la $(bazel cquery --output=files //cmd/avalanchego 2>/dev/null)
```

### Commit
Message: `Add Bazel build tasks to Taskfile`

---

## Troubleshooting Guide

### If rules_nixpkgs_go fails

**Symptom**: Errors about missing extensions or module not found

**Solution**: Fall back to explicit SDK registration. Replace the nixpkgs_go section in MODULE.bazel with:

```python
# Fallback: Explicit Go SDK registration
# Uses same SHA256 checksums as nix/go/default.nix for identical binaries
go_sdk = use_extension("@rules_go//go:extensions.bzl", "go_sdk")
go_sdk.download(
    name = "go_sdk",
    version = "1.24.9",
    sdks = {
        # SHA256s from nix/go/default.nix lines 24-29
        "linux_amd64": ("go1.24.9.linux-amd64.tar.gz", "5b7899591c2dd6e9da1809fde4a2fad842c45d3f6b9deb235ba82216e31e34a6"),
        "linux_arm64": ("go1.24.9.linux-arm64.tar.gz", "9aa1243d51d41e2f93e895c89c0a2daf7166768c4a4c3ac79db81029d295a540"),
        "darwin_amd64": ("go1.24.9.darwin-amd64.tar.gz", "961aa2ae2b97e428d6d8991367e7c98cb403bac54276b8259aead42a0081591c"),
        "darwin_arm64": ("go1.24.9.darwin-arm64.tar.gz", "af451b40651d7fb36db1bbbd9c66ddbed28b96d7da48abea50a19f82c6e9d1d6"),
    },
)
use_repo(go_sdk, "go_sdk")
```

### If Nix sandbox conflicts with Bazel

**Symptom**: Errors about /nix not being accessible

**Solution**: Add to `.bazelrc`:
```
build --sandbox_add_mount_pair=/nix
# Or disable sandbox for specific actions:
build --spawn_strategy=local
```

### If circular dependency errors appear

**Symptom**: Import cycle errors involving graft/coreth

**Solution**: Ensure `# gazelle:exclude graft/coreth` is in root BUILD.bazel and re-run gazelle.

---

## Success Criteria

After completing all steps:

1. ✅ `nix develop --command bazel build //cmd/avalanchego` produces working binary
2. ✅ Binary shows correct git commit in `--version` output
3. ✅ `task build-bazel` succeeds
4. ✅ Original `task build` still works (no regression)
5. ✅ All changes committed to `maru/bazel` branch

---

## What's Deferred (Future Work)

- **Coreth integration**: Handle circular dependencies with proper Bazel configuration
- **Code generation**: Protobuf, mocks, canoto via Bazel rules
- **Test execution**: `bazel test //...`
- **CI integration**: Remote caching, build event streaming
- **Rust/Firewood**: Add rules_nixpkgs_rust when Firewood is integrated

---

## References

- [rules_nixpkgs GitHub](https://github.com/tweag/rules_nixpkgs)
- [rules_go bzlmod docs](https://github.com/bazel-contrib/rules_go/blob/master/docs/go/core/bzlmod.md)
- [nix-bazel.build](https://nix-bazel.build/)
- [Bazel Central Registry - rules_nixpkgs_core](https://registry.bazel.build/modules/rules_nixpkgs_core)
- [Bazel git_override documentation](https://bazel.build/rules/lib/globals/module#git_override)
