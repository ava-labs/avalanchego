# Releasing

## When to release

- When [AvalancheGo](https://github.com/ava-labs/avalanchego/releases) increases its RPC chain VM protocol version, which you can also check in [its `version/compatibility.json`](https://github.com/ava-labs/avalanchego/blob/master/version/compatibility.json)
- When Subnet-EVM needs to release a new feature or bug fix.

## Procedure

### Release candidate

ℹ️ you should always create a release candidate first, and only if everything is fine, you can create a release.

In this section, we create a release candidate `v0.7.3-rc.0`. We therefore assign these environment variables to simplify copying instructions:

```bash
export VERSION_RC=v0.7.3-rc.0
export VERSION=v0.7.3
```

Remember to use the appropriate versioning for your release. 

1. Create your branch, usually from the tip of the `master` branch:

    ```bash
    git fetch origin master
    git checkout master
    git checkout -b "releases/$VERSION_RC"
    ```

2. Update the [RELEASES.md](../../RELEASES.md) file with the new release version `$VERSION`.
3. Modify the [plugin/evm/version.go](../../plugin/evm/version.go) `Version` global string variable and set it to the desired `$VERSION`.
4. Ensure the AvalancheGo version used in [go.mod](../../go.mod) is [its last release](https://github.com/ava-labs/avalanchego/releases). If not, upgrade it with, for example:
    ```bash
      go get github.com/ava-labs/avalanchego@v1.13.0
      go mod tidy
    ```
    And fix any errors that may arise from the upgrade. If it requires significant changes, you may want to create a separate PR for the upgrade and wait for it to be merged before continuing with this procedure.

5. Add an entry in the object in [compatibility.json](../../compatibility.json), adding the target release `$VERSION` as key and the AvalancheGo RPC chain VM protocol version as value, to the `"rpcChainVMProtocolVersion"` JSON object. For example, we would add:

    ```json
    "v0.7.3": 39,
    ```

    💁 If you are unsure about the RPC chain VM protocol version, set the version to `0`, for example `"v0.7.3": 0`, and then run:

    ```bash
    go test -run ^TestCompatibility$ github.com/ava-labs/subnet-evm/plugin/evm
    ```

    This will fail with an error similar to:

    ```text
    compatibility.json has subnet-evm version v0.7.3 stated as compatible with RPC chain VM protocol version 0 but AvalancheGo protocol version is 39
    ```

    This message can help you figure out what the correct RPC chain VM protocol version (here `39`) has to be in compatibility.json for your current release. Alternatively, you can refer to the [Avalanchego repository `version/compatibility.json` file](https://github.com/ava-labs/avalanchego/blob/main/version/compatibility.json) to find the RPC chain VM protocol version matching the AvalancheGo version we use here.
6. Specify the AvalancheGo compatibility in the [README.md relevant section](../../README.md#avalanchego-compatibility). For example we would add:

    ```text
    ...
    [v0.7.3] AvalancheGo@v1.12.2/1.13.0-fuji/1.13.0 (Protocol Version: 39)
    ```

7. Commit your changes and push the branch

    ```bash
    git add .
    git commit -S -m "chore: release $VERSION_RC"
    git push -u origin "releases/$VERSION_RC"
    ```

8. Create a pull request (PR) from your branch targeting master, for example using [`gh`](https://cli.github.com/):

    ```bash
    gh pr create --repo github.com/ava-labs/subnet-evm --base master --title "chore: release $VERSION_RC"
    ```

9. Wait for the PR checks to pass with

    ```bash
    gh pr checks --watch
    ```

10. Squash and merge your release branch into `master`, for example:

    ```bash
    gh pr merge "releases/$VERSION_RC" --squash --subject "chore: release $VERSION_RC" --body "\n- Update AvalancheGo from v1.1X.X to v1.1X.X"
    ```
    Ensure you properly label the AvalancheGo version.

11. Create and push a tag from the `master` branch:

    ```bash
    git fetch origin master
    git checkout master
    # Double check the tip of the master branch is the expected commit
    # of the squashed release branch
    git log -1
    git tag -s "$VERSION_RC"
    git push origin "$VERSION_RC"
    ```

Once the tag is created, you need to test it on the Fuji testnet both locally and then as canaries, using the Dispatch and Echo subnets.

#### Local deployment

💁 If your machine is too low on resources (memory, disk, CPU, network), or the subnet is quite big to bootstrap (notably *dfk*, *shrapnel* and *gunzilla*), you can run an [AWS EC2 instance](https://github.com/ava-labs/eng-resources/blob/main/dev-node-setup.md) with the following steps.

1. Find the Dispatch and Echo L1s blockchain ID and subnet ID:
    - [Dispatch L1 details](https://subnets-test.avax.network/dispatch/details). Its subnet id is `7WtoAMPhrmh5KosDUsFL9yTcvw7YSxiKHPpdfs4JsgW47oZT5`.
    - [Echo L1 details](https://subnets-test.avax.network/echo/details). Its subnet id is `i9gFpZQHPLcGfZaQLiwFAStddQD7iTKBpFfurPFJsXm1CkTZK`.
2. Get the blockchain ID and VM ID of the Echo and Dispatch L1s with:
    - Dispatch:

        ```bash
        curl -X POST --silent -H 'content-type:application/json' --data '{
            "jsonrpc": "2.0",
            "method": "platform.getBlockchains",
            "params": {},
            "id": 1
        }'  https://api.avax-test.network/ext/bc/P | \
        jq -r '.result.blockchains[] | select(.subnetID=="7WtoAMPhrmh5KosDUsFL9yTcvw7YSxiKHPpdfs4JsgW47oZT5") |  "\(.name)\nBlockchain id: \(.id)\nVM id: \(.vmID)\n"'
        ```

        Which as the time of this writing returns:

        ```text
        dispatch
        Blockchain id: 2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY
        VM id: mDtV8ES8wRL1j2m6Kvc1qRFAvnpq4kufhueAY1bwbzVhk336o
        ```

    - Echo:

        ```bash
        curl -X POST --silent -H 'content-type:application/json' --data '{
            "jsonrpc": "2.0",
            "method": "platform.getBlockchains",
            "params": {},
            "id": 1
        }'  https://api.avax-test.network/ext/bc/P | \
        jq -r '.result.blockchains[] | select(.subnetID=="i9gFpZQHPLcGfZaQLiwFAStddQD7iTKBpFfurPFJsXm1CkTZK") |  "\(.name)\nBlockchain id: \(.id)\nVM id: \(.vmID)\n"'
        ```

        Which as the time of this writing returns:

        ```text
        echo
        Blockchain id: 98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp
        VM id: meq3bv7qCMZZ69L8xZRLwyKnWp6chRwyscq8VPtHWignRQVVF
        ```

3. In the subnet-evm directory, build the VM using

    ```bash
    ./scripts/build.sh vm.bin
    ```

4. Copy the VM binary to the plugins directory, naming it with the VM ID:

    ```bash
    mkdir -p ~/.avalanchego/plugins
    cp vm.bin ~/.avalanchego/plugins/mDtV8ES8wRL1j2m6Kvc1qRFAvnpq4kufhueAY1bwbzVhk336o
    cp vm.bin ~/.avalanchego/plugins/meq3bv7qCMZZ69L8xZRLwyKnWp6chRwyscq8VPtHWignRQVVF
    rm vm.bin
    ```

5. Clone [AvalancheGo](https://github.com/ava-labs/avalanchego):

    ```bash
    git clone git@github.com:ava-labs/avalanchego.git
    ```

6. Checkout correct AvalancheGo version, the version should match the one used in Subnet-EVM `go.mod` file

    ```bash
    cd avalanchego
    git checkout v1.13.0
    ```

7. Get upgrades for each L1 and write them out to `~/.avalanchego/configs/chains/<blockchain-id>/upgrade.json`:

    ```bash
    mkdir -p ~/.avalanchego/configs/chains/2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY
    curl -X POST --silent --header 'Content-Type: application/json' --data '{
        "jsonrpc": "2.0",
        "method": "eth_getChainConfig",
        "params": [],
        "id": 1
    }' https://subnets.avax.network/dispatch/testnet/rpc | \
    jq -r '.result.upgrades' > ~/.avalanchego/configs/chains/2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY/upgrade.json
    ```

    Note it is possible there is no upgrades so the upgrade.json might just be `{}`.

    ```bash
    mkdir -p ~/.avalanchego/configs/chains/98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp
    curl -X POST --silent --header 'Content-Type: application/json' --data '{
        "jsonrpc": "2.0",
        "method": "eth_getChainConfig",
        "params": [],
        "id": 1
    }' https://subnets.avax.network/echo/testnet/rpc | \
    jq -r '.result.upgrades' > ~/.avalanchego/configs/chains/98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp/upgrade.json
    ```

8. (Optional) You can tweak the `config.json` for each L1 if you want to test a particular feature for example.
    - Dispatch: `~/.avalanchego/configs/chains/2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY/config.json`
    - Echo: `~/.avalanchego/configs/chains/98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp/config.json`
9. (Optional) If you want to reboostrap completely the chain, you can remove `~/.avalanchego/chainData/<blockchain-id>/db/pebbledb`, for example:
    - Dispatch: `rm -r ~/.avalanchego/chainData/2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY/db/pebbledb`
    - Echo: `rm -r ~/.avalanchego/chainData/98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp/db/pebbledb`

    AvalancheGo keeps its database in `~/.avalanchego/db/fuji/v1.4.5/*.ldb` which you should not delete.
10. Build AvalancheGo:

    ```bash
    ./scripts/build.sh
    ```

11. Run AvalancheGo tracking the Dispatch and Echo Subnet IDs:

    ```bash
    ./build/avalanchego --network-id=fuji --partial-sync-primary-network --public-ip=127.0.0.1 \
    --track-subnets=7WtoAMPhrmh5KosDUsFL9yTcvw7YSxiKHPpdfs4JsgW47oZT5,i9gFpZQHPLcGfZaQLiwFAStddQD7iTKBpFfurPFJsXm1CkTZK
    ```

12. Follow the logs and wait until you see the following lines:
    - line stating the health `check started passing`
    - line containing `consensus started`
    - line containing `bootstrapped healthy nodes`
13. In another terminal, check you can obtain the current block number for both chains:

    - Dispatch:

        ```bash
        curl -X POST --silent --header 'Content-Type: application/json' --data '{
            "jsonrpc": "2.0",
            "method": "eth_blockNumber",
            "params": [],
            "id": 1
        }' localhost:9650/ext/bc/2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY/rpc
        ```

    - Echo:

        ```bash
        curl -X POST --silent --header 'Content-Type: application/json' --data '{
            "jsonrpc": "2.0",
            "method": "eth_blockNumber",
            "params": [],
            "id": 1
        }' localhost:9650/ext/bc/98qnjenm7MBd8G2cPZoRvZrgJC33JGSAAKghsQ6eojbLCeRNp/rpc
        ```

#### Canary deployment

1. Create a branch from the `main` branch of [the externals plugin builder repository](https://github.com/ava-labs/external-plugins-builder).

    ```bash
    git checkout main
    git pull
    git checkout -b "echo-dispatch-$VERSION_RC"
    ```

2. Modify [`configs/dispatch.yml`] and [`configs/echo.yml`] similarly by:
    - changing the `app_version` to `$VERSION_RC`
    - if necessary, change the `avalanchego_version`
    - if necessary, change the `golang_version`
3. Commit your changes and push the branch

    ```bash
    git add .
    git commit -m "Bump echo and dispatch to $VERSION_RC"
    git push -u origin "echo-dispatch-$VERSION_RC"
    ```

4. Open a pull request targeting `main`, for example using [`gh`](https://cli.github.com/):

    ```bash
    gh pr create --repo github.com/ava-labs/external-plugins-builder --base main --title "Bump echo and dispatch to $VERSION_RC"
    ```

5. Once the PR checks pass, you can squash and merge it. The [Subnet EVM build Github action](https://github.com/ava-labs/external-plugins-builder/actions/workflows/subnet-evm-image-build.yaml) then creates [one or more pull requests in devops-argocd](https://github.com/ava-labs/devops-argocd/pulls), for example `Auto image update for testnet/echo` and `Auto image update for testnet/dispatch`.
6. Once an automatically created pull request gets merged, it will be deployed, you can then monitor:
    - For Dispatch:
        - [Deployment progress](https://app.datadoghq.com/container-images?query=short_image:dispatch)
        - [Logs](https://app.datadoghq.com/logs?query=subnet%3Adispatch%20%40logger%3A%2A&live=true)
        - [Metrics](https://app.datadoghq.com/dashboard/jrv-mm2-vuc/dispatch-testnet-subnets?live=true)
    - For Echo:
        - [Deployment progress](https://app.datadoghq.com/container-images?query=short_image:echo)
        - [Logs](https://app.datadoghq.com/logs?query=subnet:echo%20@logger:*&live=true)
        - [Metrics](https://app.datadoghq.com/dashboard/jrv-mm2-vuc/echo-testnet-subnets?live=true)

    Note some metrics might be not showing up until a test transaction is ran.
7. Launch a test transaction:
    1. If you have no wallet setup, create a new one using the [Core wallet](https://core.app/)
    1. Go to the settings and enable **Testnet Mode**
    1. You need DIS (Dispatch) and ECH (Echo) testnet tokens. If you don't have one or the other, send your C-chain AVAX address to one of the team members who can send you some DIS/ECH testnet tokens. The portfolio section of the core wallet should then show the DIS and ECH tokens available.
    1. For both Dispatch and Echo, in the "Command center", select **Send**, enter your own C-Chain AVAX address in the **Send To** field, set the **Amount** to 1 and click on **Send**. Finally, select a maximum network fee, usually *Slow* works, and click on **Approve**.
8. You should then see the transaction impact the logs and metrics, for example

    ```log
    Apr 03 10:35:00.000 i-0158b0eef8b774d39 subnets Commit new mining work
    Apr 03 10:34:59.599 i-0158b0eef8b774d39 subnets Resetting chain preference
    Apr 03 10:34:56.085 i-0aca0a4088f607b7e subnets Served eth_getBlockByNumber
    Apr 03 10:34:55.619 i-0ccd28afbac6d9bfc subnets built block
    Apr 03 10:34:55.611 i-0ccd28afbac6d9bfc subnets Commit new mining work
    Apr 03 10:34:55.510 gke-subnets-testnet subnets Submitted transaction
    ```

### Release

If a successful release candidate was created, you can now create a release.

Following the previous example in the [Release candidate section](#release-candidate) we will create a release `v0.7.3` indicated by the `$VERSION` variable.

1. Create and push a tag from the `master` branch:

    ```bash
    git checkout master
    git pull origin
    # Double check the tip of the master branch is the expected commit
    # of the squashed release branch
    git log -1
    git tag -s "$VERSION"
    git push origin "$VERSION"
    ```

2. Create a new release on Github, either using:
    - the [Github web interface](https://github.com/ava-labs/subnet-evm/releases/new)
        1. In the "Choose a tag" box, select the tag previously created `$VERSION` (`v0.7.3`)
        2. Pick the previous tag, for example as `v0.7.2`.
        3. Set the "Release title" to `$VERSION` (`v0.7.3`)
        4. Set the description using this format:

            ```markdown
            # AvalancheGo Compatibility

            The plugin version is unchanged at 39 and is compatible with AvalancheGo version v1.13.0.

            # Breaking changes

            # Features

            # Fixes

            # Documentation

            ```

        5. Only tick the box "Set as the latest release"
        6. Click on the "Create release" button
    - the Github CLI `gh`:

        ```bash
        PREVIOUS_VERSION=v0.7.2
        NOTES="# AvalancheGo Compatibility

        The plugin version is unchanged at 39 and is compatible with AvalancheGo version v1.13.0.

        # Breaking changes

        # Features

        # Fixes

        # Documentation

        "
        gh release create "$VERSION" --notes-start-tag "$PREVIOUS_VERSION" --notes-from-tag "$VERSION" --title "$VERSION" --notes "$NOTES" --verify-tag
        ```

3. Monitor the [release Github workflow](https://github.com/ava-labs/subnet-evm/actions/workflows/release.yml) to ensure the GoReleaser step succeeds and check the binaries are then published to [the releases page](https://github.com/ava-labs/subnet-evm/releases). In case this fails, you can trigger the workflow manually:
    1. Go to [github.com/ava-labs/subnet-evm/actions/workflows/release.yml](https://github.com/ava-labs/subnet-evm/actions/workflows/release.yml)
    1. Click on the "Run workflow" button
    1. Enter the branch name, usually with goreleaser related fixes
    1. Enter the tag name `$VERSION` (i.e. `v0.7.3`)
4. Monitor the [Publish Docker image workflow](https://github.com/ava-labs/subnet-evm/actions/workflows/publish_docker.yml) succeeds. Note this workflow is triggered when pushing the tag, unlike Goreleaser which triggers when publishing the release.
5. Finally, [create a release for precompile-evm](https://github.com/ava-labs/precompile-evm/blob/main/docs/releasing/README.md)

### Post-release
After you have successfully released a new subnet-evm version, you need to bump all of the versions again in preperation for the next release. Note that the release here is not final, and will be reassessed, and possibly changer prior to release. Some releases require a major version update, but this will usually be `$VERSION` + `0.0.1`. For example:
```bash
export P_VERSION=v0.7.4
```
1. Create a branch, from the tip of the `master` branch after the release PR has been merged:
    ```bash
    git fetch origin master
    git checkout master
    git checkout -b "prep-$P_VERSION-release"
    ```
1. Bump the version number to the next pending release version, `$P_VERSION`
  - Update the [RELEASES.md](../../RELEASES.md) file with `$P_VERSION`, creating a space for maintainers to place their changes as they make them. 
  - Modify the [plugin/evm/version.go](../../plugin/evm/version.go) `Version` global string variable and set it to `$P_VERSION`.
1. Add an entry in the object in [compatibility.json](../../compatibility.json), adding the next pending release versionas key and the AvalancheGo RPC chain VM protocol version as value, to the `"rpcChainVMProtocolVersion"` JSON object. For example, we would add:

    ```json
    "v0.7.4": 39,
    ```

    💁 If you are unsure about the RPC chain VM protocol version, set the version to `0`, for example `"v0.7.4": 0`, and then run:

    ```bash
    go test -run ^TestCompatibility$ github.com/ava-labs/subnet-evm/plugin/evm
    ```

    This will fail with an error similar to:

    ```text
    compatibility.json has subnet-evm version v0.7.4 stated as compatible with RPC chain VM protocol version 0 but AvalancheGo protocol version is 39
    ```

    This message can help you figure out what the correct RPC chain VM protocol version (here `39`) has to be in compatibility.json for your current release. Alternatively, you can refer to the [Avalanchego repository `version/compatibility.json` file](https://github.com/ava-labs/avalanchego/blob/main/version/compatibility.json) to find the RPC chain VM protocol version matching the AvalancheGo version we use here.
1. Commit your changes and push the branch

    ```bash
    git add .
    git commit -S -m "chore: prep release $P_VERSION"
    git push -u origin "prep-$P_VERSION-release"
    ```
1. Create a pull request (PR) from your branch targeting master, for example using [`gh`](https://cli.github.com/):
    ```bash
    gh pr create --repo github.com/ava-labs/subnet-evm --base master --title "chore: prep next release $P_VERSION"
    ```
1. Wait for the PR checks to pass with
    ```bash
    gh pr checks --watch
    ```
1. Squash and merge your branch into `master`, for example:
    ```bash
    gh pr merge "prep-$P_VERSION-release" --squash --subject "chore: prep next release $P_VERSION"
    ```
1. Pat yourself on the back for a job well done.
