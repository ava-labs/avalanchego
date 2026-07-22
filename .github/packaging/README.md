# Linux Packaging for AvalancheGo

## Overview

This directory contains the Linux packaging implementation for `avalanchego` and
`subnet-evm`:

- RPM packages for RHEL 9.x-compatible systems
- DEB packages for Ubuntu 22.04 (jammy) and 24.04 (noble)

It exists to ship signed Linux packages without making future maintainers
reconstruct the packaging model from CI YAML and shell scripts alone.

This document is for two audiences:

- **users/release engineers** who need to build or publish packages
- **maintainers** who need to change the packaging implementation safely

## Usage

### What gets produced

The packaging pipeline builds two packages per format and architecture:

| Format | Package | Installed path | Dependency floor |
| --- | --- | --- | --- |
| RPM | `avalanchego` | `/var/opt/avalanchego/bin/avalanchego` | `glibc >= 2.34` |
| RPM | `subnet-evm` | `/var/opt/avalanchego/plugins/<VM_ID>` | `glibc >= 2.34` |
| DEB | `avalanchego` | `/usr/bin/avalanchego` | `libc6 (>= 2.35)` |
| DEB | `subnet-evm` | `/usr/lib/avalanchego/plugins/<VM_ID>` | `libc6 (>= 2.35)` |

`<VM_ID>` is sourced from `graft/subnet-evm/scripts/constants.sh`.

### Main entrypoints

- Workflow: `.github/workflows/build-linux-packages.yml` (unified RPM + DEB)
- Build/validate composite action: `.github/packaging/actions/build-package/action.yml`
- Packaging taskfile: `.github/packaging/Taskfile.yml`
- Build script: `.github/packaging/scripts/build-package.sh`
- Validation scripts:
  - `.github/packaging/scripts/validate-rpm.sh`
  - `.github/packaging/scripts/validate-deb.sh`
- `nfpm` package definitions: `.github/packaging/nfpm/*.yml`
- Builder images:
  - `.github/packaging/Dockerfile.rpm`
  - `.github/packaging/Dockerfile.deb`

### Local build and validation

Build and validate the full RPM pipeline locally with:

```bash
./scripts/run_task.sh packaging:test-build-rpms
```

Build and validate the full DEB pipeline locally with:

```bash
./scripts/run_task.sh --taskfile .github/packaging/Taskfile.yml test-build-debs
```

Or, via the root Taskfile include:

```bash
task packaging:test-build-rpms
task packaging:test-build-debs
```

Useful environment variables:

- `PACKAGING_TAG` - tag/version label to embed; defaults to `v0.0.0` for local
  testing
- `PACKAGE_ARCH` - package architecture override (`x86_64`/`aarch64` for RPM,
  `amd64`/`arm64` for DEB); defaults to the host architecture mapped to the
  package format
- `GPG_KEY_FILE` - private signing key file; empty means generate/reuse an
  ephemeral local key
- `GPG_KEY_PASSPHRASE` - passphrase for `GPG_KEY_FILE`; mirrored internally to
  the `NFPM_<FORMAT>_PASSPHRASE` variable expected by `nfpm`

Without `GPG_KEY_FILE`, the build generates a short-lived ephemeral RSA key with
a known throwaway passphrase. This keeps local and PR builds exercising the same
signing path used by release builds without requiring release credentials.

## CI behavior

`.github/workflows/build-linux-packages.yml` builds both RPM and DEB
packages from a single workflow (format × arch matrix delegating bulk work
to `.github/packaging/actions/build-package`) and runs in three modes:

- **tag push** - builds release packages for the pushed tag
- **workflow_dispatch** - builds packages for an explicitly provided tag
- **pull_request** - smoke-tests the packaging pipeline for changes under
  `.github/packaging/` or the workflow itself

PR builds use a synthetic tag (`v0.0.0-pr.<sha>`) and ephemeral signing.
Non-PR builds import the real signing key from Actions secrets and upload
packages plus the exported public key as artifacts.

The exported public key is named `GPG-KEY-avalanchego` and lives next to
each format's packages (`build/rpm/` for RPM, `build/deb/` for DEB).

DEB release runs additionally upload packages to S3 under
`linux/debs/ubuntu/{jammy,noble}/{arch}/`; the DEB public key is uploaded to
`linux/debs/ubuntu/{jammy,noble}/` because one signing key serves all
architectures of a release.

## Conceptual model

The packaging pipeline treats packaging as a layer around the source release
tree rather than as part of the historical release tree itself.

The model is:

1. Check out the source revision being packaged
2. Overlay the current `.github/packaging` implementation for manual tag builds
   when the requested tag may predate packaging support
3. Build inside a target Linux distribution container so the binary links against the right
   glibc floor
4. Package and sign with `nfpm` (single `nfpm package` invocation)
5. Validate by installing the package in a fresh target Linux distribution container and
   running smoke tests

This directory is code-adjacent because the Dockerfiles, task entrypoints,
package manifests, workflow glue, and shell scripts all evolve together.

Four architecture naming schemes are in play and must stay aligned:

| `uname -m` | Docker `TARGETARCH` / Go downloads | RPM arch | DEB arch |
| --- | --- | --- | --- |
| `x86_64` | `amd64` | `x86_64` | `amd64` |
| `arm64` / `aarch64` | `arm64` | `aarch64` | `arm64` |

The Taskfile normalizes host architecture to the names used by a given package
format; the Dockerfiles use Docker's `TARGETARCH` values for Go and `nfpm`
downloads.

The packaging path assumes Docker's target platform matches the host's
`uname -m`. `TARGETARCH` is set by Docker BuildKit from the resolved target
platform (`--platform`, `DOCKER_DEFAULT_PLATFORM`, or host default in that
priority), but `build-builder-image.sh` and the Taskfile's `PACKAGE_ARCH`
default both derive from host `uname -m` independently. If those diverge
— e.g. `DOCKER_DEFAULT_PLATFORM=linux/amd64` on an arm64 host — the build
either fails (Go checksum mismatch when the script's host-derived
`GO_CHECKSUM` doesn't match the tarball Docker downloaded for the resolved
target) or mislabels packages (the produced `.rpm`/`.deb` carries the
host's arch in its filename despite being built in a target-arch
container). Cross-arch local builds are out of scope; use the CI matrix
or a remote builder.

## Maintenance notes

### Why builds run in target Linux distribution containers

AvalancheGo and Subnet-EVM are dynamically linked against glibc. To produce
binaries that run on every supported Linux distribution, each build runs inside
a container based on the *oldest* supported release for that target Linux
distribution/family — the resulting binary's glibc floor matches that release,
and newer releases run it via glibc's forward compatibility.

#### RPM — built in `rockylinux:9`

RPMs target RHEL 9.x-compatible systems, so RPM builds run in a `rockylinux:9`
container and produce binaries linked against glibc 2.34.

#### DEB — built in `ubuntu:22.04`

DEBs target Ubuntu 22.04 (jammy), the oldest LTS we support, so DEB builds run
in an `ubuntu:22.04` container and produce binaries linked against glibc 2.35.
Compatibility of the produced packages is then verified by installing and
smoke-testing in both `ubuntu:22.04` and `ubuntu:24.04` (the newest LTS we
support).

Building on the default GitHub runner image would link against the runner's
Ubuntu glibc, which is typically newer than the intended jammy floor.

### Why workflows overlay `.github/packaging` for manual tag builds

`workflow_dispatch` accepts any tag whose source tree the current packaging
implementation can still build, including tags created before this packaging
implementation existed in the repository. The practical floor is the commit that
introduced `graft/` populated with both coreth and subnet-evm: `lib-build-common.sh`
sources `graft/subnet-evm/scripts/{build,constants}.sh`, so tags predating `graft`
cannot be rebuilt (see "Implications for maintainers" below).

Those older tags may not contain:

- `.github/packaging/`
- the current packaging scripts and `nfpm` manifests
- the current packaging Dockerfiles and helper logic

To keep manual rebuilds of older releases possible, workflows first check out
the requested tag as the source tree and then overlay `.github/packaging` from
the workflow branch:

1. `actions/checkout` of the requested tag into the workspace root
2. second sparse checkout of `.github/packaging` into `.packaging-overlay`
3. replace the tagged tree's `.github/packaging` with the overlay contents
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

RPMs and DEBs are always built through the signing path.

- **CI release/manual builds** import the real private key from
  `secrets.RPM_GPG_PRIVATE_KEY`
- **local and PR builds** generate an ephemeral RSA key with a known throwaway
  passphrase

This is deliberate: unsigned fast paths tend to rot, while always exercising the
signing path keeps packaging validation closer to what is shipped.

Release events (tag push and `workflow_dispatch`) fail fast in
`workflow-setup-packaging.sh` when no signing key is configured, so a
misconfigured release cannot silently fall back to the ephemeral key.

The build exports the matching public key next to the packages. On DEB release
events (tag push / `workflow_dispatch`), `upload-debs-s3` publishes it alongside
the `.deb`s under `s3://${BUCKET}/linux/debs/ubuntu/<release>/` so DEB consumers
can fetch and import it before installing. On PR/local builds the key is
ephemeral and is not published.

### Validation strategy

The main validation entrypoints are `test-build-rpms` and `test-build-debs`.
They:

1. build both `avalanchego` and `subnet-evm` packages
2. start a fresh target Linux distribution container
3. import the exported public key
4. verify package signatures
5. install both packages
6. run smoke tests on the installed binaries

RPM validation runs in `rockylinux:9`. DEB validation runs in both
`ubuntu:22.04` (the build/target floor) and `ubuntu:24.04` (the newest LTS) to
catch any compatibility regressions across the supported Ubuntu range.

The smoke tests intentionally check a small set of high-value invariants:

- the package can be installed on the target distro base
- signature verification works
- `avalanchego --version` runs and starts with `avalanchego/`
- `avalanchego --version` contains the git commit hash captured during the build
- the Subnet-EVM plugin is installed at the expected VM-ID-derived path
- the plugin binary also reports the expected commit hash

This does **not** replace broader AvalancheGo test coverage. It is specifically
for validating that the packaging layer produced installable, correctly wired
artifacts for the target runtime.

### Binary linking rationale

The current approach prefers dynamic glibc linking over static musl linking.
Future maintainers are likely to revisit alternative linking strategies, and the
trade-offs are not recoverable from the packaging scripts alone.

#### Problem the packaging layer is solving

We need to ship Linux packages for RHEL-family systems and Ubuntu, while general
CI runs mostly on Ubuntu. Different distros ship different glibc versions, so a
dynamically linked binary built against a newer Ubuntu glibc may not run on an
older target distribution. The packaging pipeline therefore builds inside
target Linux distribution containers so each package targets the oldest
supported glibc for its target Linux distribution family.

#### Why dynamic glibc is the current choice

- glibc compatibility is the practical constraint for the supported Linux
  distributions
- glibc avoids taking on musl-specific performance and compatibility risk for a
  performance-sensitive node binary
- Go race-detector workflows remain compatible with glibc-based builds
- binaries linked against older glibc generally run on newer glibc releases,
  which is the compatibility we want to guarantee for each supported distro

#### Why not static musl right now

Static musl would look attractive because it avoids host glibc dependencies, but
it carries project-specific risks that future maintainers should assume are
still live unless new evidence says otherwise:

- musl allocator behavior can be materially worse under multithreaded load; for
  a blockchain node, that is not a casual trade-off
- musl's smaller default thread stacks have caused surprises in other CGO-heavy
  projects
- Go's race detector does not work with musl, which would complicate validation
- the Firewood/FFI side of the build has historically been more naturally
  aligned with `*-linux-gnu` targets than with a musl packaging path
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

Building in `rockylinux:9` and `ubuntu:22.04` is the current way to target the
correct glibc floors. That leaves a residual maintenance concern: broad CI on
Ubuntu is not the same thing as testing the final release binary on the exact
same runtime used for packaging. Package smoke tests reduce that gap by
verifying installation, signatures, and basic execution on target Linux distribution
containers, but they are not a substitute for full release-path parity.

Future changes should not assume the current packaging smoke tests fully
eliminate host-vs-release-runtime divergence. They are a pragmatic guardrail,
not proof that the entire release artifact path has become runtime-agnostic.

#### Longer-term direction: hermetic Bazel toolchain

The longer-term direction is still a hermetic Bazel toolchain that can make
"test what you ship" easier without relying on containerized release-only build
plumbing. The key design idea is that build and test should use the same glibc
baseline regardless of the host runner image.

Earlier design work also identified likely rough edges for anyone revisiting
that path, including Rust/CGO coordination, toolchain-selection bugs around
non-default targets, and third-party native dependencies that may need extra
Bazel integration work. The important point for repository documentation is not
that each historical detail must remain exhaustive forever, but that "move
release builds into Bazel" is not a trivial mechanical swap.

Future maintainers revisiting this area should treat the current container build
as a practical bridge, not necessarily the final architecture.

### Alternative approaches and trade-offs

This section records alternative approaches that help explain the current design
and may be relevant when future maintainers revisit packaging decisions:

- **Build on the default GitHub runner** - this risks linking against a newer
  host glibc than the target distributions provide.
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
- DEBs install under `/usr/bin` and `/usr/lib` (Debian Policy §9.1.2 forbids
  `/usr/local` for packaged files)
- Subnet-EVM installs under the VM ID from
  `graft/subnet-evm/scripts/constants.sh`
- RPM release builds target glibc 2.34 / Rocky Linux 9 compatibility
- DEB release builds target glibc 2.35 / Ubuntu 22.04 compatibility
- the same pipeline path is used for both local smoke testing and CI builds
- manual tag builds continue to package historical source trees via the overlay
  mechanism, unless a documented compatibility cutoff is introduced

### Revisit this design if

The current design should be reconsidered if any of these change:

- the supported Linux distribution baselines move away from RHEL 9.x / glibc
  2.34 or Ubuntu 22.04 / glibc 2.35
- Bazel becomes the canonical release build path and can unify build and test
  against the same glibc baseline
- packaging needs to cover more historical tags than the current repository
  interfaces can support
- performance or compatibility data makes static linking newly attractive
- historical rebuild compatibility is no longer a requirement, or a documented
  cutoff replaces the current overlay behavior
- release distribution needs move beyond GitHub Actions artifacts and the
  current S3 publishing path structure for DEB releases

## References

### Repository entrypoints

- Workflow: `/.github/workflows/build-linux-packages.yml`
- Build/validate composite action: `/.github/packaging/actions/build-package/action.yml`
- Taskfile: `/.github/packaging/Taskfile.yml`
- RPM builder image: `/.github/packaging/Dockerfile.rpm`
- DEB builder image: `/.github/packaging/Dockerfile.deb`
- Build script: `/.github/packaging/scripts/build-package.sh`
- RPM validation script: `/.github/packaging/scripts/validate-rpm.sh`
- DEB validation script: `/.github/packaging/scripts/validate-deb.sh`
- Root task include: `/Taskfile.yml`

### Maintainer background for revisiting linking choices

The maintainer-facing conclusions in this README are intended to stand on their
own, but future revisits of static vs dynamic linking will go better if the
underlying concern classes are easy to rediscover.

The important background to preserve is:

- **musl is not just a portability shortcut** - it changes allocator/runtime
  behavior in ways that may matter for a performance-sensitive, multithreaded,
  CGO-using node process
- **musl changes the validation surface** - in particular, Go's race detector is
  not available in the same way it is for the current glibc-based build/test
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
