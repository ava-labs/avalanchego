# depctl: Switch to go-git Library

## Status: Planning

## Motivation

### Current Problems with CLI git

1. **Unpredictable behavior** - relies on git binary being present and compatible
2. **Poor error messages** - just "exit status 128" with no context about what failed
3. **Hard to debug** - need to parse stderr text to understand failures
4. **No type safety** - everything is strings and command-line args
5. **SHA cloning issues** - shallow clone with `--branch <sha>` doesn't work, requiring complex fallback logic:
   - Try shallow clone with `--branch`
   - If that fails, do full clone
   - Then fetch and checkout
6. **External dependency** - requires git binary installed and in PATH
7. **Platform inconsistencies** - git behavior varies across versions and platforms

### Real-World Issues

**Issue**: User tried to clone with SHA `3cfedb685`:
```bash
$ depctl clone --version 3cfedb685
warning: Could not find remote branch 3cfedb685 to clone.
fatal: Remote branch 3cfedb685 not found in upstream origin
```

The shallow clone fails because `git clone --branch` doesn't work with SHAs. The code then falls back to a full clone (cloning the entire default branch), then fetches, then checks out the SHA. This is wasteful and confusing.

### Benefits of go-git

- **Pure Go** - no external dependencies, works everywhere
- **Better error handling** - typed errors with context (e.g., `ErrObjectNotFound`, `ErrReferenceNotFound`)
- **Direct API** - `Clone()`, `Fetch()`, `Checkout()` functions instead of command-line args
- **Type safety** - `plumbing.Hash` for SHAs, `plumbing.ReferenceName` for branches/tags
- **Better debugging** - can add detailed logging at each step with structured data
- **SHA support** - can resolve and checkout by SHA directly without shallow/full fallback dance
- **Testability** - can mock go-git interfaces for unit tests
- **Consistency** - same behavior across all platforms

## Logging Strategy

**Info level** (user-facing outcomes only):
- Repository already exists at correct revision: `"Already at <version> (<sha>) - use --update-existing to update to <default-or-provided-version>"`
- Successful checkout: `"Successfully checked out <repo-name> at <version> (<sha>)"`

**Warn/Error level**:
- Authentication failures
- Network errors
- Version not found errors
- Filesystem errors
- Verification failures

**Debug level** (everything else):
- Starting clone operations
- Fetch operations
- Resolving versions
- Checkout operations
- Internal state transitions
- All operation parameters (URLs, paths, flags)

## Implementation Plan

### Phase 1: Research and Setup

**Tasks:**
1. Add `github.com/go-git/go-git/v5` dependency to `tools/depctl/go.mod`
2. Add required imports to `tools/depctl/dep/clone.go`:
   ```go
   import (
       "github.com/go-git/go-git/v5"
       "github.com/go-git/go-git/v5/plumbing"
       "github.com/go-git/go-git/v5/config"
   )
   ```
3. Review go-git documentation:
   - Clone operations: `git.PlainClone()`, `git.PlainCloneContext()`
   - Repository operations: `git.PlainOpen()`
   - Fetch: `repo.Fetch()`
   - Resolve references: `repo.ResolveRevision()`
   - Checkout: `worktree.Checkout()`
   - Branch creation: `repo.CreateBranch()` or `worktree.Checkout()` with `Create: true`
4. Understand how to handle:
   - Shallow clones (`Depth: 1` in `CloneOptions`)
   - Branch creation during checkout
   - Detached HEAD state
   - Tag dereferencing

**Key go-git APIs to use:**
```go
// Clone repository (shallow)
git.PlainClone(path, false, &git.CloneOptions{
    URL: cloneURL,
    Depth: 1,  // shallow clone
    SingleBranch: true,
    ReferenceName: plumbing.ReferenceName("refs/heads/master"),
    Progress: os.Stdout,  // show progress
})

// Open existing repo
repo, err := git.PlainOpen(path)

// Fetch updates
err := repo.Fetch(&git.FetchOptions{
    RemoteName: "origin",
    Progress: os.Stdout,
})

// Resolve version (tag/branch/SHA)
hash, err := repo.ResolveRevision(plumbing.Revision("v1.11.0"))
// or
hash, err := repo.ResolveRevision(plumbing.Revision("3cfedb685"))
// or
hash, err := repo.ResolveRevision(plumbing.Revision("master"))

// Get worktree
worktree, err := repo.Worktree()

// Checkout with branch creation
err := worktree.Checkout(&git.CheckoutOptions{
    Hash: *hash,
    Branch: plumbing.NewBranchReferenceName("local/v1.11.0"),
    Create: true,  // create branch if it doesn't exist
    Force: true,   // overwrite existing branch if needed
})

// Get HEAD
head, err := repo.Head()
currentHash := head.Hash()

// Compare hashes
if currentHash != *expectedHash {
    return fmt.Errorf("verification failed")
}
```

### Phase 2: Replace cloneOrUpdate Function

**File:** `tools/depctl/dep/clone.go`

**Current signature:**
```go
func cloneOrUpdate(path, cloneURL, version string, shallow, updateExisting bool, log logging.Logger) error
```

**Changes needed:**

1. **Replace git clone** (lines ~113-146)
   - Remove `exec.Command("git", "clone", ...)`
   - Replace with:
   ```go
   cloneOpts := &git.CloneOptions{
       URL: cloneURL,
       Progress: os.Stdout,
       Tags: git.AllTags,
   }

   if shallow {
       cloneOpts.Depth = 1
       cloneOpts.SingleBranch = true
       // Note: ReferenceName should be set to the default branch
       // We'll checkout the specific version after clone
   }

   log.Debug("Cloning repository with go-git",
       zap.String("url", cloneURL),
       zap.String("path", path),
       zap.Bool("shallow", shallow),
   )

   repo, err := git.PlainClone(path, false, cloneOpts)
   if err != nil {
       return stacktrace.Errorf("failed to clone repository: %w", err)
   }
   ```

2. **Replace git fetch** (lines ~169-174)
   - Remove `exec.Command("git", "fetch", ...)`
   - Replace with:
   ```go
   repo, err := git.PlainOpen(path)
   if err != nil {
       return stacktrace.Errorf("failed to open repository: %w", err)
   }

   log.Debug("Fetching updates from remote")
   err = repo.Fetch(&git.FetchOptions{
       RemoteName: "origin",
       Progress: os.Stdout,
       Tags: git.AllTags,
   })
   // Ignore ErrAlreadyUpToDate
   if err != nil && err != git.NoErrAlreadyUpToDate {
       log.Debug("Fetch completed with note", zap.Error(err))
   }
   ```

3. **Replace git rev-parse** (lines ~176-201)
   - Remove `exec.Command("git", "rev-parse", ...)`
   - Replace with:
   ```go
   log.Debug("Resolving version to commit SHA",
       zap.String("version", version),
   )

   hash, err := repo.ResolveRevision(plumbing.Revision(version))
   if err != nil {
       return stacktrace.Errorf("failed to resolve version %s: %w", version, err)
   }

   log.Debug("Resolved version",
       zap.String("version", version),
       zap.String("hash", hash.String()),
   )
   ```

4. **Replace git checkout** (lines ~203-214)
   - Remove `exec.Command("git", "checkout", ...)`
   - Replace with:
   ```go
   worktree, err := repo.Worktree()
   if err != nil {
       return stacktrace.Errorf("failed to get worktree: %w", err)
   }

   branchName := fmt.Sprintf("local/%s", version)
   branchRef := plumbing.NewBranchReferenceName(branchName)

   log.Debug("Checking out version to branch",
       zap.String("hash", hash.String()),
       zap.String("branch", branchName),
   )

   err = worktree.Checkout(&git.CheckoutOptions{
       Hash: *hash,
       Branch: branchRef,
       Create: true,
       Force: true,
   })
   if err != nil {
       return stacktrace.Errorf("failed to checkout version %s: %w", version, err)
   }
   ```

5. **Remove directory change logic** (lines ~160-167)
   - go-git operates on repository paths directly, no need to `cd` into the directory
   - Remove `os.Getwd()`, `os.Chdir()`, and `defer` for directory restoration

### Phase 3: Replace verifyCloneVersion Function

**File:** `tools/depctl/dep/clone.go`

**Current implementation** (lines ~230-259):
- Uses `exec.Command("git", "rev-parse", "HEAD")`
- Uses `exec.Command("git", "rev-parse", version)`
- Compares string SHAs

**New implementation:**
```go
func verifyCloneVersion(repo *git.Repository, version string) error {
    // Get current HEAD
    head, err := repo.Head()
    if err != nil {
        return stacktrace.Errorf("failed to get HEAD: %w", err)
    }
    headHash := head.Hash()

    // Resolve expected version
    expectedHash, err := repo.ResolveRevision(plumbing.Revision(version))
    if err != nil {
        return stacktrace.Errorf("failed to resolve version %s: %w", version, err)
    }

    // Compare hashes
    if headHash != *expectedHash {
        return stacktrace.Errorf("verification failed: HEAD %s does not match version %s (%s)",
            headHash.String(), version, expectedHash.String())
    }

    return nil
}
```

**Changes needed:**
- Add `repo *git.Repository` parameter to function
- Remove all `exec.Command` calls
- Use go-git API for HEAD and version resolution
- Compare `plumbing.Hash` values directly

### Phase 4: Smart Shallow Clone Strategy

**Goal:** Always use shallow clones by default for performance, but handle all version types (tags, branches, SHAs) correctly.

**Solution:** List remote refs first, then choose the optimal clone strategy.

**Implementation:**

```go
func cloneOrUpdate(path, cloneURL, version string, shallow, updateExisting bool, log logging.Logger) error {
    // Step 1: List remote refs to understand what's available
    log.Debug("Listing remote references",
        zap.String("url", cloneURL),
        zap.String("version", version),
    )

    remoteRefs, err := git.ListRemote(&git.ListRemoteOptions{
        URL: cloneURL,
        Auth: nil, // Add auth if needed
    })
    if err != nil {
        return stacktrace.Errorf("failed to list remote refs: %w", err)
    }

    // Step 2: Determine what the version refers to
    refToClone, refType := analyzeVersion(version, remoteRefs, log)

    // Step 3: Clone with appropriate strategy
    cloneOpts := &git.CloneOptions{
        URL: cloneURL,
        Progress: os.Stdout,
        Tags: git.NoTags, // We'll fetch tags separately if needed
    }

    if shallow {
        cloneOpts.Depth = 1

        switch refType {
        case refTypeBranch:
            // Shallow clone specific branch
            cloneOpts.SingleBranch = true
            cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(refToClone)
            log.Debug("Shallow cloning branch",
                zap.String("branch", refToClone),
            )

        case refTypeTag:
            // Shallow clone specific tag
            cloneOpts.SingleBranch = true
            cloneOpts.ReferenceName = plumbing.NewTagReferenceName(refToClone)
            log.Debug("Shallow cloning tag",
                zap.String("tag", refToClone),
            )

        case refTypeSHA:
            // For SHAs: clone default branch shallow, then fetch the specific commit
            // We can't shallow clone a SHA directly, but we can minimize data transfer
            cloneOpts.SingleBranch = true
            // Clone HEAD (default branch)
            log.Debug("Shallow cloning default branch for SHA checkout",
                zap.String("sha", version),
            )

        case refTypeUnknown:
            // Version not found in remote refs - let full clone happen and fail naturally
            cloneOpts.Depth = 0 // Full clone
            log.Debug("Version not found in remote refs, attempting full clone",
                zap.String("version", version),
            )
        }
    } else {
        // Full clone requested
        cloneOpts.Tags = git.AllTags
    }

    log.Debug("Cloning repository",
        zap.String("url", cloneURL),
        zap.String("path", path),
        zap.Bool("shallow", shallow),
        zap.String("refType", string(refType)),
    )

    repo, err := git.PlainClone(path, false, cloneOpts)
    if err != nil {
        return stacktrace.Errorf("failed to clone repository: %w", err)
    }

    // Step 4: For SHA references, fetch and checkout
    if refType == refTypeSHA {
        log.Debug("Fetching SHA commit",
            zap.String("sha", version),
        )

        // Try to fetch the specific SHA
        // Note: This may require a full fetch if the SHA is not in the shallow clone
        err = repo.Fetch(&git.FetchOptions{
            RemoteName: "origin",
            RefSpecs: []config.RefSpec{
                config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
            },
            Depth: 0, // Need full history to find arbitrary SHA
            Tags: git.NoTags,
        })
        if err != nil && err != git.NoErrAlreadyUpToDate {
            log.Debug("Fetch for SHA required full history", zap.Error(err))
        }
    }

    // Step 5: Resolve and checkout the version
    hash, err := repo.ResolveRevision(plumbing.Revision(version))
    if err != nil {
        return stacktrace.Errorf("failed to resolve version %s: %w", version, err)
    }

    log.Debug("Resolved version",
        zap.String("version", version),
        zap.String("hash", hash.String()),
    )

    worktree, err := repo.Worktree()
    if err != nil {
        return stacktrace.Errorf("failed to get worktree: %w", err)
    }

    branchName := fmt.Sprintf("local/%s", version)
    branchRef := plumbing.NewBranchReferenceName(branchName)

    log.Debug("Checking out version",
        zap.String("hash", hash.String()),
        zap.String("branch", branchName),
    )

    err = worktree.Checkout(&git.CheckoutOptions{
        Hash: *hash,
        Branch: branchRef,
        Create: true,
        Force: true,
    })
    if err != nil {
        return stacktrace.Errorf("failed to checkout version %s: %w", version, err)
    }

    return verifyCloneVersion(repo, version, log)
}

// Helper function to analyze what type of ref the version is
type refType string

const (
    refTypeBranch  refType = "branch"
    refTypeTag     refType = "tag"
    refTypeSHA     refType = "sha"
    refTypeUnknown refType = "unknown"
)

func analyzeVersion(version string, refs []*plumbing.Reference, log logging.Logger) (string, refType) {
    // Check if version matches a branch
    for _, ref := range refs {
        if ref.Name().IsBranch() {
            branchName := ref.Name().Short()
            if branchName == version {
                log.Debug("Version matches branch", zap.String("branch", branchName))
                return branchName, refTypeBranch
            }
        }
    }

    // Check if version matches a tag
    for _, ref := range refs {
        if ref.Name().IsTag() {
            tagName := ref.Name().Short()
            if tagName == version {
                log.Debug("Version matches tag", zap.String("tag", tagName))
                return tagName, refTypeTag
            }
        }
    }

    // Check if version looks like a SHA (hex string)
    if matched, _ := regexp.MatchString(`^[0-9a-f]{7,40}$`, version); matched {
        log.Debug("Version appears to be SHA", zap.String("sha", version))
        return version, refTypeSHA
    }

    // Check if version matches a SHA prefix
    for _, ref := range refs {
        if strings.HasPrefix(ref.Hash().String(), version) {
            log.Debug("Version matches SHA prefix",
                zap.String("prefix", version),
                zap.String("full_sha", ref.Hash().String()),
            )
            return version, refTypeSHA
        }
    }

    log.Debug("Version type unknown", zap.String("version", version))
    return version, refTypeUnknown
}
```

**Key improvements:**

1. **Always shallow by default** - when shallow=true
2. **Smart ref detection** - uses `git.ListRemote()` to check what exists before cloning
3. **Optimal strategy per ref type:**
   - Branches: `--depth 1 --single-branch --branch <name>`
   - Tags: `--depth 1 --single-branch --branch refs/tags/<name>`
   - SHAs: shallow clone default branch, then fetch to find SHA (may upgrade to full fetch)
4. **Better errors** - if version doesn't exist, we'll know before cloning
5. **No wasted clones** - we clone exactly what we need

**Note:** For SHAs, we may still need to fetch more history if the SHA isn't in the shallow clone. This is unavoidable, but we start shallow and only fetch more if needed.

### Phase 5: Update Tests

**File:** `tools/depctl/dep/clone_test.go`

**Changes:**
- Update `CloneOptions` initialization to not use `Logger: log` with `logging.Off` level
- Or set logger to `logging.Debug` to see go-git operations
- Verify tests still pass with go-git implementation

**File:** `tools/depctl/dep/clone_integration_test.go`

**Tests to verify:**
1. Clone avalanchego with explicit version (tag)
2. Clone firewood with version from avalanchego
3. Clone coreth with version from avalanchego
4. Clone avalanchego with version from coreth
5. Clone firewood with default (main branch)
6. **Clone with commit hash (SHA)** - This is the key test that should now work reliably

Run tests:
```bash
go test -tags=integration -v ./dep
```

### Phase 6: Update Documentation

**File:** `.agents/plans/depctl/implementation.md`

Add section:
```markdown
## Migration to go-git (v2)

### Motivation

Replaced CLI git commands with go-git library for:
- Better error messages with context
- No external git binary dependency
- Reliable SHA cloning without shallow/full fallback
- Type-safe git operations
- Easier debugging and testing

### Changes

- Removed all `exec.Command("git", ...)` invocations
- Using `github.com/go-git/go-git/v5` library:
  - `git.PlainClone()` for cloning
  - `repo.Fetch()` for fetching
  - `repo.ResolveRevision()` for version resolution
  - `worktree.Checkout()` for branch checkout
- Simplified shallow clone logic (or removed entirely for reliability)

### Benefits

- SHAs now work directly: `depctl clone --version 3cfedb685`
- Better error messages: "object not found: 3cfedb685abc..." instead of "exit status 128"
- No dependency on git binary version or configuration
- More testable with go-git's mock interfaces
```

## Implementation Steps

1. **Add dependency:** `go get github.com/go-git/go-git/v5`
2. **Replace cloneOrUpdate:** Convert git commands to go-git API calls
3. **Replace verifyCloneVersion:** Use go-git to compare HEAD and expected hash
4. **Remove exec.Command:** Delete all CLI git invocations
5. **Test with SHA:** Verify `depctl clone --version 3cfedb685` works
6. **Run integration tests:** `go test -tags=integration -v ./dep`
7. **Update documentation:** Document the migration and benefits

## Testing Checklist

- [ ] Clone with tag (e.g., `v1.11.0`)
- [ ] Clone with full SHA (e.g., `3cfedb685abc...`)
- [ ] Clone with short SHA (e.g., `3cfedb685`)
- [ ] Clone with branch name (e.g., `master`, `main`)
- [ ] Clone with default version from go.mod
- [ ] Clone with `--update-existing` flag
- [ ] Clone to existing directory (skip behavior)
- [ ] Shallow clone (if implemented)
- [ ] Full clone
- [ ] All integration tests pass
- [ ] Error messages are clear and helpful

## Expected Improvements

1. **SHA cloning works reliably** - no more "Remote branch not found" errors
2. **Better error messages** - `ErrObjectNotFound{hash: 3cfedb685}` vs "exit status 128"
3. **Simpler code** - direct API calls instead of command-line arg building
4. **More reliable** - no dependency on git binary version or configuration
5. **Better debugging** - structured logging of go-git operations
6. **Faster development** - can mock go-git for unit tests

## Risks and Mitigation

**Risk:** go-git might have different behavior than CLI git
**Mitigation:** Run comprehensive integration tests, compare with existing test suite

**Risk:** go-git might be slower than CLI git
**Mitigation:** Benchmark before/after, optimize if needed (unlikely to be an issue)

**Risk:** go-git might not support some edge cases
**Mitigation:** Identify edge cases early in testing, file issues with go-git if needed

**Risk:** Breaking existing workflows
**Mitigation:** Maintain backwards compatibility in command-line interface, only change internal implementation

## Error Message Strategy

Map go-git errors to user-friendly messages:

| go-git Error | User-Facing Message |
|--------------|---------------------|
| `git.ErrRepositoryNotExists` | "Repository not found or not accessible: <url>" |
| `plumbing.ErrObjectNotFound` | "Version '<version>' not found in repository" |
| `plumbing.ErrReferenceNotFound` | "Branch or tag '<ref>' not found in repository" |
| `transport.ErrAuthenticationRequired` | "Authentication required for private repository: <url>" |
| `transport.ErrAuthorizationFailed` | "Authorization failed - check credentials for: <url>" |
| `transport.ErrRepositoryNotFound` | "Repository not found: <url>" |
| Network timeouts | "Network timeout while accessing repository: <url>" |

All user-facing errors should include context (repository URL, version requested) and use stacktrace for internal error chains.

## Future Enhancements

After migration is complete and stable:

1. **Optimize shallow clones** - Implement smart shallow clone for tags/branches
2. **Progress reporting** - Use go-git's progress hooks for better UX (verify current Progress: os.Stdout is acceptable)
3. **Parallel operations** - Clone multiple repos concurrently
4. **Advanced git operations** - Rebase, merge, cherry-pick if needed
5. **Better testing** - Mock go-git interfaces for unit tests
