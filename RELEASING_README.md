# AvalancheGo Release Guide

This document covers the complete release process for AvalancheGo and its integrated components (Coreth and Subnet-EVM).

## Overview

AvalancheGo is a monorepo which contains:

- **AvalancheGo** - The main Avalanche node implementation
- **Coreth** (in [graft/coreth/](graft/coreth/)) - C-Chain EVM implementation, compiled into AvalancheGo
- **Subnet-EVM** (in [graft/subnet-evm/](graft/subnet-evm/)) - Subnet-EVM plugin, released as a separate binary

For the rationale behind the multi-module tagging process, see [Multi-Module Release Strategy](docs/design/multi-module-release.md).

### Versioning Strategy

All components follow aligned versioning:

- Same version number - When AvalancheGo releases v1.14.0, Subnet-EVM is also v1.14.0
- Coordinated tags - Each release creates tags for the main module and all submodules (e.g., `v1.14.0`, `graft/evm/v1.14.0`, `graft/coreth/v1.14.0`, `graft/subnet-evm/v1.14.0`)

### Component Release Notes

| Component | Release Artifact | Notes |
| --------- | ---------------- | ----- |
| AvalancheGo | `avalanchego` binary | Main node binary |
| Coreth | None (compiled into AvalancheGo) | No separate release |
| Subnet-EVM | `subnet-evm` binary | Separate plugin binary for L1s |

## Release Procedure

### 1. Preparation

You should always create a release candidate first, and only if everything is fine, can you create a release. In this section we create a release candidate `v1.14.1-rc.0`. We therefore assign these environment variables to simplify copying instructions:

```bash
export VERSION_RC=v1.14.1-rc.0
export VERSION=v1.14.1
```

### 2. Create Release Branch

```bash
git fetch origin master
git checkout master
git checkout -b "releases/$VERSION_RC"
```

### 3. Prepare Release Changes

These changes prepare the merge commit that will be tagged.

1. Update [`version/constants.go`](version/constants.go):

   ```go
   Current = &Application{
       Name:  Client,
       Major: 1,
       Minor: 14,
       Patch: 1,
   }
   ```

1. Update [`RELEASES.md`](RELEASES.md) - rename "Pending" section to the new version and create a new "Pending" section.

1. If RPC chain VM protocol version changed, update [`version/constants.go`](version/constants.go):

   ```go
   RPCChainVMProtocol uint = 45
   ```

   And update [`version/compatibility.json`](version/compatibility.json) to add the new version.

**Note:** Coreth and Subnet-EVM versions are automatically derived from `version/constants.go` and do not require manual updates.

1. Update submodule require directives to reference the future tag:

   ```bash
   ./scripts/run_task.sh tags-update-require-directives -- "$VERSION_RC"
   ```

### 4. Commit and Create PR

```bash
git add .
git commit -S -m "chore: release $VERSION_RC"
git push -u origin "releases/$VERSION_RC"
```

Create PR:

```bash
gh pr create --repo github.com/ava-labs/avalanchego --base master --title "chore: release $VERSION_RC"
```

Wait for checks:

```bash
gh pr checks --watch
```

Merge:

```bash
gh pr merge "releases/$VERSION_RC" --squash --subject "chore: release $VERSION_RC"
```

### 5. Create Release Candidate Tags

Tag the merge commit from step 4:

```bash
git fetch origin master
git checkout master
# Double check the tip of the master branch is the expected commit
# of the squashed release branch
git log -1
./scripts/run_task.sh tags-create -- "$VERSION_RC"
./scripts/run_task.sh tags-push -- "$VERSION_RC"
```

Optionally verify from a fresh directory (to avoid local replace directives):

```bash
cd $(mktemp -d)
go mod init test
go get github.com/ava-labs/avalanchego@"$VERSION_RC"
go list -m all | grep avalanchego
```

All submodules should resolve to matching versions.

### 6. Test the Release Candidate

#### Local Deployment on Fuji

If your machine is too low on resources, you can run an [AWS EC2 instance](https://github.com/ava-labs/eng-resources/blob/main/dev-node-setup.md).

##### Find L1 Info

Get Dispatch and Echo L1 details:

- [Dispatch L1 details](https://subnets-test.avax.network/dispatch/details) - Subnet ID: `7WtoAMPhrmh5KosDUsFL9yTcvw7YSxiKHPpdfs4JsgW47oZT5`
- [Echo L1 details](https://subnets-test.avax.network/echo/details) - Subnet ID: `i9gFpZQHPLcGfZaQLiwFAStddQD7iTKBpFfurPFJsXm1CkTZK`

Get blockchain and VM IDs:

```bash
# Dispatch
curl -X POST --silent -H 'content-type:application/json' --data '{
    "jsonrpc": "2.0",
    "method": "platform.getBlockchains",
    "params": {},
    "id": 1
}'  https://api.avax-test.network/ext/bc/P | \
jq -r '.result.blockchains[] | select(.subnetID=="7WtoAMPhrmh5KosDUsFL9yTcvw7YSxiKHPpdfs4JsgW47oZT5") |  "\(.name)\nBlockchain id: \(.id)\nVM id: \(.vmID)\n"'

# Echo
curl -X POST --silent -H 'content-type:application/json' --data '{
    "jsonrpc": "2.0",
    "method": "platform.getBlockchains",
    "params": {},
    "id": 1
}'  https://api.avax-test.network/ext/bc/P | \
jq -r '.result.blockchains[] | select(.subnetID=="i9gFpZQHPLcGfZaQLiwFAStddQD7iTKBpFfurPFJsXm1CkTZK") |  "\(.name)\nBlockchain id: \(.id)\nVM id: \(.vmID)\n"'
```

As of this writing:

- **Dispatch**: Blockchain `2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY`, VM `mDtV8ES8wRL1j2m6Kvc1qRFAvnpq4kufhueAY1bwbzVhk336o`
- **Echo**: Blockchain `98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp`, VM `meq3bv7qCMZZ69L8xZRLwyKnWp6chRwyscq8VPtHWignRQVVF`

##### Build and Deploy

1. Build Subnet-EVM:

   ```bash
   cd graft/subnet-evm
   ./scripts/build.sh vm.bin
   ```

1. Install the VM plugin:

   ```bash
   mkdir -p ~/.avalanchego/plugins
   cp vm.bin ~/.avalanchego/plugins/mDtV8ES8wRL1j2m6Kvc1qRFAvnpq4kufhueAY1bwbzVhk336o
   cp vm.bin ~/.avalanchego/plugins/meq3bv7qCMZZ69L8xZRLwyKnWp6chRwyscq8VPtHWignRQVVF
   rm vm.bin
   ```

1. Get chain upgrades:

   ```bash
   # Dispatch
   mkdir -p ~/.avalanchego/configs/chains/2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY
   curl -X POST --silent --header 'Content-Type: application/json' --data '{
       "jsonrpc": "2.0",
       "method": "eth_getChainConfig",
       "params": [],
       "id": 1
   }' https://subnets.avax.network/dispatch/testnet/rpc | \
   jq -r '.result.upgrades' > ~/.avalanchego/configs/chains/2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY/upgrade.json

   # Echo
   mkdir -p ~/.avalanchego/configs/chains/98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp
   curl -X POST --silent --header 'Content-Type: application/json' --data '{
       "jsonrpc": "2.0",
       "method": "eth_getChainConfig",
       "params": [],
       "id": 1
   }' https://subnets.avax.network/echo/testnet/rpc | \
   jq -r '.result.upgrades' > ~/.avalanchego/configs/chains/98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp/upgrade.json
   ```

1. Build and run AvalancheGo:

   ```bash
   cd ../..
   ./scripts/build.sh
   ./build/avalanchego --network-id=fuji --partial-sync-primary-network --public-ip=127.0.0.1 \
     --track-subnets=7WtoAMPhrmh5KosDUsFL9yTcvw7YSxiKHPpdfs4JsgW47oZT5,i9gFpZQHPLcGfZaQLiwFAStddQD7iTKBpFfurPFJsXm1CkTZK
   ```

1. Wait for bootstrap (look for `check started passing`, `consensus started`, `bootstrapped healthy nodes`).

1. Verify block production:

   ```bash
   # Dispatch
   curl -X POST --silent --header 'Content-Type: application/json' --data '{
       "jsonrpc": "2.0",
       "method": "eth_blockNumber",
       "params": [],
       "id": 1
   }' localhost:9650/ext/bc/2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY/rpc

   # Echo
   curl -X POST --silent --header 'Content-Type: application/json' --data '{
       "jsonrpc": "2.0",
       "method": "eth_blockNumber",
       "params": [],
       "id": 1
   }' localhost:9650/ext/bc/98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp/rpc
   ```

#### Canary Deployment

1. Clone [external-plugins-builder](https://github.com/ava-labs/external-plugins-builder):

   ```bash
   git checkout main
   git pull
   git checkout -b "echo-dispatch-$VERSION_RC"
   ```

1. Update `configs/dispatch.yml` and `configs/echo.yml`:
   - Set `app_version` to `$VERSION_RC`
   - Update `avalanchego_version` if needed
   - Update `golang_version` if needed

1. Create PR and merge:

   ```bash
   git add .
   git commit -m "Bump echo and dispatch to $VERSION_RC"
   git push -u origin "echo-dispatch-$VERSION_RC"
   gh pr create --repo github.com/ava-labs/external-plugins-builder --base main --title "Bump echo and dispatch to $VERSION_RC"
   ```

1. Monitor deployments after merge:
   - **Dispatch**: [Logs](https://app.datadoghq.com/logs?query=subnet%3Adispatch%20%40logger%3A%2A&live=true) | [Metrics](https://app.datadoghq.com/dashboard/jrv-mm2-vuc/dispatch-testnet-subnets?live=true)
   - **Echo**: [Logs](https://app.datadoghq.com/logs?query=subnet:echo%20@logger:*&live=true) | [Metrics](https://app.datadoghq.com/dashboard/jrv-mm2-vuc/echo-testnet-subnets?live=true)

1. Test transactions:
   1. If you have no wallet setup, create a new one using the [Core wallet](https://core.app/)
   1. Go to the settings and enable **Testnet Mode**
   1. You need DIS (Dispatch) and ECH (Echo) testnet tokens. If you don't have one or the other, send your C-chain AVAX address to one of the team members who can send you some DIS/ECH testnet tokens. The portfolio section of the core wallet should then show the DIS and ECH tokens available.
   1. For both Dispatch and Echo, in the "Command center", select **Send**, enter your own C-Chain AVAX address in the **Send To** field, set the **Amount** to 1 and click on **Send**. Finally, select a maximum network fee, usually *Slow* works, and click on **Approve**.

1. You should then see the transaction impact the logs and metrics, for example:

   ```log
   Apr 03 10:35:00.000 i-0158b0eef8b774d39 subnets Commit new mining work
   Apr 03 10:34:59.599 i-0158b0eef8b774d39 subnets Resetting chain preference
   Apr 03 10:34:56.085 i-0aca0a4088f607b7e subnets Served eth_getBlockByNumber
   Apr 03 10:34:55.619 i-0ccd28afbac6d9bfc subnets built block
   Apr 03 10:34:55.611 i-0ccd28afbac6d9bfc subnets Commit new mining work
   Apr 03 10:34:55.510 gke-subnets-testnet subnets Submitted transaction
   ```

### 7. Create Final Release Tags

After successful testing, update the require directives from the RC version
to the final version, merge, then tag the resulting commit:

```bash
git fetch origin master
git checkout -b "tags/$VERSION" origin/master
./scripts/run_task.sh tags-update-require-directives -- "$VERSION"
git add .
git commit -S -m "chore: set require directives for $VERSION"
git push -u origin "tags/$VERSION"
gh pr create --repo github.com/ava-labs/avalanchego --base master --title "chore: set require directives for $VERSION"
gh pr checks --watch
gh pr merge "tags/$VERSION" --squash --subject "chore: set require directives for $VERSION"
```

Tag the merge commit:

```bash
git checkout master
git pull origin
git log -1  # Verify expected commit
./scripts/run_task.sh tags-create -- "$VERSION"
./scripts/run_task.sh tags-push -- "$VERSION"
```

### 8. Create GitHub Release

Create a release at [github.com/ava-labs/avalanchego/releases/new](https://github.com/ava-labs/avalanchego/releases/new):

1. Select tag `$VERSION`
1. Set title to `$VERSION`
1. Write release notes including:
    - Network upgrade information (if applicable)
    - Plugin version changes
    - Breaking changes
    - Features
    - Fixes

    Example:

    ```markdown
    This release schedules the activation of...


    The plugin version is updated to `45`; all plugins must update to be compatible.

    ### Breaking Changes

    ### Features

    ### Fixes

    **Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.14.0...v1.14.1
    ```

1. Check "Set as the latest release"
1. Publish

### 9. Automated Builds

The tag push triggers these workflows automatically:

- `build-linux-binaries.yml` - Linux amd64/arm64 tarballs
- `build-macos-release.yml` - macOS zip
- `build-ubuntu-amd64-release.yml` / `build-ubuntu-arm64-release.yml` - Debian packages
- `publish_docker_image.yml` - Docker images

Artifacts produced:

**Binaries:**

- `avalanchego-linux-amd64-$VERSION.tar.gz`
- `avalanchego-linux-arm64-$VERSION.tar.gz`
- `avalanchego-macos-$VERSION.zip`
- `subnet-evm-linux-amd64-$VERSION.tar.gz`
- `subnet-evm-linux-arm64-$VERSION.tar.gz`
- `subnet-evm-macos-$VERSION.zip`

**Docker Images:**

- `avaplatform/avalanchego:$VERSION` (multi-arch: linux/amd64, linux/arm64)
- `avaplatform/subnet-evm:$VERSION` (multi-arch: linux/amd64, linux/arm64)
- `avaplatform/bootstrap-monitor:$VERSION` (multi-arch: linux/amd64, linux/arm64)

**Antithesis Images:**

Antithesis test images are built and pushed to Google Artifact Registry on every merge to master via `publish_antithesis_images.yml`:

- `antithesis-avalanchego-{config,node,workload}:latest`
- `antithesis-xsvm-{config,node,workload}:latest`
- `antithesis-subnet-evm-{config,node,workload}:latest`

These are triggered daily for testing:

- `trigger-antithesis-avalanchego.yml` - 10PM UTC
- `trigger-antithesis-xsvm.yml` - 6AM UTC
- `trigger-antithesis-subnet-evm.yml` - 2PM UTC

### 10. Post-Release Version Bump

Prepare for the next release:

```bash
export NEXT_VERSION=v1.14.2
```

1. Create branch:

   ```bash
   git fetch origin master
   git checkout master
   git checkout -b "prep-$NEXT_VERSION-release"
   ```

1. Update all version files (as in step 3) to the next version.

1. Create PR and merge:

   ```bash
   git add .
   git commit -S -m "chore: prep release $NEXT_VERSION"
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
   RPCChainVMProtocol uint = 45
   ```

2. Update [`version/compatibility.json`](version/compatibility.json):

   ```json
   "45": ["v1.14.1"]
   ```

To verify compatibility:

```bash
go test -run ^TestCompatibility$ github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm
```

## Development Tags

To share work-in-progress without merging to master:

1. On your branch, run `./scripts/run_task.sh tags-update-require-directives -- v0.0.0-mybranch`
2. Commit and push to your branch (tags must reference a commit reachable on the remote)
3. Run `./scripts/run_task.sh tags-create -- --no-sign v0.0.0-mybranch`
4. Run `./scripts/run_task.sh tags-push -- v0.0.0-mybranch`

External consumers can then `go get github.com/ava-labs/avalanchego@v0.0.0-mybranch`.

## Tagging Task Reference

### `tags-update-require-directives`

Updates `require` directives in all go.mod files to reference the
specified version. Version must match `vX.Y.Z` or `vX.Y.Z-suffix`.

### `tags-create`

Creates signed tags for the main module and all submodules at the current
commit. Pass `--no-sign` for unsigned tags (e.g., development tags).

### `tags-push`

Pushes tags for the main module and all submodules. Set `GIT_REMOTE` to
override the default remote (`origin`).

### `check-require-directives`

Verifies that all internal module `require` directives across go.mod files
reference the same version.
