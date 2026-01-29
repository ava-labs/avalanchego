# Release Procedure

This document describes how to create releases for AvalancheGo and its
submodules.

For the rationale behind this process, see [Multi-Module Release
Strategy](design/multi_module_release.md).

## Prerequisites

- Push access to the repository
- All tests passing on the target commit

## Tagging Process

Both release and development tags follow the same core process:

### 1. Update Require Directives

```bash
./scripts/run_task.sh tags-set-require-directives -- <version>
```

Commit the go.mod changes.

### 2. Create and Push Tags

```bash
./scripts/run_task.sh tags-create -- <version>
./scripts/run_task.sh tags-push -- <version>
```

### 3. Verify

From a fresh directory (to avoid local replace directives):

```bash
cd $(mktemp -d)
go mod init test
go get github.com/ava-labs/avalanchego@<version>
go list -m all | grep avalanchego
```

All submodules should resolve to matching versions.

## Release Tags

For official releases, the tagging process is applied to master after
merging the require directive updates:

1. Create a PR with `./scripts/run_task.sh tags-set-require-directives -- vX.Y.Z`
2. Merge to master
3. From the merge commit, run `./scripts/run_task.sh tags-create -- vX.Y.Z`
4. Run `./scripts/run_task.sh tags-push -- vX.Y.Z` and verify

## Development Tags

To share work-in-progress without merging to master:

1. On your branch, run `./scripts/run_task.sh tags-set-require-directives -- v0.0.0-mybranch`
2. Commit and push to your branch
3. Run `./scripts/run_task.sh tags-create -- v0.0.0-mybranch`
4. Run `./scripts/run_task.sh tags-push -- v0.0.0-mybranch`

External consumers can then `go get github.com/ava-labs/avalanchego@v0.0.0-mybranch`.

## Task Reference

### `tags-set-require-directives`

Updates `require` directives in all go.mod files to reference the
specified version. Version must match `vX.Y.Z` or `vX.Y.Z-suffix`.

### `tags-create`

Creates tags for the main module and all submodules at the current commit.

### `tags-push`

Pushes tags for the main module and all submodules. Set `GIT_REMOTE` to
override the default remote (`origin`).
