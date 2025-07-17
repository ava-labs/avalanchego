# Releasing firewood

Releasing firewood is straightforward and can mostly be done in CI. Updating the
Cargo.toml file is currently manual.

Firewood is made up of several sub-projects in a workspace. Each project is in
its own crate and has an independent version.

## Git Branch

Start off by crating a new branch:

```console
$ git fetch
$ git switch -c release/v0.0.10 origin/main
branch 'release/v0.0.10' set up to track 'origin/main'.
Switched to a new branch 'release/v0.0.10'
```

## Package Version

Next, update the workspace version and ensure all crates within the firewood
project are using the version of the new release. The root [Cargo.toml](Cargo.toml)
file uses the [`[workspace.package]`](https://doc.rust-lang.org/cargo/reference/workspaces.html#the-package-table)
table to define the version for all subpackages.

```toml
[workspace.package]
version = "0.0.7"
```

Each package inherits this version by setting `package.version.workspace = true`.

```toml
[package]
name = "firewood"
version.workspace = true
```

Therefore, updating only the version defined in the root config is needed.

## Dependency Version

The next step is to bump the dependency declarations to the new version. Packages
within the workspace that are used as libraries are also defined within the
[`[workspace.dependencies]`](https://doc.rust-lang.org/cargo/reference/workspaces.html#the-dependencies-table)
table. E.g.,:

```toml
[workspace.dependencies]
# workspace local packages
firewood = { path = "firewood", version = "0.0.10" }
```

This allows packages within the workspace to inherit the dependency,
including path, version, and workspace-level features by adding `workspace = true`
to the dependency table (note: using `cargo add -p firewood-fwdctl firewood-metrics`
would automatically add the dependency with `workspace = true`).

```toml
[dependencies]
firewood-macros.workspace = true

# more complex example
[target.'cfg(target_os = "linux")'.dependencies]
firewood-storage = { workspace = true, features = ["io-uring"] }

[target.'cfg(not(target_os = "linux"))'.dependencies]
firewood-storage.workspace = true
```

Thefefore, after updating the `workspace.package.version` value, we must update
the dependency versions to match.

## Changelog

To build the changelog, see git-cliff.org. Short version:

```sh
cargo install git-cliff
git cliff --tag v0.0.10 | sed -e 's/_/\\_/g' > CHANGELOG.md
```

## Review

> ❗ Be sure to update the versions of all sub-projects before creating a new
> release. Open a PR with the updated versions and merge it before continuing to
> the next step.

## Publish

To trigger a release, push a tag to the main branch matching the new version,

```sh
git tag -S v0.0.10
git push origin v0.0.10
```

for `v0.0.10` for the merged version change. The CI will automatically publish a
draft release which consists of release notes and changes (see
[.github/workflows/release.yaml](.github/workflows/release.yaml)).

## Milestone

> NOTE: this should be automated as well.

Close the GitHub milestone for the version that was released. Be sure to create
a new milestone for the next version if one does not already exist. Carry over
any incomplete tasks to the next milestone.
