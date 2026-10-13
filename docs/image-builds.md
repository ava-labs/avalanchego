# Container image builds

## Table of contents

- [Overview](#overview)
- [Supported tasks](#supported-tasks)
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

Image builds compile Go code without downloading dependencies from the network.
The build scripts provide the repository's host module cache as an offline
proxy. This design separates dependency preparation from image construction and
keeps module downloads out of Dockerfile instructions. It prevents image
construction from depending on network downloads.

## Supported tasks

Use these repository tasks to build images:

```sh
task build-image
task build-xsvm-image
task build-subnet-evm-image
task build-antithesis-images-avalanchego
task build-antithesis-images-subnet-evm
```

Each task starts the product-specific build script in the repository's
configured environment.

## Conceptual model

An image build has three layers:

1. A repository task selects the build and configures its environment.
2. A product-specific script prepares build inputs and invokes Buildx.
3. Buildx runs the Dockerfile stages and produces or publishes the image.

The scripts supply inputs such as image tags, target platforms, the Go version,
the source commit, and host build contexts. The exact inputs depend on the
image. Builder stages compile source code. Runtime stages receive the required
artifacts instead of the builder caches.

Local builds prepare required dependencies immediately before Buildx starts. CI
workflows restore and prepare shared caches before they run an image-build task.
The build scripts consume those caches in CI instead of preparing them again.

## Go module dependencies

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

Next, verify the offline restriction. Run the following command with an empty
proxy cache. This command bypasses host-cache preparation.

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
