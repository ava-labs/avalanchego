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

**CRITICAL**: ALL changes to the codebase MUST be documented in CLAUDE.md. This is not optional. Documentation is as important as the code itself because it prevents future developers (including yourself) from:
- Repeating mistakes
- Breaking working code due to lack of context
- Spending time re-discovering knowledge you already learned

**What to document:**
- **All features/capabilities/flags** - Even simple additions like command-line flags must be documented in the relevant sections
- **All bug fixes** - With root cause analysis and what was learned
- **Hard-won knowledge from debugging** - Edge cases, quirks, non-obvious behavior
- **New functions and their purpose** - Update Implementation Details section
- **New tests** - Add to the test list so others know what coverage exists
- **Quirks in third-party libraries** - (e.g., go-git doesn't support worktrees)
- **Non-obvious internals** - (e.g., worktree `.git` file structure)
- **Error patterns that required special handling**
- **Why certain approaches were rejected**

**When to document:**
- **IMMEDIATELY after completing any change** - Don't wait until "later"
- **As you discover non-obvious behavior** - Document during manual testing
- **After writing tests** - Add test names to the documentation

This documentation is more valuable than the code itself because code shows "what" but documentation explains "why" and "how we got here".

### Test-Driven Development (TDD)

**ALWAYS follow a test-first approach (Red-Green-Refactor cycle):**

1. **Enumerate corner cases FIRST** - Before writing any implementation code, list all the scenarios your code must handle:
   - Normal cases
   - Edge cases
   - Error conditions
   - Special configurations (e.g., git worktrees, shallow clones)
   - Boundary conditions

2. **Write integration tests for corner cases** - For each corner case, write an automated test that verifies the behavior

3. **🔴 RED: Verify tests FAIL** - **CRITICAL AND MANDATORY STEP:**
   - Run the tests you just wrote
   - Verify they fail with appropriate errors (e.g., "undefined: FunctionName" for missing functions)
   - **DO NOT SKIP THIS STEP** - This confirms your tests actually test something
   - If tests pass when they should fail, the tests are broken
   - Document the failure output to confirm tests are correctly written

   **Example:**
   ```bash
   go test ./path/to/tests -v -run=TestMyNewFeature
   # Should see: undefined: MyNewFeature
   ```

4. **Use manual testing to inform test design** - Manual testing is expensive and should primarily serve to:
   - Discover corner cases you didn't anticipate
   - Validate that tests match real-world behavior
   - Immediately convert manual test steps into automated integration tests

5. **🟢 GREEN: Implement the code** - Write the minimal code needed to make tests pass

6. **🟢 GREEN: Verify tests PASS** - Run tests again to confirm they now pass with your implementation

7. **♻️ REFACTOR: Improve code quality** - Refactor while keeping tests green

### When Fixing Bugs: TEST FIRST, ALWAYS

**CRITICAL LESSON**: When a bug is discovered (whether through manual testing, user report, or code review), the SECOND step after identifying the nature of the problem is to write a test that reproduces it. **DO NOT jump straight to fixing the code.**

**Why this matters:**
1. **Faster iteration** - A failing test provides immediate feedback when you fix the code, without needing to manually reproduce the issue
2. **Prevents regression** - The test ensures this bug never comes back
3. **Documents the bug** - The test serves as living documentation of what went wrong
4. **Poor test coverage is the root cause** - If a bug reached production/manual testing, it means test coverage was inadequate

**The Bug-Fix Workflow:**
1. **Identify the bug** - Understand what's failing and why
2. **Write a test that reproduces the bug** - The test should fail initially
3. **Verify the test fails** - Run the test to confirm it catches the bug
4. **Fix the code** - Make minimal changes to make the test pass
5. **Verify the test passes** - Confirm the fix works
6. **Add additional tests** - Cover related corner cases you may have missed

**Example from this codebase:**
- **Bug**: `polyrepo sync coreth` failed with "couldn't find remote ref refs/heads/v0.15.4-rc.4"
- **Root cause**: Tags were being treated as branches (refs/heads/ vs refs/tags/)
- **What went wrong**: No test coverage for cloning with tag references - only branches and SHAs were tested
- **Correct approach**:
  1. Identify that CloneRepo lacks tag handling
  2. Write test: `TestCloneRepo_WithTag` that clones a repo using a tag reference
  3. See it fail with the exact error from manual testing
  4. Fix the code to detect and handle tags
  5. See the test pass
  6. Add more tag tests (annotated tags, lightweight tags, etc.)

**Remember**: Every bug that makes it to manual testing or production represents a gap in test coverage. Fill that gap IMMEDIATELY with automated tests before fixing the bug.

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

### Operating Modes

The polyrepo tool supports two distinct operating modes:

#### 1. Primary Repo Mode
When running polyrepo from within a repository that contains:
- `go.mod` (for avalanchego or coreth)
- `ffi/go.mod` (for firewood)

**Behavior:**
- The current repository is detected as the "primary repo"
- Other repos are synced into subdirectories relative to the primary repo
- Replace directives are automatically configured based on dependencies in go.mod
- If no explicit refs are specified, versions are read from the primary repo's go.mod

**Example:**
```bash
cd avalanchego/
./scripts/run_polyrepo.sh sync coreth firewood
# Clones coreth/ and firewood/ as subdirectories
# Uses versions from avalanchego/go.mod if not explicitly specified
```

**Special Case: Version Discovery Without Primary Repo Dependencies**

When there is no primary repo with go.mod dependencies to guide version selection, version discovery follows the dependency chain:

**Avalanchego ↔ Coreth Discovery:**

When neither avalanchego nor coreth can be discovered from a primary repo's go.mod (firewood is primary, or no primary repo):

- **Both requested without versions**: Clone avalanchego at master, discover coreth from avalanchego/go.mod:
  ```bash
  cd firewood/  # or any directory without go.mod
  ./scripts/run_polyrepo.sh sync avalanchego coreth
  # Clones avalanchego at master
  # Discovers coreth from avalanchego/go.mod
  ```

- **One version specified, other not**: Clone the specified repo first, discover the other:
  ```bash
  cd firewood/
  ./scripts/run_polyrepo.sh sync avalanchego@v1.11.0 coreth
  # Clones avalanchego at v1.11.0
  # Discovers coreth from avalanchego/go.mod
  ```

- **One already cloned**: Discover the other from the cloned repo's go.mod:
  ```bash
  cd firewood/
  # avalanchego/ already exists
  ./scripts/run_polyrepo.sh sync coreth
  # Discovers coreth from avalanchego/go.mod
  ```

**Firewood Discovery:**

Firewood version is discovered through the dependency chain (coreth → firewood direct, avalanchego → firewood indirect):

- **Explicit version**: Use it
  ```bash
  ./scripts/run_polyrepo.sh sync avalanchego coreth firewood@v0.2.0
  ```

- **Coreth involved (cloned or being synced)**: Discover from coreth/go.mod (direct dependency)
  ```bash
  ./scripts/run_polyrepo.sh sync avalanchego coreth firewood
  # Clones avalanchego at master
  # Discovers coreth from avalanchego/go.mod
  # Discovers firewood from coreth/go.mod (direct dependency)
  ```

- **Only avalanchego involved**: Discover from avalanchego/go.mod (indirect dependency)
  ```bash
  ./scripts/run_polyrepo.sh sync avalanchego firewood
  # Clones avalanchego at master
  # Discovers firewood from avalanchego/go.mod (indirect via coreth)
  ```

- **Firewood alone, no primary repo**: Use default branch (main)
  ```bash
  mkdir /tmp/workspace
  ./scripts/run_polyrepo.sh sync firewood --target-dir /tmp/workspace
  # Clones firewood at main (no way to discover version)
  ```

**Key Principle**: Follow the dependency chain: avalanchego ↔ coreth (bidirectional), coreth → firewood (direct), avalanchego → firewood (indirect). Only repos explicitly listed are synced.

#### 2. Standalone Mode (No Primary Repo)
When running polyrepo from a directory that does NOT contain a go.mod file:

**Behavior:**
- No primary repo is detected
- For avalanchego and coreth: version discovery follows the same logic as described in "Version Discovery Without Primary Repo Dependencies" above
- For firewood: discovered from coreth or avalanchego (if involved), or uses default branch (main) if synced alone
- If explicit refs are provided (e.g., `avalanchego@v1.11.0`), those are used
- No replace directives are configured (since there's no go.mod to update)
- Useful for setting up a fresh workspace or cloning repos for inspection

**Example:**
```bash
mkdir /tmp/test-workspace
cd /tmp/test-workspace
./scripts/run_polyrepo.sh sync avalanchego coreth
# Clones avalanchego at master
# Discovers coreth from avalanchego/go.mod (ensures compatibility)
# No replace directives (no go.mod files to update)
```

**Use Cases:**
- Setting up a new development workspace from scratch
- Cloning specific versions of repos for testing/comparison
- Inspecting repository contents without setting up full development environment

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

### Global Flags

All commands support these flags:

- `--target-dir <path>`: Target directory to operate in (defaults to current directory)
  - Accepts both absolute and relative paths
  - The tool will change to this directory before executing any command
  - Useful for running polyrepo commands on a different directory without `cd`ing into it

- `--log-level <level>`: Set logging level (debug, info, warn, error). Default: info

**Examples:**
```bash
# Run status on a different directory (absolute path)
./scripts/run_polyrepo.sh --target-dir /path/to/avalanchego status

# Run status on a different directory (relative path)
./scripts/run_polyrepo.sh --target-dir ../other-project status

# Sync with a specific target directory
./scripts/run_polyrepo.sh --target-dir /path/to/repo sync coreth

# Reset in a different directory
./scripts/run_polyrepo.sh --target-dir /path/to/repo reset
```

### Development Workflow

**IMPORTANT**: Follow this workflow for all changes:

1. **Document corner cases** - List all scenarios your change must handle, including edge cases
2. **Write tests first** - Create integration tests for each corner case identified
3. **🔴 RED: Verify tests FAIL** - **MANDATORY STEP**:
   ```bash
   go test ./path/to/tests -v -run=TestYourNewFeature
   # Verify you see "undefined" or appropriate failure messages
   ```
   This confirms tests actually test something. If tests pass when they should fail, the tests are broken.
4. **Capture hard-won knowledge** - If you discover something non-obvious during manual testing, add it to CLAUDE.md immediately
5. **🟢 GREEN: Implement your code changes** - Write the minimal code to make tests pass
6. **Run the linter BEFORE executing code** (must pass without errors):
   ```bash
   ./scripts/run_task.sh lint
   ```
   **CRITICAL**: ALWAYS run the linter immediately after making code changes and BEFORE running or testing the code. The linter catches:
   - Syntax errors
   - Formatting issues
   - Unused variables/imports
   - Logic errors (nil checks, etc.)

   Running the linter first saves time by catching errors at compile-time rather than discovering them at runtime. Never skip this step.

7. **🟢 GREEN: Run tests to verify they PASS** - Confirm your implementation makes the tests pass
8. **Verify with manual testing** - Use manual testing to discover corner cases you missed, then add tests for them
9. **Convert manual tests to automated tests** - Any manual test you perform should be converted to an integration test
10. **Update CLAUDE.md documentation** - **MANDATORY for ALL changes**:
   - **New features/flags/capabilities**: Document in "Usage" → relevant command section, add to "Global Flags" or command-specific flags, and create entry in "Recent Fixes" section
   - **Bug fixes**: Document in "Recent Fixes" section with problem description, solution, test coverage, and root cause analysis
   - **New core functions**: Update "Implementation Details" → "Key Functions" section
   - **New tests**: Add test names to "Testing" → "Key integration tests" list
   - **Hard-won knowledge**: Add to relevant sections (e.g., "Git Worktree Implementation Notes")
   - **This step is MANDATORY, not optional** - If you skip this, future developers (including yourself) will lack critical context

**NEVER run `go build` directly in the polyrepo directory.**

### Sync Command

Clone/update repositories and configure replace directives:

```bash
# Primary repo mode: Sync specific repos with explicit refs
cd avalanchego/
polyrepo sync firewood@7cd05ccda8baba48617de19684db7da9ba73f8ba coreth --force

# Primary repo mode: Sync without explicit refs (uses versions from go.mod)
cd avalanchego/
polyrepo sync coreth firewood

# Primary repo mode: Auto-sync based on current repo
cd avalanchego/
polyrepo sync

# Standalone mode: Clone repos into empty directory
mkdir /tmp/workspace
polyrepo sync avalanchego@v1.11.0 coreth@main --target-dir /tmp/workspace
```

**How sync works:**

**In Primary Repo Mode (go.mod exists):**
1. Detects current repository as primary repo
2. Clones each specified repo at the given ref (or determines ref from go.mod if not specified)
3. Runs nix build for repos that require it (firewood)
4. Calls `UpdateAllReplaceDirectives()` to update go.mod files for ALL repos (primary + synced)
5. This ensures the complete dependency graph has replace directives

**In Standalone Mode (no go.mod):**
1. Clones each specified repo at the given ref (or uses default branch if not specified)
2. Runs nix build for repos that require it (firewood)
3. No replace directives are configured (no go.mod files present)

**Flags:**
- `--depth, -d`: Clone depth (0=full, 1=shallow, >1=partial). Default: 1
- `--force, -f`: Force sync even if directory is dirty or already exists
- `--target-dir`: Target directory to operate in (defaults to current directory)
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

- **main.go**: CLI argument parsing and routing to core functions (thin layer, minimal business logic)
- **core/sync.go**: Sync orchestration (`Sync()`), version discovery, and `UpdateAllReplaceDirectives()`
- **core/repo.go**: Git operations (clone, checkout, status)
- **core/gomod.go**: go.mod parsing and manipulation
- **core/reset.go**: Reset directive removal (`Reset()`)
- **core/update.go**: Dependency version updates (`UpdateAvalanchego()`)
- **core/config.go**: Repository configurations
- **core/status.go**: Status reporting and display (`Status()`)
- **core/errors.go**: Custom error types

### Architecture: Separation of Concerns

**Design Principle**: All business logic lives in the `core/` package. The `main.go` file contains only:
- CLI command definitions (cobra commands)
- Argument parsing and validation
- Routing to appropriate core functions
- Minimal logic (typically 5-10 lines per command)

**Benefits**:
- **Testability**: Business logic can be unit tested without invoking the CLI
- **Reusability**: Core functions can be called from other Go code, not just the CLI
- **Maintainability**: Clear separation makes code easier to understand and modify
- **Bug Prevention**: Unit tests catch bugs that previously required integration tests

**Pattern for Adding New Commands**:
1. Write unit tests first for the core function (TDD)
2. Implement business logic in a `core/` function
3. Create a thin CLI command handler in `main.go` that just calls the core function
4. The CLI handler should only handle: argument parsing, getting baseDir, calling core function

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

#### `Sync(log, baseDir, repoArgs, depth, force)`
**Location**: core/sync.go

Orchestrates the complete repository synchronization workflow. This is the main entry point for the sync command.

**Modes:**
- **Primary Repo Mode** (go.mod exists): Detects current repo, discovers versions from go.mod, updates replace directives
- **Standalone Mode** (no go.mod): Uses version discovery functions, no replace directives

**Algorithm:**
1. Detect operating mode (check if go.mod exists)
2. Parse repo arguments (`repo[@ref]` format)
3. Determine refs for each repo (explicit, from go.mod, or via version discovery)
4. Validate (cannot sync repo into itself in primary mode)
5. Execute sync loop (clone/update, nix build)
6. Update replace directives (primary mode only)

**Version Discovery Integration:**
- Calls `DiscoverAvalanchegoCorethVersions()` in standalone mode
- Calls `DiscoverFirewoodVersion()` in standalone mode
- These functions were previously unused but are now integrated

**Error Handling**: Fails fast on first error

#### `Reset(log, baseDir, repoNames)`
**Location**: core/reset.go

Removes replace directives from go.mod for specified repositories.

**Behavior:**
- If `repoNames` is empty: removes all polyrepo replace directives
- If `repoNames` provided: removes only specified repos' replace directives
- Validates that go.mod exists in baseDir (returns error if missing)

**Usage:**
- Called by reset command in main.go
- Can be unit tested without CLI

#### `UpdateAvalanchego(log, baseDir, version)`
**Location**: core/update.go

Updates the avalanchego dependency version in go.mod.

**Behavior:**
- If `version` is empty: uses current version from go.mod
- If `version` provided: updates to specified version
- Validates that go.mod exists in baseDir
- Cannot be run from avalanchego repository itself

**Usage:**
- Called by update-avalanchego command in main.go
- Can be unit tested without CLI

#### `Status(log, baseDir, writer)`
**Location**: core/status.go

Displays the status of all polyrepo-managed repositories.

**Algorithm:**
1. Detect primary repository via `DetectCurrentRepo()`
2. Determine go.mod path (primary repo's go.mod or baseDir/go.mod)
3. Display "Primary Repository" section with status
4. Display "Other Repositories" section (all repos except primary)

**Parameters:**
- `writer io.Writer`: Allows testing without stdout (can use bytes.Buffer)

**Output Format:**
```
Primary Repository: avalanchego
  avalanchego: /path/to/avalanchego (branch: master, clean)

Other Repositories:
  coreth: not cloned
  firewood: not cloned
```

**Usage:**
- Called by status command in main.go with `os.Stdout`
- Can be unit tested with `bytes.Buffer` to capture output

#### `DetectCurrentRepo(log, dir)`
**Location**: core/sync.go:16-76

Detects which repository we're currently in by examining go.mod files.

**Detection logic:**
- Checks for `go.mod` in current directory for avalanchego and coreth
- Checks for `ffi/go.mod` for firewood (special case)
- Returns the repo name or empty string if not in a known repo

#### `DiscoverAvalanchegoCorethVersions(log, baseDir, requestedRepos, explicitRefs)`
**Location**: core/sync.go (to be implemented)

Handles version discovery for avalanchego and coreth when they cannot be discovered from a primary repo's go.mod (firewood as primary, or no primary repo).

**Algorithm:**
1. For each requested repo (avalanchego/coreth):
   - If explicit ref provided: use it
   - If no explicit ref: attempt discovery
2. Discovery logic:
   - If avalanchego is cloned: read coreth version from avalanchego/go.mod
   - If coreth is cloned: read avalanchego version from coreth/go.mod
   - If one has explicit ref but not cloned: clone it first, read other from its go.mod
   - If avalanchego has no ref and isn't cloned: use master; if coreth also requested, discover from avalanchego after cloning
   - If coreth has no ref and isn't cloned and avalanchego not requested: use main

**Cloning Strategy for Compatibility:**
When syncing both without explicit versions:
- Clone avalanchego at master (default branch)
- Discover coreth's version from avalanchego/go.mod
- This ensures a compatible pair rather than two potentially incompatible tips

**Returns:** Map of repo names to refs for avalanchego/coreth

#### `DiscoverFirewoodVersion(log, baseDir, explicitRef, requestedRepos, discoveredVersions)`
**Location**: core/sync.go (to be implemented)

Handles version discovery for firewood following the dependency chain.

**Algorithm:**
1. If explicit ref provided: use it
2. If coreth is cloned OR being synced (in requestedRepos): read from coreth/go.mod (direct dependency)
3. If coreth not involved but avalanchego is cloned OR being synced: read from avalanchego/go.mod (indirect dependency)
4. If no primary repo and firewood is only requested repo: use default branch (main)

**Priority order:**
- Explicit ref (highest)
- Coreth's go.mod (direct dependency)
- Avalanchego's go.mod (indirect dependency)
- Default branch (lowest - only when standalone)

**Example Scenarios:**
```go
// Scenario 1: All three repos, no versions
// avalanchego → master, coreth discovered, firewood from coreth
DiscoverFirewoodVersion(log, baseDir, "",
    []string{"avalanchego", "coreth", "firewood"},
    map[string]string{"avalanchego": "master", "coreth": "v0.13.8"})
// Reads firewood version from coreth/go.mod
// Returns: "v0.2.0"

// Scenario 2: avalanchego and firewood only
DiscoverFirewoodVersion(log, baseDir, "",
    []string{"avalanchego", "firewood"},
    map[string]string{"avalanchego": "master"})
// Reads firewood version from avalanchego/go.mod (indirect)
// Returns: "v0.2.0"

// Scenario 3: firewood alone, no primary repo
DiscoverFirewoodVersion(log, baseDir, "",
    []string{"firewood"},
    map[string]string{})
// Returns: "main" (default branch)
```

**Returns:** Ref for firewood

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
- `TestTargetDir_*`: Tests --target-dir flag functionality with various paths and validation
- `TestDiscoverAvalanchegoCorethVersions_BothExplicit`: Both versions specified explicitly
- `TestDiscoverAvalanchegoCorethVersions_AvalanchegoExplicit_CorethDiscovered`: Avalanchego explicit, coreth discovered
- `TestDiscoverAvalanchegoCorethVersions_CorethExplicit_AvalanchegoDiscovered`: Coreth explicit, avalanchego discovered
- `TestDiscoverAvalanchegoCorethVersions_AvalanchegoCloned_DiscoverCoreth`: Avalanchego exists locally, discover coreth
- `TestDiscoverAvalanchegoCorethVersions_CorethCloned_DiscoverAvalanchego`: Coreth exists locally, discover avalanchego
- `TestDiscoverAvalanchegoCorethVersions_NeitherExplicit_DefaultAvalanchego`: Default avalanchego to master, discover coreth
- `TestDiscoverAvalanchegoCorethVersions_FromStandaloneMode`: From directory with no primary repo
- `TestDiscoverAvalanchegoCorethVersions_FromFirewoodPrimary`: From firewood as primary repo
- `TestDiscoverFirewoodVersion_Explicit`: Explicit version provided
- `TestDiscoverFirewoodVersion_FromCoreth`: Coreth being synced, discover from coreth/go.mod
- `TestDiscoverFirewoodVersion_FromAvalanchego`: Only avalanchego involved, discover from avalanchego/go.mod
- `TestDiscoverFirewoodVersion_StandaloneDefaultBranch`: Firewood alone, no primary repo, use main
- `TestDiscoverFirewoodVersion_CorethAlreadyCloned`: Coreth exists locally, discover firewood from it
- `TestDiscoverFirewoodVersion_AvalanchegoAlreadyCloned`: Avalanchego exists locally (no coreth), discover firewood from it

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

#### Version Discovery Without Primary Repo Dependencies

**Avalanchego ↔ Coreth Discovery:**

When neither can be discovered from a primary repo's go.mod (firewood as primary, or no primary repo):
- Both versions specified explicitly
- One explicit, other discovered from its go.mod
- One already cloned, discover the other
- Neither explicit: default avalanchego to master, discover coreth
- Only avalanchego requested without version: use master
- Only coreth requested without version: use main
- Error: go.mod doesn't contain the dependency

**Firewood Discovery:**

Following the dependency chain (coreth direct, avalanchego indirect):
- Explicit version provided
- Coreth being synced: discover from coreth/go.mod (direct dependency)
- Coreth already cloned: discover from coreth/go.mod (direct dependency)
- Only avalanchego involved (synced or cloned): discover from avalanchego/go.mod (indirect dependency)
- Firewood alone, no primary repo: use default branch (main)
- Error: go.mod doesn't contain firewood dependency

**Key Principles:**
- Only sync repos explicitly requested in the command (never auto-sync dependencies)
- Follow dependency chain: avalanchego ↔ coreth (bidirectional), coreth → firewood (direct), avalanchego → firewood (indirect)
- Prioritize compatibility: when both avalanchego and coreth requested without versions, default avalanchego to master and discover coreth

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

4. **Test --target-dir flag:**
   ```bash
   # Create a test directory with go.mod
   mkdir -p /tmp/test-polyrepo
   cat > /tmp/test-polyrepo/go.mod << 'EOF'
   module github.com/ava-labs/avalanchego
   go 1.24
   EOF

   # Run status on the test directory
   ./scripts/run_polyrepo.sh --target-dir /tmp/test-polyrepo status

   # Test with non-existent directory (should fail)
   ./scripts/run_polyrepo.sh --target-dir /tmp/does-not-exist status

   # Cleanup
   rm -rf /tmp/test-polyrepo
   ```

   Verify:
   - `--target-dir` with valid directory works correctly
   - Error message when directory doesn't exist
   - Error message when path is a file, not a directory

## Recent Fixes

### Main.go Refactoring: Business Logic Moved to Core (2025-10)

**Problem**: Business logic was embedded in main.go CLI command handlers (~350 lines total), making it:
- Impossible to unit test without invoking the CLI
- Difficult to reuse functionality outside the CLI
- Error-prone (bugs could only be caught by integration tests)
- Hard to maintain and understand

**Example Bug Caught**: The standalone mode (--target-dir with empty directory) was documented in CLAUDE.md and had helper functions implemented, but the main.go sync command still required go.mod to exist, causing "go.mod not found" errors. This bug would have been caught immediately by unit tests if the logic was in core/.

**Solution**: Refactored all four CLI commands to move business logic into testable core functions:

1. **Sync Command** → `core.Sync()`
   - Moved ~180 lines of logic from main.go to core/sync.go
   - Integrated previously unused version discovery functions
   - Fixed standalone mode bug (now works without go.mod)
   - Added unit test that would have caught the bug

2. **Reset Command** → `core.Reset()`
   - Moved go.mod validation from main.go into core/reset.go
   - Updated signature: `goModPath` parameter → `baseDir` parameter
   - Added unit tests for error handling

3. **Update-Avalanchego Command** → `core.UpdateAvalanchego()`
   - Moved go.mod validation from main.go into core/update.go
   - Updated signature: `goModPath` parameter → `baseDir` parameter
   - Added 5 comprehensive unit tests

4. **Status Command** → `core.Status()`
   - Moved ~85 lines of display logic from main.go to core/status.go
   - Added `io.Writer` parameter to enable testing without stdout
   - Added 7 comprehensive unit tests

**Architecture Change**:
- **Before**: main.go contained ~350 lines of untestable business logic
- **After**: main.go contains ~40 lines total (each command is 5-10 lines)
- **Pattern**: CLI handler only does: parse args, get baseDir, call core function

**Testing Strategy**:
- All business logic is now unit testable in the core/ package
- Unit tests use TDD approach (write tests first, verify they fail, then implement)
- Integration tests remain but are no longer the only way to catch bugs

**Test Coverage Added**:
- `TestSync_StandaloneMode_*`: Tests standalone mode (caught the bug!)
- `TestReset_NoGoMod_Error`: Tests error handling
- `TestUpdateAvalanchego_*`: 5 tests covering all scenarios
- `TestStatus_*`: 7 tests covering all output scenarios

**Benefits**:
- **Faster development**: Unit tests run instantly, no need to build and invoke CLI
- **Better bug prevention**: Logic bugs caught by unit tests instead of integration tests
- **Easier maintenance**: Clear separation of concerns
- **Reusability**: Core functions can be called from other Go code
- **Documentation**: Function signatures and tests serve as API documentation

**Development Process**:
- Followed strict TDD (Test-Driven Development)
- Each refactoring done in separate commit for easy review
- All changes verified with linter and full test suite

**Hard-Won Knowledge**:
- **ALWAYS put business logic in core/**, not in main.go
- **ALWAYS write unit tests for core functions** before implementation
- If you find yourself unable to test something without invoking the CLI, that's a sign the logic belongs in core/
- Integration tests are valuable but should not be the ONLY tests for business logic

### Target Directory Flag Support (2025-10)

**Enhancement**: Added `--target-dir` flag to allow running polyrepo commands on any directory without having to `cd` into it first.

**Changes**:
- Added `--target-dir` persistent flag that applies to all commands
- The tool validates the target directory exists and is a directory (not a file)
- Changes to the target directory before executing any command
- Accepts both absolute and relative paths
- Relative paths are resolved from the current working directory, not from where the script is located

**Implementation**:
- Added validation in `PersistentPreRunE` to check directory exists and is valid
- Uses `os.Chdir()` to change to the target directory before command execution
- All existing commands (status, sync, reset, update-avalanchego) automatically work with `--target-dir`

**Test Coverage**:
- `TestTargetDir_AbsolutePath`: Tests with absolute path
- `TestTargetDir_RelativePath`: Tests with relative path
- `TestTargetDir_NonExistentDir`: Tests validation for non-existent directory
- `TestTargetDir_FileNotDir`: Tests validation when path is a file, not directory
- `TestTargetDir_DefaultBehavior`: Tests default behavior (uses current directory)
- `TestTargetDir_WithResetCommand`: Tests integration with reset command

**Use Cases**:
```bash
# Check status of a different project
./scripts/run_polyrepo.sh --target-dir /path/to/other/project status

# Sync repositories in a different directory
./scripts/run_polyrepo.sh --target-dir /path/to/avalanchego sync coreth

# Reset replace directives in another project
./scripts/run_polyrepo.sh --target-dir ../other-workspace reset
```

**Benefits**:
- No need to `cd` into the target directory first
- Useful for automation scripts that need to operate on multiple directories
- Cleaner workflow when working with multiple projects simultaneously

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

### Adding a New CLI Command

**CRITICAL**: Always follow this pattern when adding new commands. ALL business logic must be in core/, not in main.go.

**Pattern**:

1. **Write unit tests FIRST** (TDD - Red phase):
   ```go
   // In core/myfeature_test.go
   func TestMyFeature_SuccessCase(t *testing.T) {
       // Test the core function, not the CLI
   }

   func TestMyFeature_ErrorCase(t *testing.T) {
       // Test error handling
   }
   ```

2. **Run tests - verify they FAIL** (TDD - Red phase):
   ```bash
   go test ./core/... -v -run=TestMyFeature
   # Should see: undefined: MyFeature
   ```

3. **Implement core function** (TDD - Green phase):
   ```go
   // In core/myfeature.go
   func MyFeature(log logging.Logger, baseDir string, params ...string) error {
       // ALL business logic goes here
       // - Validation
       // - File operations
       // - Git operations
       // - Error handling
       return nil
   }
   ```

4. **Run tests - verify they PASS** (TDD - Green phase):
   ```bash
   go test ./core/... -v -run=TestMyFeature
   # All tests should pass
   ```

5. **Create thin CLI command handler** in main.go:
   ```go
   var myFeatureCmd = &cobra.Command{
       Use:   "myfeature [args]",
       Short: "Description",
       RunE: func(cmd *cobra.Command, args []string) error {
           log := getLogger(cmd)

           baseDir, err := os.Getwd()
           if err != nil {
               return fmt.Errorf("failed to get current directory: %w", err)
           }

           return core.MyFeature(log, baseDir, args...)
       },
   }
   ```

6. **Register command** in `init()`:
   ```go
   func init() {
       rootCmd.AddCommand(myFeatureCmd)
   }
   ```

**Key Rules**:
- **main.go should be ~10 lines per command**: parse args, get baseDir, call core function
- **core/ contains ALL business logic**: validation, operations, error handling
- **Write tests FIRST**: Unit tests in core/ catch bugs without needing integration tests
- **Use io.Writer for output**: Pass `os.Stdout` from CLI, use `bytes.Buffer` in tests
- **Fail fast**: Return first error, don't collect multiple errors

**Anti-Patterns (DO NOT DO THIS)**:
```go
// ❌ BAD: Business logic in main.go
RunE: func(cmd *cobra.Command, args []string) error {
    // Validation logic
    if len(args) == 0 {
        return errors.New("missing args")
    }

    // File operations
    if _, err := os.Stat(goModPath); err != nil {
        return err
    }

    // More logic...
    // This is UNTESTABLE without invoking the CLI!
}
```

```go
// ✅ GOOD: Thin CLI handler, business logic in core
RunE: func(cmd *cobra.Command, args []string) error {
    log := getLogger(cmd)
    baseDir, err := os.Getwd()
    if err != nil {
        return fmt.Errorf("failed to get current directory: %w", err)
    }
    return core.MyFeature(log, baseDir, args...)
}
```

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

### Process Requirements

**Architecture (MOST IMPORTANT)**:
- **ALL business logic MUST live in core/**, not in main.go
- **main.go is ONLY for CLI argument parsing and routing** - Keep command handlers to 5-10 lines
- **If you can't unit test it without invoking the CLI, it's in the wrong place** - Move it to core/
- **Every core function MUST have unit tests** - No exceptions

**Test-Driven Development (TDD)**:
- **ALWAYS write tests FIRST** - Before any implementation code
- **ALWAYS verify tests FAIL** - Run tests and confirm they fail with expected errors (e.g., "undefined: FunctionName")
- **Only then implement** - Write minimal code to make tests pass
- **Enumerate corner cases first** - List all scenarios before writing tests
- **Manual testing is for discovering corner cases** - Then convert to automated tests immediately

**Documentation**:
- **ALWAYS document ALL changes in CLAUDE.md** - This is MANDATORY, not optional. New features, bug fixes, new functions, and new tests must all be documented in the appropriate sections
- **ALWAYS document hard-won knowledge immediately** - Don't wait until implementation is complete
- **Document WHY, not just WHAT** - Explain the reasoning behind decisions

**Code Quality**:
- **Run linter immediately after code changes, before execution** - Catch errors at compile-time, not runtime
- **Use proactive detection, not error-based fallbacks** - Detect special cases upfront
- **Fail fast** - Return first error, don't collect multiple errors

### Technical Knowledge
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
