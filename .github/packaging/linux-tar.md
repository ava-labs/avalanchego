# Linux tarball packaging

## Overview

This document covers the Linux binary tarball packaging implementation for
AvalancheGo and Subnet-EVM. It is a sibling to
[`README.md`](./README.md) (which covers RPM packaging) and lives in the
same directory because both flows share the builder-image script, the
packaging Taskfile, and the same release workflow conventions.

The tarball flow exists to ship GPG-signed `*.tar.gz` artifacts and detached
`*.tar.gz.sig` signatures for amd64 and arm64 Linux, parallel to the existing
signed RPMs, without making future maintainers reconstruct the signing model
from CI YAML and shell scripts alone.

This document is for two audiences:

- **users/release engineers** who need to build, validate, or publish signed
  Linux tarballs
- **maintainers** who need to change the tarball packaging implementation
  safely

## Usage

### What gets produced

Per release tag, the workflow produces eight artifacts (two packages, two
architectures, each with a detached signature):

| Artifact | Signature |
| --- | --- |
| `avalanchego-linux-amd64-<TAG>.tar.gz` | `…tar.gz.sig` |
| `avalanchego-linux-arm64-<TAG>.tar.gz` | `…tar.gz.sig` |
| `subnet-evm-linux-amd64-<TAG>.tar.gz` | `…tar.gz.sig` |
| `subnet-evm-linux-arm64-<TAG>.tar.gz` | `…tar.gz.sig` |

The build also exports `GPG-KEY-avalanchego` into `build/tgz/` so that the
validation and upload stages can verify signatures locally. The public key
itself is **not** republished per release — it lives at a stable S3 key.

### Main entrypoints

- CI workflow: `.github/workflows/build-linux-binaries.yml`
- Packaging taskfile: `.github/packaging/Taskfile.yml`
- Builder image: `.github/packaging/Dockerfile.tgz`
- Build script: `.github/packaging/scripts/build-tgz.sh`
- Validation script: `.github/packaging/scripts/validate-tgz.sh`
- Upload script: `.github/packaging/scripts/upload-tgz.sh`
- Local signing harness: `.github/packaging/scripts/test-tgz-signing.sh`

### Local build and validation

Build and validate the full tarball pipeline locally with:

```bash
./scripts/run_task.sh --taskfile .github/packaging/Taskfile.yml test-build-tarballs
```

Or, via the root Taskfile include:

```bash
task packaging:test-build-tarballs
```

Useful environment variables:

- `PACKAGING_TAG` - tag/version label to embed, defaults to `v0.0.0`
- `GPG_KEY_FILE` - path to a real signing key for non-ephemeral signing
- `GPG_PASSPHRASE` - passphrase for the signing key

Without `GPG_KEY_FILE`, the build still produces tarballs but skips signing.
Unsigned local output is suitable only for local development; the
`validate-tarballs` and `upload-tarballs` tasks require signatures and the
exported public key and will fail closed otherwise.

For the upload step:

- `BUCKET` - S3 bucket name
- `TGZ_ARCH` - tarball arch suffix (`amd64` or `arm64`)
- `TGZ_RELEASE` - Ubuntu release segment in the S3 path (e.g. `jammy`)
- `TAG` - release tag

### CI behavior

`.github/workflows/build-linux-binaries.yml` runs on tag push and on
`workflow_dispatch` with an explicit tag. It uses a matrix to build amd64
and arm64 in parallel on native runners:

| Architecture | Runner |
| --- | --- |
| amd64 | `ubuntu-22.04` |
| arm64 | `ubuntu-22.04-arm` |

The workflow writes `secrets.RPM_GPG_PRIVATE_KEY` to a temporary file and
passes it as `GPG_KEY_FILE`, with `secrets.RPM_GPG_PASSPHRASE` as
`GPG_PASSPHRASE`. The same key infrastructure is reused across RPM and
tarball signing on purpose - one key, one publication, one verification
story for consumers.

Each matrix job builds tarballs, verifies signatures, smoke tests the
binaries in a fresh `ubuntu:22.04` container, and uploads the tarballs and
signatures to S3 under `linux/binaries/ubuntu/${TGZ_RELEASE}/${TGZ_ARCH}/`.
Tarballs and signatures are also exposed as GitHub Actions artifacts.

## Conceptual model

The tarball pipeline is split into three scripts, each owning a single
concern:

1. `build-tgz.sh` - inside the builder container: build binaries, sign each
   tarball, verify each signature, and export the public key for the next
   stages.
2. `validate-tgz.sh` - in a fresh `ubuntu:22.04` container with no Go
   toolchain present: import the exported public key, verify both
   signatures, extract the tarballs, and run `--version` smoke tests on the
   binaries.
3. `upload-tgz.sh` - on the runner: re-verify both signatures, then
   `aws s3 cp` exactly the expected `.tar.gz` and `.tar.gz.sig` files. The
   set of files is hard-coded by name rather than discovered with a glob,
   so stray content in `build/tgz/` cannot be published accidentally.

Two architecture naming schemes are in play across the packaging directory
and must stay aligned:

| Context | x86_64 | arm64 |
| --- | --- | --- |
| `uname -m` | `x86_64` | `aarch64` |
| Tarball / Debian / Docker / Go downloads | `amd64` | `arm64` |

The tarball pipeline normalizes host architecture to the `amd64`/`arm64`
convention used in tarball filenames and S3 paths. Cross-architecture
builds are rejected up front by `build-builder-image.sh` (see Maintenance
notes), so the resolved arch is always the host arch.

## Maintenance notes

### Why Ubuntu 22.04 (jammy)

The tarball deployment baseline is Ubuntu 22.04, which ships glibc 2.35.
Builds run inside an `ubuntu:22.04` container so the produced binaries link
against that glibc version. Building on a newer Ubuntu host would risk
linking against a glibc release the deployment target does not provide.

The same general linking rationale used for RPMs applies here -
dynamic-glibc over static-musl, container-as-stopgap, hermetic Bazel as the
longer-term direction. The full reasoning is preserved in
[`README.md`'s "Binary linking rationale"](./README.md#binary-linking-rationale)
and is not duplicated here; revisit decisions about static linking or
toolchain changes should be informed by that section.

### Why detached `.tar.gz.sig` signatures

Detached signatures match the existing RPM convention and let consumers
verify each artifact independently without re-downloading the tarball.
The same private key is used for both RPMs and tarballs, so consumers
need to import only one public key.

### Why cross-architecture Docker builds are rejected

`build-builder-image.sh` rejects a `DOCKER_DEFAULT_PLATFORM` that does not
match the host architecture instead of supporting cross-arch builds.

The reason is that the rest of the packaging pipeline derives architecture
from `uname -m` independently:

- `validate-tgz.sh` derives `TGZ_ARCH` from `uname -m`
- the Taskfile's `PACKAGING_TGZ_HOST_ARCH` is `uname -m`-derived
- the RPM `PACKAGE_ARCH` defaults to `PACKAGING_HOST_ARCH`, also
  `uname -m`-derived

Allowing the builder image's platform to diverge from the host would
mislabel artifacts (e.g. an amd64 binary inside an `…-arm64.tar.gz`),
silently break validation (the validator would search for the wrong
arch), and require threading the resolved target arch end-to-end through
the Taskfile, the build script, and the validator. None of that is worth
the complexity for a use case CI does not exercise: release tarballs are
built per-arch on native runners, not cross-built.

If cross-arch local builds become important, the right shape is to plumb
a single resolved target-arch variable through Taskfile vars, the build
script, and the validator - not to add a per-script override.

### Why signing fails closed

`build-tgz.sh` distinguishes three signing states:

- `GPG_KEY_FILE` unset - unsigned build, local development only.
- `GPG_KEY_FILE` set but the file is empty - **fail** with a clear error
  before any tarball is produced. This protects against a misconfigured CI
  secret (missing or blank value) silently producing unsigned release
  artifacts.
- `GPG_KEY_FILE` set and non-empty - require a non-empty `GPG_PASSPHRASE`
  and sign each archive.

This is deliberate: silently producing unsigned release artifacts because
of a secrets glitch is a much worse failure mode than failing the workflow.

### Validation strategy

`test-build-tarballs` builds tarballs and then validates them in a fresh
`ubuntu:22.04` container with no Go toolchain. The validator:

1. imports `GPG-KEY-avalanchego` exported by the build stage
2. verifies both `*.tar.gz.sig` signatures
3. extracts the tarballs and runs `--version` smoke tests on the binaries
4. confirms each binary reports the expected commit hash

This intentionally exercises the same artifact path consumers would, on a
clean Linux base, with only the verification toolchain present.

A standalone signing-specific harness lives at
`.github/packaging/scripts/test-tgz-signing.sh` for focused signing-path
regression testing without rebuilding binaries.

### Why explicit per-file uploads instead of `find -exec`

`upload-tgz.sh` uses four explicit `aws s3 cp` invocations keyed on
`${TGZ_ARCH}` and `${TAG}`, each gated by `require_file`. An earlier
`find -name '*.tar.gz' -exec aws s3 cp …` shape was rejected: a sweep
would publish whatever happens to be sitting in `build/tgz/`, which is
not safe for a release-bucket destination. Each artifact is also signature
verified again immediately before upload.

### Why `TGZ_RELEASE` is a required env var

The S3 path is `s3://${BUCKET}/linux/binaries/ubuntu/${TGZ_RELEASE}/${TGZ_ARCH}/`.
`TGZ_RELEASE` is **required** at the call site (no default) so that moving
the build runner to a newer Ubuntu release (`noble`, etc.) is a one-line
workflow-YAML change rather than a script edit. The workflow currently sets
`TGZ_RELEASE: jammy`, matching the `ubuntu-22.04` runner family.

### Invariants to preserve

If you change this area, preserve these unless you are intentionally
revisiting the design:

- each `*.tar.gz` ships alongside a matching `*.tar.gz.sig`
- `GPG-KEY-avalanchego` distribution stays on its stable S3 key, not
  republished per release
- release builds always exercise the signing path (no "skip signing in
  CI" branch)
- the S3 upload list stays explicit per file, not a glob over
  `build/tgz/`
- the builder image's `--platform` stays pinned to the host arch and
  cross-arch builds remain rejected up front
- the same Taskfile path is used for local validation and CI builds

### Revisit this design if

The current design should be reconsidered if any of these change:

- the Ubuntu deployment baseline moves away from jammy/glibc 2.35
- cross-architecture local builds become a documented use case (in
  which case the resolved target-arch should be threaded end-to-end,
  not added as a per-script override)
- the GPG key distribution model changes (e.g. multiple keys, per-product
  keys, or a key-server approach replacing the stable S3 location)
- the tarball validation surface grows beyond `--version` smoke testing
  and needs separate per-product harnesses
- distribution moves beyond S3 + GitHub Actions artifacts

## References

### Repository entrypoints

- Workflow: `/.github/workflows/build-linux-binaries.yml`
- Taskfile: `/.github/packaging/Taskfile.yml`
- Builder image: `/.github/packaging/Dockerfile.tgz`
- Builder-image build script: `/.github/packaging/scripts/build-builder-image.sh`
- Build script: `/.github/packaging/scripts/build-tgz.sh`
- Validation script: `/.github/packaging/scripts/validate-tgz.sh`
- Upload script: `/.github/packaging/scripts/upload-tgz.sh`
- Signing harness: `/.github/packaging/scripts/test-tgz-signing.sh`
- Root task include: `/Taskfile.yml`

### Sibling packaging documentation

- RPM packaging: [`./README.md`](./README.md) - rationale for the
  containerized build, signing-path-always-exercised policy, and the
  detailed dynamic-glibc vs static-musl trade-off discussion.

### Related release documentation

- Release process: `/RELEASING_README.md`
