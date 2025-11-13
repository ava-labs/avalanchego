# Polyrepo Logging Test Scenarios

This document provides test scenarios to evaluate the utility and completeness of logging in the polyrepo fixture.

## Test Environment Setup

All tests assume you're running from a directory with a `go.mod` file (typically avalanchego, coreth, or a test directory).

## Scenario 1: Fresh Sync from Avalanchego

**Setup:** Start in avalanchego repo with go.mod

```bash
cd /path/to/avalanchego
polyrepo --log-level=info sync
```

**Expected Info-Level Output:**
- ✓ Shows command starting with arguments and flags
- ✓ Shows current repo detected (avalanchego)
- ✓ Shows repos to sync (coreth, firewood) and count
- ✓ For each repo:
  - Shows determining default reference
  - Shows version found in go.mod (or default branch used)
  - Shows version-to-ref conversion for pseudo-versions
  - Shows repository configuration (git URL, module, nix build requirement)
  - Shows which ref is being used and whether it was defaulted
  - Shows nix build running (for firewood)
  - Shows replace directive being added
  - Shows successful sync completion
- ✓ All defaults visible (depth=1, force=false)

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync
```

**Expected Debug-Level Output:**
- ✓ Can trace: DetectCurrentRepo → GetDirectDependencies → GetDefaultRefForRepo
- ✓ Can see go.mod parsing steps (file read, size, require count)
- ✓ Can see dependency lookup and results
- ✓ Can see pseudo-version parsing (split parts, hash extraction)
- ✓ Can see git operations:
  - Repository exists check
  - Clone configuration (depth, single branch)
  - Clone execution
  - Checkout operations (if SHA)
- ✓ Can see nix build command and execution
- ✓ Can see go.mod modification steps (check existing, add/update, format, write)
- ✓ Can identify which function is executing at any point

**Evaluation Criteria:**
- [ ] User can understand what happened without debug logs
- [ ] All inputs and derivations are visible at info level
- [ ] Debug logs sufficient to trace execution and localize problems
- [ ] No missing context that would leave user confused

---

## Scenario 2: Sync Specific Version with SHA

**Setup:** Sync a specific commit

```bash
cd /path/to/avalanchego
polyrepo --log-level=info sync firewood@abc123def456
```

**Expected Info-Level Output:**
- ✓ Shows parsing repository arguments (count=1)
- ✓ Shows explicit ref provided (firewood, abc123def456)
- ✓ Shows ref is NOT defaulted (wasDefaulted=false)
- ✓ Shows final ref being used

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync firewood@abc123def456
```

**Expected Debug-Level Output:**
- ✓ SHA detection logic (looksLikeSHA returns true)
- ✓ Clone configuration decision (singleBranch=false for SHA)
- ✓ Clone execution
- ✓ Checkout after clone (resolving ref, getting hash, executing checkout)
- ✓ If shallow clone fails: see fallback to full clone

**Evaluation Criteria:**
- [ ] Clear distinction between user-provided and defaulted values
- [ ] Can trace SHA handling logic through clone and checkout
- [ ] Fallback behavior is visible and understandable

---

## Scenario 3: Sync with Existing Dirty Repo (Error Case)

**Setup:** Modify existing repo then try to sync

```bash
cd /path/to/avalanchego
# Manually modify coreth/README.md
echo "test" >> coreth/README.md
polyrepo --log-level=info sync coreth
```

**Expected Info-Level Output:**
- ✓ Shows sync starting
- ✓ Shows repository exists
- ✓ Error message is clear about which repo is dirty and path

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync coreth
```

**Expected Debug-Level Output:**
- ✓ Repository exists check (exists=true)
- ✓ Force flag check (force=false)
- ✓ Dirty check execution
- ✓ Dirty check result (isDirty=true, modifiedFiles count)
- ✓ Clear execution path to error location

**Evaluation Criteria:**
- [ ] Error message provides actionable information
- [ ] Can see where check failed without running debug
- [ ] Debug logs show exact decision path to error

---

## Scenario 4: Sync with Force Flag

**Setup:** Force update of existing repo

```bash
cd /path/to/avalanchego
polyrepo --log-level=info sync coreth --force
```

**Expected Info-Level Output:**
- ✓ Shows force flag is set (force=true)
- ✓ Shows updating existing repository (not cloning)

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync coreth --force
```

**Expected Debug-Level Output:**
- ✓ Repository exists check (exists=true)
- ✓ Force flag check (force=true) - shows decision to update
- ✓ UpdateRepo path taken (fetch, checkout operations)
- ✓ Fetch results (already up to date or completed)
- ✓ Checkout execution

**Evaluation Criteria:**
- [ ] Clear that force flag changed behavior
- [ ] Can distinguish update path from clone path
- [ ] Git fetch and checkout operations are traceable

---

## Scenario 5: Reset Specific Repos

**Setup:** Remove specific replace directives

```bash
cd /path/to/avalanchego
polyrepo --log-level=info reset coreth firewood
```

**Expected Info-Level Output:**
- ✓ Shows repositories being reset
- ✓ Shows removed replace directives confirmation

**Debug-Level Test:**
```bash
polyrepo --log-level=debug reset coreth firewood
```

**Expected Debug-Level Output:**
- ✓ Shows repo names being processed
- ✓ For each repo: name → config → module path resolution
- ✓ For each module: finding replace directive (found/not found)
- ✓ Remove operation execution
- ✓ go.mod write operation

**Evaluation Criteria:**
- [ ] User knows what was changed
- [ ] Can trace which modules were targeted
- [ ] Can see if replace directives actually existed

---

## Scenario 6: Reset All Repos

**Setup:** Remove all polyrepo replace directives

```bash
cd /path/to/avalanchego
polyrepo --log-level=info reset
```

**Expected Info-Level Output:**
- ✓ Shows no specific repos provided
- ✓ Shows removed all polyrepo replace directives

**Debug-Level Test:**
```bash
polyrepo --log-level=debug reset
```

**Expected Debug-Level Output:**
- ✓ Decision: no args → reset all
- ✓ Shows all repo names collected
- ✓ Can trace through each removal

**Evaluation Criteria:**
- [ ] Clear that all repos were targeted
- [ ] Can see complete list of what was processed

---

## Scenario 7: Sync from Coreth

**Setup:** Run sync from coreth directory

```bash
cd /path/to/coreth
polyrepo --log-level=info sync
```

**Expected Info-Level Output:**
- ✓ Shows detected repo (coreth)
- ✓ Shows will sync avalanchego and firewood
- ✓ Shows version discovery for both from go.mod
- ✓ Shows version-to-ref conversion

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync
```

**Expected Debug-Level Output:**
- ✓ Repository detection process
- ✓ Module path matching
- ✓ For each target repo: dependency lookup in go.mod
- ✓ Version resolution and conversion

**Evaluation Criteria:**
- [ ] Context switching (different repo) is clear
- [ ] Version resolution from go.mod is traceable
- [ ] Dependency relationships are visible

---

## Scenario 8: Sync from Unknown Location

**Setup:** Run from temporary directory with go.mod

```bash
mkdir /tmp/test-polyrepo
cd /tmp/test-polyrepo
cp /path/to/avalanchego/go.mod .
polyrepo --log-level=info sync
```

**Expected Info-Level Output:**
- ✓ Shows no current repo detected (or current repo = "")
- ✓ Shows will sync all three repos
- ✓ Shows using default branches for all
- ✓ Shows where repos will be cloned

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync
```

**Expected Debug-Level Output:**
- ✓ DetectCurrentRepo returns "" or not in known repo
- ✓ Standalone mode requires explicit repos (error path)
- ✓ GetDefaultRefForRepo uses default branches (no version from go.mod)

**Evaluation Criteria:**
- [ ] Clear that no repo was detected
- [ ] Fallback to defaults is visible
- [ ] User understands what will happen

---

## Scenario 9: Sync with Invalid Ref (Error Case)

**Setup:** Try to sync non-existent ref

```bash
cd /path/to/avalanchego
polyrepo --log-level=info sync coreth@nonexistent-branch
```

**Expected Info-Level Output:**
- ✓ Shows attempting to sync with specified ref
- ✓ Error clearly indicates which ref failed

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync coreth@nonexistent-branch
```

**Expected Debug-Level Output:**
- ✓ Clone configuration (branch name, single branch)
- ✓ Git operation failure
- ✓ Exact point of failure visible

**Evaluation Criteria:**
- [ ] Error message actionable
- [ ] Can identify whether problem was in clone vs checkout
- [ ] Debug helps understand why it failed

---

## Scenario 10: Sync with Pseudo-Version

**Setup:** Sync when go.mod has pseudo-version

```bash
cd /path/to/avalanchego
# Assume go.mod has: github.com/ava-labs/coreth v0.13.9-0.20250108211834-57a74c3a7fd7
polyrepo --log-level=info sync
```

**Expected Info-Level Output:**
- ✓ Shows pseudo-version from go.mod
- ✓ Shows converted version to git reference
- ✓ Shows commit hash being used

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync
```

**Expected Debug-Level Output:**
- ✓ Version lookup in go.mod
- ✓ ConvertVersionToGitRef logic
- ✓ Pseudo-version detection (isPseudo=true)
- ✓ String splitting (parts shown)
- ✓ Commit hash extraction
- ✓ Hash used in clone/checkout

**Evaluation Criteria:**
- [ ] Pseudo-version handling is transparent
- [ ] User can see exact hash being used
- [ ] Conversion logic is traceable

---

## Scenario 11: Direct Dependencies Sync (No Args)

**Setup:** Run sync without arguments from coreth or firewood

```bash
cd /path/to/coreth
polyrepo --log-level=info sync
```

**Expected Info-Level Output:**
- ✓ Shows detected primary repo mode (go.mod exists)
- ✓ Shows detected current repository (coreth)
- ✓ Shows "no repositories specified, auto-detecting direct dependencies"
- ✓ Shows "determined repositories to sync: only avalanchego (primary dependency)"
- ✓ Shows count=1, repos=["avalanchego"]
- ✓ Does NOT sync firewood (even though coreth depends on it)

**From Firewood:**
```bash
cd /path/to/firewood
polyrepo --log-level=info sync
```

**Expected Info-Level Output:**
- ✓ Shows detected primary repo mode (go.mod exists)
- ✓ Shows detected current repository (firewood)
- ✓ Shows either:
  - "determined repositories to sync: only avalanchego (primary dependency)" (if firewood depends on avalanchego)
  - "no repositories to sync (avalanchego not found as dependency)" (if firewood doesn't depend on avalanchego)
- ✓ Does NOT sync coreth (transitive dependency only)

**From Avalanchego (Error Case):**
```bash
cd /path/to/avalanchego
polyrepo --log-level=info sync
```

**Expected Info-Level Output:**
- ✓ Shows detected primary repo mode (go.mod exists)
- ✓ Shows detected current repository (avalanchego)
- ✓ Shows "no repositories specified, auto-detecting direct dependencies"
- ✓ Error: "failed to determine direct dependencies: avalanchego is a root repository and requires explicit repository arguments"

**Debug-Level Test:**
```bash
polyrepo --log-level=debug sync
```

**Expected Debug-Level Output:**
- ✓ DetectCurrentRepo execution and result
- ✓ GetDirectDependencies execution
- ✓ Go.mod path determination for current repo
- ✓ Avalanchego config lookup
- ✓ GetDependencyVersion execution to check if avalanchego is in go.mod
- ✓ Decision: found avalanchego → return ["avalanchego"], or not found → return []
- ✓ For avalanchego: immediate error before checking dependencies

**Evaluation Criteria:**
- [ ] Clear that only direct dependency (avalanchego) is synced, not transitive dependencies
- [ ] User understands that coreth and firewood only sync avalanchego by default
- [ ] Error message for avalanchego makes it clear explicit repos are required
- [ ] Can trace dependency detection logic through GetDirectDependencies

**Test Coverage:**
- Unit tests: `TestGetDirectDependencies` (4 test cases)
- Integration tests: `TestSync_FromFirewood_NoArgs_OnlyAvalanchego`
- Integration tests: `TestSync_FromCoreth_NoArgs_OnlyAvalanchego`
- Integration tests: `TestSync_FromAvalanchego_NoArgs_ReturnsError`

---

## Overall Evaluation Checklist

### Info Logging Assessment

**Inputs Visibility:**
- [ ] All CLI arguments logged at command start
- [ ] All defaulted values clearly indicated
- [ ] All detected/inferred values visible
- [ ] All configuration from files visible

**Derivations Visibility:**
- [ ] Version-to-ref conversions clearly shown
- [ ] Path calculations visible
- [ ] Repository detection logic results visible
- [ ] Decision points show which branch was taken

**Format Assessment:**
- [ ] Log messages clear and actionable
- [ ] Structured fields make data easy to extract
- [ ] Log level appropriate (not too verbose, not too sparse)
- [ ] User can understand what happened without debug logs

### Debug Logging Assessment

**Execution Tracing:**
- [ ] Can identify which function is executing
- [ ] Can see function entry with all arguments
- [ ] Can see function exit or next operation
- [ ] Can trace call chain through multiple functions

**Decision Point Tracing:**
- [ ] All if/else branches show path taken
- [ ] Loop iterations visible (or entry/exit)
- [ ] Error handling paths visible
- [ ] Retry/fallback logic clearly traceable

**Problem Localization:**
- [ ] Can identify last successful operation before error
- [ ] Can see intermediate values in failing logic
- [ ] Can differentiate between similar operations
- [ ] Can identify which repo/file operation failed
- [ ] Can see git library results

**Logic Bug Detection:**
- [ ] Can verify correct values passed between functions
- [ ] Can see if string parsing produced expected results
- [ ] Can verify conditional logic evaluated correctly
- [ ] Can identify unexpected nil/empty values
- [ ] Can verify loops processed expected number of items

---

## Running the Full Test Suite

To run all scenarios and evaluate logging:

```bash
# Run this script to execute all scenarios
# (Requires setup of test repositories first)

#!/bin/bash

echo "=== Polyrepo Logging Evaluation ==="

scenarios=(
  "scenario1:info sync"
  "scenario1-debug:debug sync"
  "scenario2:info sync firewood@abc123"
  # ... etc
)

for scenario in "${scenarios[@]}"; do
  name="${scenario%%:*}"
  cmd="${scenario#*:}"
  echo "Running $name: polyrepo --log-level=$cmd"
  polyrepo --log-level=$cmd 2>&1 | tee "logs/$name.log"
done

echo "Review logs in logs/ directory"
```

## Success Criteria

The logging implementation is successful if:
1. **Info level:** A user can understand what the tool did, with what inputs, and what derivations occurred
2. **Debug level:** A developer can trace execution flow and localize logic bugs
3. **Errors:** All error cases provide actionable information
4. **Consistency:** Logging patterns are consistent across all commands
5. **Performance:** Logging does not noticeably impact performance
