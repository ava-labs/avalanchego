# RPM packaging

## Overview

This directory contains the RPM packaging implementation for AvalancheGo and
Subnet-EVM.

It exists to ship signed RPMs for RHEL 9.x-compatible systems without making
future maintainers reconstruct the packaging model from CI YAML and shell
scripts alone.

This document is for two audiences:

- **users/release engineers** who need to build or publish RPMs
- **maintainers** who need to change the packaging implementation safely

## Usage

### What gets produced

The packaging pipeline builds two RPMs per architecture:

| Package | Installed path |
| --- | --- |
| `avalanchego` | `/var/opt/avalanchego/bin/avalanchego` |
| `subnet-evm` | `/var/opt/avalanchego/plugins/<VM_ID>` |

Both packages declare `glibc >= 2.34`.

### Main entrypoints

- CI workflow: `.github/workflows/build-rpm-release.yml`
- Packaging taskfile: `.github/packaging/Taskfile.yml`
- Build script: `.github/packaging/scripts/build-package.sh`
- Validation script: `.github/packaging/scripts/validate-rpm.sh`
- nfpm package definitions: `.github/packaging/nfpm/*.yml`

### Local build and validation

Build and validate the full RPM pipeline locally with:

```bash
./scripts/run_task.sh --taskfile .github/packaging/Taskfile.yml test-build-rpms
```

Or, via the root Taskfile include:

```bash
task packaging:test-build-rpms
```

Useful environment variables:

- `PACKAGING_TAG` - tag/version label to embed, defaults to `v0.0.0`
- `GPG_KEY_FILE` - real signing key for non-ephemeral signing
- `GPG_KEY_PASSPHRASE` - passphrase for the signing key

Without `GPG_KEY_FILE`, the build generates a short-lived ephemeral GPG key
so that local and PR builds still exercise the signing path.

### CI behavior

`.github/workflows/build-rpm-release.yml` runs in three modes:

- **tag push** - builds release RPMs for the pushed tag
- **workflow_dispatch** - builds RPMs for an explicitly provided tag
- **pull_request** - smoke-tests the packaging pipeline for changes under
  `.github/packaging/` or the workflow itself

PR builds use a synthetic tag (`v0.0.0-pr.<sha>`) and ephemeral signing.
Non-PR builds import the real RPM signing key from Actions secrets and upload
RPMs plus the exported public key as artifacts.

## Conceptual model

The RPM pipeline intentionally treats packaging as a layer *around* the source
release tree rather than as part of the historical release tree itself.

The model is:

1. Check out the source revision being packaged
2. Build inside a Rocky Linux 9 container so the binary links against glibc 2.34
3. Package with `nfpm`
4. Sign the RPMs
5. Validate by installing the built RPMs into a fresh Rocky Linux 9 container

This directory is code-adjacent because the implementation lives here:
Dockerfile, task entrypoints, package manifests, and shell scripts all evolve
together.

Two architecture naming schemes are in play and must stay aligned:

| Context | x86_64 | arm64 |
| --- | --- | --- |
| `uname -m` / RPM | `x86_64` | `aarch64` |
| Docker / Go downloads | `amd64` | `arm64` |

The Taskfile normalizes host architecture to RPM names; the Dockerfile maps
Docker `TARGETARCH` values to the names expected by upstream downloads.

## Maintenance notes

### Why the build uses Rocky Linux 9

The packaging target is RHEL 9.x-compatible systems, so builds run in a
`rockylinux:9` container and produce binaries linked against glibc 2.34.
Building on the default GitHub runner image would instead link against the host
Ubuntu glibc, which is too new for the intended target.

Current supported architectures are:

- `x86_64`
- `aarch64`

### Why the workflow overlays `.github/packaging` for manual tag builds

`workflow_dispatch` accepts an arbitrary tag, including tags created before RPM
packaging existed in this repository.

Those older tags may not contain:

- `.github/packaging/`
- the current packaging scripts and nfpm manifests
- the current packaging Dockerfile and helper logic

To keep manual rebuilds of older releases possible, the workflow first checks
out the requested tag as the source tree and then overlays `.github/packaging`
from the workflow branch:

1. `actions/checkout` of the requested tag into the workspace root
2. second sparse checkout of `.github/packaging` into `.packaging-overlay`
3. copy overlay contents into `.github/packaging` in the tagged tree
4. run the current packaging pipeline against the historical source

This is intentionally narrow: it overlays **only the packaging implementation**,
not the rest of the branch. That lets maintainers improve packaging logic over
time while still packaging historical source revisions.

#### Implications for maintainers

When changing `.github/packaging`, keep in mind that `workflow_dispatch` may run
those scripts against trees that predate the packaging feature.

Changes are safer when they depend only on repository interfaces that already
exist across the intended release range, such as:

- `scripts/build.sh`
- `scripts/constants.sh`
- `graft/subnet-evm/scripts/build.sh`
- `graft/subnet-evm/scripts/constants.sh`

If packaging changes need newer repository structure or newer build interfaces,
manual rebuilds of older tags may stop working. If that trade-off is acceptable,
document the cutoff explicitly in this README and in the workflow change.

### Signing behavior

RPMs are always built through the signing path.

- **CI release/manual builds** import the real private key from
  `secrets.RPM_GPG_PRIVATE_KEY`
- **local and PR builds** generate an ephemeral RSA key with no passphrase

This is deliberate: unsigned fast paths tend to rot, while always exercising the
signing path keeps packaging validation closer to what is shipped.

The build exports `GPG-KEY-avalanchego` next to the RPMs so validation and
consumers can import the matching public key.

### Validation strategy

The main validation entrypoint is `test-build-rpms`, which:

1. builds both RPMs
2. starts a fresh `rockylinux:9` container
3. imports the public key
4. verifies RPM signatures
5. installs both RPMs
6. runs smoke tests on the installed binaries

The smoke tests intentionally check a small set of high-value invariants:

- the RPM can be installed on the target distro base
- signature verification works
- `avalanchego --version` runs and contains the built commit hash
- the Subnet-EVM plugin is installed at the expected VM-ID-derived path
- the plugin binary also reports the expected commit hash

This does **not** replace broader AvalancheGo test coverage. It is specifically
for validating that the packaging layer produced installable, correctly wired
artifacts for the target runtime.

### Binary linking rationale

The current approach prefers **dynamic glibc linking** over static musl linking.
This part of the design is worth documenting in more detail because future
maintainers are likely to revisit alternative linking strategies, and the
trade-offs are not recoverable from the packaging scripts alone.

#### Problem the packaging layer is solving

We need to ship RPMs for RHEL-family systems, but CI runners normally build on
Ubuntu. Different distros ship different glibc versions, so a dynamically linked
binary built against a newer Ubuntu glibc may not run on an older RHEL-family
system. The packaging pipeline therefore builds inside `rockylinux:9` so the
result targets glibc 2.34, which matches the current deployment baseline.

#### Why dynamic glibc is the current choice

- glibc compatibility is the practical constraint for RHEL-family deployment
- glibc avoids taking on musl-specific performance and compatibility risk for a
  performance-sensitive node binary
- Go race-detector workflows remain compatible with glibc-based builds
- binaries linked against older glibc generally run on newer glibc releases,
  which is the compatibility direction we want for a RHEL 9 baseline

#### Why not static musl right now

Static musl would look attractive because it avoids host glibc dependencies, but
it carries project-specific risks that future maintainers should assume are
still live unless new evidence says otherwise:

- musl allocator behavior can be materially worse under multithreaded load; for
  a blockchain node, that is not a casual trade-off
- musl's smaller default thread stacks have caused surprises in other CGO-heavy
  projects
- Go's race detector does not work with musl, which would complicate validation
- the Firewood/FFI side of the build has historically been more naturally aligned
  with `*-linux-gnu` targets than with a musl packaging path
- we do not have a dedicated performance validation loop that would confidently
  catch musl-induced regressions before release

The important maintenance takeaway is: **do not switch to musl just because it
seems simpler operationally unless there is fresh performance and compatibility
validation to justify it.**

#### Why not static glibc

Static glibc avoids some musl concerns, but it does not remove the need to pick
and validate against a glibc baseline. That means it does not actually solve the
main portability problem that motivated the current containerized build. It also
pushes the build into a less common and less well-exercised CGO deployment shape
than the current dynamic-glibc approach.

#### Container builds are the current stopgap

Building in `rockylinux:9` is the current way to target the correct glibc.
That does leave a residual maintenance concern: broad CI on Ubuntu is not the
same thing as testing the final release binary on the exact same runtime used
for packaging. The RPM smoke tests reduce that gap by verifying installation,
signatures, and basic execution on Rocky Linux 9, but they are not a substitute
for full release-path parity.

The maintenance implication is that future changes should not assume the
current packaging smoke tests fully eliminate host-vs-release-runtime
divergence. They are a pragmatic guardrail, not proof that the entire release
artifact path has become runtime-agnostic.

#### Longer-term direction: hermetic Bazel toolchain

The longer-term direction is still a hermetic Bazel toolchain that can make
"test what you ship" easier without relying on containerized release-only build
plumbing. The key design idea is that build and test should use the same glibc
baseline regardless of the host runner image.

Earlier design work also identified some likely rough edges for anyone revisiting
that path, including Rust/CGO coordination, toolchain-selection bugs around
non-default targets, and third-party native dependencies that may need extra
Bazel integration work. The important point for repository documentation is not
that each of those historical details must remain exhaustive forever, but that
"move release builds into Bazel" is not a trivial mechanical swap.

Future maintainers revisiting this area should treat the current container build
as a practical bridge, not necessarily the final architecture.

### Alternative approaches and trade-offs

This section records alternative approaches that help explain the current design
and may be relevant when future maintainers revisit packaging decisions:

- **Build on the default GitHub runner** - this risks linking against a newer
  host glibc than the target RHEL-family systems provide.
- **Switch to static musl for portability** - this trades away known and
  still-relevant performance, CGO, and validation properties without current
  evidence that the trade is safe.
- **Switch to static glibc** - this does not remove the need to choose and
  validate a glibc baseline, so it does not solve the central compatibility
  problem.
- **Remove the workflow overlay for older tags** - that would drop the ability
  to rebuild historical releases from before `.github/packaging` existed.

### Invariants to preserve

If you change this area, preserve these unless you are intentionally revisiting
the design:

- RPMs install under `/var/opt/avalanchego/...`
- Subnet-EVM installs under the VM ID from
  `graft/subnet-evm/scripts/constants.sh`
- release builds target glibc 2.34 / Rocky Linux 9 compatibility
- the same pipeline path is used for both local smoke testing and CI builds
- manual tag builds continue to package historical source trees via the overlay
  mechanism, unless a documented compatibility cutoff is introduced

### Revisit this design if

The current design should be reconsidered if any of these change:

- the supported Linux distribution baseline moves away from RHEL 9.x / glibc 2.34
- Bazel becomes the canonical release build path and can unify build and test
  against the same glibc baseline
- packaging needs to cover more historical tags than the current repository
  interfaces can support
- performance or compatibility data makes static linking newly attractive
- historical rebuild compatibility is no longer a requirement, or a documented
  cutoff replaces the current overlay behavior
- release distribution needs move beyond GitHub Actions artifacts

## References

### Repository entrypoints

- Workflow: `/.github/workflows/build-rpm-release.yml`
- Taskfile: `/.github/packaging/Taskfile.yml`
- Builder image: `/.github/packaging/Dockerfile.rpm`
- Build script: `/.github/packaging/scripts/build-package.sh`
- Validation script: `/.github/packaging/scripts/validate-rpm.sh`
- Shared packaging helpers: `/.github/packaging/scripts/lib-build-common.sh`
- Root task include: `/Taskfile.yml`

### Maintainer background for revisiting linking choices

The maintainer-facing conclusions in this README are intended to stand on their
own, but future revisits of static vs dynamic linking will go better if the
underlying concern classes are easy to rediscover.

The important background to preserve is:

- **musl is not just a portability shortcut** - it changes allocator/runtime
  behavior in ways that may matter for a performance-sensitive, multithreaded,
  CGO-using node process
- **musl changes the validation surface** - in particular, Go's race detector
  is not available in the same way it is for the current glibc-based build/test
  path
- **static glibc is not a free escape hatch** - it still requires deliberate
  compatibility validation and introduces its own libc caveats
- **toolchain changes interact with CGO / Rust / non-default targets** - a
  future hermetic or static-linking path should assume real integration work,
  not a mechanical flag flip

If you are actively reconsidering those choices, these references are useful
starting points rather than normative design inputs:

- musl allocator / performance concerns:
  - <https://nickb.dev/blog/default-musl-allocator-considered-harmful-to-performance/>
  - <https://andygrove.io/2020/05/why-musl-extremely-slow/>
- musl with Go static linking and race-detector limitations:
  - <https://honnef.co/articles/statically-compiled-go-programs-always-even-with-cgo-using-musl/>
- static glibc caveats:
  - <https://developers.redhat.com/articles/2023/08/31/how-we-ensure-statically-linked-applications-stay-way>
  - <https://tschottdorf.github.io/golang-static-linking-bug>
- examples of CGO-heavy projects and trade-off discussion:
  - <https://github.com/cockroachdb/cockroach/issues/3392>
  - <https://www.dolthub.com/blog/2024-05-01-cgo-tradeoffs/>
- toolchain rough edges that earlier exploration surfaced:
  - <https://github.com/ziglang/zig/issues/12833>
  - <https://github.com/bazelbuild/rules_rust/issues/2726>
