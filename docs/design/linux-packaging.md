# Linux Packaging for AvalancheGo

## Overview

Ship signed Linux packages for `avalanchego` and `subnet-evm`:

- RPM packages for RHEL 9.x customers
- DEB packages for Ubuntu customers

Packages are built with `nfpm` inside per-format containers, signed with
GPG, and published as GitHub Actions artifacts on non-PR runs. DEB
release builds also publish Ubuntu S3 assets.

## Decisions

- **Package formats:** RPM and DEB are built through the same Taskfile
  and shared packaging scripts, with format-specific `nfpm`
  configuration files.
- **Target distros:** RPM builds target RHEL 9.x compatibility with a
  Rocky Linux 9 container (`rockylinux:9`, glibc 2.34). DEB builds use an
  Ubuntu 22.04 container (`ubuntu:22.04`).
- **Target architectures:** RPM uses `x86_64` and `aarch64`; DEB uses
  `amd64` and `arm64`.
- **Binary linking:** Dynamic linking against glibc (not static musl).
  See [Binary Linking](#binary-linking) for analysis.

## Packaging

### Build tooling

Packages are built with [nfpm](https://nfpm.goreleaser.com/) inside
format-specific containers. The build is orchestrated by
`.github/packaging/Taskfile.yml`, included from the root Taskfile as
`packaging:`, and uses shared scripts under `.github/packaging/scripts/`.

### RPM packages

Two RPM packages:

| Package | Install path |
|---------|-------------|
| avalanchego | `/var/opt/avalanchego/bin/avalanchego` |
| subnet-evm | `/var/opt/avalanchego/plugins/<VM_ID>` |

Both declare `glibc >= 2.34` as a dependency. The subnet-evm plugin
path uses the VM ID from `graft/subnet-evm/scripts/default-vm-data.sh`
(with a fallback to grepping `constants.sh` on tags that predate the
dedicated data file).

### DEB packages

Two DEB packages:

| Package | Install path |
|---------|-------------|
| avalanchego | `/usr/local/bin/avalanchego` |
| subnet-evm | `/usr/local/lib/avalanchego/plugins/<VM_ID>` |

Both declare `libc6 (>= 2.34)` as a dependency. The subnet-evm plugin
path uses the VM ID from `graft/subnet-evm/scripts/default-vm-data.sh`
(with a fallback to grepping `constants.sh` on tags that predate the
dedicated data file).

### GPG signing

RPMs and DEBs are always signed. In CI release builds, a real GPG key is
provided by GitHub Actions secrets. For local builds and PR validation,
an ephemeral GPG key (RSA 4096, no passphrase, 1-day expiry) is
generated to exercise the signing pipeline without requiring a real key.
Release events (tag push and `workflow_dispatch`) fail fast in
`workflow-setup-packaging.sh` if no signing-key secret is configured, so
a misconfigured release cannot silently fall back to the ephemeral key.

RPMs are signed inline by `nfpm` via the `rpm.signature.key_file`
configuration in `.github/packaging/nfpm/*-rpm.yml`.

DEBs are signed inline by `nfpm` via the `deb.signature.key_file`
configuration in `.github/packaging/nfpm/*-deb.yml`. The signature is
written as a detached GPG signature in the `_gpgorigin` ar member,
covering the concatenation of `debian-binary`, the control archive, and
the data archive in ar-member order. Validation runs `gpg --verify`
against that concatenation; no post-build or distro-specific signing
tool is involved.

### Version smoke test

The `--version` output format is
`avalanchego/1.14.1 [database=v1.4.5, rpcchainvm=44, commit=abcd1234, ...]`.
The version comes from compiled constants (`version/constants.go`),
not the git tag, so it may not match RC tags. The smoke test verifies:
- `avalanchego --version` output starts with `avalanchego/` (binary runs)
- `avalanchego --version` output contains the git commit hash captured
  during the build (correct source was compiled)
- The subnet-evm plugin is installed at the VM ID path, is executable,
  and its `--version` output contains the same git commit hash

### CI workflow

`.github/workflows/build-rpm-release.yml` and
`.github/workflows/build-deb-release.yml` trigger on tag push,
`workflow_dispatch`, and `pull_request` for packaging paths. On PRs, the
full builds run as smoke tests with ephemeral GPG keys and synthetic tags.

Both workflows invoke the build via
`scripts/run_task.sh --taskfile .github/packaging/Taskfile.yml <task>`.
Locally, the same tasks are reachable as `task packaging:<task>` because
the root Taskfile includes the packaging Taskfile under that namespace.

RPM workflow:

1. Run `test-build-rpms` (builds both packages, then validates in a fresh
   `rockylinux:9` container: signature verification, installation, smoke
   test)
2. Upload RPMs and GPG public key as GitHub artifacts on non-PR runs

DEB workflow:

1. Run `test-build-debs` (builds both packages, then validates in fresh
   `ubuntu:22.04` and `ubuntu:24.04` containers: signature verification,
   installation, smoke test)
2. Upload DEBs and the exported GPG public key as artifacts on non-PR runs
3. Upload release artifacts to S3 on tag push and `workflow_dispatch`:
   `.deb` files go under `linux/debs/ubuntu/{jammy,noble}/{arch}/`; the
   GPG public key goes one level above, at
   `linux/debs/ubuntu/{jammy,noble}/`, since one signing key serves all
   architectures of a given release.

### Architecture mapping

RPM mapping:

| `uname -m` | Docker `TARGETARCH` | RPM arch |
|-------------|-------------------|----------|
| `x86_64` | `amd64` | `x86_64` |
| `arm64` / `aarch64` | `arm64` | `aarch64` |

DEB mapping:

| `uname -m` | Docker `TARGETARCH` | DEB arch |
|-------------|-------------------|----------|
| `x86_64` | `amd64` | `amd64` |
| `arm64` / `aarch64` | `arm64` | `arm64` |

The Taskfile maps `uname -m` to package-format arch names. Dockerfiles
use Docker's `TARGETARCH` (Go-style) for the Go download URL.

## Binary Linking

### Problem Statement

We need to ship Linux packages for RHEL/Rocky and Ubuntu, while general
CI runs mostly on Ubuntu. Different distros ship different glibc
versions, so a dynamically-linked Ubuntu binary may not run on older
target distros. A binary linked on Ubuntu 24.04 (glibc 2.39) would not
run on RHEL 9 (glibc 2.34), and may not run on Ubuntu 22.04 (glibc
2.35).

### Proposed Solution

Build dynamically-linked binaries against the target glibc version using
container builds as a stopgap, with Bazel hermetic toolchains as the
target solution. Minimize investment in pre-Bazel linking workarounds —
the container approach is acceptable for shipping Linux packages now. Do
not adopt static musl compilation without a performance test suite that
would catch regressions.

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

Build inside a container matching the target glibc version: `rockylinux:9`
for RPMs and `ubuntu:22.04` for DEBs. The packaging Taskfile runs these
builder containers with Docker. This has a testing gap: CI commonly runs
on Ubuntu 24.04 (glibc 2.39), so packages built in an older container can
have a different glibc floor than binaries built directly on the runner.
To truly "test what we ship," CI would need to also run tests inside the
same older-glibc container, potentially doubling CI overhead. For now,
this testing gap is probably acceptable. Package smoke tests validate
that the binary loads and runs correctly on the target platform. Full e2e
testing on the target glibc is likely not worth the effort until Bazel
enables testing against the same glibc version used for the release
build.

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
