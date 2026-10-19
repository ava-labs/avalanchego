# Bazel multi-architecture image validation

## Contents

- [Overview](#overview)
- [Run the validation](#run-the-validation)
- [Validation model](#validation-model)
- [CI caches](#ci-caches)
- [Maintainer rules](#maintainer-rules)
- [Validation checklist](#validation-checklist)

## Overview

This validation builds AvalancheGo and Subnet-EVM images with Bazel. It
validates Linux amd64 and Linux arm64 images.

This path does not publish a production image. The Dockerfile and Buildx paths
remain independent comparison paths.

The AvalancheGo validation checks:

- the amd64 and arm64 AvalancheGo binaries;
- the ELF machine type of each binary;
- an OCI index with amd64 and arm64 manifests; and
- `avalanchego --version` in each binary and final image.

The Subnet-EVM validation checks the same platform index properties. It extracts
the plugin from its packaged layer. It checks the plugin ELF machine type. It
runs `plugin --version` in a Debian container and in each final image.

## Run the validation

Run these commands from the repository root:

```text
task bazel-build-image-avalanchego
task bazel-test-build-image-avalanchego
task bazel-build-image-subnet-evm
task bazel-test-build-image-subnet-evm
```

The two build tasks build their OCI indexes. They do not start a registry or
run containers.

The two test tasks start a local registry. They run direct binaries or plugins
and the final images for both platforms.

The tests require a Linux Docker host, the Docker socket, host networking, and
QEMU. QEMU runs only the non-native runtime image. Bazel always runs in a
native builder container.

## Validation model

The script selects a Debian Bookworm builder for the host architecture. The
builder uses its native GCC for that architecture. It uses Debian's cross
compiler for the other architecture.

The outer Bazel process builds and loads the builder. The inner Bazel process
runs `//bazel/image:push_avalanchego` once. Its `image_from_binary` dependency
builds both target binaries in one Bazel analysis.

The AvalancheGo script reads the OCI index and the push target runfiles. It
extracts each binary from its exact runtime layer. It checks the binary before
it tests the final image.

The Subnet-EVM script uses the same builder and platform transition. Its image
uses the platform-matched AvalancheGo manifest as its base. It adds the
platform-matched plugin at:

```text
/avalanchego/build/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
```

The script extracts the plugin from that final layer. It does not copy a plugin
through a Docker staging directory.

Test mode pushes each index to a disposable registry at `localhost:5000`. It
inspects the index and runs both final image platforms.

Local AvalancheGo validation stores outer and inner Bazel data in
`.cache/bazel-image/`. Local Subnet-EVM validation uses
`.cache/bazel-subnet-evm-image/`. Set `BAZEL_IMAGE_CACHE_ROOT` to select another
location. Keep `.cache` in `.bazelignore`. Bazel metadata generation must not
scan nested Bazel output roots.

Set `BAZEL_IMAGE_REPOSITORY_CACHE` to reuse a prepared outer repository cache.
CI sets this value for the Bazel dependency cache.

## CI caches

The Linux `build-avalanchego-image` job runs
`bazel-test-build-image-avalanchego`. The Linux `build-subnet-evm-image` job
runs `bazel-test-build-image-subnet-evm`. Both jobs are in
`.github/workflows/bazel.yml`. They run after `setup`, so they run in parallel.
They set up QEMU and use the Docker disk-space action. Both jobs are part of
`bazel-required`.

The setup job fetches the builder and image targets. It stores downloaded Bazel
repositories. It does not store compiled action outputs.

The remote Bazel cache stores binary action outputs. The validation script
passes remote-cache settings into the inner builder only when CI supplies both
values. OCI layers, manifests, indexes, pushes, and the builder use
`no-remote-cache`. These outputs are large or local-only.

Both image jobs restore the same Docker image archive. The archive contains
the native Bazel builder, the local registry, and both Debian runtime bases.
The cache key includes the runner architecture and all archive inputs. Only
`master` writes a new archive. Pull requests can restore it without downloading
these images. The jobs do not share local Bazel outputs. They reuse remote
Bazel action-cache entries when the action inputs match.

Do not add remote-cache or registry credentials to the builder image, workspace,
or command output.

## Maintainer rules

Keep the Bazel AvalancheGo image comparable with the Dockerfile image. Check
both platforms after changes to the base image, executable path, working
directory, command, entrypoint, or platform list.

Keep the Subnet-EVM plugin path and VM ID. Keep the platform-matched
AvalancheGo base. Do not build a plugin outside Bazel and copy it into a Docker
staging directory.

Keep the direct binary or plugin checks and the final-image checks. A successful
Bazel build alone does not prove that the image contains the correct binary or
plugin.

The local registry is a test input. Do not use its address, insecure transport,
or host networking as a production publishing design.

Do not replace the Dockerfile and Buildx path as part of this validation work.
Production publishing needs a separate decision about names, tags, signing,
credentials, retention, and ownership.

## Validation checklist

Run these checks after changes to the image rules, script, or CI workflow:

```text
bash -n scripts/bazel_image_spike.sh
bash -n scripts/bazel_subnet_evm_image.sh
task bazel-build-image-avalanchego
task bazel-test-build-image-avalanchego
task bazel-build-image-subnet-evm
task bazel-test-build-image-subnet-evm
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
