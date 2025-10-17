# Polyrepo Tool - Developer Guide

## Overview

The polyrepo tool manages local development across three interdependent repositories:
- **avalanchego** - Main repository (github.com/ava-labs/avalanchego)
- **coreth** - EVM implementation (github.com/ava-labs/coreth)
- **firewood** - Storage layer (github.com/ava-labs/firewood)

### Dependency Graph

```
avalanchego ←→ coreth
    ↓           ↓
  firewood ← firewood
```

- avalanchego and coreth have bidirectional dependencies
- Both avalanchego and coreth depend on firewood
- Firewood has no dependencies on the other repos

## Key Concepts

### Replace Directives

The tool manages `replace` directives in go.mod files to point to local clones instead of remote versions. This enables:
- Local development across repos without publishing intermediate changes
- Testing changes in dependencies before committing
- Faster iteration cycles

### Repository Structure

- **Primary repo**: The repo you're currently working in (detected via go.mod)
- **Synced repos**: Repos cloned by polyrepo into subdirectories

Example directory structure after syncing from avalanchego:
```
avalanchego/              # Primary repo
├── go.mod                # Has replace directives for coreth and firewood
├── coreth/               # Synced repo
│   └── go.mod            # Has replace directives for avalanchego and firewood
└── firewood/             # Synced repo
    └── ffi/
        └── go.mod        # No replace directives (no deps on other repos)
```

## Usage

### ⚠️ CRITICAL: Never Build the Binary Directly

**DO NOT run `go build` in the polyrepo directory.** The binary must NEVER be committed to the repository.

**ONLY USE**: `./scripts/run_polyrepo.sh` - This wrapper handles building and running without cluttering the working tree.

### Basic Commands

**ALWAYS invoke via the wrapper script:**

```bash
./scripts/run_polyrepo.sh <command>
```

This is the ONLY supported way to run the tool. It builds and runs the binary in a temporary location without leaving artifacts in the working tree.

### Development Workflow

**IMPORTANT**: Follow this workflow for all changes:

1. Make your code changes
2. Run the linter (must pass without errors):
   ```bash
   ./scripts/run_task.sh lint
   ```
3. Test your changes:
   ```bash
   ./scripts/run_polyrepo.sh <command>
   ```

**NEVER run `go build` directly in the polyrepo directory.**

### Sync Command

Clone/update repositories and configure replace directives:

```bash
# Sync specific repos with explicit refs
polyrepo sync firewood@7cd05ccda8baba48617de19684db7da9ba73f8ba coreth --force

# Sync without explicit refs (uses versions from go.mod)
polyrepo sync coreth firewood

# Auto-sync based on current repo
polyrepo sync
```

**How sync works:**
1. Clones each specified repo at the given ref (or determines ref from go.mod)
2. Runs nix build for repos that require it (firewood)
3. Calls `UpdateAllReplaceDirectives()` to update go.mod files for ALL repos (primary + synced)
4. This ensures the complete dependency graph has replace directives

**Flags:**
- `--depth, -d`: Clone depth (0=full, 1=shallow, >1=partial). Default: 1
- `--force, -f`: Force sync even if directory is dirty or already exists
- `--log-level`: Set logging level (debug, info, warn, error). Default: info

### Status Command

Show status of all polyrepo repositories:

```bash
polyrepo status
```

The status command automatically detects which repository you're currently in and displays:
- **Primary Repository**: The repo you're currently working in (or "none" if not in a known repo)
- **Other Repositories**: Status of other polyrepo-managed repos

**Output from avalanchego:**
```
Primary Repository: avalanchego
  avalanchego: /path/to/avalanchego

Other Repositories:
  coreth: not cloned
  firewood: not cloned
```

**Output from unknown location:**
```
Primary Repository: none (not in a known repository)

Other Repositories:
  avalanchego: not cloned
  coreth: not cloned
  firewood: not cloned
```

For each repository, displays:
- Whether the repo is cloned
- Current branch or commit
- Whether working directory is dirty
- Whether replace directives are active

### Reset Command

Remove replace directives from go.mod:

```bash
# Reset all repos
polyrepo reset

# Reset specific repos
polyrepo reset coreth firewood
```

### Update Command

Update avalanchego dependency version (for coreth repo):

```bash
# Update to specific version
polyrepo update-avalanchego v1.13.6

# Update to version from go.mod
polyrepo update-avalanchego
```

## Implementation Details

### Core Files

- **main.go**: CLI commands and command handlers
- **core/sync.go**: Sync logic and `UpdateAllReplaceDirectives()`
- **core/repo.go**: Git operations (clone, checkout, status)
- **core/gomod.go**: go.mod parsing and manipulation
- **core/config.go**: Repository configurations
- **core/status.go**: Status reporting
- **core/errors.go**: Custom error types

### Key Functions

#### `UpdateAllReplaceDirectives(log, baseDir, syncedRepos)`
**Location**: core/sync.go:164-316

This is the critical function that ensures all repos have correct replace directives. Called after syncing completes.

**Algorithm:**
1. Build set of all repos (primary + synced)
2. For each repo:
   - Locate its go.mod file
   - For each OTHER repo in the set:
     - Check if this repo depends on the other repo
     - If yes, calculate relative path and add replace directive

**Path Calculation:**
- Primary → synced: `./coreth`, `./firewood/ffi/result/ffi`
- Synced → primary: `..`
- Synced → synced: `../coreth`, `../firewood/ffi/result/ffi`

#### `CloneOrUpdateRepo(log, url, path, ref, depth, force)`
**Location**: core/repo.go:238-371

Handles cloning and updating with special logic for:
- Shallow clones with commit SHAs (fetches from GitHub API to find branch)
- Force updates (removes and re-clones)
- Fallback to full clone if shallow fails

#### `GetRepoStatus(log, repoName, baseDir, goModPath, isPrimary)`
**Location**: core/status.go:26-74

Returns the status of a repository including whether it exists, current ref, dirty status, and replace directives.

**Parameters:**
- `isPrimary`: If true, the repo path is `baseDir` itself (we're currently in this repo). If false, the repo path is `baseDir/repoName` (synced repo).

This allows the status command to correctly check the primary repository (current directory) vs synced repositories (subdirectories).

#### `DetectCurrentRepo(log, dir)`
**Location**: core/sync.go:16-76

Detects which repository we're currently in by examining go.mod files.

**Detection logic:**
- Checks for `go.mod` in current directory for avalanchego and coreth
- Checks for `ffi/go.mod` for firewood (special case)
- Returns the repo name or empty string if not in a known repo

### Repository Configurations

Defined in core/config.go:

```go
avalanchegoConfig = &RepoConfig{
    Name:                  "avalanchego",
    GoModule:              "github.com/ava-labs/avalanchego",
    ModuleReplacementPath: ".",  // Root of repo
    GitRepo:               "https://github.com/ava-labs/avalanchego",
    DefaultBranch:         "master",
    GoModPath:             "go.mod",  // Relative to repo root
}

firewoodConfig = &RepoConfig{
    Name:                  "firewood",
    GoModule:              "github.com/ava-labs/firewood/ffi",
    ModuleReplacementPath: "./ffi/result/ffi",  // Special nix build output
    GitRepo:               "https://github.com/ava-labs/firewood",
    DefaultBranch:         "main",
    GoModPath:             "ffi/go.mod",  // go.mod is in ffi subdir
    RequiresNixBuild:      true,
    NixBuildPath:          "ffi",
}
```

## Testing

### Unit Tests

Run unit tests (no network access required):
```bash
go test ./tests/fixture/polyrepo/core/... -v
```

### Integration Tests

Run integration tests (requires network access, clones real repos):
```bash
go test ./tests/fixture/polyrepo/core/... -v -tags=integration
```

**Key integration tests:**
- `TestUpdateAllReplaceDirectives_FromAvalanchego`: Validates complete dependency graph updates
- `TestUpdateAllReplaceDirectives_MultipleRepos`: Tests all sync combinations
- `TestCloneOrUpdateRepo_ShallowToSHAWithForce`: Tests shallow clone → SHA update
- `TestGetDefaultRefForRepo_WithPseudoVersion`: Tests pseudo-version extraction
- `TestDetectCurrentRepo_FromAvalanchego`: Tests repo detection from avalanchego
- `TestDetectCurrentRepo_FromCoreth`: Tests repo detection from coreth
- `TestDetectCurrentRepo_FromFirewood`: Tests repo detection from firewood (ffi/go.mod)
- `TestDetectCurrentRepo_FromUnknownLocation`: Tests repo detection from unknown location
- `TestGetRepoStatus_*`: Tests status reporting for primary and synced repos

### Manual Testing

**Before testing, always run lint:**
```bash
./scripts/run_task.sh lint
```

1. **From avalanchego, sync coreth and firewood:**
   ```bash
   cd avalanchego
   ./scripts/run_polyrepo.sh sync
   ```

   Verify:
   - `avalanchego/go.mod` has replace directives for coreth and firewood
   - `avalanchego/coreth/go.mod` has replace directives for avalanchego and firewood
   - `go build ./...` succeeds

2. **Sync specific commit:**
   ```bash
   ./scripts/run_polyrepo.sh sync firewood@7cd05ccda8baba48617de19684db7da9ba73f8ba --force
   ```

   Verify:
   - Firewood is cloned at the specific commit
   - Replace directives are updated

3. **Check status:**
   ```bash
   ./scripts/run_polyrepo.sh status
   ```

## Recent Fixes

### Status Command Primary Repo Detection (2025-10)

**Enhancement**: The status command now detects and displays which repository you're currently in.

**Changes**:
- Added `DetectCurrentRepo()` to identify the primary repository based on go.mod
- Modified `GetRepoStatus()` to accept `isPrimary` parameter to correctly handle primary vs synced repos
- Updated status command output to show "Primary Repository" and "Other Repositories" sections
- Correctly handles firewood's special `ffi/go.mod` structure

**Test Coverage**:
- `TestDetectCurrentRepo_FromAvalanchego`: Tests detection from avalanchego
- `TestDetectCurrentRepo_FromCoreth`: Tests detection from coreth
- `TestDetectCurrentRepo_FromFirewood`: Tests detection from firewood with ffi/go.mod
- `TestDetectCurrentRepo_FromUnknownLocation`: Tests from unknown location

**Behavior**:
- When run from avalanchego/coreth/firewood, shows that repo as primary
- When run from an unknown location, shows "none (not in a known repository)"
- Primary repo status checks the current directory, not a subdirectory
- Other repos check for clones in subdirectories

### Replace Directive Propagation (2025-10)

**Problem**: Replace directives were only added to the primary repo's go.mod. Synced repos (like coreth) didn't get replace directives for their dependencies (like firewood), causing build failures.

**Solution**:
- Removed per-repo `AddReplaceDirective` calls during sync loop
- Added `UpdateAllReplaceDirectives()` call after all syncing completes
- Function analyzes full dependency graph and updates ALL go.mod files

**Test Coverage**:
- `TestUpdateAllReplaceDirectives_FromAvalanchego`: Full graph validation
- `TestUpdateAllReplaceDirectives_MultipleRepos`: Various combinations
  - Sync both coreth + firewood
  - Sync only firewood
  - Sync only coreth

## Common Patterns

### Adding a New Repository

1. Add config to core/config.go:
   ```go
   newRepoConfig = &RepoConfig{
       Name:                  "newrepo",
       GoModule:              "github.com/ava-labs/newrepo",
       ModuleReplacementPath: ".",
       GitRepo:               "https://github.com/ava-labs/newrepo",
       DefaultBranch:         "main",
       GoModPath:             "go.mod",
   }
   ```

2. Add to `allRepos` map in config.go

3. Update `GetReposToSync()` in core/sync.go if needed

4. Add integration tests

### Debugging

Enable debug logging:
```bash
./scripts/run_polyrepo.sh --log-level=debug sync coreth
```

Check what would be updated:
```bash
./scripts/run_polyrepo.sh status
```

## Troubleshooting

### "replacement module without version must be directory path"

This error means a replace directive doesn't start with `./`, `../`, or `/`.

**Cause**: Path calculation in `UpdateAllReplaceDirectives()` is incorrect.

**Fix**: Ensure paths are built to maintain the `./` or `../` prefix (don't use filepath.Join for the first segment).

### "repository already exists"

**Cause**: Trying to sync without `--force` when repos already exist.

**Fix**: Use `--force` flag or delete the directory first.

### Shallow clone SHA issues

**Cause**: Trying to checkout a SHA that's not in a shallow clone's history.

**Fix**: Tool automatically detects this and re-clones with the branch containing the SHA.

## Notes for Future Development

- **NEVER build the polyrepo binary directly** - ALWAYS use `./scripts/run_polyrepo.sh`
- **The binary must NEVER be committed to the repository** - it will clutter the git history
- Replace directives must always use relative paths starting with `./` or `../`
- `filepath.Join()` normalizes away `./` prefix, so construct paths carefully
- Firewood has a special structure: go.mod is in `ffi/` subdir, replace points to `ffi/result/ffi` (nix output)
- The dependency graph is bidirectional between avalanchego and coreth
- Always test with all three repo combinations when modifying sync logic
