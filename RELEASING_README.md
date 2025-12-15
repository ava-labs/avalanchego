# AvalancheGo Release Guide

This document covers the complete release process for AvalancheGo and its integrated components (Coreth and Subnet-EVM).

## Overview

AvalancheGo is a monorepo containing:

- **AvalancheGo** - The main Avalanche node implementation
- **Coreth** (in [graft/coreth/](graft/coreth/)) - C-Chain EVM implementation, compiled into AvalancheGo
- **Subnet-EVM** (in [graft/subnet-evm/](graft/subnet-evm/)) - Subnet-EVM plugin, released as a separate binary

### Versioning Strategy

All components follow aligned versioning:

- Same version number - When AvalancheGo releases v1.14.0, Subnet-EVM is also v1.14.0
- Single tag - One git tag (e.g., `v1.14.0`) releases everything together

### Component Release Notes

| Component | Release Artifact | Notes |
|-----------|-----------------|-------|
| AvalancheGo | `avalanchego` binary | Main node binary |
| Coreth | None (compiled into AvalancheGo) | No separate release; version in `version.go` is informational only |
| Subnet-EVM | `subnet-evm` binary | Separate plugin binary for L1s |

## Release Procedure

### 1. Preparation

Set environment variables for the release:

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

### 3. Update Version Files

#### AvalancheGo

1. Update [`version/constants.go`](version/constants.go):

   ```go
   Current = &Application{
       Name:  Client,
       Major: 1,
       Minor: 14,
       Patch: 1,
   }
   ```

2. Update [`RELEASES.md`](RELEASES.md) - rename "Pending" section to the new version and create a new "Pending" section.

3. If RPC chain VM protocol version changed, update [`version/constants.go`](version/constants.go):

   ```go
   RPCChainVMProtocol uint = 45
   ```

   And update [`version/compatibility.json`](version/compatibility.json) to add the new version.

#### Coreth

Coreth is compiled directly into AvalancheGo - there is no separate release artifact. The version string is informational only (it is used in logs and debugging).

1. Update [`graft/coreth/plugin/evm/version.go`](graft/coreth/plugin/evm/version.go) (optional, for tracking purposes):

   ```go
   Version string = "v0.15.1"
   ```

2. Update [`graft/coreth/RELEASES.md`](graft/coreth/RELEASES.md) - rename "Pending" section.

#### Subnet-EVM

1. Update [`graft/subnet-evm/plugin/evm/version.go`](graft/subnet-evm/plugin/evm/version.go):

   ```go
   Version string = "v1.14.1"  // align with AvalancheGo
   ```

2. Update [`graft/subnet-evm/compatibility.json`](graft/subnet-evm/compatibility.json):

   ```json
   "v1.14.1": 45,
   ```

3. Update [`graft/subnet-evm/RELEASES.md`](graft/subnet-evm/RELEASES.md).

4. Update [`graft/subnet-evm/README.md`](graft/subnet-evm/README.md) compatibility section.

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

### 5. Create Release Candidate Tag

```bash
git fetch origin master
git checkout master
git log -1 
git tag -s "$VERSION_RC"
git push origin "$VERSION_RC"
```

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

2. Install the VM plugin:

   ```bash
   mkdir -p ~/.avalanchego/plugins
   cp vm.bin ~/.avalanchego/plugins/mDtV8ES8wRL1j2m6Kvc1qRFAvnpq4kufhueAY1bwbzVhk336o
   cp vm.bin ~/.avalanchego/plugins/meq3bv7qCMZZ69L8xZRLwyKnWp6chRwyscq8VPtHWignRQVVF
   rm vm.bin
   ```

3. Get chain upgrades:

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

4. Build and run AvalancheGo:

   ```bash
   cd ../..  
   ./scripts/build.sh
   ./build/avalanchego --network-id=fuji --partial-sync-primary-network --public-ip=127.0.0.1 \
     --track-subnets=7WtoAMPhrmh5KosDUsFL9yTcvw7YSxiKHPpdfs4JsgW47oZT5,i9gFpZQHPLcGfZaQLiwFAStddQD7iTKBpFfurPFJsXm1CkTZK
   ```

5. Wait for bootstrap (look for `check started passing`, `consensus started`, `bootstrapped healthy nodes`).

6. Verify block production:

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

2. Update `configs/dispatch.yml` and `configs/echo.yml`:
   - Set `app_version` to `$VERSION_RC`
   - Update `avalanchego_version` if needed
   - Update `golang_version` if needed

3. Create PR and merge:

   ```bash
   git add .
   git commit -m "Bump echo and dispatch to $VERSION_RC"
   git push -u origin "echo-dispatch-$VERSION_RC"
   gh pr create --repo github.com/ava-labs/external-plugins-builder --base main --title "Bump echo and dispatch to $VERSION_RC"
   ```

4. Monitor deployments after merge:
   - **Dispatch**: [Logs](https://app.datadoghq.com/logs?query=subnet%3Adispatch%20%40logger%3A%2A&live=true) | [Metrics](https://app.datadoghq.com/dashboard/jrv-mm2-vuc/dispatch-testnet-subnets?live=true)
   - **Echo**: [Logs](https://app.datadoghq.com/logs?query=subnet:echo%20@logger:*&live=true) | [Metrics](https://app.datadoghq.com/dashboard/jrv-mm2-vuc/echo-testnet-subnets?live=true)

5. Test transactions:
   1. If you have no wallet setup, create a new one using the [Core wallet](https://core.app/)
   2. Go to the settings and enable **Testnet Mode**
   3. You need DIS (Dispatch) and ECH (Echo) testnet tokens. If you don't have one or the other, send your C-chain AVAX address to one of the team members who can send you some DIS/ECH testnet tokens. The portfolio section of the core wallet should then show the DIS and ECH tokens available.
   4. For both Dispatch and Echo, in the "Command center", select **Send**, enter your own C-Chain AVAX address in the **Send To** field, set the **Amount** to 1 and click on **Send**. Finally, select a maximum network fee, usually *Slow* works, and click on **Approve**.

6. You should then see the transaction impact the logs and metrics, for example:

   ```log
   Apr 03 10:35:00.000 i-0158b0eef8b774d39 subnets Commit new mining work
   Apr 03 10:34:59.599 i-0158b0eef8b774d39 subnets Resetting chain preference
   Apr 03 10:34:56.085 i-0aca0a4088f607b7e subnets Served eth_getBlockByNumber
   Apr 03 10:34:55.619 i-0ccd28afbac6d9bfc subnets built block
   Apr 03 10:34:55.611 i-0ccd28afbac6d9bfc subnets Commit new mining work
   Apr 03 10:34:55.510 gke-subnets-testnet subnets Submitted transaction
   ```

### 7. Create Final Release

After successful testing:

```bash
git checkout master
git pull origin
git log -1  # Verify expected commit
git tag -s "$VERSION"
git push origin "$VERSION"
```

### 8. Create GitHub Release

Create a release at [github.com/ava-labs/avalanchego/releases/new](https://github.com/ava-labs/avalanchego/releases/new):

1. Select tag `$VERSION`
2. Set title to `$VERSION`
3. Write release notes including:
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

4. Check "Set as the latest release"
5. Publish

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

2. Update all version files (as in step 3) to the next version.

3. Create PR and merge:

   ```bash
   git add .
   git commit -S -m "chore: prep release $NEXT_VERSION"
   git push -u origin "prep-$NEXT_VERSION-release"
   gh pr create --repo github.com/ava-labs/avalanchego --base master --title "chore: prep next release $NEXT_VERSION"
   gh pr checks --watch
   gh pr merge "prep-$NEXT_VERSION-release" --squash --subject "chore: prep next release $NEXT_VERSION"
   ```

## Version Files Reference

| Component | Version File | Other Files | Notes |
|-----------|-------------|-------------|-------|
| AvalancheGo | [`version/constants.go`](version/constants.go) | [`RELEASES.md`](RELEASES.md), [`version/compatibility.json`](version/compatibility.json) | Primary version |
| Coreth | [`graft/coreth/plugin/evm/version.go`](graft/coreth/plugin/evm/version.go) | [`graft/coreth/RELEASES.md`](graft/coreth/RELEASES.md) | Informational only (no separate release) |
| Subnet-EVM | [`graft/subnet-evm/plugin/evm/version.go`](graft/subnet-evm/plugin/evm/version.go) | [`graft/subnet-evm/RELEASES.md`](graft/subnet-evm/RELEASES.md), [`graft/subnet-evm/compatibility.json`](graft/subnet-evm/compatibility.json) | Aligned with AvalancheGo |

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

3. Update [`graft/subnet-evm/compatibility.json`](graft/subnet-evm/compatibility.json):

   ```json
   "v1.14.1": 45,
   ```

To verify compatibility:

```bash
go test -run ^TestCompatibility$ github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm
```

## Historical Notes

- Prior to v1.14.0, Subnet-EVM had independent versioning (v0.8.x and earlier)
- Coreth has its own version string (v0.x.x) but is compiled into AvalancheGo with no separate release artifact
