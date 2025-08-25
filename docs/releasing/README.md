# Releasing

## Procedure

### Release candidate

ℹ️ you should always create a release candidate first, and only if everything is fine, you can create a release.

In this section, we create a release candidate `v0.15.0-rc.0`. We therefore assign these environment variables to simplify copying instructions:

```bash
export VERSION_RC=v0.15.0-rc.0
export VERSION=v0.15.0
```

1. Create your branch, usually from the tip of the `master` branch:

    ```bash
    git fetch origin master:master
    git checkout master
    git checkout -b "releases/$VERSION_RC"
    ```

1. Update the [RELEASES.md](../../RELEASES.md) file by renaming the "Pending" section to the new release version `$VERSION` and creating a new "Pending" section at the top.
1. Modify the [plugin/evm/version.go](../../plugin/evm/version.go) `Version` global string variable and set it to the desired `$VERSION`.
1. Because AvalancheGo and coreth depend on each other, and that we create releases of AvalancheGo before coreth, you can use a recent commit hash or recent release candidate of AvalancheGo in your `go.mod` file. Coreth releases should be tightly coordinated with AvalancheGo releases.
1. Commit your changes and push the branch

    ```bash
    git add .
    git commit -S -m "chore: release $VERSION_RC"
    git push -u origin "releases/$VERSION_RC"
    ```

1. Create a pull request (PR) from your branch targeting master, for example using [`gh`](https://cli.github.com/):

    ```bash
    gh pr create --repo github.com/ava-labs/coreth --base master --title "chore: release $VERSION_RC"
    ```

1. Wait for the PR checks to pass with

    ```bash
    gh pr checks --watch
    ```

1. Squash and merge your release branch into `master`, for example:

    ```bash
    gh pr merge "releases/$VERSION" --squash --delete-branch --subject "chore: release $VERSION" --body "\n- Update AvalancheGo from v1.12.3 to v1.13.0"
    ```

1. Create and push a tag from the `master` branch:

    ```bash
    git fetch origin master:master
    git checkout master
    # Double check the tip of the master branch is the expected commit
    # of the squashed release branch
    git log -1
    git tag -s "$VERSION_RC"
    git push origin "$VERSION_RC"
    ```

1. Once the release candidate tag is created, create a pull request on the AvalancheGo repository, bumping the coreth dependency to use this release candidate. Once proven stable, an AvalancheGo release should be created, after which you can create a coreth release.

### Release

If a successful release candidate was created and integrated in a release of AvalancheGo, you can now create a release.

Following the previous example in the [Release candidate section](#release-candidate) we will create a release `v0.15.0` indicated by the `$VERSION` variable.

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

1. Create a new release on Github, either using:
    - the [Github web interface](https://github.com/ava-labs/coreth/releases/new)
        1. In the "Choose a tag" box, select the tag previously created `$VERSION` (`v0.15.0`)
        1. Pick the previous tag, for example as `v0.14.0`.
        1. Set the "Release title" to `$VERSION` (`v0.15.0`)
        1. Set the description using this format:

            ```markdown
            This is the Coreth version used in AvalancheGo@v1.13.1

            # Breaking changes

            # Features

            # Fixes

            # Documentation

            ```

        1. Only tick the box "Set as the latest release"
        1. Click on the "Create release" button
    - the Github CLI `gh`:

        ```bash
        PREVIOUS_VERSION=v0.14.0
        NOTES="This is the Coreth version used in AvalancheGo@v1.13.1

        # Breaking changes

        # Features

        # Fixes

        # Documentation

        "
        gh release create "$VERSION" --notes-start-tag "$PREVIOUS_VERSION" --notes-from-tag "$VERSION" --title "$VERSION" --notes "$NOTES" --verify-tag
        ```

Note this release will likely never be used in AvalancheGo, which should always be using release candidates to accelerate the development process. However it is still useful to have a release to indicate the last stable version of coreth.

### Post-release
After you have successfully released a new coreth version, you need to bump all of the versions again in preperation for the next release. Note that the release here is not final, and will be reassessed, and possibly changer prior to release. Some releases require a major version update, but this will usually be `$VERSION` + `0.0.1`. For example:
```bash
export P_VERSION=v1.15.1
```
1. Create a branch, from the tip of the `master` branch after the release PR has been merged:
    ```bash
    git fetch origin master:master
    git checkout master
    git checkout -b "prep-$P_VERSION-release"
    ```
1. Bump the version number to the next pending release version, `$P_VERSION`
  - Update the [RELEASES.md](../../RELEASES.md) file with `$P_VERSION`, creating a space for maintainers to place their changes as they make them. 
  - Modify the [plugin/evm/version.go](../../plugin/evm/version.go) `Version` global string variable and set it to `$P_VERSION`.
1. Create a pull request (PR) from your branch targeting master, for example using [`gh`](https://cli.github.com/):
    ```bash
    gh pr create --repo github.com/ava-labs/coreth --base master --title "chore: prep next release $P_VERSION"
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
