# Release Procedure

This document describes how to create releases for AvalancheGo and its
submodules.

For the rationale behind this process, see [Multi-Module Release
Strategy](design/multi-module-release.md).

## Prerequisites

- Push access to the repository
- All tests passing on the target commit

## Tagging Process

Both release and development tags follow the same core process:

### 1. Update Require Directives

```bash
./scripts/update_require_directives.sh <version>
```

Commit the go.mod changes.

### 2. Create and Push Tags

```bash
./scripts/create_release_tags.sh <version>
```

Push the tags as shown in the script output.

### 3. Verify

From a fresh directory (to avoid local replace directives):

```bash
cd $(mktemp -d)
go mod init test
go get github.com/ava-labs/avalanchego@<version>
go list -m all | grep ava-labs
```

All submodules should resolve to matching versions.

## Release Tags

For official releases, the tagging process is applied to master after
merging the require directive updates:

1. Create a PR with `./scripts/update_require_directives.sh vX.Y.Z`
2. Merge to master
3. From the merge commit, run `./scripts/create_release_tags.sh vX.Y.Z`
4. Push tags and verify

## Development Tags

To share work-in-progress without merging to master:

1. On your branch, run `./scripts/update_require_directives.sh v0.0.0-mybranch`
2. Commit and push to your branch
3. Run `./scripts/create_release_tags.sh v0.0.0-mybranch`
4. Push tags

External consumers can then `go get github.com/ava-labs/avalanchego@v0.0.0-mybranch`.

## Script Reference

### [`scripts/update_require_directives.sh`](../scripts/update_require_directives.sh)

Updates `require` directives in all go.mod files to reference the
specified version. Version must match `vX.Y.Z` or `vX.Y.Z-suffix`.

### [`scripts/create_release_tags.sh`](../scripts/create_release_tags.sh)

Creates release tags for the main module and all submodules at the
current commit. Tags are created locally; you must push them separately.
