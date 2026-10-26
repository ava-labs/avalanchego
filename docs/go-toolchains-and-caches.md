# Go toolchains and dependency caches

This guide explains compiler and cache requirements across repository tasks.
Use it to distinguish dependency preparation, compilation, and execution.

## Table of contents

- [Toolchains, builds, and prebuilt binaries](#toolchains-builds-and-prebuilt-binaries)
- [Separate caches](#separate-caches)
- [Dependency preparation](#dependency-preparation)
- [Download restrictions](#download-restrictions)
- [Changing cache setup safely](#changing-cache-setup-safely)

## Toolchains, builds, and prebuilt binaries

A Go toolchain supplies the compiler and related tools. Bazel uses a managed Go
toolchain for its targets. Those builds do not require a separately installed
Go compiler. See [Bazel's toolchain strategy](./bazel.md#toolchain-strategy).
Go commands outside Bazel require their own toolchain, supplied through Go setup
or the Nix development shell in CI.

Executing a prebuilt binary requires no Go compilation. For example, CI downloads
a Task release instead of building Task from source. A Bazel build is different:
it can compile targets even when all dependency inputs are already cached.
The local Task launcher can also build Task through `go tool` when no Task binary
is available. That fallback requires Go outside Bazel. See
[Task setup in CI](./ci.md#task).

## Separate caches

A Go module cache stores dependency inputs, such as module archives and extracted
source files. It does not store compilation results. The repository uses these
separate caches:

| Cache | Contents | Configuration |
| --- | --- | --- |
| Gazelle module cache | Go dependency inputs for external Bazel repositories | CI configures a separate path through the repository-rule environment. See [Bazel dependency preparation](./bazel.md#go-dependency-preparation). |
| Module cache for Go tasks outside Bazel | Go dependency inputs | `go env GOMODCACHE` in the command's environment selects the path. |
| Bazel repository cache | Downloaded repository inputs | See [Bazel CI caches](./bazel.md#what-is-cached). |
| Bazel action and test-result caches | Action outputs and cacheable test results | `.bazelrc` selects `~/.cache/bazel-disk-cache`. CI can also configure a separate remote cache. |
| Go compilation cache | Compilation results | Go commands outside Bazel manage it at `go env GOCACHE`. |

## Dependency preparation

`setup-go-module-cache` prepares repository modules and manifest entries for Go
tasks outside Bazel. `setup-go-for-project` and `install-nix` call it.
Module-cache preparation does not restore compilation results. See
[CI module-cache guidance](./ci.md#go-module-cache) for cache ownership and controls.

Preparing dependencies for Go tasks outside Bazel does not necessarily prepare
Gazelle's cache. CI configures different paths for these operations. Gazelle
uses its module cache to prepare external source repositories. Bazel then
compiles those sources with its managed toolchain.

## Download restrictions

`GOPROXY=off` disables Go module downloads. CI also uses
`--repository_disable_download` to disable Bazel repository downloads. Neither
setting makes the complete workload network-isolated. Remote caches, tests,
and other tools can still use the network.

Container builds use a separate
[`--network=none` restriction](./image-builds.md#dockerfile-requirements) for
individual Go build instructions. Base-image pulls and package installation
can still require the network.

## Changing cache setup safely

Check each command's compiler and dependency requirements. Do not infer them
from the task name or from the presence of a prebuilt binary.

Run Bazel preparation before Go setup disables module downloads. An exact cache
hit can hide an incorrect setup order. Check both exact hits and cold-cache
preparation. See [CI provisioning](./ci.md#provision-ci-job-dependencies) and
[Bazel cache validation](./bazel.md#changing-this-safely).
