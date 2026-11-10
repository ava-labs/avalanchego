# AvalancheGo Release Guide

This document covers the complete release process for AvalancheGo and its integrated components (Coreth and Subnet-EVM).

## Overview

AvalancheGo is a monorepo which contains:

- **AvalancheGo** - The main Avalanche node implementation
- **Coreth** (in [graft/coreth/](graft/coreth/)) - C-Chain types, params and precompiles used by the [C-Chain VM](vms/evm/imp/cchain/), compiled into AvalancheGo
- **Subnet-EVM** (in [graft/subnet-evm/](graft/subnet-evm/)) - Subnet-EVM plugin, released as a separate binary

For the rationale behind the multi-module tagging process, see [Multi-Module Release Strategy](docs/design/multi-module-release.md).

### Versioning Strategy

All components follow aligned versioning:

- Same version number - When AvalancheGo releases v1.15.0, Subnet-EVM is also v1.15.0
- Coordinated tags - Each release creates tags for the main module and all submodules (e.g., `v1.15.0`, `graft/evm/v1.15.0`, `graft/coreth/v1.15.0`, `graft/subnet-evm/v1.15.0`)

## Release Procedure

Master always names the next version in `version.Current` and the internal `require` directives (enforced by [`check-require-directives`](#check-require-directives)). Any master commit can be tagged as a release candidate, the final release tags that same commit, and prep for the next version happens after the release.

### 1. Preparation

This section uses `v1.15.1-rc.0` as its example. Set these variables so you can copy the commands below as-is:

```bash
export VERSION_RC=v1.15.1-rc.0
export VERSION=v1.15.1
```

### 2. Pre-Release Changes

If this release schedules or activates a network upgrade, merge a PR to master with the changes below before you tag the first release candidate. Otherwise, [skip to step 3](#3-create-release-candidate-tags).

#### Scheduling a Network Upgrade

A release that schedules a new network upgrade increases the minor version (for example, `v1.16.0`). The prep PR from [step 8](#8-prepare-the-next-release) sets master to the next patch version, so change master to `$VERSION`:

1. In [`version/constants.go`](version/constants.go), set `Current` to `$VERSION`. Bump the compatibility floor in the same file: `MinimumCompatibleVersion` to this release's minor version and `PrevMinimumCompatibleVersion` to the one before, both with `Patch: 0`:

   ```go
   Current = &Application{
       Name:  Client,
       Major: 1,
       Minor: 16,
       Patch: 0,
   }
   MinimumCompatibleVersion = &Application{
       Name:  Client,
       Major: 1,
       Minor: 16,
       Patch: 0,
   }
   PrevMinimumCompatibleVersion = &Application{
       Name:  Client,
       Major: 1,
       Minor: 15,
       Patch: 0,
   }
   ```

1. Set [`version/current.txt`](version/current.txt) to `$VERSION`.

1. Update the submodule require directives:

   ```bash
   ./scripts/run_task.sh tags-update-require-directives -- "$VERSION"
   ```

1. In [`version/compatibility.json`](version/compatibility.json), replace the patch version from the prep PR with `$VERSION`.

1. In [`RELEASES.md`](RELEASES.md), change the version in the first section's heading and link to `$VERSION`.

#### Activating a Network Upgrade on Mainnet

If this release activates a new network upgrade on Mainnet:

1. In [`upgrade/upgrade.go`](upgrade/upgrade.go), set the upgrade's time in `Default` — the local-network schedule — to `InitiallyActiveTime`:

   ```go
   Default = Config{
       // ...
       HeliconTime: InitiallyActiveTime,
   }
   ```

   Then update any tests that pin the local network's upgrade schedule or genesis (e.g. its genesis hash).

1. In [`scripts/tests.upgrade.sh`](scripts/tests.upgrade.sh), set `DEFAULT_VERSION` to `$VERSION` without the leading `v`, naming the upgrade in the comment above it:

   ```bash
   # v1.15.1 is the earliest version that activates Helicon on local networks.
   DEFAULT_VERSION="1.15.1"
   ```

1. In [`.github/workflows/bazel.yml`](.github/workflows/bazel.yml), remove the `upgrade` entry from the `test_matrix` of the `monitored` job. The matrix is JSON, so remove the comma after the new last entry, and put the `TODO` comment above `test_matrix:`:

   ```yaml
   # TODO: Restore the upgrade entry once v1.15.1 is published:
   # {"name": "upgrade", "task": "bazel-test-upgrade", "network_owner": "avalanchego-upgrade"}
   test_matrix: |
     {"include": [
       ...
       {"name": "load", "task": "bazel-test-load-ci"}
     ]}
   ```

The upgrade test runs the published `DEFAULT_VERSION` binary, which doesn't exist until `$VERSION` is released. [Step 8](#8-prepare-the-next-release) turns it back on.

### 3. Create Release Candidate Tags

Tag a commit on master:

```bash
git fetch origin master
git checkout --detach origin/master  # or any other commit on master
# Double check this is the expected commit
git log -1
./scripts/run_task.sh tags-create -- "$VERSION_RC"
./scripts/run_task.sh tags-push -- "$VERSION_RC"
```

The `require` directives at this commit reference `$VERSION`, which is not tagged yet. So `go get github.com/ava-labs/avalanchego@$VERSION_RC` does not resolve outside the repository.

### 4. Test the Release Candidate

Deploy `$VERSION_RC` to Ava Labs-operated nodes by setting their image tags to `$VERSION_RC` in the infrastructure deployment repository. Update the API and validator image tags for:

- **Primary Network canaries** on both Fuji and Mainnet.
- **Echo and Dispatch**, which are Fuji chains that run the public `avaplatform/subnet-evm` image. Echo is an L1 and Dispatch is a Subnet, so together they cover both validator models.

After the deployments roll out, watch the Grafana logs and dashboards for each chain. Look for crashes, stalled block production, and new errors or warnings compared to the previous version.

#### Fixing Issues Found in the Release Candidate

If testing finds a bug, merge the fix to master. Then set `VERSION_RC` to the next release candidate (e.g. `v1.15.1-rc.1`) and go back to [step 3](#3-create-release-candidate-tags).

### 5. Create Final Release Tags

Tag the same commit as the release candidate that passed testing:

```bash
git fetch origin --tags
git checkout --detach "$VERSION_RC"
./scripts/run_task.sh tags-create -- "$VERSION"
./scripts/run_task.sh tags-push -- "$VERSION"
```

Optionally verify from a fresh directory (to avoid local replace directives):

```bash
cd $(mktemp -d)
go mod init test
go get github.com/ava-labs/avalanchego@"$VERSION"
go list -m all | grep avalanchego
```

All submodules should resolve to matching versions.

### 6. Automated Builds

The tag push from [step 5](#5-create-final-release-tags) triggers these workflows automatically. Wait for them to finish before creating the GitHub release:

- `build-linux-binaries.yml` - Linux amd64/arm64 tarballs
- `build-macos-release.yml` - macOS zip
- `build-linux-packages.yml` - Linux RPM/DEB packages (`.deb`s are also published to S3)
- `publish_docker_image.yml` - Docker images

Artifacts produced:

**Binaries:**

- `avalanchego-linux-amd64-$VERSION.tar.gz`
- `avalanchego-linux-arm64-$VERSION.tar.gz`
- `avalanchego-macos-$VERSION.zip`
- `subnet-evm-linux-amd64-$VERSION.tar.gz`
- `subnet-evm-linux-arm64-$VERSION.tar.gz`
- `subnet-evm-macos-$VERSION.zip`

**Docker Images** (linux/amd64 and linux/arm64):

- `avaplatform/avalanchego:$VERSION`
- `avaplatform/subnet-evm:$VERSION`
- `avaplatform/bootstrap-monitor:$VERSION`

### 7. Create GitHub Release

Create a release at [github.com/ava-labs/avalanchego/releases/new](https://github.com/ava-labs/avalanchego/releases/new):

1. Select tag `$VERSION`
1. Set title to `$VERSION`
1. For the release notes, copy the `$VERSION` section of [`RELEASES.md`](RELEASES.md) without its heading or any empty sections, and end it with the full changelog link:

    ```markdown
    This release schedules the activation of the Helicon network upgrade...

    ### Features
    ...

    ### APIs
    ...

    ### Configs
    ...

    ### Fixes
    ...

    **Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.14.2...v1.15.0
    ```

1. Attach the **Binaries** listed in [step 6](#6-automated-builds), downloaded from the artifacts of the tag's `build-linux-binaries.yml` and `build-macos-release.yml` runs
1. Check "Set as the latest release"
1. Publish

### 8. Prepare the Next Release

Update master so that it describes the next release:

```bash
export NEXT_VERSION=v1.15.2
```

1. Create branch:

   ```bash
   git fetch origin master
   git checkout -b "prep-$NEXT_VERSION-release" origin/master
   ```

1. Update `Current` in [`version/constants.go`](version/constants.go):

   ```go
   Current = &Application{
       Name:  Client,
       Major: 1,
       Minor: 15,
       Patch: 2,
   }
   ```

1. Set [`version/current.txt`](version/current.txt) to `$NEXT_VERSION`.

1. Update the submodule require directives:

   ```bash
   ./scripts/run_task.sh tags-update-require-directives -- "$NEXT_VERSION"
   ```

1. In [`version/compatibility.json`](version/compatibility.json), add `$NEXT_VERSION` to the list for the current `RPCChainVMProtocol`.

1. At the top of [`RELEASES.md`](RELEASES.md), add a section for the next release:

   ```markdown
   ## [v1.15.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.15.2)

   ### Features

   ### APIs

   ### Configs

   ### Fixes
   ```

1. PRs merged after the release candidate commit may have added notes to the `$VERSION` section of [`RELEASES.md`](RELEASES.md). Check with this diff, and move any you find to `$NEXT_VERSION`:

   ```bash
   git diff "$VERSION" origin/master -- RELEASES.md
   ```

1. If you removed the `upgrade` entry in [step 2](#activating-a-network-upgrade-on-mainnet), restore it to the `monitored` job's `test_matrix` in [`.github/workflows/bazel.yml`](.github/workflows/bazel.yml). Add a comma after the preceding entry and delete the `TODO` comment.

1. Create PR and merge:

   ```bash
   git add .
   git commit -S -m "chore: prep next release $NEXT_VERSION"
   git push -u origin "prep-$NEXT_VERSION-release"
   gh pr create --repo github.com/ava-labs/avalanchego --base master --title "chore: prep next release $NEXT_VERSION"
   gh pr checks --watch
   gh pr merge "prep-$NEXT_VERSION-release" --squash --subject "chore: prep next release $NEXT_VERSION"
   ```

1. Pat yourself on the back for a job well done

## RPC Chain VM Protocol Version

When the protocol version changes:

1. Update [`version/constants.go`](version/constants.go):

   ```go
   RPCChainVMProtocol uint = 47
   ```

2. Update [`version/compatibility.json`](version/compatibility.json):

   ```json
   "47": ["v1.15.2"]
   ```

   The version listed is the one master is preparing (`version.Current`).

To verify compatibility:

```bash
go test -run ^TestCurrentRPCChainVMCompatible$ github.com/ava-labs/avalanchego/version
```

## Development Tags

To share work-in-progress without merging to master:

1. On your branch, run `./scripts/run_task.sh tags-update-require-directives -- v0.0.0-mybranch`
2. Commit and push to your branch (tags must reference a commit reachable on the remote)
3. Run `./scripts/run_task.sh tags-create -- --no-sign v0.0.0-mybranch`
4. Run `./scripts/run_task.sh tags-push -- v0.0.0-mybranch`

External consumers can then `go get github.com/ava-labs/avalanchego@v0.0.0-mybranch`.

Do not merge these go.mod changes. [`check-require-directives`](#check-require-directives) accepts `v0.0.0-` versions so branch CI passes, but master must reference `version.Current`.

## Tagging Task Reference

### `tags-update-require-directives`

Updates `require` directives in all go.mod files to reference the specified version. Version must match `vX.Y.Z` or `vX.Y.Z-suffix`.

### `tags-create`

Creates signed tags for the main module and all submodules at the current commit. Pass `--no-sign` for unsigned tags (e.g., development tags).

### `tags-push`

Pushes tags for the main module and all submodules, then verifies all tags exist on the remote. Set `GIT_REMOTE` to override the default remote (`origin`).

### `tags-verify-remote`

Verifies that tags for the main module and all submodules exist on the remote. Automatically run at the end of `tags-push`, but can be run standalone to re-check.

### `check-require-directives`

Verifies that all internal module `require` directives reference the same version, and that it matches `version.Current` or is a [development tag](#development-tags). Runs in CI.

## Troubleshooting

### Partial Tag Creation

If `tags-create` fails after creating some tags (e.g. due to GPG signing error), the remaining tags won't exist. The script checks for existing tags before creating any, so re-running it will fail with details on which tags already exist.

To recover, delete the partially created tags and re-run:

```bash
# The error output lists existing tags. Delete them:
git tag -d v1.15.1 graft/evm/v1.15.1
# Then re-run:
./scripts/run_task.sh tags-create -- "$VERSION"
```

### Tag Push Failure

If `tags-push` fails partway through (e.g., network error), some tags may have been pushed while others haven't.

To recover, re-run the push — git push is idempotent for tags that already exist at the correct commit:

```bash
./scripts/run_task.sh tags-push -- "$VERSION"
```

If a tag was pushed pointing to the wrong commit, delete the remote tag and re-push:

```bash
git push origin :refs/tags/graft/evm/v1.15.1
./scripts/run_task.sh tags-push -- "$VERSION"
```

### Require Directive Update Failure

If `tags-update-require-directives` fails partway through, some go.mod files may have been updated while others haven't. The consistency check will catch this:

```bash
./scripts/run_task.sh check-require-directives
```

To recover, re-run the update — it's idempotent:

```bash
./scripts/run_task.sh tags-update-require-directives -- <version>
```
