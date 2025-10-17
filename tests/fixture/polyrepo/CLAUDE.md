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

## Development Principles

### Document Knowledge First

**CRITICAL**: When you discover hard-won knowledge through debugging, manual testing, or implementation challenges, **DOCUMENT IT IMMEDIATELY** before continuing with implementation. This documentation is more valuable than the code itself because it prevents future developers (including yourself) from making the same mistakes.

Examples of knowledge to document immediately:
- Edge cases discovered during manual testing
- Quirks in third-party libraries (e.g., go-git doesn't support worktrees)
- Non-obvious git internals (e.g., worktree `.git` file structure)
- Error patterns that required special handling
- Why certain approaches were rejected

### Test-Driven Development (TDD)

**ALWAYS follow a test-first approach:**

1. **Enumerate corner cases FIRST** - Before writing any implementation code, list all the scenarios your code must handle:
   - Normal cases
   - Edge cases
   - Error conditions
   - Special configurations (e.g., git worktrees, shallow clones)
   - Boundary conditions

2. **Write integration tests for corner cases** - For each corner case, write an automated test that verifies the behavior

3. **Use manual testing to inform test design** - Manual testing is expensive and should primarily serve to:
   - Discover corner cases you didn't anticipate
   - Validate that tests match real-world behavior
   - Immediately convert manual test steps into automated integration tests

4. **Implement the code** - Write the minimal code needed to make tests pass

5. **Refactor** - Improve code quality while keeping tests green

### Proactive Detection Over Error-Based Fallbacks

**Bad Pattern (Error-Based Detection):**
```go
// Try approach A, catch errors, fall back to approach B
result, err := tryApproachA()
if err != nil {
    result, err = tryApproachB()
}
```

**Good Pattern (Proactive Detection):**
```go
// Detect the condition upfront, choose the right approach
if isSpecialCase() {
    result, err = handleSpecialCase()
} else {
    result, err = handleNormalCase()
}
```

**Why**: Error-based fallbacks hide bugs and make it unclear when each code path executes. Proactive detection makes the code self-documenting and catches errors earlier.

**Example from polyrepo**: Instead of trying go-git and falling back to git CLI on any error, we detect worktrees by checking if `.git` is a file, then read git data directly from the filesystem.

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

1. **Document corner cases** - List all scenarios your change must handle, including edge cases
2. **Write tests first** - Create integration tests for each corner case identified
3. **Capture hard-won knowledge** - If you discover something non-obvious during manual testing, add it to CLAUDE.md immediately
4. **Implement your code changes** - Write the minimal code to make tests pass
5. **Run the linter BEFORE executing code** (must pass without errors):
   ```bash
   ./scripts/run_task.sh lint
   ```
   **CRITICAL**: ALWAYS run the linter immediately after making code changes and BEFORE running or testing the code. The linter catches:
   - Syntax errors
   - Formatting issues
   - Unused variables/imports
   - Logic errors (nil checks, etc.)

   Running the linter first saves time by catching errors at compile-time rather than discovering them at runtime. Never skip this step.

6. **Verify with manual testing** - Use manual testing to discover corner cases you missed, then add tests for them
7. **Convert manual tests to automated tests** - Any manual test you perform should be converted to an integration test

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

### Corner Cases and Test Coverage

**Philosophy**: Enumerate ALL corner cases before implementing features. Each corner case should have at least one integration test.

#### Status Command Corner Cases

The status command must handle all combinations of these factors:

**Git Repository Types:**
- Normal git repository (`.git` is a directory)
- Git worktree (`.git` is a file containing `gitdir: /path/to/.git/worktrees/name`)
- Bare repository (not supported, should error gracefully)
- No git repository (`.git` doesn't exist)

**Git Directory Structure:**
- Normal repo: `.git/` contains HEAD, refs/tags/, refs/heads/, objects/, etc.
- Worktree: `.git/worktrees/name/` contains HEAD, `commondir` file
- Commondir: Points to main repo's `.git` where tags and branches are stored

**Commit References:**
- Detached HEAD at commit SHA
- HEAD pointing to local branch (`ref: refs/heads/main`)
- HEAD pointing to remote tracking branch
- Commit with multiple tags
- Commit with no tags
- Commit on multiple branches
- Commit on no branches (orphan commit)

**Replace Directives:**
- Repository with no replace directives
- Repository with replace directives for local paths (`../coreth`)
- Repository with replace directives for remote versions (`v1.2.3`)
- Repository with mixed local and remote replacements
- Primary repo vs synced repo go.mod files

**Repository Detection:**
- Running from avalanchego root
- Running from coreth root
- Running from firewood root (special case: `ffi/go.mod`)
- Running from unknown directory

**Test Coverage Goals:**
```bash
# Each of these scenarios should have automated integration tests:

# Normal git repositories
TestGetRepoStatus_NormalRepo
TestGetRepoStatus_WithTags
TestGetRepoStatus_WithMultipleBranches
TestGetRepoStatus_DetachedHead
TestGetRepoStatus_NoTags

# Git worktrees
TestGetRepoStatus_Worktree_DetachedHead
TestGetRepoStatus_Worktree_OnBranch
TestGetRepoStatus_Worktree_WithTags
TestGetRepoStatus_Worktree_CommonDirResolution

# Replace directives
TestGetRepoStatus_WithLocalReplaces
TestGetRepoStatus_WithVersionReplaces
TestGetRepoStatus_NoReplaces

# Repository detection
TestDetectCurrentRepo_FromAvalanchego
TestDetectCurrentRepo_FromCoreth
TestDetectCurrentRepo_FromFirewood  # Special ffi/go.mod case
TestDetectCurrentRepo_FromWorktree
TestDetectCurrentRepo_FromUnknownLocation

# Error conditions
TestGetRepoStatus_NoGitRepo
TestGetRepoStatus_InvalidGoMod
TestGetRepoStatus_MissingCommondir
```

#### Git Worktree Implementation Notes

**Critical Knowledge**: go-git library (github.com/go-git/go-git/v5) does NOT support git worktrees created with `git worktree add`. Attempting `PlainOpen()` on a worktree directory will fail.

**Solution**: Read git data directly from the filesystem:

1. **Detect worktree proactively**:
   ```go
   // Check if .git is a file (worktree) or directory (normal repo)
   gitPath := filepath.Join(repoPath, ".git")
   info, err := os.Stat(gitPath)
   if !info.IsDir() {
       // .git is a file, parse "gitdir: /path/to/.git/worktrees/name"
       content, _ := os.ReadFile(gitPath)
       gitDir := strings.TrimPrefix(string(content), "gitdir: ")
   }
   ```

2. **Find common directory** (where tags/branches are stored):
   ```go
   // Read .git/worktrees/name/commondir file
   // Points to main repo's .git directory (e.g., "../../..")
   commonDirPath := filepath.Join(gitDir, "commondir")
   content, _ := os.ReadFile(commonDirPath)
   commonDir := resolveRelativePath(gitDir, string(content))
   ```

3. **Read HEAD from worktree git dir**:
   ```go
   headPath := filepath.Join(gitDir, "HEAD")
   content, _ := os.ReadFile(headPath)
   if strings.HasPrefix(content, "ref: ") {
       // Symbolic ref, resolve from common dir
       refPath := strings.TrimPrefix(content, "ref: ")
       refContent, _ := os.ReadFile(filepath.Join(commonDir, refPath))
       commitSHA := string(refContent)
   } else {
       // Direct SHA
       commitSHA := string(content)
   }
   ```

4. **Read tags and branches from common dir**:
   ```go
   // Walk commonDir/refs/tags/ for tags
   // Walk commonDir/refs/heads/ for branches
   // Compare each ref's SHA to current commit SHA
   ```

**Testing worktrees**:
```bash
# Create a test worktree
git worktree add ../test-worktree main

# Run status from worktree
cd ../test-worktree
polyrepo status

# Cleanup
git worktree remove ../test-worktree
```

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

### Git Worktree Support and Proactive Detection (2025-10)

**Problem**: Initial implementation used error-based fallbacks (try go-git, catch any error, fall back to git CLI). This pattern:
- Hid bugs by catching all errors indiscriminately
- Made it unclear which code path would execute
- Didn't work for git worktrees since go-git doesn't support them

**Solution**: Proactive detection and direct filesystem access:
- Detect worktrees upfront by checking if `.git` is a file vs directory
- Parse `gitdir:` reference from `.git` file to find worktree's git directory
- Read `commondir` file to find main repo's `.git` where tags/branches live
- Read git data directly from filesystem (HEAD, refs/tags/, refs/heads/)
- Eliminate dependency on go-git library for worktrees

**Key Functions**:
- `getGitDir(repoPath)`: Detects normal repo vs worktree, returns git directory path
- `getCommonDir(gitDir)`: Finds the common directory where refs are stored
- `GetCommitSHA()`: Reads HEAD from worktree git dir, resolves refs from common dir
- `getTagsFromGitDir()`: Walks refs/tags/ directory, compares SHAs
- `getBranchesFromGitDir()`: Walks refs/heads/ directory, compares SHAs

**Test Coverage**:
- `TestGetRepoStatus_Worktree_*`: Tests for various worktree scenarios
- Tests cover: detached HEAD in worktree, branch in worktree, tags, commondir resolution

**Hard-Won Knowledge**:
- go-git library doesn't support worktrees (open issue on GitHub)
- Worktrees have `.git` as a file, not a directory
- `commondir` file contains relative path (e.g., `../../..`) or absolute path
- HEAD can be in worktree git dir, but refs are in common dir
- Tags and branches are ALWAYS in common dir, never in worktree git dir
- Must handle both symbolic refs (`ref: refs/heads/main`) and direct SHAs

**Development Pattern**:
This fix demonstrates the importance of:
1. Documenting third-party library limitations (go-git)
2. Proactive detection over error-based fallbacks
3. Reading documentation/specs (git internals) before implementing
4. Comprehensive integration testing for edge cases (worktrees)

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

- **ALWAYS document hard-won knowledge immediately** - Don't wait until implementation is complete
- **Write tests before implementation** - Enumerate corner cases first, then write tests
- **Run linter immediately after code changes, before execution** - Catch errors at compile-time, not runtime
- **Use proactive detection, not error-based fallbacks** - Detect special cases upfront
- **Manual testing is for discovering corner cases** - Then convert to automated tests
- **Git worktrees require direct filesystem access** - go-git doesn't support them
- **Worktree .git file contains `gitdir:` reference** - Parse it to find worktree's git directory
- **Worktree refs are in commondir, not worktree git dir** - Read commondir file to find main repo
- **NEVER build the polyrepo binary directly** - ALWAYS use `./scripts/run_polyrepo.sh`
- **The binary must NEVER be committed to the repository** - it will clutter the git history
- Replace directives must always use relative paths starting with `./` or `../`
- `filepath.Join()` normalizes away `./` prefix, so construct paths carefully
- Firewood has a special structure: go.mod is in `ffi/` subdir, replace points to `ffi/result/ffi` (nix output)
- The dependency graph is bidirectional between avalanchego and coreth
- Always test with all three repo combinations when modifying sync logic
