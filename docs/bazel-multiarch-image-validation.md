# Bazel multi-architecture image validation

## Contents

- [Overview](#overview)
- [Use the validation](#use-the-validation)
- [How the validation works](#how-the-validation-works)
- [CI behavior](#ci-behavior)
- [Limits and safe changes](#limits-and-safe-changes)
- [Validation checklist](#validation-checklist)

## Overview

This validation checks the Bazel image path for Linux amd64 and arm64.

The path is a local-validation spike. It is not a production publishing
interface. The Dockerfile and Buildx path remains an independent comparison
path.

The Bazel path checks these outputs:

- amd64 and arm64 AvalancheGo binaries;
- the runtime behavior of both binaries;
- one OCI index with amd64 and arm64 manifests; and
- the runtime behavior of both image platforms.

## Use the validation

Use these Task entrypoints from the repository root:

```text
task bazel-build-image
```

This command builds the pinned builder image, both binaries, and the combined
OCI image index. It does not start a registry or run containers.

```text
task bazel-test-build-image
```

This command performs the build and the runtime checks. It starts a disposable
registry on `localhost:5000`. It inspects the index and runs both platforms
with explicit platform selections.

The test path needs a Linux Docker host. It needs the Docker socket, host
networking, and QEMU support. These settings exist only for local validation.

## How the validation works

The outer Bazel invocation builds the locked Debian Bookworm builder image.
The script loads that image into Docker. The builder contains Bazel and the
Debian cross compiler.

The script then starts the builder and runs Bazel twice:

1. It builds the amd64 binary with the native Debian toolchain.
2. It builds the arm64 binary with the registered Debian aarch64 toolchain.

The script checks the ELF machine value before it runs each binary. It runs
each binary in a fresh Debian 12 slim container.

A separate Bazel invocation builds the runtime image target. The target uses
`image_from_binary` for both platforms. The `image_push` target sends the
resulting index to the disposable registry. The script uses
`docker buildx imagetools inspect` to inspect the index. It then runs the
index once for each explicit platform.

The `push_avalanchego` target is a local validation action. Its name identifies
the executable `image_push` target. It does not define production publishing
semantics.

The builder and inner Bazel process use separate disk and output caches. The
outer repository cache can use `BAZEL_IMAGE_REPOSITORY_CACHE`. CI sets this
value to the repository cache prepared by the Bazel setup job.

## CI behavior

The Linux `image` job in `.github/workflows/bazel-ci.yml` runs
`bazel-test-build-image`. The job sets up QEMU and uses the Docker disk-space
action. The job is part of `bazel-required`.

The Bazel dependency list includes the builder and image-push targets. The
setup job fetches these targets before the image job. This avoids making the
image job fetch the builder dependencies for the first time.

The normal Dockerfile/Buildx image validation remains in Go CI. Do not remove
that path or treat the two paths as the same test. Compare their results when
the Bazel image job changes.

## Limits and safe changes

Do not generalize the local registry settings. The following values are local
test details:

- `localhost:5000`;
- `IMG_INSECURE=1`;
- the `--insecure` push option; and
- Docker host networking.

Production image publishing still needs decisions about the destination name,
tags and stamping, TLS, authentication, credentials, retention, and ownership.
This validation does not make those decisions.

Keep the runtime image comparable with the Dockerfile image. Changes to the
base digest, executable path, working directory, command, entrypoint, or
platform list need runtime checks for both platforms and a comparison with the
Dockerfile path.

Keep the direct binary checks. Keep the index metadata check. Keep the two
explicit image-platform checks. Do not replace these checks with only a
successful Bazel build.

The Docker socket gives the outer process access to Docker. It does not define
a registry credential model. Do not use it as a reason to add credentials or
publishing behavior to this spike.

## Validation checklist

Run the following checks after changes to the Bazel image path or its CI job:

```text
task bazel-build-image
task bazel-test-build-image
bash -n scripts/bazel_image_spike.sh
shellcheck scripts/bazel_image_spike.sh scripts/tests.build_image.sh
git diff --check
```

Also run Actionlint and Buildifier when the change modifies a workflow or a
Bazel BUILD file.

A successful test reports both Linux platforms in the OCI index. It reports a
successful `--version` run for both direct binaries and both image platforms.
