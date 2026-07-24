# DEB packaging

## Overview

This directory contains the DEB packaging implementation for AvalancheGo and
Subnet-EVM.

It exists to ship signed DEBs for Ubuntu 22.04 (jammy) and 24.04 (noble)
without making future maintainers reconstruct the packaging model from CI YAML
and shell scripts alone.

This document is for two audiences:

- **users/release engineers** who need to build or publish DEBs
- **maintainers** who need to change the packaging implementation safely

## Usage

### What gets produced

The packaging pipeline builds two DEBs per architecture:

| Package | Installed path |
| --- | --- |
| `avalanchego` | `/usr/bin/avalanchego` |
| `subnet-evm` | `/usr/lib/avalanchego/plugins/<VM_ID>` |

Both packages declare `libc6 (>= 2.35)`. `<VM_ID>` is sourced from
`graft/subnet-evm/scripts/constants.sh`.

### Main entrypoints

- CI workflow: `.github/workflows/build-linux-packages.yml` (unified RPM + DEB)
- Build/validate composite action: `.github/packaging/actions/build-package/action.yml`
- Packaging taskfile: `.github/packaging/Taskfile.yml`
- Build script: `.github/packaging/scripts/build-package.sh`
- Validation script: `.github/packaging/scripts/validate-deb.sh`
- nfpm package definitions: `.github/packaging/nfpm/*.yml`

### Local build and validation

Build and validate the full DEB pipeline locally with:

```bash
./scripts/run_task.sh --taskfile .github/packaging/Taskfile.yml test-build-debs
```

Or, via the root Taskfile include:

```bash
task packaging:test-build-debs
```

Useful environment variables:

- `PACKAGING_TAG` - tag/version label to embed, defaults to `v0.0.0`
- `PACKAGE_ARCH` - package architecture override (`amd64`/`arm64`); defaults to
  the host architecture mapped to the DEB naming scheme
- `GPG_KEY_FILE` - real signing key for non-ephemeral signing
- `GPG_KEY_PASSPHRASE` - passphrase for the signing key; mirrored internally to
  the `NFPM_DEB_PASSPHRASE` variable expected by `nfpm`

Without `GPG_KEY_FILE`, the build generates a short-lived ephemeral GPG key so
that local and PR builds still exercise the signing path.

### CI behavior

`.github/workflows/build-linux-packages.yml` builds both RPM and DEB packages
from a single workflow (format × arch matrix delegating bulk work to
`.github/packaging/actions/build-package`) and runs in three modes:

- **tag push** - builds release DEBs for the pushed tag
- **workflow_dispatch** - builds DEBs for an explicitly provided tag
- **pull_request** - smoke-tests the packaging pipeline for changes under
  `.github/packaging/` or the workflow itself

PR builds use a synthetic tag (`v0.0.0-pr.<sha>`) and ephemeral signing.
Non-PR builds import the real signing key from Actions secrets and upload
DEBs plus the exported public key as artifacts. See [`rpm.md`](rpm.md) for the
RPM half of the same workflow.

DEB release runs additionally upload packages to S3 under
`linux/debs/ubuntu/{jammy,noble}/{arch}/`; the DEB public key is uploaded to
`linux/debs/ubuntu/{jammy,noble}/` because one signing key serves all
architectures of a release.

## Conceptual model

The DEB pipeline intentionally treats packaging as a layer *around* the source
release tree rather than as part of the historical release tree itself.

The model is:

1. Check out the source revision being packaged
2. Build inside an Ubuntu 22.04 container so the binary links against glibc 2.35
3. Package with `nfpm`
4. Sign the DEBs
5. Validate by installing the built DEBs into fresh `ubuntu:22.04` and
   `ubuntu:24.04` containers

This directory is code-adjacent because the implementation lives here:
Dockerfile, task entrypoints, package manifests, and shell scripts all evolve
together.

Two architecture naming schemes are in play and must stay aligned:

| Context | x86_64 | arm64 |
| --- | --- | --- |
| `uname -m` | `x86_64` | `aarch64` |
| Docker / Go downloads / DEB | `amd64` | `arm64` |

The Taskfile normalizes host architecture to DEB names; the Dockerfile maps
Docker `TARGETARCH` values to the names expected by upstream downloads.

## Maintenance notes

### Why the build uses Ubuntu 22.04

DEBs target Ubuntu 22.04 (jammy), the oldest LTS we support, so DEB builds run
in an `ubuntu:22.04` container and produce binaries linked against glibc 2.35.
Compatibility of the produced packages is then verified by installing and
smoke-testing in both `ubuntu:22.04` and `ubuntu:24.04` (the newest LTS we
support).

Building on the default GitHub runner image would link against the runner's
Ubuntu glibc, which is typically newer than the intended jammy floor.

### Why the workflow overlays `.github/packaging` for manual tag builds

`workflow_dispatch` accepts an arbitrary tag, including tags created before DEB
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
document the cutoff explicitly and in the workflow change.

### Signing behavior

DEBs are always built through the signing path.

- **CI release/manual builds** import the real private key from
  `secrets.RPM_GPG_PRIVATE_KEY`
- **local and PR builds** generate an ephemeral RSA key with a known throwaway
  passphrase

This is deliberate: unsigned fast paths tend to rot, while always exercising the
signing path keeps packaging validation closer to what is shipped.

The build exports the matching public key (`GPG-KEY-avalanchego`) next to the
DEBs. On DEB release events (tag push / `workflow_dispatch`), `upload-debs-s3`
publishes it alongside the `.deb`s under
`s3://${BUCKET}/linux/debs/ubuntu/<release>/` so DEB consumers can fetch and
import it before installing. On PR/local builds the key is ephemeral and is not
published.

The GPG key identifies the project, not the package format:
`secrets.RPM_GPG_PRIVATE_KEY` signs RPM, DEB, and macOS zip artifacts. The
secret name predates DEB and macOS zip signing; see [`macos-zip.md`](macos-zip.md)
for the rotation-path notes.

### Validation strategy

The main validation entrypoint is `test-build-debs`, which:

1. builds both DEBs
2. starts fresh `ubuntu:22.04` and `ubuntu:24.04` containers
3. imports the public key
4. verifies DEB signatures
5. installs both DEBs
6. runs smoke tests on the installed binaries

DEB validation runs in both `ubuntu:22.04` (the build/target floor) and
`ubuntu:24.04` (the newest LTS) to catch any compatibility regressions across
the supported Ubuntu range.

The smoke tests intentionally check a small set of high-value invariants:

- the DEB can be installed on the target distro base
- signature verification works
- `avalanchego --version` runs and contains the built commit hash
- the Subnet-EVM plugin is installed at the expected VM-ID-derived path
- the plugin binary also reports the expected commit hash

This does **not** replace broader AvalancheGo test coverage. It is specifically
for validating that the packaging layer produced installable, correctly wired
artifacts for the target runtime.

### Binary linking rationale

DEB builds share the RPM pipeline's **dynamic glibc linking** rationale: the
build runs in a target-distribution container (`ubuntu:22.04` for DEB,
`rockylinux:9` for RPM) so the binary's glibc floor matches the oldest supported
release for that family. The full trade-off discussion (why not static musl, why
not static glibc, container builds as the current stopgap, and the longer-term
hermetic Bazel direction) lives in [`rpm.md`](rpm.md#binary-linking-rationale)
and applies verbatim, with DEB targeting glibc 2.35 / Ubuntu 22.04 rather than
glibc 2.34 / Rocky Linux 9.

### Invariants to preserve

If you change this area, preserve these unless you are intentionally revisiting
the design:

- DEBs install under `/usr/bin` and `/usr/lib` (Debian Policy §9.1.2 forbids
  `/usr/local` for packaged files)
- Subnet-EVM installs under the VM ID from
  `graft/subnet-evm/scripts/constants.sh`
- release builds target glibc 2.35 / Ubuntu 22.04 compatibility
- the same pipeline path is used for both local smoke testing and CI builds
- manual tag builds continue to package historical source trees via the overlay
  mechanism, unless a documented compatibility cutoff is introduced

### Revisit this design if

The current design should be reconsidered if any of these change:

- the supported Ubuntu baseline moves away from 22.04 / glibc 2.35
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
- Builder image: `/.github/packaging/Dockerfile.deb`
- Build script: `/.github/packaging/scripts/build-package.sh`
- Validation script: `/.github/packaging/scripts/validate-deb.sh`
- Shared packaging helpers: `/.github/packaging/scripts/lib-build-common.sh`
- Root task include: `/Taskfile.yml`

### Related documents

- [`rpm.md`](rpm.md) - RPM pipeline, and the shared binary-linking rationale
- [`macos-zip.md`](macos-zip.md) - macOS zip pipeline and shared GPG-key notes
