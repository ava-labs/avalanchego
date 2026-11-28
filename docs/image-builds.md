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

The runtime image contains the executable, not the builder filesystem or
toolchain. The CI job validates the Bazel build path as well as the image:
Bazel runs on every image build so a cached Docker layer cannot bypass the
current cache settings or download restrictions. Bazel can still reuse its own
cached results; this is not a clean compilation on every run.

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
changing the checkout's configuration. For the file's general behavior, see
[local Bazel configuration](./bazel.md#local-bazel-configuration).

Outside CI, the Linux builder prepares dependencies with network access. Bazel
repository rules fetch toolchains for the platform they run on, so preparation
on a non-Linux host would fetch the wrong toolchains. CI instead supplies the
caches prepared by `setup-bazel` on the Linux runner. Both paths use the same
[CI dependency list](./bazel.md#checked-in-bazel-dependency-list) and supply
read-only caches to compilation. The shared list fetches more dependencies than
the image needs, but avoids maintaining a separate list.

Local preparation uses a fresh Bazel output tree so existing repositories and
registry metadata cannot hide missing cache inputs. BuildKit retains dependency
caches between builds under `BAZEL_CACHE_NAMESPACE`.

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

Persistent caches can hold inputs from earlier builds and hide a missing
input. A new namespace starts the builder caches empty, so offline compilation
checks whether preparation supplied the required inputs. CI checks the caches
prepared by `setup-bazel`. `BAZEL_REPOSITORY_CACHE` and `BAZEL_GO_MOD_CACHE`
apply to CI only, where they name the prepared host caches.

The [compilation script](../scripts/build_bazel_image_binary.sh) adds a builder
fingerprint to Bazel's action environment to separate cached outputs when known
compiler inputs change. The architecture distinguishes images selected from
the same multi-platform base-image digest; installed package versions account
for tools added after the base image is pulled. Bazel separately tracks declared
source files, toolchains, build options, and action environment variables.

This is best-effort cache separation, not a hermetic build environment. The
fingerprint does not capture every possible compiler input or prevent cache
poisoning. PR CI accepts the risk of incorrect cached outputs to gain speed;
scheduled CI checks the merged source without remote or local compilation
caches. That check can detect problems after merge, but cannot guarantee PR
results. See the [CI cache policy](./bazel.md#test-platforms-and-cache-policy)
for the trust boundary and requirements for published artifacts.

When changing the builder, keep its base reference connected to the fingerprint
and account for compilation tools installed outside Debian packages. Consider a
prebuilt toolchain image if maintaining this fingerprint becomes insufficient.

## Conceptual model

An image build has three layers:

1. A repository task selects the build and configures its environment.
2. A product-specific script prepares build inputs and invokes Buildx.
3. Buildx runs the Dockerfile stages and produces or publishes the image.

The scripts supply inputs such as image tags, target platforms, the Go version,
the source commit, and host build contexts. The exact inputs depend on the
image. Builder stages compile source code. Runtime stages receive the required
artifacts instead of the builder caches.

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
