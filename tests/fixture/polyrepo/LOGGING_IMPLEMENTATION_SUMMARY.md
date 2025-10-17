# Polyrepo Logging Implementation Summary

## Overview

The polyrepo fixture has been updated to use structured logging with the zap logger, following the pattern established in `tests/fixture/tmpnet/`. This implementation adds comprehensive logging at both info and debug levels to support normal operations and troubleshooting.

## Changes Made

### 1. CLI Flag for Log Level Control

**File:** `main.go`

- Added `--log-level` persistent flag accepting: `debug`, `info`, `warn`, `error`
- Default level: `info`
- Logger created in `main()` and passed via context to all commands
- Usage: `polyrepo --log-level=debug sync`

### 2. Core Package Updates

All core package functions now accept `log logging.Logger` as the **first parameter**, matching tmpnet conventions.

#### Updated Files:

**`core/gomod.go`:**
- `ReadGoMod(log, path)` - Debug: file read, parse steps
- `GetModulePath(log, goModPath)` - Debug: module path discovery
- `AddReplaceDirective(log, goModPath, oldPath, newPath)` - Debug: check existing, add/update, write
- `RemoveReplaceDirective(log, goModPath, oldPath)` - Debug: find directive, remove, write
- `GetDependencyVersion(log, goModPath, modulePath)` - Debug: parse, search, result
- `ConvertVersionToGitRef(log, version)` - Debug: pseudo-version detection, parsing, hash extraction

**`core/nix.go`:**
- `RunNixBuild(log, buildPath)` - Debug: command execution; Error: build failures with output

**`core/repo.go`:**
- `CloneRepo(log, url, path, ref, depth)` - Debug: depth config, SHA detection, clone execution
- `CheckoutRef(log, repoPath, ref)` - Debug: resolve revision, checkout execution
- `GetCurrentRef(log, repoPath)` - Debug: HEAD type (branch/detached), value
- `IsRepoDirty(log, repoPath)` - Debug: dirty check result, modified file count
- `CloneOrUpdateRepo(log, url, path, ref, depth, force)` - Debug: exists check, force logic, retry with full clone
- `UpdateRepo(log, repoPath, ref)` - Debug: fetch operation, checkout

**`core/sync.go`:**
- `DetectCurrentRepo(log, dir)` - Debug: go.mod detection, module matching
- `GetDefaultRefForRepo(log, currentRepo, targetRepo, goModPath)` - **Info:** determination logic, version discovery, conversion; Debug: config details

**`core/reset.go`:**
- `ResetRepos(log, goModPath, repoNames)` - Debug: repos to reset, each removal operation

**`core/update.go`:**
- `UpdateAvalanchego(log, goModPath, version)` - Debug: validation checks, version resolution

### 3. Main.go Command Handlers

All command handlers updated to:
1. Get logger from context: `log := getLogger(cmd)`
2. Pass logger as first parameter to all core functions
3. Replace `fmt.Printf` with structured logging
4. Use `log.Warn` instead of `fmt.Fprintf(os.Stderr, ...)`

**Status Command:**
- Warnings for failed status checks

**Sync Command:**
- Info: Command start with args/flags
- Info: Repository detection and selection
- Info: Configuration display
- Info: Reference determination and defaulting
- Info: Major operations (clone, nix build, replace directive)
- Info: Success confirmations

**Reset Command:**
- Info: Repositories being reset
- Info: Completion confirmation

**Update-Avalanchego Command:**
- Info: Operation start and completion

## Logging Design Principles

### Info Level
**Purpose:** Enable users to understand what happened without debug logs

**Includes:**
- All CLI inputs (arguments, flags)
- Defaulted values (marked as such)
- Detected/inferred values (current repo, module paths)
- Configuration loaded from files
- Key derivations (version → ref conversions)
- Major operations and their parameters
- Success/completion messages

### Debug Level
**Purpose:** Enable developers to trace execution and localize bugs

**Includes:**
- Function entry with all parameters
- Decision points and branches taken
- Intermediate values during processing
- Git operation details
- File system operations
- Parsing steps and results
- Loop iterations or entry/exit
- Retry/fallback logic

## Example Output

### Info Level Example
```
2025-10-14T10:30:00.000Z	info	polyrepo	starting sync command	{"args": [], "depth": 1, "force": false, "baseDir": "/path/to/avalanchego"}
2025-10-14T10:30:00.001Z	info	polyrepo	detected current repository	{"currentRepo": "avalanchego"}
2025-10-14T10:30:00.002Z	info	polyrepo	determined repositories to sync	{"count": 2, "repos": ["coreth", "firewood"]}
2025-10-14T10:30:00.003Z	info	polyrepo	determining default reference for repository	{"currentRepo": "avalanchego", "targetRepo": "coreth", "goModPath": "go.mod"}
2025-10-14T10:30:00.004Z	info	polyrepo	found dependency version in go.mod	{"targetRepo": "coreth", "module": "github.com/ava-labs/coreth", "version": "v0.13.9-0.20250108211834-57a74c3a7fd7"}
2025-10-14T10:30:00.005Z	info	polyrepo	converted version to git reference	{"version": "v0.13.9-0.20250108211834-57a74c3a7fd7", "gitRef": "57a74c3a7fd7"}
```

### Debug Level Example
```
2025-10-14T10:30:00.005Z	debug	polyrepo	converting version to git ref	{"version": "v0.13.9-0.20250108211834-57a74c3a7fd7"}
2025-10-14T10:30:00.006Z	debug	polyrepo	checked if version is pseudo-version	{"isPseudo": true}
2025-10-14T10:30:00.007Z	debug	polyrepo	extracting commit hash from pseudo-version	{"pseudoVersion": "v0.13.9-0.20250108211834-57a74c3a7fd7"}
2025-10-14T10:30:00.008Z	debug	polyrepo	split pseudo-version into parts	{"partCount": 3, "parts": ["v0.13.9", "20250108211834", "57a74c3a7fd7"]}
2025-10-14T10:30:00.009Z	debug	polyrepo	extracted commit hash	{"commitHash": "57a74c3a7fd7"}
2025-10-14T10:30:00.010Z	debug	polyrepo	entering CloneOrUpdateRepo	{"url": "https://github.com/ava-labs/coreth", "path": "/path/to/coreth", "ref": "57a74c3a7fd7", "depth": 1, "force": false}
```

## Testing and Evaluation

A comprehensive test scenarios document has been created: `LOGGING_TEST_SCENARIOS.md`

**Contains:**
- 10 detailed test scenarios covering:
  - Fresh sync operations
  - Specific version/SHA syncs
  - Error cases (dirty repos, invalid refs)
  - Force flag behavior
  - Reset operations
  - Different repository contexts
  - Pseudo-version handling
- Evaluation checklists for info and debug levels
- Success criteria for logging implementation

**Next Steps for Testing:**
1. Run scenarios from `LOGGING_TEST_SCENARIOS.md`
2. Collect actual log output
3. Evaluate against checklist criteria
4. Identify any gaps or improvements needed
5. Iterate on logging based on findings

## Benefits

1. **Consistency:** Matches tmpnet logging patterns (logger as first parameter)
2. **Structured Logging:** Easy to parse and filter with tools like jq
3. **Debug Visibility:** Can enable verbose logging to see execution flow
4. **Better Troubleshooting:** Clear indication of operations and failure points
5. **No Behavior Changes:** Only adds logging, doesn't change functionality
6. **User Control:** Log level adjustable via CLI flag

## Files Modified

1. `main.go` - Logger setup, flag, context handling, command updates
2. `core/gomod.go` - All functions updated with logging
3. `core/nix.go` - RunNixBuild updated
4. `core/repo.go` - All git operation functions updated
5. `core/sync.go` - Detection and derivation functions updated
6. `core/reset.go` - Reset logic updated
7. `core/update.go` - Update logic updated

## Files Created

1. `LOGGING_TEST_SCENARIOS.md` - Comprehensive test scenarios and evaluation criteria
2. `LOGGING_IMPLEMENTATION_SUMMARY.md` - This document

## Compatibility Notes

- Existing code calling core functions will need to pass logger as first parameter
- Tests will need to provide a logger (can use a no-op logger for tests that don't care about output)
- All logging uses structured fields (zap.String, zap.Int, zap.Bool, etc.) for machine parsing

## Future Enhancements

Potential improvements based on usage:
1. Add metrics/timing logging for performance analysis
2. Add sampling for high-frequency debug logs if needed
3. Add log aggregation guidance for CI environments
4. Consider adding trace IDs for correlation across operations
