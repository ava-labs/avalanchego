# DEB Packaging for AvalancheGo

## Overview

Ship signed DEB packages for `avalanchego` and `subnet-evm`.
Packages are built inside an Ubuntu 22.04 container, signed with GPG,
and published as GitHub Actions artifacts and Ubuntu S3 assets.

For shared packaging background, see
[RPM Packaging](./rpm-packaging.md).

## Decisions

- **Target distro:** Ubuntu 22.04 (jammy) build environment.
  Container base image: `ubuntu:22.04`.
- **Target architectures:** `amd64` and `arm64`.
- **Binary linking:** Dynamic linking against glibc.
  See [Binary Linking](./rpm-packaging.md#binary-linking) in the RPM
  packaging doc for shared rationale.

## Packaging

### Packages

Two DEB packages:

| Package | Install path |
|---------|-------------|
| avalanchego | `/usr/local/bin/avalanchego` |
| subnet-evm | `/usr/local/lib/avalanchego/plugins/<VM_ID>` |

Both declare `libc6 (>= 2.34)` as a dependency. The subnet-evm plugin
path uses the VM ID from `graft/subnet-evm/scripts/constants.sh`.

### Build tooling

Packages are built with [nfpm](https://nfpm.goreleaser.com/) inside an
Ubuntu 22.04 container. The build is orchestrated by
`.github/packaging/Taskfile.yml` and uses `Dockerfile.deb` plus the
shared packaging scripts under `.github/packaging/scripts/`.

### GPG signing

Shared key handling matches
[RPM Packaging / GPG signing](./rpm-packaging.md#gpg-signing).

DEBs are signed after `nfpm` produces the package, using `dpkg-sig`.
This is required because `nfpm` inline signing is not compatible with
`dpkg-sig --verify`.

### Version smoke test

DEB validation uses the same version and commit-hash smoke test
described in
[RPM Packaging / Version smoke test](./rpm-packaging.md#version-smoke-test).

### CI workflow

`.github/workflows/build-deb-release.yml` uses the same trigger model as
[RPM Packaging / CI workflow](./rpm-packaging.md#ci-workflow).

DEB-specific behavior:

1. Build and validate via
   `scripts/run_task.sh --taskfile .github/packaging/Taskfile.yml test-build-debs`
2. Upload DEBs and the exported GPG public key as artifacts on non-PR runs
3. Upload release artifacts to
   `linux/debs/ubuntu/{jammy,noble}/{arch}/` in S3

### Architecture mapping

| `uname -m` | Docker `TARGETARCH` | DEB arch |
|-------------|-------------------|----------|
| `x86_64` | `amd64` | `amd64` |
| `arm64` / `aarch64` | `arm64` | `arm64` |

## Binary Linking

Shared glibc and container-build rationale is documented in
[RPM Packaging / Binary Linking](./rpm-packaging.md#binary-linking).

DEB-specific validation differs in two places:

- Packages are built in `ubuntu:22.04`
- Validation runs in both `ubuntu:22.04` and `ubuntu:24.04`
- Signature verification runs only in `ubuntu:22.04` because
  `dpkg-sig` is not available in Ubuntu 24.04
