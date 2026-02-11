# RPM Packaging for AvalancheGo

## Overview

Ship signed RPM packages for avalanchego and subnet-evm targeting
RHEL 9.x customers. Packages are built inside a Rocky Linux 9 container
(glibc 2.34), signed with GPG, and distributed via S3.

## Decisions

- **Target distro:** RHEL 9.4 (glibc 2.34). Container base image:
  `rockylinux:9`. Bazel hermetic toolchain target: `gnu.2.34`.
- **Target architectures:** x86_64 and aarch64.
- **Binary linking:** Dynamic linking against glibc (not static musl).
  See [Binary Linking](#binary-linking) for analysis.

## Packaging

### Packages

Two RPM packages:

| Package | Install path |
|---------|-------------|
| avalanchego | `/var/opt/avalanchego/bin/avalanchego` |
| subnet-evm | `/var/opt/avalanchego/plugins/<VM_ID>` |

Both declare `glibc >= 2.34` as a dependency. The subnet-evm plugin
path uses the VM ID from `graft/subnet-evm/scripts/constants.sh`.

### Build tooling

Packages are built with [nfpm](https://nfpm.goreleaser.com/) inside a
Rocky Linux 9 container. The container installs Go (version from
`go.mod`), nfpm, gcc, and rpm-sign/gnupg2 for signing.

The build is orchestrated by a Taskfile at
`.github/packaging/Taskfile.yml`, included from the root Taskfile as
`packaging:`. It handles building the Docker image, compiling binaries,
packaging RPMs, running tests, and post-upload S3 validation.

### GPG signing

RPMs are always signed. In CI, a real GPG key is provided via
`secrets.RPM_GPG_PRIVATE_KEY`. For local builds, an ephemeral GPG key
(RSA 4096, no passphrase, 1-day expiry) is generated to exercise the
signing pipeline without requiring a real key.

### Version smoke test

The `--version` output format is
`avalanchego/1.14.1 [database=v1.4.5, rpcchainvm=44, commit=abcd1234, ...]`.
The version comes from compiled constants (`version/constants.go`),
not the git tag, so it may not match RC tags. The smoke test verifies:
- Output starts with `avalanchego/` (binary runs)
- Output contains `commit=` followed by the git commit hash captured
  during the build (correct source was compiled)

### CI workflow

`.github/workflows/build-rpm-release.yml` triggers on tag push,
`workflow_dispatch`, and `pull_request` (for paths under
`.github/packaging/`). On PRs, the full build runs as a smoke test
with an ephemeral GPG key and synthetic tag, but S3 upload and
artifact publishing are skipped. Matrix strategy covers x86_64 and
aarch64. Steps (for tag/dispatch triggers):

1. Build RPMs via `scripts/run_task.sh packaging:build-rpms`
2. Upload to `s3://${BUCKET}/linux/rpms/${RPM_ARCH}/`
3. Upload GPG public key to `s3://${BUCKET}/linux/rpms/RPM-GPG-KEY-avalanchego`
4. Validate: download from S3, verify signature, install, smoke test
   in a fresh `rockylinux:9` container
5. Upload RPMs as GitHub artifacts

### Architecture mapping

| `uname -m` | Docker `TARGETARCH` | RPM arch |
|-------------|-------------------|----------|
| `x86_64` | `amd64` | `x86_64` |
| `arm64` / `aarch64` | `arm64` | `aarch64` |

The Taskfile maps `uname -m` to RPM arch names. The Dockerfile uses
Docker's `TARGETARCH` (Go-style) for the Go download URL.

## Binary Linking

### Problem Statement

We need to ship RPMs for RHEL/Fedora/Rocky, but CI builds and tests on
Ubuntu. Different distros ship different glibc versions, so a
dynamically-linked Ubuntu binary may not run on RHEL. CI tests on
`ubuntu-latest` (Ubuntu 24.04, glibc 2.39). Target RPM distros range
from RHEL 8 (glibc 2.28) to RHEL 9 (glibc 2.34). A binary linked
against glibc 2.39 will not run on either.

### Proposed Solution

Build dynamically-linked binaries against the target glibc version using
container builds as a stopgap, with Bazel hermetic toolchains as the
target solution. Minimize investment in pre-Bazel linking workarounds —
the container approach is acceptable for shipping RPMs now. Do not adopt
static musl compilation without a performance test suite that would
catch regressions.

### Why dynamic glibc

- glibc's memory allocator is dramatically faster than musl's under
  multi-threaded workloads. Synthetic benchmarks show 2x-700x slowdowns
  for musl malloc under contention ([nickb.dev][1]). More broadly,
  musl multi-threaded performance can degrade significantly — a 30x
  slowdown was observed in DataFusion query execution ([Andy Grove][2]).
  With firewood using jemalloc and Go managing its own heap, the
  practical impact on this binary is unquantified but likely much
  smaller. No profiling has been done. Performance is critical for a
  blockchain node and this risk is not acceptable without data.
- glibc has full locale, NSS, and DNS support. No thread stack size
  surprises (default 8MB, set by ulimit -s vs musl's 128KB).
- Go's race detector works with glibc but not musl.
- glibc has strong forward compatibility — binaries linked against an
  older glibc run on newer versions (though subtle behavioral changes
  between versions are possible; see residual risk under Bazel section).

### Portability via container builds (stopgap)

Build inside a container matching the target glibc version (e.g.,
`rockylinux:9` for glibc 2.34). GitHub Actions' `container:` directive
makes this straightforward. This has a testing gap: CI currently tests
on `ubuntu-latest` (glibc 2.39), so RPMs built in an older container
include an effectively different and untested binary. To truly "test
what we ship," CI would need to also run tests inside the same
older-glibc container, potentially doubling CI overhead. For the
stopgap phase (until bazel is implemented), this testing gap is
probably acceptable. RPM smoke tests (install and run on target
distro) could provide basic validation that the binary loads and
starts. Full e2e testing on the target glibc is likely not worth the
effort until Bazel enables testing against the same glibc version used
for the release build.

### Bazel hermetic toolchain (target solution)

Bazelification is a near-term goal (initial build PR is up, bazelified
firewood in progress) but not immediately available.

Bazel 8 with `hermetic_cc_toolchain` would solve the "test what you
ship" problem without doubling CI or requiring containers. The
toolchain would be decoupled from the host — `bazel test` and `bazel
build` would use the same toolchain targeting the same glibc version
(e.g., glibc 2.34), regardless of the CI runner's host glibc. The test
binary and the release binary would be linked against the same glibc —
no duplication, no divergence. The toolchain bundles glibc versions
2.17 through 2.38.

This would cover both Rust and Go. `rust_static_library` would produce
a `.a` with `CcInfo`, directly consumable by `go_library` via `cdeps`.
Both Rust compilation and Go's CGO link would use the same sysroot, so
a single `--platforms` flag would ensure that the Rust FFI and Go
binary target the same glibc version. The build graph would be:

```
rust_static_library (firewood_ffi, targeting x86_64-unknown-linux-gnu)
    → provides CcInfo
go_library (firewood bridge, cgo=True, cdeps=[firewood_ffi])
    → go_binary (avalanchego)
    → linked by hermetic_cc_toolchain targeting gnu.2.34
```

Known rough edges that the firewood bazel work would need to explore:
- TLS errors reported with zig toolchain + Rust async runtimes
  ([zig#12833][3])
- `rules_rust` musl target selection had bugs requiring explicit
  platform constraints ([rules_rust#2726][4])
- Building `tikv-jemalloc-sys` under Bazel requires a
  `rules_foreign_cc` workaround since it uses autotools

There is a residual risk: a binary built and tested against an older
glibc but run against a newer one in production. glibc's forward
compatibility is strong in practice, but newer versions can change
behavior in subtle ways (bug fixes that code inadvertently depended
on, performance characteristics of allocator or threading primitives,
new default security hardening). This risk is low but non-zero, and is
inherent to any strategy that doesn't test on the exact production
glibc.

### Alternatives Considered

#### Static linking with musl

Static musl binaries have no runtime dependency on host glibc, which
makes them portable across any Linux distro regardless of glibc version.
This means a single binary per architecture — no need to build and test
against multiple glibc versions, no container matrix in CI, and no risk
of glibc version mismatch at deploy time. This simplicity is why many
projects (including Alpine-based Docker images) default to musl.

However, the risks are significant for this project:

- **Allocator performance:** See "Why dynamic glibc" above for details.
  Firewood's jemalloc mitigates Rust-side allocations, but any C-level
  malloc calls in CGO-invoked code still hit musl's allocator.
- **Thread stack size:** musl defaults to 128KB (vs glibc 8MB, set by
  ulimit -s). This has caused segfaults in other CGO projects (Grafana
  with SQLite, [grafana#79773][5]). Not observed with firewood, but
  untested under deep call stacks.
- **Firewood FFI:** `firewood-go-ethhash/ffi` ships pre-compiled `.a`
  for `*-linux-gnu` only. Would need to build and publish
  musl-compatible `.a` files.
- **Race detector:** Go's race detector does not work with musl
  ([Honnef][6]). Must maintain a separate glibc build for race-enabled
  CI.
- **Testing gap:** We lack a performance test suite that would catch
  musl-induced regressions. Switching to static musl without adequate
  perf testing risks shipping slower binaries unknowingly.

#### Static linking with glibc

Would combine glibc's performance with static portability. The classic
objections (NSS/iconv/dlopen, [RedHat][7]) don't actually apply to this
stack: Go's `netgo`+`osusergo` build tags bypass NSS entirely, neither
Go nor Rust uses iconv, and firewood's file I/O (mmap, pwrite, fsync)
uses only thin syscall wrappers that work identically in static and
dynamic builds. The CockroachDB TLS crash ([Schottdorf][8]) was
triggered by NSS module loading, which `netgo`+`osusergo` eliminates.

However, this approach is still not recommended: (1) you still must
target a specific glibc version, so the portability benefit over dynamic
linking evaporates — you need container builds or a hermetic toolchain
either way; (2) no one in the Go ecosystem appears to ship Go+CGO
statically linked against glibc in production, making this untested
territory; (3) the Bazel hermetic toolchain with dynamic linking is
strictly better, avoiding all static glibc edge cases while providing
the same reproducibility.

[1]: https://nickb.dev/blog/default-musl-allocator-considered-harmful-to-performance/
[2]: https://andygrove.io/2020/05/why-musl-extremely-slow/
[3]: https://github.com/ziglang/zig/issues/12833
[4]: https://github.com/bazelbuild/rules_rust/issues/2726
[5]: https://github.com/grafana/grafana/issues/79773
[6]: https://honnef.co/articles/statically-compiled-go-programs-always-even-with-cgo-using-musl/
[7]: https://developers.redhat.com/articles/2023/08/31/how-we-ensure-statically-linked-applications-stay-way
[8]: https://tschottdorf.github.io/golang-static-linking-bug
