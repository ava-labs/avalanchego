# Bazel Multi-Architecture AvalancheGo Image Build

## Status

Draft for iteration. This document records the intended direction, constraints,
open decisions, risks, and an implementation sequence. The existing Dockerfile
image build remains the production path until the Bazel path reaches parity and
is explicitly selected to replace it.

## Summary

Add a parallel Bazel-owned build for the published AvalancheGo Debian image for
`linux/amd64` and `linux/arm64`.

Bazel must own the complete dependency graph from source code through the OCI
image:

```text
Go and, in the future, Firewood Rust source
  -> native/FFI libraries
  -> AvalancheGo executable
  -> OCI filesystem layer
  -> architecture-specific OCI image
  -> multi-architecture OCI image index
```

Run Bazel inside one pinned Debian Bookworm amd64 builder container. It uses
Debian's native amd64 compiler and Debian's arm64 cross compiler, glibc,
headers, and linker rather than reproducing a Debian sysroot. The published
runtime image remains based on `debian:12-slim`, and AvalancheGo remains
dynamically linked against glibc.

Local builds use persistent Bazel disk caches. CI restores Bazel external build
inputs with GitHub Actions caches and retrieves or stores action outputs with
the existing optional Bazel remote-cache configuration.

## Motivation

The current Dockerfile compiles AvalancheGo with `go build`. BuildKit caching or
mounted Go caches could improve that build, but they would only optimize the Go
build as a separate operation.

The near-term Firewood direction is to move its Rust implementation into this
repository and build it from source with Bazel. The image build must therefore
consume the Bazel executable target rather than independently rebuilding the
executable with `go build`. This lets Bazel track and cache invalidation through
Rust, native/FFI, Go, linking, and image construction actions.

The objective is not only to cache Docker layers. The objective is to make the
published image a downstream output of the same Bazel graph used to build the
executable.

## Scope

### In scope

- The standard AvalancheGo runtime image currently built from the root
  `Dockerfile`.
- Linux amd64 and Linux arm64 architecture-specific images.
- A multi-architecture OCI index containing those two images.
- A local developer build and smoke-test path.
- A CI build and smoke-test path on one amd64 runner.
- Existing image tags needed to compare the Bazel path with the current path.
- The race-detector variant after the standard image path works.
- Local disk caching and optional remote caching.

### Out of scope

- Replacing the existing `Dockerfile`, `scripts/build_image.sh`, or its CI job in
  the initial implementation.
- Antithesis images.
- Subnet-EVM images.
- Bootstrap monitor images.
- Other application images in the repository.
- Static linking with either musl or glibc.
- A custom hermetic Debian sysroot or compiler toolchain. Bazel configuration
  that selects Debian's packaged arm64 cross compiler is in scope.
- Non-Linux runtime images.
- Firewood's Rust migration itself. The image graph must permit that future
  dependency, but this project does not perform the migration.

## Constraints and invariants

### Runtime image

- Downstream consumers must continue to receive a Debian-based image.
- The initial Bazel image must use `debian:12-slim`, pinned by digest rather than
  by a mutable tag alone.
- The image must support only `linux/amd64` and `linux/arm64` initially.
- Preserve the current runtime contract:
  - executable: `/avalanchego/build/avalanchego`
  - working directory: `/avalanchego/build`
  - command: `./avalanchego`
  - `/avalanchego/build` exists in the image
- Preserve the expected version and Git commit stamping.

### Linking and build environment

- AvalancheGo remains dynamically linked against glibc.
- Do not introduce musl.
- Do not statically link glibc.
- Build against a glibc baseline compatible with the runtime image. The initial
  implementation uses a Debian Bookworm builder to match the existing root
  Dockerfile's Bookworm builder and runtime stages.
- CGO remains enabled, including `CGO_CFLAGS=-O2 -D__BLST_PORTABLE__`.
- The race-detector build must remain possible; do not select a toolchain design
  that prevents it.

The package-building rules under `.github/packaging` have separate Ubuntu and
Rocky Linux compatibility floors. Those package artifacts are not part of this
image project. Their documented rationale still establishes why this project
must not casually change libc or linking behavior.

### Bazel graph ownership

- The OCI image target must directly depend on the Bazel AvalancheGo executable
  target or on a Bazel packaging target that directly depends on it.
- Do not build a binary with Bazel, copy it to an unmanaged staging directory,
  and invoke an unrelated Docker build. That would break dependency tracking at
  the image boundary.
- Image layer and manifest creation must be cacheable Bazel actions.
- Registry tags and credentials are publication policy and should remain outside
  the content dependency graph where possible.

### Existing path

- Keep the current image implementation working while the Bazel implementation
  is developed and evaluated.
- Add distinct tasks and scripts for the Bazel path.
- Do not route Antithesis, Subnet-EVM, bootstrap monitor, or other callers of
  `scripts/build_image.sh` through the new implementation.

## Proposed architecture

### Single-runner cross-architecture build

Run one Bazel target in a Debian Bookworm amd64 builder container. The container
uses Debian's native amd64 compiler and `gcc-aarch64-linux-gnu` to build both
Linux target architectures without QEMU:

```text
Debian Bookworm amd64 builder
  -> Bazel linux/amd64 executable -> OCI manifest
  -> Bazel linux/arm64 executable -> OCI manifest
  -> Bazel OCI index
```

This matches the current root Dockerfile's cross-compiler model while retaining
Bazel ownership of the graph. It is preferred over a native-runner matrix
because one runner can construct the complete multi-architecture index as a
single Bazel output. It needs no registry or artifact handoff between
architectures, compiles the arm64 target without emulation, and gives both
architectures the same source revision and build environment.

The implementation must add Bazel platform and C/C++ toolchain definitions for
`linux/amd64` and `linux/arm64`. These definitions select Debian-provided tools;
they do not package or maintain a separate sysroot. The arm64 toolchain must
provide the target headers, libraries, linker, and compiler needed by CGO.

Local and CI builds run on amd64 initially. An arm64 host implementation is not
required for the first path. If it is added later, it may use an analogous amd64
cross compiler or a separate native builder, but it must not change the
one-amd64-runner CI design.

### Bazel-built builder image

Add a dedicated builder image as a Bazel OCI target, separate from the published
runtime image. It is an outer Bazel output that Docker loads and runs; the inner
Bazel invocation in that container builds AvalancheGo and its OCI image.

```text
outer Bazel: locked Debian package inputs -> builder OCI image
Docker:      load and run that builder image
inner Bazel: AvalancheGo source -> binary -> final OCI image/index
```

The builder image should contain only the host facilities inner Bazel actions
require, initially:

- Debian Bookworm;
- CA certificates and download utilities;
- Git;
- Debian GCC/binutils and required CGO development files;
- `gcc-aarch64-linux-gnu` and its arm64 cross-development dependencies; and
- Bazelisk or another pinned way to launch the Bazel version in `.bazelversion`.

Use a maintained Bazel-aware Debian-package integration rather than an opaque
Docker `apt-get install` layer. The initial spike evaluates `rules_distroless`,
which is intended to replace Debian installation commands and supports locked
package repositories. It must prove that Bookworm and the complete native/cross
compiler package closure are supported before this ruleset becomes a committed
dependency. If it is unsuitable, stop and select another maintained integration;
do not hide mutable APT resolution in a `genrule` or Dockerfile.

The package source, package versions, and package closure must be locked. The
lock and pinned Debian base are inputs to the outer builder-image target. This
makes package downloads reusable through Bazel's repository cache and GitHub
Actions external-input cache, while `rules_img` makes builder-layer outputs
eligible for the Bazel remote cache. A cold cache miss may download the locked
package archives, but must not resolve a different toolchain.

Continue to let Bazel/rules_go obtain the Go SDK version from `go.mod`. Avoid a
second independently versioned Go installation unless bootstrap testing proves
one is necessary. The future Rust toolchain should be declared through Bazel so
its version and inputs participate in action keys. Do not install an unpinned
Rust toolchain in the builder image as a shortcut.

The resulting immutable builder digest is a build-toolchain input to the inner
Bazel build, not merely a Docker implementation detail. Pass it through a
declared inner-Bazel configuration value (for example, a fixed `--action_env`
value set by the wrapper). This prevents inner local or remote action-cache
entries built with one compiler/libc image from being reused after the locked
builder changes. The wrapper must derive the value from the actual outer-Bazel
image output that Docker loads.

### OCI rules

Use `rules_img`, initially pinned to the vetted BCR release (`0.3.19` at the
time of this decision).

`rules_img` is the better match for this project's remote-cache motivation:

- `image_from_binary` makes the image a direct downstream target of
  `//main:avalanchego` and supports an explicit binary path.
- Its layer rules store a compact stream representation instead of full layer
  tarballs in the Bazel remote cache.
- Base-image pulls are shallow by default, so Debian base layers are not copied
  into Bazel action outputs merely to construct a derived manifest.
- It supports local loading, index push, Bzlmod, and Bazel 8.
- It is under active development and receives ongoing upstream support.

`rules_oci` was considered because its API is established and it is stable in
maintenance mode. Its own documentation recommends evaluating `rules_img` when
remote-cache and remote-execution byte transfer matters: `rules_oci` passes
files and directories between actions and can transfer substantially more data.
That conflicts with one of this project's primary goals.

Limit exposure to the newer `rules_img` API:

- Start with its basic pull, binary layer/image, load, and push APIs.
- Use the default eager local push initially. Do not adopt lazy push,
  push-at-build-time, CAS-registry, BES, signing, eStargz, or SOCI features in
  the first implementation.
- Keep registry and tag policy in the orchestration layer.
- Leave image creation timestamps unset so image content remains reproducible.
- Keep the image definition small enough to migrate through standard OCI layout
  interoperability if maintenance changes.

The image configuration must explicitly override `image_from_binary` defaults
to preserve the current contract. In particular, package the binary at
`/avalanchego/build/avalanchego`, set the working directory to
`/avalanchego/build`, clear the generated entrypoint, and set the command to
`./avalanchego`. Confirm during the spike whether `include_runfiles = False` is
correct for the dynamically linked Go executable; dynamic system libraries are
provided by Debian rather than Bazel runfiles.

### Multi-architecture index and publication

The `rules_img` image target uses its platform split transition to configure the
same executable and image definition for `linux/amd64` and `linux/arm64` in one
Bazel invocation. Its `image_index` output references the two Bazel-produced
manifests and must contain exactly those platforms.

A single `rules_img` push target publishes that completed index and applies
commit and image tags. It must not first publish separately tagged architecture
images or rely on GitHub artifacts to transfer OCI layouts. The push target may
also apply architecture-specific tags only if a later operational requirement
needs them; they are not part of the current published-image contract.

Local tests load or push the completed index to the existing local registry and
run both images with explicit `--platform`. On an amd64 host, only runtime smoke
testing of arm64 requires QEMU; compilation does not.

Untrusted pull-request jobs must not receive production registry credentials.
They build, load, inspect, and smoke-test the completed index locally. Trusted
release jobs push the same Bazel target to the production registry.

## Cache model

The build graph is identical in local development and CI. Only cache backends
and credentials differ.

### Local development

Use persistent local paths or Docker volumes for:

- the outer Bazel repository cache, including locked Debian package archives;
- the outer Bazel disk and action caches for builder-image outputs;
- the inner Bazel repository cache;
- the inner shared Gazelle `GOMODCACHE`, while the current `go_repository` setup
  requires it;
- the inner Bazel disk action cache; and
- optionally the inner Bazel output user root for faster analysis and server
  reuse.

Keep outer and inner cache roots separate. They execute with different
configuration and toolchain identities; sharing a directory would obscure cache
behavior and can create ownership or concurrency problems.

Use separate output roots for amd64 and arm64. A shared content-addressed disk
cache may be considered if Bazel supports the intended concurrent access, but
correctness must not depend on cross-architecture sharing.

Developers must be able to build without remote-cache access. Developers with
credentials may opt into the remote cache through local configuration; the
builder image must not embed remote-cache URLs or credentials.

### CI

Reuse the model already implemented by `.github/actions/setup-bazel`:

- GitHub Actions caches restore Bazel's `repository_cache` and shared Gazelle
  `GOMODCACHE`. These are downloaded external build inputs, not compiled action
  outputs.
- The Bazel remote cache stores action outputs and cacheable results.
- On a remote-cache miss, CI executes the action and uploads the result when
  authorized.

The existing action currently configures host paths in `$HOME/.bazelrc`. The
containerized implementation must deliberately bridge that configuration into
the builder container. Likely approaches are mounting the restored directories
at fixed container paths and generating or mounting a container-specific
bazelrc. Do not assume host absolute paths or `$HOME` values match inside the
container. That generated configuration must also include the immutable builder
image digest as a declared action input, so remote results are segregated by the
actual Debian tooling image.

Do not log `BAZEL_REMOTE_CACHE_AUTH_HEADER`. Preserve the existing behavior in
which remote caching is optional when URL or credentials are absent and can be
disabled for scheduled cache-independent validation.

### Cache validation

Do not use elapsed time as the only cache test. During implementation, capture
Bazel execution summaries or build-event data showing that:

- a second unchanged local build uses disk-cache hits;
- a clean CI workspace can use remote-cache hits after external inputs are
  restored;
- changing a narrow Go input rebuilds only affected actions and downstream
  linking/image actions;
- later, changing a narrow Rust input has the equivalent bounded invalidation.

`bazel clean` and deleting the disk cache are different operations. Tests and
documentation must say which state is cleared.

## User and CI entrypoints

Follow `docs/tasks.md`: keep Taskfile entries small and put non-trivial logic in
scripts. Tentative public tasks are:

- `bazel-build-image`: build the host-architecture Bazel OCI image;
- `bazel-load-image`: build and load the host-architecture image into Docker;
- `bazel-test-build-image`: run Bazel image smoke tests;
- a distinct multi-architecture or CI task if one command cannot remain simple.

Final names should be chosen with the existing alphabetically sorted Taskfile
and current `bazel-*` naming conventions in mind.

The orchestration script should be responsible for:

- selecting host or requested architecture;
- starting the matching builder container;
- mounting source and caches;
- passing Bazel cache configuration without exposing credentials;
- invoking the Bazel target;
- loading or pushing outputs as requested;
- assembling the index when both architectures are requested.

Avoid reproducing all existing tag policy in BUILD files. Reuse or factor the
current `scripts/git_commit.sh` and `scripts/image_tag.sh` behavior where
practical.

## Implementation sequence

Each phase should leave the existing image path unchanged and establish the
fastest practical feedback loop before proceeding.

### Phase 0: Capture current-image parity requirements

Before implementation, build or inspect the current image and record:

- base image and supported architectures;
- image config, working directory, command, and relevant environment;
- executable path and permissions;
- `avalanchego --version` output and embedded commit;
- dynamic loader and shared-library requirements;
- normal and race image tag behavior;
- current compressed/uncompressed size as comparison data, not a hard parity
  requirement.

Turn stable runtime expectations into a smoke-test script that can run against
both current and Bazel images.

### Phase 1: Spike the Bazel-built builder and cross-architecture binary

1. Add a small outer-Bazel builder-image prototype using a pinned Debian
   Bookworm base, `rules_img`, and a candidate locked Debian-package integration
   (`rules_distroless` first).
2. Resolve and lock the native amd64 compiler, `gcc-aarch64-linux-gnu`, and all
   required target development packages. Review the lock as the intended
   toolchain closure.
3. Build the builder OCI image with outer Bazel, load it into Docker, and record
   its digest.
4. Confirm an unchanged outer build gets disk-cache hits locally and that a
   clean CI workspace can restore its package inputs from GitHub Actions cache
   and its builder-image actions from the remote cache.
5. Add Bazel Linux amd64 and Linux arm64 platform definitions and C/C++
   toolchain registrations that select the builder's Debian tools.
6. Run an inner optimized, release-stamped Bazel executable build for each
   target platform inside that amd64 builder, passing the loaded builder digest
   as a declared action input.
7. Mount persistent, separate outer and inner local cache directories.
8. Verify neither outer nor inner build leaves untracked files or modifications
   in the source checkout.
9. Verify both inner outputs:
   - are the expected architecture;
   - are dynamically linked through normal Debian loader paths;
   - contain no `/nix/store` references;
   - run in fresh target-architecture `debian:12-slim` containers; and
   - report the expected Git commit.
10. Run twice and verify inner disk-cache hits for both configurations.
11. Change the locked builder toolchain identity in a controlled test and
    verify that inner Bazel does not reuse the prior action-cache entries.

Do not add the AvalancheGo OCI rules until this phase works. This isolates
Debian package locking, outer-image caching, Docker loading, compiler selection,
CGO, cross-linking, stamping, permissions, and cache-mount problems from the
final image-rule problem.

### Phase 2: Build one AvalancheGo runtime image

1. Reuse the `rules_img` dependency added for the builder-image spike.
2. Pin the Debian 12 slim base for the host architecture.
3. Add an `image_from_binary` target that directly depends on the executable
   target.
4. Explicitly preserve the current binary path, working directory, empty
   entrypoint, and command.
5. Keep image timestamps and optional `rules_img` optimizations disabled.
6. Build and load the image into Docker with the eager local strategy.
7. Run the shared current/Bazel smoke tests.
8. Inspect remote-cache output volume as well as cache hits.
9. Rebuild after a narrow source change and inspect cache invalidation.

Start with the non-race host-architecture image.

### Phase 3: Add the multi-architecture index

1. Configure the `rules_img` platform split for `linux/amd64` and
   `linux/arm64`.
2. Build the resulting index in one amd64 builder invocation.
3. Verify its two manifests, platform metadata, Debian base, and runtime
   independently.
4. Load or push the completed index to a local registry.
5. Verify the index contains exactly the intended Linux architectures.
6. Run both images by explicit `--platform`; use QEMU only to execute the arm64
   image on the amd64 host.
7. Add a trusted-registry digest/tag inspection test for the eventual push
   target, without publishing production tags from pull-request jobs.

### Phase 4: Integrate CI caches

1. Extend or add a container-aware Bazel setup action without regressing current
   Bazel CI.
2. Add the image target to `scripts/bazel_ci_dependency_list.sh` if it is invoked
   through `run_bazel_ci_command.sh` and covered by the dependency-prefetch
   enforcement model.
3. Restore external inputs with GitHub Actions caches.
4. Mount or generate cache configuration for the builder container.
5. Enable remote action-cache reads and writes under the existing credential
   policy.
6. Verify a fresh job receives remote hits from a prior job.
7. Keep a scheduled or explicit cache-disabled build path to detect undeclared
   dependencies and unavailable external inputs.

### Phase 5: Add race and tag parity

1. Add the Bazel race image using the existing `--config=race` behavior.
2. Confirm dynamic glibc linking and runtime support on both architectures.
3. Preserve the known QEMU/kernel exception for an arm64 race image tested on
   incompatible amd64 GitHub kernels, if it still applies.
4. Implement commit, branch/release, `-r`, `latest`, and test-only `master` tag
   behavior needed for parity.
5. Ensure tag creation reuses manifests and does not rebuild image content.

### Phase 6: Parallel evaluation

Run the current and Bazel image jobs in parallel. Compare:

- smoke-test results;
- architecture/index metadata;
- embedded version and commit;
- dynamic dependencies;
- image configuration;
- image size;
- cold-build behavior;
- warm local disk-cache behavior;
- clean-workspace remote-cache behavior in CI.

Do not switch production publishing until parity criteria are agreed and met.
The cutover and removal of the old path require a separate explicit decision.

## Validation criteria

The initial implementation is successful when:

- `linux/amd64` and `linux/arm64` images are outputs of Bazel targets that depend
  on the Bazel AvalancheGo executable.
- The multi-architecture index advertises exactly those architectures.
- Both images use the pinned Debian 12 slim base.
- Both executables are dynamically linked against compatible glibc and contain
  no Nix store runtime paths.
- Both images run `avalanchego --version` and report the expected commit.
- The runtime filesystem path, working directory, and command match the existing
  contract.
- A local developer can build the host image with no remote-cache credentials
  and receives disk-cache hits on an unchanged rebuild.
- CI can restore external inputs independently of the remote action cache.
- Configured CI jobs can read and write remote action results without leaking
  credentials.
- The existing Dockerfile image build and unrelated image builds remain
  unchanged.

Race-image and full publishing-tag parity may be delivered after the standard
image criteria, but must be complete before replacing the existing production
path.

## Risks and mitigations

### Locked Debian package integration cannot construct the builder

The builder spike depends on a maintained package integration being able to
resolve and lock Bookworm's native compiler plus the arm64 cross compiler and
its complete development closure. Prove this before writing the final builder
rules. If `rules_distroless` cannot do it, evaluate a maintained alternative;
do not replace the lock with an unpinned APT command.

### Cross-architecture CGO toolchain resolution fails

The current Bazel configuration does not resolve a C/C++ toolchain when an arm64
Linux target is selected. The Phase 1 spike must prove that Bazel selects
Debian's `aarch64-linux-gnu` compiler, target headers, libraries, and linker for
all CGO actions. Capture the relevant Bazel toolchain-resolution and compile
commands in the spike notes. Do not proceed to OCI rules if either target falls
back to the host compiler or fails at link time.

QEMU is only needed to run arm64 smoke tests on the amd64 builder host; it is
not part of arm64 compilation.

### Builder and runtime glibc drift

A mutable builder or base tag could silently change ABI expectations. Pin image
digests and validate the dynamic loader and required symbol versions. Update
builder and runtime pins deliberately together when appropriate.

### Container cache mounts do not match existing CI configuration

The existing setup action writes host-specific paths to `$HOME/.bazelrc`.
Introduce explicit fixed container mount points and test with an empty runner.
Do not rely on accidental matching home directories.

### Source checkout is modified or outputs are root-owned

Bazel may create workspace symlinks, update the Bzlmod lockfile under the current
`--lockfile_mode=update`, or write files as the container user. Decide whether to
run with the caller UID or isolate all writable Bazel paths outside the checkout.
CI should fail if the build changes tracked metadata. Prefer read-only source
mounts once Bazel lockfile and workspace-status behavior support them.

### Remote-cache keys differ from existing host Bazel jobs

Containerized Debian toolchains can legitimately produce different action keys
from Nix or host Ubuntu/macOS builds. Do not require image builds to hit outputs
created by existing host builds. Require repeatable hits between equivalent
containerized builds. Sharing pure-language actions is an optimization, not a
correctness condition.

### OCI outputs consume excessive remote-cache or artifact storage

Base layers and OCI layouts can be large. Prefer shallow base pulls and
content-addressed registry reuse. Measure remote-cache and artifact transfer
before enabling broad uploads. Configure exclusions or split push behavior if
large unchanged base blobs would be repeatedly stored.

### Cross-compiled Rust/FFI output does not select the C linker

The initial Go/CGO spike is necessary but not sufficient for the Firewood goal.
When Rust sources enter the repository, their rules must select the same target
platform and C linker as the Go/CGO executable. Add a focused Rust-to-Go FFI
smoke target before treating Firewood integration as complete. Keep this as a
future implementation gate rather than adding a Rust toolchain to the initial
image change.

### Stamping defeats useful caching

Only stable release metadata required by the binary should affect its action
key. Do not include the current time in the image or binary unless required.
Ensure OCI creation metadata is deterministic. Tagging should happen after
content creation and should not invalidate build actions.

### `rules_img` API maturity or maintenance changes

`rules_img` is actively maintained but still uses a pre-1.0 version. Pin the
version and lockfile, use only the small basic API subset described above, and
review release notes before upgrades. Keep image definitions small and retain
standard OCI layout interoperability. Avoid coupling tag policy and CI
authentication deeply to ruleset-specific BUILD APIs.

### Future Rust/FFI integration requires additional system libraries

Keep the builder image minimal now, but treat newly required native packages as
pinned build inputs. Prefer Bazel-managed Rust toolchains and explicit FFI
rules. Validate that future Firewood changes invalidate the expected Rust,
linking, and image actions rather than causing an opaque external rebuild.

## Open decisions

Resolve these during the indicated spike rather than guessing during full
implementation:

1. Does `rules_distroless` resolve and lock the required Bookworm native and
   arm64 cross-toolchain package closure? If not, which maintained alternative
   does?
2. Exact builder-image definition and Bazelisk bootstrap mechanism.
3. Run the builder as the caller UID or isolate all outputs in root-owned Docker
   volumes?
4. Persist only repository/disk caches, or also persist the Bazel output user
   root locally?
5. Which maintained Bazel C/C++ toolchain-definition approach can correctly
   select Debian's native and `aarch64-linux-gnu` compilers without vendoring a
   sysroot?
6. Which existing tag-policy code should be factored for reuse rather than
   copied?
7. What image-size and remote-cache storage thresholds require a design change?
8. What exact parity is required for the race image before production cutover?

## Relevant existing files

- `Dockerfile` — current Debian Bookworm build and runtime image.
- `scripts/build_image.sh` — current Buildx build, push, architecture, race, and
  tag policy.
- `scripts/tests.build_image.sh` — current local registry and multi-architecture
  smoke test.
- `Taskfile.yml` — current Bazel and image task entrypoints.
- `main/BUILD.bazel` — Bazel AvalancheGo executable and Git commit stamping.
- `.bazelrc` — CGO, local disk cache, race, and release stamping configuration.
- `scripts/bazel_workspace_status.sh` — stable Git commit workspace status.
- `.github/actions/setup-bazel/` — CI external-input and remote-cache setup.
- `scripts/bazel_ci_dependency_list.sh` — checked-in CI dependency fetch set.
- `.github/workflows/bazel-ci.yml` — existing optional remote-cache policy.
- `.github/workflows/go-ci.yml` — current image test job.
- `.github/packaging/README.md` — dynamic glibc rationale and native
  architecture runner precedent.
