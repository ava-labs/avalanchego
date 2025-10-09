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
- Should it be a standalone binary or integrated into existing tooling?

### 2. Hardcoded Configuration
- What are the actual repository names and GitHub URLs?
- What are the default refs (branches) for each repo?
- What is the GoModPath for each repo (especially repo-c)?

### 3. Go Module Parsing and Manipulation
- Which library/approach should be used for parsing and modifying go.mod files?
- How should we handle go.sum files?

### 4. CLI Framework
- Which CLI framework to use (cobra, flag package, urfave/cli, etc.)?
- Preferred command structure and flag conventions?

### 5. Error Handling and Edge Cases
- How should the tool handle dirty working directories?
- What happens if a repo is already cloned but at the wrong version?
- Should the tool verify network connectivity before attempting clones?
- How to handle authentication issues with private repos?

### 6. Testing Strategy
- Unit tests for config and parsing logic?
- Integration tests that actually clone and manipulate repos?
- Mock/fixture strategy?

### 7. Clone Location
- Where should cloned repos be stored (relative to current repo, temp directory, user config directory)?
- Should this be configurable?

### 8. Update Behavior
- Should `polyrepo sync` update existing clones or skip them?
- Should there be a `--force` flag to re-clone?

### 9. GitHub Actions Update Logic
- What specific GitHub Actions files need to be updated by `update-repo-a`?
- What is the exact format/pattern for the references that need updating?
