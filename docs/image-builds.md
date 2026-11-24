# Container image builds

## Table of contents

- [Overview](#overview)
- [Supported tasks](#supported-tasks)
- [Bazel-built AvalancheGo image](#bazel-built-avalanchego-image)
- [Conceptual model](#conceptual-model)
- [Go module dependencies](#go-module-dependencies)
  - [Host cache preparation](#host-cache-preparation)
  - [Dockerfile requirements](#dockerfile-requirements)
- [Design constraints and alternatives](#design-constraints-and-alternatives)
- [Manual verification](#manual-verification)
- [When to revisit](#when-to-revisit)

## Overview

The repository builds container images for AvalancheGo, Subnet-EVM, XSVM, and
Antithesis test setups. Repository tasks provide the standard interface for
these builds. Product-specific scripts configure Buildx and supply the inputs
that each image needs.

Go build instructions outside Bazel compile code without network access. The build scripts
provide a local file proxy from the module cache for Go tasks outside Bazel.
This separates dependency preparation from compilation in Dockerfiles. Other
image-build operations, such as base-image pulls and package installation, can
still require the network.

## Supported tasks

Use these repository tasks to build images:

```sh
task build-image
task bazel-build-image-avalanchego
task build-xsvm-image
task build-subnet-evm-image
task build-antithesis-images-avalanchego
task build-antithesis-images-subnet-evm
```

Each task starts the product-specific build script in the repository's
configured environment.

## Bazel-built AvalancheGo image

`task bazel-build-image-avalanchego` builds AvalancheGo with Bazel and packages
it in a separate `debian:12-slim` runtime image. It then smoke-tests the image.
The script builds Linux images for the host architecture, either amd64 or
arm64. It rejects other architectures instead of cross-compiling.

A multi-stage Docker build copies the source into a builder stage and runs
Bazel there. BuildKit cache mounts persist Bazel's output tree and disk cache
across builds. The runtime image contains the executable, not the builder
filesystem or toolchain.

This CI job validates the Bazel build path, not just whether the image runs.
Bazel runs on every image build so a cached Docker layer cannot bypass the
current cache settings or dependency-download restrictions. Bazel can still
reuse its own cached results; this is not a clean compilation on every run.

The default image is `avalanchego:bazel-local`, distinct from the Go/Docker
image's tags. Both build paths remain available for comparison. Set
`BAZEL_IMAGE` to choose a different image reference:

```sh
BAZEL_IMAGE=avalanchego:bazel-dev task bazel-build-image-avalanchego
```

The image task runs `--version` and checks the reported source commit. It
copies any existing `.bazelrc.local` into a temporary configuration. Local
builds add the image's dependency-cache settings; CI inherits the settings
written by `setup-bazel` unchanged. BuildKit mounts this
configuration as a secret, without copying credentials into an image layer or
changing the checkout's configuration.

Installing Bazel inside the Linux builder keeps preparation independent of
the host operating system's Bazel installation. Keep its installation layer
separate from the source copy so source changes do not trigger another download.

Outside CI, a `prepare` stage in the Dockerfile fetches dependencies inside the
Linux container, with network access, on any host. Bazel repository rules fetch
toolchains for the platform they run on, so host preparation would only work on
Linux. The stage runs `scripts/cache_bazel_ci_build_dependencies.sh`, the same
script `setup-bazel` uses, with the installed Bazel executable and a fresh
temporary Bazel output tree, so existing repositories and registry metadata
cannot hide missing cache inputs. It fetches every target in `bazel_ci_dependency_list.sh`,
not only the image's, so local and CI preparation share one definition. This
fetches more dependencies than the image needs, but avoids maintaining a
separate dependency list for image builds. BuildKit cache mounts hold the
repository and module caches between builds, keyed by `BAZEL_CACHE_NAMESPACE`.
The stage copies the results into two stages, `bazel_repository_cache` and
`bazel_go_mod_cache`.

CI skips that stage. It overrides those two stages with `--build-context`,
using the caches that `setup-bazel` prepared on the host. Both paths mount the
Bazel repository and Gazelle module caches read-only and apply Bazel and Go
download restrictions during container compilation, so the compilation stages
are the same either way.

Compilation uses `--network=none` unless
`BAZEL_REMOTE_CACHE_CONFIGURED=true`. In CI, `setup-bazel` exports this flag
only when it configures the remote cache. The image task uses that decision for
network selection and inherits the cache settings from `.bazelrc.local`.
`BAZEL_REMOTE_CACHE_ENABLED=false` tells `setup-bazel` to disable the cache.

For local usage of a bazel remote cache, configure a `grpcs://` cache URL,
authentication, and other cache options in `.bazelrc.local`. Set
`BAZEL_REMOTE_CACHE_CONFIGURED=true` when building an image to allow
compilation to access that cache. Local image builds do not generate these
settings. See [why the cache uses
gRPC](./bazel.md#why-the-remote-cache-uses-grpc) for the transport
requirements. Without the flag, compilation disables the remote cache even if
imported Bazel configuration enables it.  The default local build and scheduled
CI therefore compile without network access. With the cache enabled,
compilation has network access, not an enforced cache-only connection. Bazel
can still download registry metadata because its download restriction exempts
those files. Use the offline validation below to check that preparation
supplies all required inputs. Base-image pulls, package installation, and
Bazel installation can still require network access in either mode.

The Dockerfile has two alternative compilation stages because `RUN --network`
does not support argument expansion. Only the selected stage runs. Both stages
use the same compilation script; keep their mount declarations identical.

To validate preparation from empty dependency caches and fresh builder caches,
use a new namespace:

```sh
BAZEL_REMOTE_CACHE_CONFIGURED=false \
BAZEL_CACHE_NAMESPACE="validation-$(date +%s)" \
  task bazel-build-image-avalanchego
```

The persistent cache mounts can hold inputs from earlier builds, which would hide
a missing input. A new namespace starts all builder caches empty, and the
offline compilation then succeeds only if the `prepare` stage supplied every
input. CI checks the same property for the caches that `setup-bazel` prepares.
`BAZEL_REPOSITORY_CACHE` and `BAZEL_GO_MOD_CACHE` apply to CI only, where they
name the prepared host caches.

The builder passes a combined fingerprint through the Bazel action environment
variable `BAZEL_IMAGE_BUILDER`. It includes:

- the base-image reference, pinned by its multi-platform image digest
- all installed Debian package names and versions, sorted before hashing
- the target architecture
- the pinned Bazel version from `.bazelversion`

The architecture distinguishes images selected from the same multi-platform
base-image digest. The package fingerprint accounts for packages installed
after the base image is pulled. Bazel separately tracks declared source files,
toolchains, build options, and action environment variables.

This is best-effort cache separation, not a hermetic build environment. The
fingerprint does not capture every possible compiler input or prevent cache
poisoning. PR CI prioritizes speed and accepts potentially incorrect cached
outputs when an input is untracked or the cache is poisoned. Scheduled CI
checks the merged source without remote cached outputs; fresh GitHub-hosted
runners also start without local compilation caches. This independent check
can detect problems after merge, but does not guarantee correct PR CI results.
Do not use the cached validation image as a release artifact.

The builder uses a digest-pinned `debian:bookworm-slim` base and installs native
compiler prerequisites during the image build. Bazel supplies the Go SDK used
for compilation. Keep the base reference and fingerprint connected when
updating the Dockerfile. Account for any additional compilation tools installed
outside Debian packages. Consider a prebuilt toolchain image if maintaining this
fingerprint becomes insufficient.

## Conceptual model

An image build has three layers:

1. A repository task selects the build and configures its environment.
2. A product-specific script prepares build inputs and invokes Buildx.
3. Buildx runs the Dockerfile stages and produces or publishes the image.

The scripts supply inputs such as image tags, target platforms, the Go version,
the source commit, and host build contexts. The exact inputs depend on the
image. Builder stages compile source code. Runtime stages receive the required
artifacts instead of the builder caches.

Local non-Bazel builds prepare dependencies before Buildx starts. Local Bazel
builds prepare them inside the Linux builder. CI workflows restore and prepare
shared caches before they run an image-build task. The build scripts consume
those caches in CI instead of preparing them again.

## Go module dependencies

These builds use the module cache for Go tasks outside Bazel, not CI's separate
Gazelle cache. See [Go toolchains and dependency caches](./go-toolchains-and-caches.md)
for the distinction between dependency inputs and compilation results.

The build scripts pass only Go's module-download cache (`cache/download`) to
BuildKit. They do not pass the extracted module trees, which are often larger
and can contain modules from unrelated repositories. Each Dockerfile `RUN`
instruction that runs Go mounts the download cache as read-only. Go uses
`GOPROXY=file:///gomodproxy,off` to read modules only from this cache. Go then
extracts the required modules into the builder-local module cache.

If you run Buildx directly, provide the module cache as the `gomodcache` build
context:

```sh
docker buildx build \
  --build-context "gomodcache=$(go env GOMODCACHE)/cache/download" \
  ...
```

### Host cache preparation

The host cache must contain the repository workspace's dependency graph before
Buildx starts. Cache preparation uses the repository's Go workspace and
toolchain. This configuration resolves the same module versions that the image
build uses.

For local builds, each image-build task populates the host cache before it
starts Buildx. In CI, the workflow restores and prepares the cache before it
runs an image-build task. The image-build tasks skip module downloads in CI
because the workflow has already prepared the cache.

### Dockerfile requirements

For each builder stage that runs Go commands:

1. Set `GOPROXY=file:///gomodproxy,off`.
2. Add `--network=none` and
   `--mount=type=bind,from=gomodcache,source=.,target=/gomodproxy,ro` to each
   `RUN` instruction that runs Go directly or through a script. `--network=none`
   prevents those instructions from accessing the network; earlier package-install
   instructions can retain the network access they require.
3. Do not copy the complete host module cache into `/go/pkg/mod`.

The read-only mount prevents the builder from changing the host cache. The
runtime image contains only the built artifacts. It does not contain the builder
module cache.

Antithesis child Dockerfiles also mount the proxy for their Go build
instructions. The tagged Antithesis builder image does not retain an extracted
module cache.

## Design constraints and alternatives

A named BuildKit context is not a host bind mount. Buildx transfers the context
to the BuildKit daemon. Tests showed that a complete-cache context made image
builds too slow. It included large extracted module trees that the build did not
need.

The current design transfers only the module-download cache. This cache can
still contain multiple gigabytes, but it is smaller than the complete host
module cache.

A versioned dependency-builder image and a remote persistent BuildKit cache are
two alternatives. Either alternative can reduce work on a builder that has no
cache. Both alternatives require policies for publication, retention,
invalidation, and initial transfer.

## Manual verification

To verify that the AvalancheGo builder instructions do not depend on cached
layers, run:

```sh
./scripts/build_image.sh --no-cache
```

The uncached build succeeds only if the host proxy cache contains every required
module.

Next, verify that the Go build cannot download missing modules. Run the following
command with an empty proxy cache. This command bypasses host-cache preparation.

```sh
(
  empty_cache="$(mktemp -d)"
  trap 'rm -rf "$empty_cache"' EXIT
  go_version="$(go list -m -f '{{.GoVersion}}' | head -1)"

  docker buildx build \
    --no-cache \
    --target builder \
    --build-context "gomodcache=$empty_cache" \
    --build-arg "GO_VERSION=$go_version" \
    --build-arg "AVALANCHEGO_COMMIT=$(git rev-parse HEAD)" \
    --progress=plain \
    -f Dockerfile \
    .
)
```

The build must fail when Go tries to read a required module from the empty local
proxy. This proves the builder cannot download a missing module from the
network.

To verify that cache preparation does not change module metadata, run:

```sh
git diff --exit-code -- \
  go.work.sum \
  go.sum \
  graft/coreth/go.sum \
  graft/evm/go.sum \
  graft/subnet-evm/go.sum
```

Run the corresponding image-build task after you change another image path.

## When to revisit

Revisit the dependency design if measurements show that module-cache transfer
uses enough build time or bandwidth to justify additional cache maintenance.
