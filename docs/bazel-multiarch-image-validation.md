# Bazel multi-architecture AvalancheGo image validation

## Contents

- [Overview](#overview)
- [Run the validation](#run-the-validation)
- [Validation model](#validation-model)
- [CI caches](#ci-caches)
- [Maintainer rules](#maintainer-rules)
- [Validation checklist](#validation-checklist)

## Overview

This validation builds the AvalancheGo runtime image with Bazel. It validates
Linux amd64 and Linux arm64 images.

This path does not publish a production image. The Dockerfile and Buildx path
remains an independent comparison path.

The validation checks:

- the amd64 and arm64 AvalancheGo binaries;
- the ELF machine type of each binary;
- an OCI index with amd64 and arm64 manifests; and
- `avalanchego --version` in each binary and final image.

## Run the validation

Run these commands from the repository root:

```text
task bazel-build-image
task bazel-test-build-image
```

`bazel-build-image` builds the OCI index. It does not start a registry or run
containers.

`bazel-test-build-image` also starts a local registry. It runs the direct
binaries and the final images for both platforms.

The test requires a Linux Docker host, the Docker socket, host networking, and
QEMU. QEMU runs only the non-native runtime image. Bazel always runs in a
native builder container.

## Validation model

The script selects a Debian Bookworm builder for the host architecture. The
builder uses its native GCC for that architecture. It uses Debian's cross
compiler for the other architecture.

The outer Bazel process builds and loads the builder. The inner Bazel process
runs `//bazel/image:push_avalanchego` once. Its `image_from_binary` dependency
builds both target binaries in one Bazel analysis.

The script reads the OCI index and the push target runfiles. It extracts each
binary from its exact runtime layer. It checks the binary before it tests the
final image.

Test mode pushes the index to a disposable registry at `localhost:5000`. It
inspects the index and runs both final image platforms.

Local validation stores outer and inner Bazel data in `.cache/bazel-image/`.
Set `BAZEL_IMAGE_CACHE_ROOT` to select another location. Keep `.cache` in
`.bazelignore`. Bazel metadata generation must not scan nested Bazel output
roots.

Set `BAZEL_IMAGE_REPOSITORY_CACHE` to reuse a prepared outer repository cache.
CI sets this value for the Bazel dependency cache.

## CI caches

The Linux `build-avalanchego-image` job in `.github/workflows/bazel.yml` runs
`bazel-test-build-image`.

The setup job fetches the builder and image targets. It stores downloaded Bazel
repositories. It does not store compiled action outputs.

The remote Bazel cache stores binary action outputs. The validation script
passes remote-cache settings into the inner builder only when CI supplies both
values. OCI layers, manifests, indexes, pushes, and the builder use
`no-remote-cache`. These outputs are large or local-only.

The job also restores a Docker image archive. The archive contains the native
Bazel builder, the local registry, and both Debian runtime bases. The cache key
includes the runner architecture and all archive inputs. Only `master` writes a
new archive. Pull requests can restore it without downloading these images.

Do not add remote-cache or registry credentials to the builder image, workspace,
or command output.

## Maintainer rules

Keep the Bazel AvalancheGo image comparable with the Dockerfile image. Check
both platforms after changes to the base image, executable path, working
directory, command, entrypoint, or platform list.

Keep the direct binary checks and the final-image checks. A successful Bazel
build alone does not prove that the image contains the correct binary.

The local registry is a test input. Do not use its address, insecure transport,
or host networking as a production publishing design.

Do not replace the Dockerfile and Buildx path as part of this validation work.
Production publishing needs a separate decision about names, tags, signing,
credentials, retention, and ownership.

## Validation checklist

Run these checks after changes to the image rules, script, or CI workflow:

```text
bash -n scripts/bazel_image_spike.sh
task bazel-build-image
task bazel-test-build-image
task lint-shell
task lint-action
task bazel-fmt
git diff --check
```

Run `task bazel-cache-ci-build-dependencies` after changes to the CI dependency
list or image targets.

For a Docker archive cache change, run a trusted cache-fill job. Then run an
unchanged job. The second job must load the archive without pulling its cached
images.

For a remote action-cache change, run an unchanged job after a cache-fill job.
The inner Bazel summary must report remote cache hits for binary actions.
