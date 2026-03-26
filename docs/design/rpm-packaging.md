# RPM Packaging for AvalancheGo

## Overview

Ship signed Linux packages for avalanchego and subnet-evm targeting
RHEL 9.x and Ubuntu customers. RPMs are built inside a Rocky Linux 9
container (glibc 2.34), DEBs are built on the Ubuntu release runners,
and release signing is moving to a single AWS KMS-backed GPG identity.

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
Rocky Linux 9 container. The build is orchestrated by a Taskfile at
`.github/packaging/Taskfile.yml`, included from the root Taskfile as
`packaging:`.

### Package signing

Release signing uses a single AWS KMS-backed GPG identity exposed to CI
as metadata only (`PACKAGE_SIGNING_KMS_KEY_ID`) plus AWS OIDC-issued
credentials. The GPG private key never lives in GitHub secrets, runner
disk, or container volumes.

The bridge from GPG to KMS is:
- `gnupg-pkcs11-scd`
- `aws-kms-pkcs11`

DEBs are signed after `dpkg-deb --build` with `dpkg-sig`. RPMs are built
first and then signed with `rpmsign --addsign` in the Rocky container.
Both release paths export the public key used for verification and gate
artifact publication on successful signature verification.

PR RPM builds still use the ephemeral GPG path so the packaging pipeline
can be exercised without AWS credentials. DEB workflows do not run on PRs.

### Version smoke test

The `--version` output format is
`avalanchego/1.14.1 [database=v1.4.5, rpcchainvm=44, commit=abcd1234, ...]`.
The version comes from compiled constants (`version/constants.go`),
not the git tag, so it may not match RC tags. The smoke test verifies:
- Output starts with `avalanchego/` (binary runs)
- Output contains `commit=` followed by the git commit hash captured
  during the build (correct source was compiled)

### CI workflow

RPM releases use `.github/workflows/build-rpm-release.yml` and DEB releases
use `.github/workflows/build-ubuntu-amd64-release.yml` plus
`.github/workflows/build-ubuntu-arm64-release.yml`.

RPM workflow behavior:
1. On tag push and `workflow_dispatch`, configure AWS credentials via OIDC
2. Build RPMs in the Rocky container
3. Use the KMS-backed GPG bridge to sign finished RPMs
4. Validate in a fresh `rockylinux:9` container with `rpm -K`, install,
   and smoke tests
5. Upload RPMs plus the exported public key as artifacts

On PRs, the same RPM workflow keeps the existing ephemeral signing path and
synthetic tag so packaging changes still get end-to-end coverage.

DEB workflow behavior:
1. Build the `.deb` on the matching Ubuntu runner
2. Configure the KMS-backed GPG bridge on that runner
3. Sign the finished `.deb` with `dpkg-sig`
4. Verify on the runner, then verify/install/smoke-test in a fresh Ubuntu
   container matching the target release
5. Upload the signed `.deb` plus public key artifact and only then publish
   to S3

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
`ubuntu-latest` (Ubuntu 24.04, glibc 2.39). Target RPM distros could
range from RHEL 8 (glibc 2.28) to RHEL 9 (glibc 2.34). A binary linked
against glibc 2.39 would not run on either.

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
  slowdown was observed in multi-threaded benchmarks for a distributed
  query engine ([Andy Grove][2]).
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
`rockylinux:9` for glibc 2.34). GitHub Actions' `container:` directive makes
this straightforward. This has a testing gap: CI currently tests on
`ubuntu-latest` (glibc 2.39), so RPMs built in an older container include an
effectively different and untested binary. To truly "test what we ship," CI
would need to also run tests inside the same older-glibc container,
potentially doubling CI overhead. For now, this testing gap is probably
acceptable. RPM smoke tests validate that the binary loads and runs correctly
on the target platform. Full e2e testing on the target glibc is likely not
worth the effort until Bazel enables testing against the same glibc version
used for the release build.

### Bazel hermetic toolchain (target solution)

Bazelification is in progress but not immediately available.

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
objections (NSS/iconv/dlopen, [RedHat][7]) don't actually apply to this stack:
Go's `netgo`+`osusergo` build tags bypass NSS entirely, neither Go nor Rust
uses iconv, and firewood's file I/O (mmap, pwrite, fsync) uses only thin
syscall wrappers that work identically in static and dynamic builds. The
CockroachDB thread-local-storage crash ([Schottdorf][8]) was triggered by NSS
module loading, which `netgo`+`osusergo` eliminates.

However, this approach is still not recommended: (1) you still must
target a specific glibc version, so the portability benefit over dynamic
linking evaporates — you need container builds or a hermetic toolchain
either way; (2) no one in the Go ecosystem appears to ship Go+CGO
statically linked against glibc in production, making this untested
territory; (3) the Bazel hermetic toolchain with dynamic linking is
strictly better, avoiding all static glibc edge cases while providing
the same reproducibility.

### Industry Practice: How Go Projects Ship Binaries

A common misconception is that "distroless" container images imply
static linking. In fact, Google's distroless project provides three
image variants serving different linking models:

| Image | Contents | Use case |
|-------|----------|----------|
| `distroless/static` | CA certs, tzdata only | Statically linked binaries (no libc) |
| `distroless/base-nossl` | Above + **glibc** | Dynamically linked binaries |
| `distroless/base` | Above + **glibc + libssl** | Dynamically linked binaries needing TLS |

The vast majority of Go projects using `distroless/static` build with
`CGO_ENABLED=0` — producing pure Go binaries with no libc dependency
at all. This is not static glibc linking; it is no libc linking.

Projects that require CGO (for C libraries like RocksDB, SQLite, GEOS,
or in our case firewood FFI) fall into one of three camps. No
well-known Go project statically links against glibc in production:

| Strategy | Projects | Container base |
|----------|----------|---------------|
| Pure Go (`CGO_ENABLED=0`) | etcd, Kubernetes, Prometheus | `distroless/static` or `scratch` |
| CGO + dynamic glibc | CockroachDB ([cockroach#3392][9]) | Red Hat UBI minimal |
| CGO + static musl | Recommended for CGO projects needing static binaries ([DoltHub blog][10]) | `distroless/static` |

[1]: https://nickb.dev/blog/default-musl-allocator-considered-harmful-to-performance/
[2]: https://andygrove.io/2020/05/why-musl-extremely-slow/
[3]: https://github.com/ziglang/zig/issues/12833
[4]: https://github.com/bazelbuild/rules_rust/issues/2726
[5]: https://github.com/grafana/grafana/issues/79773
[6]: https://honnef.co/articles/statically-compiled-go-programs-always-even-with-cgo-using-musl/
[7]: https://developers.redhat.com/articles/2023/08/31/how-we-ensure-statically-linked-applications-stay-way
[8]: https://tschottdorf.github.io/golang-static-linking-bug
[9]: https://github.com/cockroachdb/cockroach/issues/3392
[10]: https://www.dolthub.com/blog/2024-05-01-cgo-tradeoffs/
