# Releasing firewood

Releasing firewood is straightforward and can be done entirely in CI.

Firewood is made up of several sub-projects in a workspace. Each project is in
its own crate and has an independent version.

The first step in drafting a release is ensuring all crates within the firewood
project are using the version of the new release.  There is a utility to ensure
all versions are updated simultaneously in `cargo-workspace-version`. To use it
to update to 0.0.5, for example:

```sh
   cargo install cargo-workspace-version
   cargo workspace-version update v0.0.5
```

See the [source code](https://github.com/ava-labs/cargo-workspace-version) for
more information on the tool.

> ❗ Be sure to update the versions of all sub-projects before creating a new
> release. Open a PR with the updated versions and merge it before continuing to
> the next step.

To trigger a release, simply push a semver-compatible tag to the main branch,
for example `v0.0.5`. The CI will automatically publish a draft release which
consists of release notes and changes.
