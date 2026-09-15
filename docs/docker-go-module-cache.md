# Go module cache for Docker builds

## Table of contents

- [Overview](#overview)
- [Usage](#usage)
  - [Build entrypoints](#build-entrypoints)
  - [Direct Buildx calls](#direct-buildx-calls)
  - [Host cache preparation](#host-cache-preparation)
  - [Go toolchain](#go-toolchain)
  - [CI cache preparation](#ci-cache-preparation)
- [Conceptual model](#conceptual-model)
- [Maintainer guidance](#maintainer-guidance)
  - [Add a Go image build](#add-a-go-image-build)
  - [Validate a change](#validate-a-change)
  - [Revisit the design](#revisit-the-design)

## Overview

The Go image builds use the host Go module cache. This design downloads shared
modules one time instead of downloading them in each image build. It also keeps
module proxy failures outside Docker builds.

Each builder stage sets `GOPROXY=off`. Thus, the builder cannot download a
missing module from a module proxy. The build fails if the host Go module cache
does not contain a required module.

## Usage

### Build entrypoints

Run an image build from the repository root:

```sh
task build-image
task build-xsvm-image
task build-subnet-evm-image
task build-antithesis-images-avalanchego
task build-antithesis-images-subnet-evm
```

The corresponding scripts are:

- `scripts/build_image.sh`
- `scripts/build_xsvm_image.sh`
- `graft/subnet-evm/scripts/build_docker_image.sh`
- `scripts/build_antithesis_images.sh`

The scripts prepare the host Go module cache before they start the applicable
builder image. `task build-image` builds standard and race variants. Its script
prepares the host Go module cache one time for both variants.

### Direct Buildx calls

Use a Task or script entrypoint when possible. These entrypoints supply the
`gomodcache` named BuildKit context.

A direct `docker buildx build` call must supply the same context:

```sh
--build-context "gomodcache=$(go env GOMODCACHE)"
```

The Dockerfile cannot resolve `from=gomodcache` if the call omits this option.

### Host cache preparation

Run [`go mod download`](https://go.dev/ref/mod#go-mod-download) from the
repository root with workspace mode enabled:

```sh
GOWORK="$(pwd)/go.work" go mod download
```

This downloads the combined dependency graph of the modules in `go.work` to one
host Go module cache. All image builders compile that workspace, including the
Subnet-EVM and Antithesis builders. The shared
`prepare_go_module_cache` helper runs this command for each image entrypoint.

The scripts pass the directory from `go env GOMODCACHE` to BuildKit. Run the
prefetch command and `docker buildx build` with the same Go environment. This
ensures that both commands use the same host Go module cache.

### Go toolchain

Use the Task entrypoints or the pinned repository Go toolchain. The host
prefetch and the image build should use the same Go version.

The Go module cache can contain data from multiple Go versions. Matching Go
versions ensures that the host prefetch selects the same dependency set as the
image build. The Task entrypoints use the repository toolchain. The scripts
also pass the module Go version to the Go base image.

### CI cache preparation

CI can restore the host Go module cache before an image build. A restored cache
prevents repeat downloads between jobs. The image build scripts always run the
host prefetch command because a CI or local cache can be empty or incomplete.

## Conceptual model

A [named BuildKit context](https://docs.docker.com/reference/cli/docker/buildx/build/#build-context)
gives a build access to a directory outside its normal build context. The image
scripts name the host Go module cache `gomodcache`:

```sh
--build-context "gomodcache=$(go env GOMODCACHE)"
```

This context contains the complete host Go module cache. It can contain modules
from other repositories. BuildKit can transfer this large local directory to
the build driver. Unrelated cache changes can alter the context digest and
invalidate the cache-copy layer.

This local transfer is an accepted trade-off. It prevents repeated module proxy
access in image builds. It is not external module network traffic.

The Dockerfile uses a [read-only mount](https://docs.docker.com/reference/dockerfile/#run---mounttypebind)
for one `RUN` instruction. The instruction copies the modules to `/go/pkg/mod`
in the builder stage. This copy is the builder module cache. Later Go commands
use it while `GOPROXY=off` prevents access to a module proxy.

The read-only mount prevents the image build from changing the host Go module
cache. The copy can change only the filesystem of the builder stage.

The final runtime stage copies the built binary from the builder stage. It does
not copy `/go/pkg/mod`. Therefore, the final image does not contain the builder
module cache.

The instrumented Antithesis builder creates the builder module cache before
instrumentation. The later build in `/instrumented/customer` uses the same
builder module cache.

The host prefetch command uses the repository workspace. The Dockerfiles
copy the complete repository, including `go.work`, before they run Go build
commands. The host command must use the same workspace module view as those
build commands.

## Maintainer guidance

### Add a Go image build

When you add a Go Docker image, complete these steps:

1. Ensure the image build compiles the repository workspace.
2. Call `prepare_go_module_cache` with the repository root before Buildx.
3. Pass `$(go env GOMODCACHE)` as the `gomodcache` named BuildKit context.
4. Add the named BuildKit context to every direct Buildx call.
5. Set `GOPROXY=off` in each builder stage that runs Go commands.
6. Mount `gomodcache` as read-only and copy its contents to `/go/pkg/mod`.
7. Complete these actions before the first Go command that needs modules.

Keep the prefetch outside the Dockerfile. The host prefetch has no retry. Host
downloads have not shown the same failure frequency as image-local downloads.

If transient host failures become frequent, add a bounded retry around the
single host prefetch. Do not add retries to each Dockerfile. This policy keeps
one module network access point for all image variants in a job.

Use `go mod download` for the module dependency set. Do not use `go mod download
all` only to support image builds. The `all` argument fetches unnecessary
modules and can add unnecessary entries to the
[`go.sum` file](https://go.dev/ref/mod#go-sum-files).

An image build must consume the repository module metadata. It must not maintain
or rewrite that metadata. Run `task go-mod-tidy` when you need `go mod tidy`.
Do not run `go mod tidy` in a Dockerfile dependency step.

For an entrypoint that builds multiple image variants, run the prefetch one
time. Reuse the named BuildKit context for each variant.

### Validate a change

Force the affected builder instructions to run without old layer results. A
normal build can reuse a layer and hide an incomplete host Go module cache.

The root image script passes its arguments to Buildx. Use this command to build
both root image variants without old layer results:

```sh
./scripts/build_image.sh --no-cache
```

For another image, start with the Buildx call in its script. Add `--no-cache`
and keep all required build arguments and contexts. Also run the applicable
repository entrypoint from the [build entrypoints](#build-entrypoints) section.

Check that each builder stage has `GOPROXY=off`. A successful uncached build
proves that no required module was missing. Log inspection alone does not prove
this condition.

Use a direct Buildx call to test an incomplete host Go module cache. Do not use
the normal script because it runs the host prefetch first. This tested command
passes an empty directory and forces the builder instructions to run:

```sh
(
  empty_cache="$(mktemp -d ./build/empty-gomodcache.XXXXXX)"
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

Expect the build to fail with `module lookup disabled by GOPROXY=off`. This
result proves that the builder cannot download a missing module from a module
proxy.

Check that the host prefetch does not change `go.sum`:

```sh
git diff -- go.sum graft/subnet-evm/go.sum
```

### Revisit the design

Revisit this design if the local context transfer becomes too expensive. Also
revisit it if the project changes its BuildKit driver or build system. Any new
design must keep module downloads outside repeated image builds, or document why
that constraint no longer applies.
