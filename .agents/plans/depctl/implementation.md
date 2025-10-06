# depctl Implementation Notes

## Implementation Status: Complete

All 6 phases completed and tested. Tool is fully functional with comprehensive test coverage.

## Key Implementation Details

### Version Detection with Defaults

**Problem**: Originally, `GetVersion()` would error when a module wasn't found in go.mod.

**Solution**: Return default branch when module not found (tools/depctl/dep/version.go:55-75):
- avalanchego → `master`
- coreth → `master`
- firewood → `main`

Returns a `VersionInfo` struct:
```go
type VersionInfo struct {
    Version   string
    IsDefault bool
}
```

The CLI displays "(default)" indicator when using fallback branches.

### Indirect Dependency Detection

**Problem**: Firewood is typically an indirect dependency in avalanchego's go.mod.

**Solution**: `GetVersion()` searches ALL require directives in go.mod, both direct and indirect (tools/depctl/dep/version.go:32-43). The `modfile.Parse()` includes indirect dependencies in the `Require` slice.

### Shallow Clone with Fallback

**Problem**: `git clone --depth 1 --branch <ref>` fails when ref doesn't exist on remote (e.g., local branches, commit hashes not yet pushed).

**Solution** (tools/depctl/dep/clone.go:94-108):
1. Try shallow clone with `--depth 1 --branch <version>`
2. If that fails and shallow was requested, fall back to full clone
3. Then fetch and checkout the version

This handles:
- Tags that exist on remote (shallow clone succeeds)
- Branches that exist on remote (shallow clone succeeds)
- Commit hashes (shallow clone fails → full clone → checkout succeeds)
- Local branches not yet pushed (shallow clone fails → full clone → checkout succeeds)

### Version Resolution for Checkout

**Problem**: Short commit SHAs and some refs need to be resolved to full SHAs before `git checkout -B` works reliably.

**Solution** (tools/depctl/dep/clone.go:130-142):
```go
// Resolve version to full commit SHA if it's a short ref
resolvedVersion := version
resolveCmd := exec.Command("git", "rev-parse", "--verify", version+"^{commit}")
if output, err := resolveCmd.Output(); err == nil {
    resolvedVersion = strings.TrimSpace(string(output))
} else {
    // Try without ^{commit} for branches/commit SHAs
    resolveCmd = exec.Command("git", "rev-parse", "--verify", version)
    if output, err := resolveCmd.Output(); err == nil {
        resolvedVersion = strings.TrimSpace(string(output))
    }
}
```

This handles:
- Annotated tags (dereferences with `^{commit}`)
- Lightweight tags
- Short commit SHAs (expands to full SHA)
- Branch names (resolves to commit SHA)

### Self-Verification

**Feature**: After checkout, verify that HEAD matches the expected version (tools/depctl/dep/clone.go:138-167).

```go
func verifyCloneVersion(version string) error {
    // Get current HEAD commit
    headCommit := git rev-parse HEAD

    // Get commit for expected version (dereference annotated tags)
    versionCommit := git rev-parse version^{commit} || git rev-parse version

    // Compare commits
    if headCommit != versionCommit {
        return error
    }
}
```

This catches issues where:
- Git checkout silently succeeded but ended up at wrong commit
- Tag was moved after we fetched
- Repository is in unexpected state

### CloneResult Return Type

**Problem**: CLI was calling `GetVersion()` again after clone, which could return different version info than what was actually cloned.

**Solution** (tools/depctl/dep/clone.go:24-28, 74-77):
```go
type CloneResult struct {
    VersionInfo VersionInfo
    Path        string
}

func Clone(opts CloneOptions) (CloneResult, error)
```

`Clone()` now returns the actual `VersionInfo` used during cloning, so the success message shows the correct version.

### Quiet Git Operations

**Problem**: Git operations produced "wall of scary text" with detached HEAD warnings and verbose output.

**Solution** (tools/depctl/dep/clone.go:85-96):
- Use `git -c advice.detachedHead=false` to suppress detached HEAD warning
- Add `--quiet` flag to clone and other git operations
- Use `-q` flag on checkout
- Changed branch naming from `test-<version>` to `local/<version>` (clearer intent)
- Added success message: "Checked out v0.0.12 to branch local/v0.0.12"

### Integration Test Coverage

Comprehensive test suite (tools/depctl/dep/clone_integration_test.go):

1. **Clone avalanchego with no version** - uses default (master)
2. **Clone firewood with version in avalanchego** - tests indirect dependency detection
3. **Clone coreth with version in avalanchego**
4. **Clone avalanchego with version in coreth** - tests reverse dependency
5. **Clone firewood with no version in coreth** - uses default (main)
6. **Clone with commit hash (shallow fallback)** - tests shallow→full fallback and resolution

Each test:
- Creates isolated temp directory with test go.mod
- Performs actual git clone operations
- Verifies git repository exists
- Validates checked out version matches expected commit
- Validates branch name is `local/<version>`
- Can optionally retain test directories with `DEPCTL_KEEP_TEMP=1`

All tests pass in ~27 seconds total.

## Deviations from Original Plan

1. **Branch naming**: Changed from `test-<version>` to `local/<version>` for clarity
2. **Version detection**: Added fallback to default branches instead of erroring
3. **Shallow clone**: Added automatic fallback to full clone for refs not on remote
4. **Return type**: `Clone()` returns `CloneResult` instead of just error
5. **Verification**: Added self-verification step not in original plan
6. **go mod tidy**: Made non-fatal with warning (can fail in test scenarios)

## Usage Examples

```bash
# Get version from go.mod
depctl get-version --dep=firewood

# Update version
depctl update-version v1.11.11 --dep=avalanchego

# Clone at version from go.mod (shallow by default)
depctl clone --dep=firewood

# Clone at specific version
depctl clone --dep=avalanchego --version=v1.11.11

# Clone with commit hash (automatic fallback to full clone)
depctl clone --dep=avalanchego --version=e4a8e93f7

# Clone to specific path
depctl clone --dep=coreth --path=../coreth

# Non-shallow clone
depctl clone --dep=firewood --shallow=false
```

## Files Modified

- tools/depctl/main.go - CLI entry point
- tools/depctl/dep/repo.go - Alias resolution
- tools/depctl/dep/version.go - Version detection with defaults
- tools/depctl/dep/clone.go - Clone with fallback and verification
- tools/depctl/dep/clone_integration_test.go - Comprehensive tests
- tools/depctl/dep/clone_test.go - Unit tests
- tools/depctl/dep/version_test.go - Version parsing tests
- tools/depctl/dep/repo_test.go - Alias resolution tests
- tools/depctl/go.mod - Separate module with dependencies

## Known Limitations

1. **Nonexistent refs**: If a ref doesn't exist anywhere (not on remote, not in cloned repo), clone will fail. This is expected behavior matching git.

2. **go mod tidy failures**: In test scenarios where replace directives point to incomplete repos, `go mod tidy` will fail. This is non-fatal and logs a warning.

3. **Network dependency**: Integration tests require network access to clone real repositories. Tests use shallow clones where possible to minimize bandwidth.

4. **GitHub API rate limiting**: `update-version` command for avalanchego uses GitHub API to get full SHA. Use `GITHUB_TOKEN` env var for authenticated requests with higher rate limits.
