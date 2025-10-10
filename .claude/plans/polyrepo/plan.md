# polyrepo - Multi-repo Development Tool
## Overview
`polyrepo` is a tool for managing local development across multiple interdependent Go repositories with circular dependencies.
## Commands
### `polyrepo status`
Shows the current state of all repos in the polyrepo setup:
Versions/refs checked out
Paths to local clones
Modification state (clean/dirty)
Active replace directives
**Usage:**
```bash
polyrepo status
```
---
### `polyrepo sync [repo@version ...]`
Syncs dependencies for local development.
**With no arguments:** Syncs all dependencies based on current repo's go.mod
```bash
polyrepo sync
```
**With specific repos:** Syncs only specified repos
```bash
polyrepo sync repo-b@v1.2.3 repo-c@v1.0.0
```
**Behavior:**
Clones repos locally (shallow by default, `--full` for full clone)
Parses go.mod to determine versions if not specified
Auto-resolves circular dependencies (repo-a :left_right_arrow: repo-b)
Sets up mutual replace directives for circular deps
Updates primary repo's go.mod with replace directives
**Special case for repo-b:** When syncing repo-b from repo-c, automatically clones repo-a (tests live there, circular dependency)
**Flags:**
`--full` - Full clone instead of shallow (depth=1)
**Examples:**
```bash
polyrepo sync                           # sync all dependencies
polyrepo sync --full                    # sync all with full clones
polyrepo sync repo-b@v1.2.3            # sync specific version
polyrepo sync --full repo-b repo-c     # sync multiple repos with full clones
```
---
### `polyrepo reset [repo ...]`
Removes local development setup by removing replace directives.
**With no arguments:** Removes all replace directives
```bash
polyrepo reset
```
**With specific repos:** Removes replace directives for specified repos only
```bash
polyrepo reset repo-b
```
---
### `polyrepo update-repo-a [version]`
Updates repo-a dependency from repo-b or repo-c.
**With version:** Updates to specific version
```bash
polyrepo update-repo-a v1.2.3
```
**Without version:** Updates to version currently in go.mod
```bash
polyrepo update-repo-a
```
**Behavior:**
Must NOT be run from repo-a (error)
Only works for updating repo-a dependency
Updates Go module dependency
Updates GitHub Actions reference to matching revision
Keeps them synchronized
---
## Key Behaviors
### Repo Discovery
Finds primary repo by looking for `go.mod` in current working directory
Known subdirectories checked (e.g., `foo/go.mod` for repo-c)
### Version Resolution
Uses go.mod dependencies when available
Falls back to configured DefaultRef if dependency not in go.mod
Circular dependencies (repo-a :left_right_arrow: repo-b) automatically resolved
### Circular Dependency Handling
repo-a and repo-b always get mutual replace directives when synced together
Syncing repo-b automatically includes repo-a (needed for tests)
### Clone Behavior
**Shallow clones by default** (depth=1) for speed and disk usage
Use `--full` flag for full history when needed
---
## Repository Structure
### repo-a
Contains all tests and builds main binary
Circular dependency with repo-b
Indirect dependency on repo-c (via repo-b)
**Tests live here**
### repo-b
Library functionality (separate for licensing)
Circular dependency with repo-a
Direct dependency on repo-c
**To test: requires repo-a**
### repo-c
Rust+Go library with Nix build chain
No dependencies on repo-a or repo-b
**To test: requires repo-a**
Published as repo-c-go with pre-built binaries
During development: replace repo-c-go with repo-c source
### repo-c-go
Published wrapper around repo-c
Contains pre-built binary libraries (*.a files)
Used by default in production
Replaced with repo-c source during development
---
## Hardcoded Configuration
Each repo will have hardcoded configuration:
```go
type RepoConfig struct {
    Name       string   // "repo-a", "repo-b", "repo-c"
    GitHubURL  string   // "github.com/org/repo-a"
    DefaultRef string   // "master", "main", or specific branch
    GoModPath  string   // "" for ./go.mod, "foo" for ./foo/go.mod (discovery only)
}
```
---
## Common Workflows
### From repo-a (testing dependency updates):
```bash
polyrepo sync repo-b@v1.2.3 repo-c@v1.0.0
# Run tests in repo-a
polyrepo reset
```
### From repo-b (testing changes to repo-b):
```bash
polyrepo sync repo-a@v2.0.5
# Make changes to repo-b
# Run tests from repo-a
polyrepo reset
```
### From repo-c (testing changes to repo-c):
```bash
polyrepo sync repo-a@v2.0.5
# Make changes to repo-c
# Run tests from repo-a (in nix shell)
polyrepo reset
```
### Updating repo-a dependency in repo-b:
```bash
cd repo-b
polyrepo update-repo-a v1.3.0
# Commits changes to go.mod and GitHub Actions references
```

---

## Open Questions / Implementation Details

### 1. Project Structure
- Where should the polyrepo tool live in the codebase?
  - ./tests/fixture/polyrepo/main.go for the entrypoint
  - ./tests/fixture/polyrepo/core/ for the implementation
- Should it be a standalone binary or integrated into existing tooling?
  - it should be a standalone tool

### 2. Hardcoded Configuration
- What is the configuration for each repo?
  - name: avalanchego
    go-module: github.com/ava-labs/avalanchego
    module-replacement-path: .
    git-repo: https://[go-module]
    default-branch: master
    # for each of the other repos will need to be updated with `go mod edit -replace`
    go-mod-path: go.mod
  - name: coreth
    go-module: github.com/ava-labs/coreth
    module-replacement-path: .
    repo: https://[go-module]
    default-branch: master
    # for each of the other repos will need to be updated with `go mod edit -replace`
    go-mod-path: go.mod
  - name: firewood
    go-module: github.com/ava-labs/firewood/ffi
    module-replacement-path: ./ffi
    git-repo: https://github.com/ava-labs/firewood
    default-branch: main
    go-mod-path: # no go mod path necessary since it doesn't depend on the other 2 repos

### 3. Go Module Parsing and Manipulation
- Which library/approach should be used for parsing and modifying go.mod files?
  - Use golang.org/x/mod/modfile for both reading and writing
- How should we handle go.sum files?
  - Run `go mod tidy` after making replace directive changes

### 4. CLI Framework
- Which CLI framework to use (cobra, flag package, urfave/cli, etc.)?
  - cobra
- Preferred command structure and flag conventions?
  - see tests/fixture/tmpnet/tmpnetctl.go

### 5. Error Handling and Edge Cases
- How should the tool handle dirty working directories?
  - Refuse to sync
- What happens if a repo is already cloned but at the wrong version?
  - Refuse to sync unless --force is provided
- Should the tool verify network connectivity before attempting clones?
  - No, the error should make that obvious
- How to handle authentication issues with private repos?
  - Private repos aren't supported

### 6. Testing Strategy
- Unit tests for config and parsing logic?
- Integration tests that actually clone and manipulate repos?
- Mock/fixture strategy?

### 7. Clone Location
- Where should cloned repos be stored (relative to current repo, temp directory, user config directory)?
  - for now, clone to the cwd
- Should this be configurable?
  - for now, no

### 8. Update Behavior
- Should `polyrepo sync` update existing clones or skip them?
  - Existing clones should be ignored without --force
- Should there be a `--force` flag to re-clone?
  - Not to reclone, only to update the version

### 9. GitHub Actions Update Logic !Ignore this for now!
- What specific GitHub Actions files need to be updated by `update-repo-a`?
- What is the exact format/pattern for the references that need updating?

### 10. What tooling should be used for git interaction
 - go-git

---

## New Open Questions for Implementation

### 11. Firewood Module Path Clarification
When syncing firewood, the tool should:
- Clone from: `https://github.com/ava-labs/firewood`
- Replace module `github.com/ava-labs/firewood/ffi` with `<cloned-path>/ffi`

Is this understanding correct?
- !! Not quite:
  - clone
  - `nix build` in ffi path (e.g. cd ffi && nix build), invoke nix as a cli command
  - replace module `github.com/ava-labs/firewood/ffi` in both coreth and avalanchego with `<cloned-path>/ffi/result/ffi`

### 12. Version Format Support
What version formats should be supported in `repo@version` syntax?
- Git tags (e.g., `v1.2.3`): !! Yes
- Commit SHAs (e.g., `abc123` or full SHA): !! Both, presumably an unresolveable/ambiguously short sha would result in an error?
- Branch names (e.g., `main`, `feature-branch`): !! Yes
- Pseudo-versions (e.g., `v0.0.0-20240101120000-abc123def456`): !! No

### 13. Testing Strategy
What level of testing is expected for the initial implementation?
- Unit tests for config and go.mod parsing logic: !! Use `golang.org/x/mod/modfile`
- Unit tests for git operations (mocked): !! Mocking is the devil, don't bother.
- Integration tests with real git operations: !! Yes, but only for the git operations:
    - create a test repo and make 3 commits
    - tag first commit
    - perform 3 tests of inital cloning, one for each supported format
      - tag (should result in first commit)
      - sha (should result in second commit)
      - branch (should result in third commit)
    - perform 3 tests of update,
      - clone with branch, update to tag
      - clone with sha, update to branch
      - clone with tag, update to tag
- !! Integration test with real repos and replace directives. This flow also indicates the desired behavior that must be implemented
  - perform a `polyrepo sync` without arguments
  - `cd avalanchego && ./scripts/run_task.sh test-unit` without error
  - the intended behavior that should be observed. The part in bracket represents tests that should be performed after `polyrepo sync` but before unit test execution
    - clone avalanchego@master (verify that ./avalanchego/.git exists)
    - determine the version of coreth from the avalanchego clone's go.mod
    - clone coreth@[avalanchego version] (verify that ./coreth/.git exists)
    - Use golang.org/x/mod/modfile to 'go mod replace' both avalanchego and coreth to their local paths and `go tidy` (or modfile equivalent) to update go.sum
    - determine the version of firewood from the avalanchego clone's go.mod
    - clone firewood@[firewood version] (verify that ./firewood/.git exists)
    - run `cd ./firewood/ffi & nix build`
    - Use golang.org/x/mod/modfile to 'go mod replace' both avalanchego and coreth to firewood's ./ffi/result/ffi path
- Manual testing only initially: !! No! The integration test should replace the need for manual testing

### 14. Clone Directory Naming
When cloning repos to the current working directory, what should the directory names be?
- Use repo name (e.g., `avalanchego/`, `coreth/`, `firewood/`): !! For now, yes. Easy to change
- Use a prefix (e.g., `polyrepo-avalanchego/`): !! No
- Use a subdirectory (e.g., `.polyrepo/avalanchego/`): !! No
- Other: !! No

### 15. Existing Clone Behavior
If a repo directory already exists:
- Without `--force`: Error and refuse to sync: !! Yes, refuse to sync
- With `--force`: What should happen?
  - Delete and re-clone: !! No
  - Git fetch and checkout the requested version: !! Yes
  - Other: !! No

### 16. Dirty Working Directory Check
Should the dirty working directory check apply to:
- Only the current repo (where polyrepo is run from): !! No
- Only the repos being synced: !! Yes, because only the repos being synced are going to be updated
- Both current repo and repos being synced: !! no

### 17. Replace Directive Paths
When adding replace directives, should the paths be:
- Relative to the current directory (e.g., `./coreth`): !! Relative
- Absolute paths: !! No

### 18. Auto-sync Avalanchego Behavior
The spec says "when syncing coreth, automatically include avalanchego" and "when syncing firewood from coreth, automatically clone avalanchego".

Should this behavior:
- Always happen automatically (silent): !! Yes - the idea is that avalanchego is the integration point without which the other 2 repos can't be tested
- Happen automatically but print a message: !! No
- Require user confirmation: !! No

### 19. Circular Dependency Resolution
When syncing avalanchego ↔ coreth:
- Should both repos always get mutual replace directives: !! yes
- Should this only happen if both repos are being synced: !! no
- Should this be automatic or require a flag: !! automatic

### 20. Command Naming
The original spec uses `update-repo-a`, but with avalanchego/coreth/firewood naming:
- Should the command be `update-avalanchego`: !! Yes
- Should it be more generic like `update-dependency <repo> [version]`: !! No, only avalanchego needs more than `go get` for updating resources since there are github actions and maybe an entry in polyrepo tools/go.mod that need to be kept in sync with a change to go.mod for a given repo.
- Keep as `update-avalanchego` for now: !! Yes

### 21. Modes of execution !!
- By default, sync (clone) all 3 repos to current working directory (CWD)
- if the CWD has a go.mod file with the module name for avalanchego, sync firewood and coreth to the CWD and don't sync avalanchego
- if the CWD has a go.mod file with the module name for coreth, sync avalanchego and firewood to the CWD and don't sync coreth
- if the CWD has ./ffi/go.mod file with the module name for firewood/ffi, sync avalanchego and coreth to the CWD and don't sync firewood

---

## Final Implementation Plan

### Project Structure
```
tests/fixture/polyrepo/
├── main.go              # CLI entry point with Cobra commands
└── core/
    ├── config.go        # Hardcoded repo configurations
    ├── repo.go          # Git operations using go-git
    ├── gomod.go         # Go module manipulation using golang.org/x/mod/modfile
    ├── status.go        # Status command implementation
    ├── sync.go          # Sync command with mode detection
    ├── reset.go         # Reset command implementation
    ├── update.go        # Update-avalanchego command implementation
    ├── nix.go           # Nix build operations for firewood
    └── errors.go        # Custom error types and utilities
```

### Core Configuration

**RepoConfig struct:**
```go
type RepoConfig struct {
    Name                   string // "avalanchego", "coreth", "firewood"
    GoModule               string // "github.com/ava-labs/avalanchego"
    ModuleReplacementPath  string // "." or "./ffi/result/ffi"
    GitRepo                string // "https://github.com/ava-labs/avalanchego"
    DefaultBranch          string // "master" or "main"
    GoModPath              string // "go.mod" or "ffi/go.mod"
    RequiresNixBuild       bool   // true for firewood
    NixBuildPath           string // "ffi" for firewood
}
```

**Hardcoded configurations:**
- **avalanchego**: module `github.com/ava-labs/avalanchego`, replacement path `.`, default branch `master`
- **coreth**: module `github.com/ava-labs/coreth`, replacement path `.`, default branch `master`
- **firewood**: module `github.com/ava-labs/firewood/ffi`, replacement path `./ffi/result/ffi`, default branch `main`, requires nix build in `ffi/`

### Key Implementation Details

#### 1. Mode Detection (Question 21)
Detect current repository context by examining go.mod:
- If CWD contains go.mod with `github.com/ava-labs/avalanchego`: sync coreth + firewood only
- If CWD contains go.mod with `github.com/ava-labs/coreth`: sync avalanchego + firewood only
- If CWD contains `./ffi/go.mod` with `github.com/ava-labs/firewood/ffi`: sync avalanchego + coreth only
- Default (no matching go.mod): sync all 3 repos

#### 2. Version Format Support (Question 12)
- Git tags: `v1.2.3` ✓
- Commit SHAs: Both full and short (error on ambiguous) ✓
- Branch names: `main`, `feature-branch` ✓
- Pseudo-versions: Not supported ✗

#### 3. Firewood Special Handling (Question 11)
1. Clone `https://github.com/ava-labs/firewood`
2. Run `nix build` in `./firewood/ffi/` directory (shell out to nix CLI)
3. Replace module `github.com/ava-labs/firewood/ffi` with `./firewood/ffi/result/ffi` in both avalanchego and coreth

#### 4. Circular Dependency Handling (Question 19)
- avalanchego ↔ coreth always get mutual replace directives
- Automatic, no flag required
- Applies whenever either repo is synced

#### 5. Auto-sync Behavior (Question 18)
- When syncing coreth or firewood, automatically include avalanchego (silent)
- No messages, no confirmation required
- Avalanchego is the integration point for testing

#### 6. Clone Behavior
- **Directory naming** (Q14): Use repo name directly (`avalanchego/`, `coreth/`, `firewood/`)
- **Clone location** (Q7): Current working directory
- **Shallow vs full** (Q8): Shallow by default (`--depth=1`), `--full` flag for full clones
- **Existing clones** (Q15):
  - Without `--force`: Error and refuse to sync
  - With `--force`: Git fetch and checkout requested version (no re-clone)

#### 7. Working Directory Checks (Question 16)
- Check only repos being synced for dirty state
- Do NOT check current repo where polyrepo is run from
- Refuse to sync if any target repo is dirty

#### 8. Replace Directive Paths (Question 17)
- Use relative paths (e.g., `./coreth`, `./firewood/ffi/result/ffi`)
- NOT absolute paths

#### 9. Go Module Manipulation (Question 3)
- Use `golang.org/x/mod/modfile` for both reading and writing
- Run `go mod tidy` after making replace directive changes
- No direct use of `go mod edit` commands

#### 10. Git Operations (Question 10)
- Use `go-git` library for all git operations
- Support tags, branches, and commit SHAs
- Proper error handling for ambiguous short SHAs

### Commands Implementation

#### `polyrepo status`
Display current state:
- List all known repos (avalanchego, coreth, firewood)
- Show: version/ref checked out, local path, clean/dirty state
- Show: active replace directives from go.mod
- Detect current mode (which repo we're running from)

#### `polyrepo sync [repo@version ...]`
Sync dependencies for local development:

**Behavior:**
1. Detect current mode (which repos to sync)
2. If no arguments: sync based on mode detection and go.mod versions
3. If arguments provided: sync specified repos at specified versions
4. Check for dirty working directories in target repos
5. Clone repos if they don't exist (error if exist without `--force`)
6. Auto-include avalanchego when syncing coreth/firewood (silent)
7. Handle circular dependencies (mutual replaces for avalanchego ↔ coreth)
8. For firewood: run `nix build` after clone/checkout
9. Update go.mod with replace directives using `modfile`
10. Run `go mod tidy` to update go.sum

**Flags:**
- `--full`: Full clone instead of shallow
- `--force`: Update existing clones via fetch + checkout

**Examples:**
```bash
polyrepo sync                           # sync based on mode detection
polyrepo sync --full                    # sync with full clones
polyrepo sync coreth@v0.13.8           # sync specific version
polyrepo sync --force coreth firewood   # force update existing clones
```

#### `polyrepo reset [repo ...]`
Remove local development setup:

**Behavior:**
1. Remove replace directives from go.mod using `modfile`
2. If no arguments: remove all polyrepo-related replaces
3. If arguments: remove only specified repo replaces
4. Run `go mod tidy` to update go.sum

**Examples:**
```bash
polyrepo reset           # remove all replaces
polyrepo reset coreth    # remove only coreth replace
```

#### `polyrepo update-avalanchego [version]`
Update avalanchego dependency (from coreth/firewood):

**Behavior:**
1. Error if run from avalanchego repo
2. If version specified: update to that version
3. If no version: use version currently in go.mod
4. Update go.mod dependency using `modfile`
5. Run `go mod tidy`
6. (GitHub Actions update: ignore for now per Q9)

**Examples:**
```bash
polyrepo update-avalanchego v1.11.11    # update to specific version
polyrepo update-avalanchego              # update to current go.mod version
```

### Testing Strategy (Question 13)

#### Unit Tests
- **Config parsing**: Test repo configuration loading
- **Go module operations**: Test modfile parsing and manipulation
- **Version parsing**: Test parsing of tags, SHAs, branches

#### Integration Tests - Git Operations
Create test repo with 3 commits:
1. First commit (tagged)
2. Second commit (untagged)
3. Third commit (on branch)

**Tests:**
- Clone with tag → verify first commit checked out
- Clone with SHA → verify second commit checked out
- Clone with branch → verify third commit checked out
- Clone with branch, update to tag → verify tag commit
- Clone with SHA, update to branch → verify branch commit
- Clone with tag, update to different tag → verify new tag

#### Integration Test - Full Workflow
Test the complete sync workflow:
1. Run `polyrepo sync` without arguments
2. Verify `./avalanchego/.git` exists
3. Verify coreth version determined from avalanchego's go.mod
4. Verify `./coreth/.git` exists
5. Verify both avalanchego and coreth have mutual replace directives
6. Verify `go mod tidy` has run (go.sum updated)
7. Verify firewood version determined from avalanchego's go.mod
8. Verify `./firewood/.git` exists
9. Verify `nix build` ran in `./firewood/ffi`
10. Verify `./firewood/ffi/result/ffi` path exists
11. Verify both avalanchego and coreth have firewood replace pointing to `./firewood/ffi/result/ffi`
12. Run `cd avalanchego && ./scripts/run_task.sh test-unit` without error

**No mocking** - use real git, nix, and modfile operations

### Dependencies
- `github.com/spf13/cobra` - CLI framework
- `github.com/go-git/go-git/v5` - Git operations
- `golang.org/x/mod/modfile` - Go module manipulation
- `go.uber.org/zap` - Logging (if needed, following tmpnetctl pattern)

### Error Handling
- Clear error messages for:
  - Dirty working directories
  - Existing clones without `--force`
  - Ambiguous short SHAs
  - Network/authentication issues
  - Nix build failures
  - Invalid version formats
  - Running update-avalanchego from avalanchego repo

### Implementation Order
1. **Core infrastructure**: config.go, errors.go
2. **Go module operations**: gomod.go with modfile
3. **Git operations**: repo.go with go-git
4. **Nix operations**: nix.go for firewood builds
5. **Command implementations**: status.go, sync.go, reset.go, update.go
6. **CLI entry point**: main.go with Cobra
7. **Unit tests**: Test each component
8. **Integration tests**: Full workflow test
