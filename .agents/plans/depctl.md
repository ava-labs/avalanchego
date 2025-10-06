# depctl - tool to manage golang dependencies

## Overview

This repo - avalanchego - is the primary repo of a set of repos that really should be a monorepo but for licensing reasons that's not yet possible. So, we have scripting in each repo that simplifies reading/maintaining the version of avalanchego and cloning the repo at a given revision and go-mod-replacing avalanchego's dependency on these repos. I'd like to switch to a new model - add a golang tool (e.g. tools/depctl/main.go) that implements the functionality, and then have these other repos run this tool with configuration specific to their needs.

The tool will look like the following:

- Aliases
  - the repo target will be one of the following:
    - avalanchego
    - firewood
    - coreth
  - if no repo target is supplied, avalanchego will be used
  - each of the targets will expanded to the module path i.e. `github.com/ava-labs/[repo target]`
  - In the case of a clone operation, the repo path will be `https://[module path]`

- depctl get-version [--dep=[repo target]]
  - retrieves the version of the given dependency as per the example of scripts/versions.sh
- depctl update-version [version] [--dep=[repo target]]
  - updates the version of the given dependency as per the example of scripts/update_avalanchego_version.sh
    - the custom action paths should only be updated if the repo target is avalanchego
    - the workflow path should be all files in .github/workflows
    - the path to search for should be `ava-labs/avalanchego/.github/actions` to allow for use of any custom actions
- depctl clone [--path=[optional clone path, defaulting to the repo name (e.g. avalanchego, firewood, coreth)]] [--dep=[repo target]] [--version]
  - if no version provided, gets the version of the repo target from go.mod with the internal equivalent of `depctl get-version`
  - performs the equivalent of scripts/clone_avalanchego.sh but with a variable repo target
    - if repo doesn't exist, clones it. if exists, tries to update it to the specified version
  - if the repo target is avalanchego, and the go.mod module name is `github.com/ava-labs/coreth`
    - update go.mod to point to the local coreth source path as per the example of the last steps in scripts/clone_avalanchego.sh (with go mod replace && go mod tidy)
  - if the repo target is avalanchego, and the go.mod module name is `github.com/ava-labs/firewood/ffi`
    - update the avalanchego clone's go.mod to point to the firewood clone
      - e.g. go mod edit -replace github.com/ava-labs/firewood-go-ethhash=.
  - if the repo target is firewood or coreth and the go.mod module name is `github.com/ava-labs/avalanchego`
    - Perform a go.mod replace to to point the avalanchego chrome to the clone at ./firewood or ./coreth

## Use cases

### Testing firewood with avalanchego

 - from the root of a clone of the firewood repo
 - go tool -modfile=tools/go.mod depctl clone
   - needs to be in run in the ./ffi path of the firewood repo since that's where the go.mod is
   - clones or updates avalanchego at the version set in the tool modfile to ./ffi/avalanchego
   - updates the avalanchego clone's go.mod entry for github.com/ava-labs/firewood-go-ethhash/ffi to point to .
 - cd avalanchego
 - ./scripts/run_task.sh [task]
   - e.g. ./scripts/run_task.sh test-unit

### Testing avalanchego with firewood

 - from the root of a clone of the avalanchego repo
 - go tool depctl clone firewood --version=mytag
   - clones or updates firewood at version mytag ./firewood
   - updates the avalanchego clone's go.mod entry for firewood to point to ./firewood/ffi
     - e.g. go mod edit -replace github.com/ava-labs/firewood-go-ethash=./firewood/ffi
 - cd ./firewood/ffi && nix develop && cd ..
   - the nix shell will ensure the golang ffi libary is built and configured for use in the shell
 - ./scripts/run_task.sh [task]
   - e.g. ./scripts/run_task.sh test-unit

### Testing coreth with avalanchego and firewood

 - from the root of a clone of the coreth repo
 - go tool depctl clone
   - clones or updates avalanchego at the version set in go.mod to ./avalanchego
   - updates the avalanchego clone's go.mod entry for coreth to point to .
 - go tool depctl clone --dep=firewood --version=mytag
   - clones or updates firewood at version `mytag` to ./firewood
   - updates coreth's go.mod entry for firewood to point to ./firewood/ffi
     - e.g. go mod edit -replace github.com/ava-labs/firewood-go-ethash=./firewood/ffi
 - cd ./firewood/ffi && nix develop && cd ..
   - the nix shell will ensure the golang ffi libary is built and configured for use in the shell
 - cd avalanchego && ./scripts/run_task.sh [task]
   - e.g. ./scripts/run_task.sh test-unit

## Implementation Strategy

### Package Structure

```
tools/depctl/
├── main.go              # CLI entry point with cobra commands
└── dep/
    ├── repo.go          # Repo target resolution & aliases
    ├── version.go       # Get/update version operations
    ├── clone.go         # Git clone/update & go mod replace
    ├── repo_test.go
    ├── version_test.go
    └── clone_test.go
```

- **Package name**: `package dep`
- **Import path**: `github.com/ava-labs/avalanchego/tools/depctl/dep`
- **main.go**: `package main`, imports and uses `dep` package
- All business logic is exportable and reusable by other tools/repos

### Alias Resolution System

```go
type RepoTarget string

const (
    TargetAvalanchego RepoTarget = "avalanchego"
    TargetFirewood    RepoTarget = "firewood"
    TargetCoreth      RepoTarget = "coreth"
)

// Resolve returns module path and clone URL
func (r RepoTarget) Resolve() (modulePath, cloneURL string)
```

**Mappings**:
- `avalanchego` → `github.com/ava-labs/avalanchego` → `https://github.com/ava-labs/avalanchego`
- `firewood` → `github.com/ava-labs/firewood-go-ethhash/ffi` → `https://github.com/ava-labs/firewood`
- `coreth` → `github.com/ava-labs/coreth` → `https://github.com/ava-labs/coreth`

Note: Firewood's clone URL differs from its module path - clone the repo root but the module is in `/ffi` subdirectory.

### Command Implementation Details

#### `depctl get-version [--dep=<target>]`

**Implementation** (in `version.go`):
1. Default dep to `avalanchego` if not specified
2. Resolve target to module path using alias system
3. Use `golang.org/x/mod/modfile` to parse `go.mod`
4. Find require directive for the module
5. Extract version (handle both tagged versions and pseudo-versions)
6. For pseudo-versions (format: `v*YYYYMMDDHHMMSS-abcdef123456`):
   - Extract last 12 chars as module hash
   - Return first 8 chars of hash
7. For tagged versions, return as-is

**Output**: Version string (tag or 8-char hash prefix)

#### `depctl update-version <version> [--dep=<target>]`

**Implementation** (in `version.go`):
1. Default dep to `avalanchego` if not specified
2. Resolve target to module path
3. Run `go get <module-path>@<version>`
4. Run `go mod tidy`
5. **If and only if target is `avalanchego`**:
   - Parse all files in `.github/workflows/*.yml`
   - Search for pattern: `uses: ava-labs/avalanchego/.github/actions/*@<ref>`
   - Get full SHA for the version from GitHub API (`https://api.github.com/repos/ava-labs/avalanchego/commits/<version>`)
   - Replace `@<ref>` with `@<full-sha>` for all matching action uses
   - Use `GITHUB_TOKEN` env var if available to avoid rate limiting

**Output**: Confirmation message with updated version

#### `depctl clone [--path=<path>] [--dep=<target>] [--version=<version>]`

**Implementation** (in `clone.go`):
1. Default dep to `avalanchego` if not specified
2. Default path to repo name (e.g., "avalanchego", "firewood", "coreth") if not specified
3. If no version provided, call internal `GetVersion()` to read from go.mod
4. Resolve target to module path and clone URL

**Git Operations**:
- If directory exists:
  ```bash
  cd <path>
  git fetch
  git checkout -B test-<version> <version>
  ```
- If directory doesn't exist:
  ```bash
  git clone <clone-url> <path>
  cd <path>
  git checkout -B test-<version> <version>
  ```

**Go Mod Replace Operations**:

After cloning, perform `go mod edit -replace` based on the current repo's module name and the cloned target:

Decision matrix:
```
Current Module                          | Clone Target   | Replace In     | Replace What              | Replace With
----------------------------------------|----------------|----------------|---------------------------|------------------
github.com/ava-labs/coreth              | avalanchego    | <cloned-path>  | coreth                    | <relative-back>
github.com/ava-labs/firewood-go-*/ffi   | avalanchego    | <cloned-path>  | firewood-go-ethhash/ffi   | <relative-back>
github.com/ava-labs/avalanchego         | firewood       | .              | firewood-go-ethhash/ffi   | <path>/ffi
github.com/ava-labs/avalanchego         | coreth         | .              | coreth                    | <path>
```

**Path Handling**:
- User can specify any path: `./avalanchego`, `../../avalanchego`, `/tmp/test/avalanchego`
- Compute relative paths between directories for `go mod replace` using `filepath.Rel()`
- Convert to absolute paths internally using `filepath.Abs()` for reliability
- Always run `go mod tidy` after replace operations

### External Dependencies

Required packages:
- `github.com/spf13/cobra` - CLI framework (add to tools/go.mod)
- `golang.org/x/mod/modfile` - Parse/modify go.mod files
- Standard library: `os/exec` for git/go commands, `filepath` for path operations, `regexp` for pattern matching, `net/http` for GitHub API

### Testing Strategy

#### Unit Tests
- Test alias resolution (repo.go)
- Test version parsing from go.mod - both tags and pseudo-versions (version.go)
- Test go.mod module name detection (clone.go)
- Mock filesystem operations where possible
- Use table-driven tests for decision matrix scenarios

#### Integration Tests
- Use `t.TempDir()` for isolated test environments
- Test shallow clone operations (`git clone --depth 1`) to minimize time/bandwidth
- Test version update workflow with network access or git bundle fixtures
- Test cross-repo replace scenarios with realistic directory structures
- Add optional flag/env var to preserve temp directories for debugging

**Target Task**: `test-unit` - tests should complete in <30s using shallow clones and minimal fixtures

### Implementation Order

1. **Phase 1: Core infrastructure (repo.go)**
   - Alias resolver with const definitions
   - Module path and clone URL resolution
   - Unit tests for all repo targets

2. **Phase 2: Version reading (version.go - read-only)**
   - Get version from go.mod using modfile parser
   - Handle tagged versions and pseudo-versions
   - Extract 8-char hash prefix for pseudo-versions
   - Unit tests with fixture go.mod files

3. **Phase 3: Version updating (version.go - writes)**
   - Update version with `go get` and `go mod tidy`
   - GitHub API integration for full SHA lookup
   - Workflow file pattern matching and replacement
   - Integration tests (may require network access)

4. **Phase 4: Git operations (clone.go - git)**
   - Git clone/update logic with branch creation
   - Error handling for git failures
   - Integration tests with shallow clones

5. **Phase 5: Cross-repo wiring (clone.go - mod replace)**
   - Current module detection from go.mod
   - Path computation (relative/absolute handling)
   - Decision matrix implementation
   - Integration tests covering all use cases from plan

6. **Phase 6: CLI and polish (main.go)**
   - Cobra command setup
   - Flag definitions and validation
   - Help text and usage examples
   - End-to-end validation of all three commands

### Error Handling

Return structured errors with actionable messages:
- Invalid repo target → suggest valid options
- Git operation failures → include git error output
- go.mod parse failures → show file path and line number
- Module not found in go.mod → list available modules
- Network failures for GitHub API → suggest checking GITHUB_TOKEN or retrying
- Path issues → clarify relative vs absolute path expectations

### Key Design Decisions

1. **Firewood special case**: Clone URL is `github.com/ava-labs/firewood` but module path is `github.com/ava-labs/firewood-go-ethhash/ffi`. Go mod replace should point to `<clone-path>/ffi` subdirectory.

2. **Shallow clones**: Use `--depth 1` for faster operations in tests. Production usage may want full clones for flexibility.

3. **Version format handling**: Support both semver tags (`v1.2.3`) and pseudo-versions (`v0.0.0-20240101120000-abcdef123456`), normalizing pseudo-versions to 8-char hash prefix to match existing scripts/versions.sh behavior.

4. **Workflow updates**: Only update `.github/workflows/*.yml` files when target is `avalanchego`, searching specifically for `ava-labs/avalanchego/.github/actions` pattern to avoid false matches.

5. **Path flexibility**: Clone path can be anywhere (relative or absolute), not just current directory. Always compute correct relative paths for `go mod replace`.

6. **Package organization**: Use `dep` subdirectory (not `internal`) to allow other repos to import and use programmatically if needed, while keeping simple flat structure within.

7. **Branch naming**: Use `test-<version>` pattern for created branches to match existing scripts/clone_avalanchego.sh behavior.
