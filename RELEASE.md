# Releasing firewood

Releasing firewood is straightforward and can mostly be done in CI. Updating the
Cargo.toml file is currently manual.

Firewood is made up of several sub-projects in a workspace. Each project is in
its own crate and has an independent version.

## Workspace Dependencies

Ensure the following tools are installed before beginning:

```shell
cargo install --locked just git-cliff cargo-edit
```

## Git Branch

Before making changes, create a new branch (if not already on one):

```console
$ git fetch
$ git switch -c release/v0.1.1 origin/main
branch 'release/v0.1.1' set up to track 'origin/main'.
Switched to a new branch 'release/v0.1.1'
```

If already on a new branch, ensure `HEAD` is the same as the remote's `main`.
As the upstream repo changes, rebase the branch onto `main` so that the changes
from `git cliff` follow the repository history in the correct linear order.

On rebase, quickly redo the generative steps with `just`:

```shell
just release-step-update-rust-dependencies && just release-step-refresh-changelog v0.1.1
```

## Dependency upgrades

### Rust MSRV

Optionally, the minimum supported rust version can be bumped. See
<https://blog.rust-lang.org/releases/latest> for the latest version. Best
effort 2 releases behind the current latest stable [^1]. However, Rust evolves
fairly quickly and we occasionally want to take advantage of a new improvement
before the stable release matures.

Updating the Rust MSRV requires edits in two places:

- [clippy.toml]
  - Update the root `msrv` setting to indicate the set MSRV. This configures
    clippy to include newer lints that would otherwise be incorrect with a lower
    MSRV; such as recommending newly stablized features that were unstable in
    the older release.
- [Cargo.toml]
  - Update `workspace.package.rust-version` which will propagate through the
    other cargo packages that have `package.rust-version.workspace = true` set.

[^1]: e.g., with 1.91 stable, 1.89 is the best effort MSRV

### Cargo Dependencies

```shell
just release-step-update-rust-dependencies
```

The recipe runs `cargo upgrade`, `cargo upgrade --incompatible`, `cargo update
--verbose`, and the test suite to verify nothing broke.

Crates listed under `[patch.crates-io]` in `Cargo.toml` reference specific git
revisions and cannot be upgraded through the registry. Exclude them explicitly
when running `cargo upgrade`:

```shell
cargo upgrade --exclude <patched-crate-name>
```

Check `[patch.crates-io]` in `Cargo.toml` to see the current set of patched
crates. Update them separately by adjusting the revision in `Cargo.toml` and
opening a dedicated PR.

If an incompatible upgrade requires code changes that are out of scope for the
release, exclude it and track the work in a new GitHub issue:

```shell
cargo upgrade --incompatible --exclude <dependency-name>
```

Open an issue titled `chore(deps): upgrade <dependency-name> to <version>`.

### Go Dependencies

Go dependencies are updated on an as-needed basis, usually for security.

## Versioning Policy

Firewood follows [Semantic Versioning](https://semver.org/). While the major
version is 0, a minor-version bump signals breaking changes and a patch-version
bump signals non-breaking changes.

### Crates with public API guarantees

| Crate | Public interface |
| ----- | ---------------- |
| `firewood` | Rust API |
| `firewood-storage` | Rust API |
| `firewood-ffi` | C ABI, metrics schema, database file format |
| `firewood-go` (via `firewood-go-ethhash`) | Go module API |
| `fwdctl` | CLI flags and config file format |

Tag pull requests with the appropriate `breaking-change/<crate>` label when the
change breaks the public interface. `git cliff` reads these labels via the GitHub
API and marks affected entries in the CHANGELOG automatically.

### Crates without public API guarantees

`firewood-macros`, `firewood-benchmark`, `firewood-replay`, and `firewood-triehash`
have no stability guarantees. `firewood-triehash` is a test helper pinned at its
current version; do not bump it during workspace releases.

### Determining whether a change is breaking

Use [`cargo-semver-checks`](https://github.com/obi1kenobi/cargo-semver-checks)
to detect Rust API-level breaking changes in `firewood` and `firewood-storage`.
This check is **informational and inconclusive in the negative direction**: a
passing result does not guarantee compatibility, because it does not cover
metrics names, file format, behavioral guarantees, or C ABI stability.

### firewood-go-ethhash versioning

The Go module at `ava-labs/firewood-go-ethhash` tracks `firewood-ffi`. When
`firewood-ffi` is bumped to `v0.6.0`, CI tags `firewood-go-ethhash` at
`ffi/v0.6.0`.

## Package Version

Next, update the workspace versions as needed. Only the packages with changes should have their version bumped. Transitive changes do not require a version bump unless the transitive dependency requires a semver upgrade.

For semver breaking changes, all downstream packages require upgrading to the new version as well.

## Dependency Version

`cargo publish` for each package requires that each dependency specify _a_ version;
therefore, the next step is to bump the dependency declarations to the new version.
Packages within the workspace that are used as libraries are also defined within
the [`[workspace.dependencies]`](https://doc.rust-lang.org/cargo/reference/workspaces.html#the-dependencies-table)
table. E.g.,:

```toml
[workspace.dependencies]
# workspace local packages
firewood = { path = "firewood", version = "0.1.1" }
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

Run `cargo update` after editing the `Cargo.toml` files to ensure the lockfile
is correct and reflects the new package versions.

## Changelog

```shell
just release-step-refresh-changelog v0.1.1
```

`git cliff` generates the full `CHANGELOG.md` by reading commits since the
previous tag. It also fetches PR metadata from the GitHub API, so export a
`GITHUB_TOKEN` with read access to the repository before running:

```shell
export GITHUB_TOKEN=<your-token>
just release-step-refresh-changelog v0.1.1
```

Breaking changes are detected automatically from both conventional commit
syntax (`feat!:`, `BREAKING CHANGE:` footer) and PR labels (`breaking-change/*`).
No manual edits to `CHANGELOG.md` are needed or expected.

## Commit

Commit the version bump and change log updates. Using the summary prefix:

> chore(release): prepare for

will cause `git-cliff` to omit the commit from the changelog. This is why
substantive changes when upgrading dependencies should be in their own commit.
If you do not use the prefix, there will be an egg-and-chicken problem as the
commit message generated by GitHub includes the pull request as a suffix.
Manually resolve this in the CHANGELOG if needed; otherwise, the CHANGELOG will
have irrelevant changes on future releases.

## Review

> ❗ Be sure to update the versions of all sub-projects before creating a new
> release. Open a PR with the updated versions and merge it before continuing to
> the next step.

## Publish

### Tag the release

Switch to the updated `main` branch and push a signed, annotated tag:

```shell
git checkout main
git pull --prune
git tag -s -a v0.1.1 -m 'Release v0.1.1'
git push origin v0.1.1
```

### What CI does automatically

Pushing the tag triggers three automated jobs:

1. **Draft GitHub release** ([`.github/workflows/release.yaml`](.github/workflows/release.yaml)):
   Creates a draft release with auto-generated notes. Do not act yet — publish
   the draft manually (see below) to trigger the crates.io workflow.

2. **firewood-go-ethhash publish** ([`.github/workflows/attach-static-libs.yaml`](.github/workflows/attach-static-libs.yaml)):
   Builds the `firewood-ffi` static library for each supported target, pushes
   the result to `ava-labs/firewood-go-ethhash`, and tags it `ffi/v0.1.1`.
   This typically takes 10–15 minutes.

### Publish the draft release (manual step)

The crates.io workflow does **not** run until the GitHub release is published.

1. Go to <https://github.com/ava-labs/firewood/releases>.
2. Find the draft (labelled **Draft**).
3. Click the pencil icon (**Edit**).
4. Review the auto-generated notes and add any missing context.
5. Scroll to the bottom and click **Publish release**.

### After publishing the release

Publishing triggers the crates.io workflow ([`.github/workflows/publish.yaml`](.github/workflows/publish.yaml)),
which obtains a short-lived OIDC token from crates.io (trusted publishing is
pre-configured for this workflow) and publishes each crate in topological
dependency order, automatically skipping:

- Crates whose current version is already published.
- Crates that transitively depend on a `[patch.crates-io]` git reference
  (crates.io rejects non-registry dependencies).

## Milestone

> NOTE: this should be automated as well.

Close the GitHub milestone for the version that was released. Be sure to create
a new milestone for the next version if one does not already exist. Carry over
any incomplete tasks to the next milestone.
