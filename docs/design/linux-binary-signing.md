# Linux Binary Tarball Signing

## Overview

Linux binary tarballs for avalanchego and subnet-evm are built for amd64
and arm64 by `.github/workflows/build-linux-binaries.yml`. Release builds
produce detached GPG signatures (`.tar.gz.sig`) for each tarball and verify
those signatures before validation and upload.

## Build Flow

The workflow runs a matrix over native Linux runners:

| Architecture | Runner |
|--------------|--------|
| amd64 | `ubuntu-22.04` |
| arm64 | `ubuntu-22.04-arm` |

The release workflow writes `secrets.RPM_GPG_PRIVATE_KEY` to a temporary
file and passes that path as `GPG_KEY_FILE`. It also passes
`secrets.RPM_GPG_PASSPHRASE` as `GPG_PASSPHRASE`.

Tarball tasks are owned by `.github/packaging/Taskfile.yml`:

1. `build-tarballs` builds avalanchego and subnet-evm inside the
   Ubuntu 22.04 tarball builder image.
2. `build-tgz.sh` imports the private key into a temporary `GNUPGHOME`,
   signs each tarball with `gpg --detach-sign`, verifies each signature,
   and exports `GPG-KEY-avalanchego` for later verification.
3. `validate-tarballs` requires both tarballs, both signatures, and the
   exported public key. It verifies signatures in a fresh `ubuntu:22.04`
   container before extracting the tarballs and smoke testing `--version`.
4. `upload-tarballs` requires the exact expected tarballs, signatures, and
   public key. It verifies the signatures again before uploading the exact
   tarball and signature files to S3.

## Artifact Names

For each release tag, the Linux binary workflow produces:

- `avalanchego-linux-amd64-$VERSION.tar.gz`
- `avalanchego-linux-amd64-$VERSION.tar.gz.sig`
- `avalanchego-linux-arm64-$VERSION.tar.gz`
- `avalanchego-linux-arm64-$VERSION.tar.gz.sig`
- `subnet-evm-linux-amd64-$VERSION.tar.gz`
- `subnet-evm-linux-amd64-$VERSION.tar.gz.sig`
- `subnet-evm-linux-arm64-$VERSION.tar.gz`
- `subnet-evm-linux-arm64-$VERSION.tar.gz.sig`

`GPG-KEY-avalanchego` is generated in `build/tgz` so validation and upload
can verify signatures. It is not uploaded as a per-release tarball artifact.

## Failure Behavior

Release signing fails closed:

- If `GPG_KEY_FILE` is set but empty, `build-tgz.sh` exits before building.
- If `GPG_KEY_FILE` is set but `GPG_PASSPHRASE` is empty or unset,
  `build-tgz.sh` exits before building.
- `validate-tgz.sh` fails unless both signatures and `GPG-KEY-avalanchego`
  are present.
- `upload-tgz.sh` fails unless both signatures and `GPG-KEY-avalanchego`
  are present and both signatures verify.

Unsigned local tarball builds are still possible by omitting `GPG_KEY_FILE`,
but unsigned output is only suitable for local development. It will not pass
the validation or upload tasks used by the release workflow.

## Architecture Handling

Tarball artifact architecture is derived from the host runner architecture:
`x86_64` maps to `amd64`, and `arm64`/`aarch64` maps to `arm64`. The builder
image rejects a `DOCKER_DEFAULT_PLATFORM` that does not match the host
architecture, so release tarballs are not cross-built or mislabeled.
