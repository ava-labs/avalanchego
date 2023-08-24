# Releasing firewood

Releasing firewood is straightforward and can be done entirely in CI. 

Firewood is made up of several sub-projects in a workspace. Each project is in
its own crate and has an independent version. 
* firewood
* growth-ring
* libaio
* shale

The first step in drafting a release is ensuring all crates within the firewood
project are using the version of the new release.  There is a utility to ensure
all versions are updated simultaneously in `cargo-workspace-version`. To use it
to update to 0.0.4, for example:

    $ cargo install cargo-workspace-version $ cargo workspace-version update
v0.0.4

See the [source code](https://github.com/ava-labs/cargo-workspace-version) for
more information on the tool.

> ❗ Be sure to update the versions of all sub-projects before creating a new
> release. Open a PR with the updated versions and merge it before continuing to
> the next step.

To trigger a release, simply push a semver-compatible tag to the main branch,
for example `v0.0.1`. The CI will automatically publish a draft release which
consists of release notes and changes. Once this draft is approved, and the new
release exists, CI will then go and publish each of the sub-project crates to
crates.io.
> ❗ Publishing a crate with the same version as the existing version will
> result in a cargo error. Be sure to update all crates within the workspace
> before publishing to crates.io.
