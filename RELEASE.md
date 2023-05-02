# Releasing firewood

Releasing firewood is straightforward and can be done entirely in CI. 

Firewood is made up of several sub-projects in a workspace. Each project is in
its own crate and has an independent version. 
* firewood
* firewood-growth-ring
* firewood-libaio
* firewood-shale

There is a utility to ensure all versions are updated simultaneously in
cargo-update-all-revs. To use it to update to 0.0.4, for example:

    cargo install --path cargo-update-all-revs
    cargo update-all-revs 0.0.4

To trigger a release, simply push a semver-compatible tag to the main branch,
for example `v0.0.1`. The CI will automatically publish a draft release which
consists of release notes and changes. Once this draft is approved, and the new
release exists, CI will then go and publish each of the sub-project crates to
crate.io
> Note: Only crates that had a version bump will be updated on crates.io. If a
> crate was changed, but the version was not bumped, it will not be updated on
> crates.io. Deploying a crate with the same version as previously will be a
> no-op. 
