# Bazel multi-architecture AvalancheGo image validation

## Contents

- [Overview](#overview)
- [Run the validation](#run-the-validation)
  - [Find the outputs](#find-the-outputs)
- [Validation model](#validation-model)
  - [Why use a builder container?](#why-use-a-builder-container)
- [CI caches](#ci-caches)
- [Maintainer rules](#maintainer-rules)
  - [Update dependencies](#update-dependencies)
- [Validation checklist](#validation-checklist)
  - [Check builder reproducibility](#check-builder-reproducibility)

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
task bazel-build-image-avalanchego
task bazel-test-image-avalanchego
```

Both tasks run the native builder container. `bazel-build-image-avalanchego`
builds the OCI index. It does not load the runtime image into Docker, start a
registry, or run runtime containers.

`bazel-test-image-avalanchego` also starts a local registry. It runs the direct
binaries and the final images for both platforms.

Run both tasks on Linux amd64 or arm64 with these prerequisites:

- a local Docker daemon with access to `/var/run/docker.sock` and host networking;
- the Docker CLI, `bazelisk`, Git, and `jq` on `PATH`; and
- `readelf` from GNU Binutils on `PATH` for the ELF machine checks.

Test mode also requires the Docker Buildx plugin and QEMU registered with the
Docker host to run non-native binaries. The inner Bazel process always runs
in a native builder container.

See [development environment setup](../CONTRIBUTING.md#nix) for the repository's
Nix environment. It supplies Bazelisk, Git, and `jq`. It does not explicitly
supply Docker, the Buildx plugin, or GNU Binutils. These host tools must remain
available when the tasks enter the development environment.

Local runs allow dependency downloads by default. To check the CI preparation
boundary locally, run:

```sh
task bazel-prepare-image-inputs
BAZEL_IMAGE_DISABLE_DOWNLOADS=true task bazel-test-image-avalanchego
```

The first command prepares inputs without compiling AvalancheGo. The second
command builds and tests with repository and Go module downloads disabled.
Missing external Docker images also cause a failure instead of a pull.

### Find the outputs

Both tasks keep their outputs under `.cache/bazel-image/` by default.
`BAZEL_IMAGE_CACHE_ROOT` changes this location. In the paths below, `<host>` is
`amd64` or `arm64`, matching the builder's architecture.

| Output | Location under the cache root |
|---|---|
| Extracted amd64 binary | `image-binaries-<host>/amd64/avalanchego` |
| Extracted arm64 binary | `image-binaries-<host>/arm64/avalanchego` |
| Inner Bazel log for build-only mode | `image-true.log` |
| Inner Bazel log for test mode | `image-false.log` |
| OCI index and associated Bazel outputs | Under `inner-output-<host>-image/` |

An OCI index lists the platform-specific image manifests. To find the generated
index on the host, run:

```sh
find "${BAZEL_IMAGE_CACHE_ROOT:-.cache/bazel-image}" -type f -name avalanchego_index.json
```

The index alone is not a runnable image or a Docker archive. These tasks provide
validation outputs, not an image export or production publishing interface.

Test mode pulls each platform into Docker under
`localhost:5000/avalanchego-bazel:test`. With Docker's classic image store, that
tag retains only arm64 after success because the script tests arm64 last.
Do not rely on this tag to retain both platforms.
The script stops the disposable registry on exit, so further pulls require
another validation run.

## Validation model

The script selects an Ubuntu 24.04 (Noble) builder for the host architecture.
The builder uses its native GCC for that architecture. It uses Ubuntu's cross
compiler for the other architecture.

The outer Bazel process resolves and loads the current builder on every run.
An existing Docker tag does not prove that the builder matches the source.
The loader can reuse image layers from the Docker archive.
The inner Bazel process runs `//bazel/image:push_avalanchego` once. Its `image_from_binary` dependency
builds both target binaries in one Bazel analysis.

The script reads the OCI index and the push target runfiles. It extracts each
binary from its exact runtime layer. It checks the binary before it tests the
final image.

Test mode pushes the index to a disposable registry at `localhost:5000`. It
inspects the index and pulls each platform image from that registry.
The smoke test reads the image's configured command and appends `--version`.
It preserves the image's entrypoint and working directory.

Local validation stores the outer repository and disk caches in
`.cache/bazel-image/`. It also stores inner Bazel data and validation outputs
there. Set `BAZEL_IMAGE_CACHE_ROOT` to an absolute path to select another location.
The outer Bazel process uses batch mode and keeps its output root under
`outer-output/` within that cache root.
Keep `.cache` in `.bazelignore`. Bazel metadata generation must not scan nested
Bazel output roots.

Set `BAZEL_IMAGE_REPOSITORY_CACHE` to reuse a prepared outer repository cache.
CI points this value at the separate outer image repository cache.

### Why use a builder container?

AvalancheGo uses CGO, which lets Go code call C code. Compilation therefore
needs C compilers, development headers, and libraries, not only a Go toolchain.
The builder container supplies these files from locked Ubuntu 24.04 packages.
It also contains Bazel and the tools needed to prepare dependencies.

Docker runs only on the host. The builder contains no Docker installation and
receives no Docker socket or socket-group access. The image-push tool sends OCI
content directly to the disposable registry over the network.

Ubuntu 24.04 supplies both native and cross-compilers as a working environment.
This avoids dependence on the host's C compiler and libraries, including those
supplied by Nix. Using Ubuntu 24.04 for compilation and runtime keeps the
binaries compatible with the runtime's glibc, Ubuntu's C library.

The builder and runtime images are separate. The runtime images contain Ubuntu
24.04 and the compiled AvalancheGo binary. They do not contain the builder's
compilers or caches.

The [C toolchain definitions](../bazel/toolchains/BUILD.bazel) refer to compiler
paths inside the builder. They do not supply compiler files themselves. The
[build script](../scripts/build_image_bazel.sh) passes Docker's builder image ID
to the `//bazel/toolchains:builder_digest` build setting. A rule writes that
value to `builder_identity.txt`, which the toolchains declare as an input.
Go's CGO compilation, standard-library, and linking actions consume this file.
A changed identity therefore invalidates disk and remote action-cache results.
Pure Go actions depend on the resulting standard library and compiled packages.

Builder execution platforms select these toolchains for both native and cross
compilation. Ordinary host builds do not select the container-specific toolchains.
Passing the digest only through `--action_env` is not sufficient: the Go rules
construct their own action environments and omit that variable.

The image ID identifies the image configuration and its filesystem layers.
Identical builder inputs must produce the same ID on workers with the same
architecture, or those workers cannot share cached compilation results.
Different builder architectures have different IDs. See
[Check builder reproducibility](#check-builder-reproducibility).

An alternative is to declare the compiler and its sysroot as Bazel inputs. A
sysroot contains the target's headers, startup objects, and libraries needed
for compilation and linking. This would move much of the same environment
from a container into Bazel repositories and toolchain definitions.

That alternative must also supply the compiler's own dependencies and configure
its paths. Downloading Ubuntu compiler packages alone does not provide a
toolchain that works from any extraction directory. We would maintain this
configuration unless a maintained upstream toolchain met our requirements.

Consider that alternative if we need remote execution, builds without Docker,
or cache keys based directly on compiler files. The current path uses remote
caching, not remote execution. It uses the container to avoid custom compiler
packaging while preserving an Ubuntu build environment. Revisit this choice
when those requirements change or a maintained upstream toolchain reduces the
maintenance cost.

## CI caches

The Linux `build-image-avalanchego` job in `.github/workflows/bazel.yml` runs
`bazel-test-image-avalanchego`.

[`setup-bazel`](../.github/actions/setup-bazel/action.yml) prepares image inputs
when its `setup-image-inputs` input is `true` on Linux. Both the dedicated setup
job and the image-validation job enable this input. Unit, E2E, and macOS jobs do
not restore the image-specific caches.

The [image-input helper](../.github/actions/setup-bazel-image-inputs/action.yml)
maintains two entries separate from the ordinary Bazel dependency cache:

- outer and inner repository caches, plus a Gazelle Go module cache; and
- a Docker image archive.

Each entry has an exact key and a fallback prefix for the runner's OS and
architecture. On a miss or inexact hit, preparation downloads missing inputs.
The inner builder runs `bazel fetch` for the same configured target as validation,
including both platform transitions. Preparation does not compile AvalancheGo.
Only the dedicated setup job on `master` can save updated entries. Other jobs
prepare missing inputs locally without saving them, as the ordinary Bazel setup does.

The inner builder uses a separate `HOME` and does not inherit the host's
`~/.bazelrc`. The validation script therefore passes `--repository_disable_download`
and `GOPROXY=off` explicitly in CI. It mounts the prepared inner input caches,
not the host's general Gazelle cache. Missing dependencies fail validation rather
than trigger downloads.

The outer image process also uses these separate input caches, so builder
packages do not enlarge the cache restored by unit and E2E jobs. Compiler outputs,
Bazel output roots, and `HOME` are not part of the saved input caches.

These restrictions do not block all network access. Remote action-cache requests
and pushes and pulls through the disposable local registry remain available.
See [Bazel CI download restrictions](./bazel.md#checked-in-bazel-dependency-list).

The remote Bazel cache stores binary action outputs. The validation script
passes remote-cache settings into the inner builder only when caching is enabled
and CI supplies both the URL and authorization header.
`BAZEL_REMOTE_CACHE_ENABLED=false` disables this forwarding, including uploads.
Scheduled CI uses this setting to validate builds without remote action results.
OCI layers, manifests, indexes, pushes, and the builder use `no-remote-cache`.
These outputs are large or local-only.

The Docker archive contains the native Bazel builder, the local registry, and
both Ubuntu runtime bases. Preparation loads the archive even on an exact hit
and resolves the builder through Bazel. An inexact hit reuses unchanged images;
only missing digest-pinned images need external pulls. A changed builder can
reuse existing layers before the helper writes the updated archive.

Validation does not pull missing external images. Docker runs use `--pull=never`;
a missing prepared image causes a failure. The final AvalancheGo images still
come from the disposable local registry, not Docker Hub. The validation wrapper
preserves prepared Docker images during any later
[disk cleanup](./ci-disk-space.md#usage); otherwise cleanup could delete the inputs
that setup just loaded.

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

### Update dependencies

Dependency updates must preserve the links between the builder, runtime bases,
and validation inputs. Use these procedures from the repository root.

**Ubuntu builder packages**

1. Run `task bazel-update-image-inputs`. The task resolves the Ubuntu 24.04 OCI
   index and its Linux amd64 and arm64 manifests.
2. The task uses the index's immutable amd64 creation time for the Ubuntu
   snapshot timestamp. It updates the index, both manifests, and the snapshot
   together. An unchanged index leaves no diff.
3. The task regenerates metadata. Check package versions, architectures, and
   snapshot URLs in `MODULE.bazel.lock`. Commit the generated metadata with the
   source changes.

The manual task is the normal update path. Run it before scheduled releases and
when maintainers decide that a security update is urgent. It does not schedule
checks, create a pull request, or decide update urgency.

Do not add a separate `apt.lock` file with this configuration. The extension
ignores that file when the facts API is available, so it can retain obsolete
packages while the active module lock changes. If a future extension changes
this behavior, check its lockfile API before changing the update procedure.

The `rules_distroless` override supplies `apt.install(mergedusr = True)`.
It uses a checksum-pinned archive of an upstream commit, not a Git checkout.
Bazel stores the archive in its repository cache and applies the dependency-download
restriction to it. A Git override would fetch again in fresh output roots because
the input caches do not contain Git checkouts. Bazel's download restriction does
not stop Git subprocesses.

When changing the override, update the archive URL, integrity checksum, and
extraction prefix together. Remove the override only when a released version
supplies the required feature.

**Bazel version**

1. Update `.bazelversion` and both Bazel downloads in `MODULE.bazel` together.
   Update the download names, URLs, and SHA-256 checksums for amd64 and arm64.
2. Update the matching download labels in
   [`bazel/image/BUILD.bazel`](../bazel/image/BUILD.bazel).
3. Run `task bazel-generate-metadata`. Check the generated module metadata.
   Commit it with the version changes.

The builder uses the full Bazel distribution, including its JRE. It must use
the version selected by `.bazelversion`, not download a launcher at runtime.

**Ubuntu runtime base**

The update task maintains `ubuntu_24_04` in `MODULE.bazel` and the matching
manifest references in `scripts/build_image_bazel.sh`. The direct binary checks
must use the same Ubuntu manifests as the final images. Otherwise, these checks
can miss runtime library incompatibilities. Keep the builder packages compatible
with the runtime's glibc when changing Ubuntu releases. An LTS release change is
a separate decision. Compare the result with the Dockerfile image too.

The Docker archive key includes the builder rules, module metadata, Bazel version,
and build script. See the
[cache-key script](../scripts/workflow-avalanchego-image-docker-cache-key.sh).
Keep its input lists current if inputs move to other files. The inner dependency
key also includes the Go module graph, BUILD and `.bzl` files, Bazel configuration,
and preparation helper.
The image script resolves the builder even when its Docker tag already exists.
The resulting builder digest changes the inner actions' cache keys.

After each dependency update, run the validation checklist below.
Check both native builder architectures before merging compiler or Bazel updates.

## Validation checklist

Run these checks after changes to the image rules, script, or CI workflow:

```text
bash -n scripts/build_image_bazel.sh
task test-bazel-image-cache-keys
task bazel-build-image-avalanchego
task bazel-test-image-avalanchego
task lint-shell
task lint-action
task bazel-fmt
git diff --check
```

Run `task bazel-cache-ci-build-dependencies` after changes to the general CI
dependency list. Run `task bazel-prepare-image-inputs` after changes to image targets.

After `task bazel-prepare-image-inputs`, run
`task test-bazel-image-builder-cache`. It first checks that the builder has no
Docker installation or mounted Docker socket. It then inspects the actual image
action graph for both target architectures. It checks that native and cross-compilation
actions declare the builder identity file as an input. It also checks that the
file contains the selected digest. Repeating a digest must produce the same
action summary; changing it must change the summary. This check does not require
remote-cache credentials and does not compile AvalancheGo.

Run `task test-bazel-image-inputs` to check preparation with real Docker and Bazel.
It downloads inputs, restores only input caches into a fresh output root, and
builds with dependency downloads disabled. It also checks that missing inner
inputs cause a failure. The test needs the same host tools as build-only mode.

After preparing image inputs, run `task test-bazel-image-repository-cache` for a
focused check of the upstream override. This check uses fresh output roots in
containers with networking disabled. Prepared repository inputs must resolve the
override; an empty cache must fail with downloads disabled, without a Git fetch.
The image-input integration test also runs this check. Network isolation applies
only to this focused test, not to normal image validation.

For an image input-cache change, check exact hits, inexact hits, and empty caches
in CI. On an inexact hit, preparation must reuse old inputs and fetch missing ones.
On an exact hit, it must load the Docker archive without pulling external images.
Validation must succeed without dependency downloads after either preparation path.
Check that only the dedicated setup job on `master` writes updated cache entries.

For a remote action-cache change, run an unchanged job after a cache-fill job.
The inner Bazel summary must report remote cache hits for binary actions.
Also run with `BAZEL_REMOTE_CACHE_ENABLED=false` while credentials remain available.
The inner build must not read or write remote action results.

### Check builder reproducibility

Prepare inputs first with `task bazel-prepare-image-inputs`. Then build the
native builder from two fresh output roots, with disk and remote action caches
disabled. Reusing downloaded repository inputs is permitted; reusing built
layers would not test reproducibility.

```sh
case "$(uname -m)" in
  x86_64) architecture=amd64 ;;
  aarch64 | arm64) architecture=arm64 ;;
  *) exit 1 ;;
esac
cache_root="${BAZEL_IMAGE_CACHE_ROOT:-$PWD/.cache/bazel-image}"
outputs="$(mktemp -d "$cache_root/reproducibility.XXXXXX")"
for run in first second; do
  bazelisk --batch --output_user_root="$outputs/$run" run \
    "//bazel/image:load_builder_$architecture" \
    --repository_cache="${BAZEL_IMAGE_REPOSITORY_CACHE:-$cache_root/outer-repository}" \
    --disk_cache= --remote_cache= --remote_upload_local_results=false
  docker image inspect avalanchego-bazel-builder:local --format '{{.Id}}' \
    > "$outputs/$run.digest"
done
cmp "$outputs/first.digest" "$outputs/second.digest"
```

Also run this check on an independent worker of the same architecture and
compare the image IDs. A repeat on one worker does not establish cross-worker
reproducibility. Run each builder architecture separately; their IDs need not match.
If identical inputs produce different IDs, find the varying input or image
metadata before relying on shared binary-cache results.
