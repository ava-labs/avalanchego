# Bazel Multiarch Image Phase 1 Toolchain Debug Handoff

## Scope and constraints

This continues the Phase 1 builder-image spike described in
`bazel-multiarch-image-phase1-spike-handoff.md`.

- Do not edit `docs/design/bazel-multiarch-image.md` without discussion.
- Keep the Dockerfile/Buildx path unchanged.
- Keep the temporary `rules_distroless` Git override at
  `a9e2012bf5935f7a8fa9c17a768abbbbd135f2a3` until a BCR release contains
  `apt.install(mergedusr = True)`.
- Do not add final AvalancheGo OCI runtime rules until both binary builds and
  runtime validation pass.
- Preserve the existing user changes in the worktree.

## Changes made in this session

- Added a direct `rules_cc` `0.2.8` dependency and registered
  `//bazel/toolchains:debian_aarch64_toolchain` in `MODULE.bazel`.
- Added `bazel/toolchains/BUILD.bazel`.
  - It defines a Debian Bookworm aarch64 C/C++ toolchain.
  - It uses the `rules_cc` Unix toolchain configuration implementation with
    Debian-installed absolute tool paths.
  - The configured tools include `aarch64-linux-gnu-gcc`, `ar`, `ld`, `nm`,
    `objcopy`, `objdump`, and `strip`.
  - Its execution constraints are Linux amd64; its target constraints are
    Linux arm64.
  - The native amd64 build continues to use rules_cc's generated Debian host
    toolchain.
- Updated `MODULE.bazel.lock` with `bazelisk mod tidy`.
- Updated `scripts/bazel_image_spike.sh` to:
  - pass `--toolchain_resolution_debug` and `--subcommands`;
  - save each inner build log under `.cache/bazel-image/inner-<arch>.log`;
  - inspect the output with `readelf`; and
  - attempt each binary in a fresh `debian:12-slim` Docker container.

The runtime commands added to the spike currently make it fail on this arm64
host because the amd64 Bazel binary segfaults under Docker's QEMU emulation.
Do not hide or skip this failure without agreement.

## Successful build evidence

The required command was run:

```bash
./scripts/nix_run.sh ./scripts/bazel_image_spike.sh
```

After removing an incorrect `-lstdc++` default from the cross configuration,
both inner builds completed. The successful arm64 run reported:

- Toolchain resolution selected
  `//bazel/toolchains:debian_aarch64` for
  `//bazel/image:linux_arm64`.
- CGO C and assembly compilation invoked
  `/usr/bin/aarch64-linux-gnu-gcc`.
- CGO archive creation invoked `/usr/bin/aarch64-linux-gnu-ar`.
- CGO shared-library linking invoked `/usr/bin/aarch64-linux-gnu-gcc`.
- The final Go external link invoked:

  ```text
  -extar /usr/bin/aarch64-linux-gnu-ar
  -extld /usr/bin/aarch64-linux-gnu-gcc
  ```

- `readelf --file-header --program-headers` on the arm64 output reports:

  ```text
  Machine: AArch64
  [Requesting program interpreter: /lib/ld-linux-aarch64.so.1]
  ```

The log containing toolchain-resolution and CGO compile/link commands is:

```text
.cache/bazel-image/inner-arm64.log
```

The amd64 build resolves the existing generated Debian host toolchain and
continues to produce an x86-64 dynamically linked ELF binary with interpreter
`/lib64/ld-linux-x86-64.so.2`.

A second unchanged spike run showed 949 action-cache hits for amd64. The
second run was interrupted by the amd64 runtime validation failure before it
could complete the arm64 build again.

## Runtime validation results

The arm64 Bazel binary runs successfully in fresh Debian slim:

```text
avalanchego/1.14.2 [database=v1.4.5, rpcchainvm=45,
commit=dba6eeb30148c9257f24a61857d178dcec798747, go=1.25.10]
```

The amd64 Bazel binary reproduces this failure when Docker runs it on the
current arm64 host:

```text
qemu: uncaught target signal 11 (Segmentation fault) - core dumped
```

This is not an inability to run amd64 containers generally:

- `docker run --platform linux/amd64 debian:12-slim /bin/true` succeeds.
- `docker run --platform linux/amd64 golang:1.25-bookworm go version`
  succeeds.
- A small Go program runs in that amd64 Go image.

The arm64 Bazel binary passes the same Docker-image invocation form used by
`scripts/tests.build_image.sh`: a temporary Debian image was created with the
binary at `/avalanchego/build/avalanchego`, then run with:

```bash
docker run -t --rm --platform linux/arm64 IMAGE \
  /avalanchego/build/avalanchego --version
```

The equivalent amd64 temporary image reproduces the QEMU segfault. The
validation temporary Docker images and containers were removed afterward.

## Important correction about the existing Docker validation

`scripts/tests.build_image.sh` uses Docker/Buildx, a temporary local registry,
and a multiarch image index. Its runtime invocation is:

```bash
docker run -t --rm --platform "linux/$arch" "$target_image" \
  /avalanchego/build/avalanchego --version
```

The user reports that `task test-build-image` passes both architectures on
this host. That means the Docker-built amd64 AvalancheGo image runs correctly
under Docker/QEMU here. Do not attribute the Bazel amd64 crash to a generic
host or QEMU limitation.

The initial Bazel validation used a bare binary bind-mounted into a freshly
pulled Debian image. A subsequent temporary Docker image test proved the bind
mount was not the cause: the Bazel amd64 binary still crashes.

The locally retained `avalanchego:*` image is arm64 only. Its tag/embedded
commit is `df5a671c`, while the current checkout and Bazel binary are stamped
`dba6eeb30148c9257f24a61857d178dcec798747`. It is not a valid amd64,
same-source comparison artifact. The passing amd64 Docker artifact existed in
the temporary registry while `task test-build-image` ran and was removed by
that script's cleanup.

## Next investigation plan

1. Build and retain a fresh Dockerfile amd64 image from the current
   `dba6eeb` checkout, without changing the Dockerfile/Buildx production path.
   Validate it using the exact `tests.build_image.sh` Docker invocation.
2. Extract the Docker and Bazel amd64 binaries and compare:
   - `go version -m`;
   - `readelf --dynamic --notes --program-headers`;
   - external-link commands;
   - embedded commit, Go version, build tags, PGO metadata, dynamic loader,
     and shared-library requirements.
3. Run a controlled Bazel bisection, validating every candidate through the
   same temporary-Docker-image invocation:
   - disable `default.pgo`;
   - remove Bazel-only `prod` and `nocmpopts` tags;
   - replace Bazel native toolchain's `gold` external linker selection with
     the Docker/direct-Go default linker;
   - test combinations only if individual changes do not isolate it.
4. Confirm the cause by re-enabling all other differences and requiring that
   only the candidate difference changes the result.
5. Keep the smallest fix that passes amd64 and arm64 runtime validation.

Do not proceed to Phase 2 OCI rules until this completes.

## Continuation results

On commit `4a503489ae7a35bb0c2738437b041da31a13db51`, the required spike command
completed successfully:

```bash
./scripts/nix_run.sh ./scripts/bazel_image_spike.sh
```

Both the amd64 and arm64 outputs reported the expected stamped commit, the
expected Debian dynamic loader, and a successful `--version` run in fresh
`debian:12-slim` containers. In particular, the previously failing amd64 Bazel
binary ran successfully under Docker/QEMU, so the segfault did not reproduce.
No bisection changes were made.

A current-checkout Dockerfile amd64 image was also retained temporarily and
passed the same runtime invocation. Its executable differs from Bazel's
executable: Bazel links with gold and relro/now flags, uses the Bazel-only
`nocmpopts` and `prod` tags, and does not expose Go module metadata through
`go version -m`. These differences are not currently runtime failures.

`task test-build-image` then passed. It built and ran the normal and race
Dockerfile images for amd64 and arm64 through a local multiarch registry. The
existing uncommitted `scripts/tests.build_image.sh` changes were preserved.

The runtime-validation gate is now satisfied. Before starting Phase 2, decide
how the inner builder will give `rules_img`'s `image_load` access to the host
Docker daemon. The current wrapper does not mount the Docker socket, and the
load target runs inside the inner-builder container.

## Working-tree state

At handoff, `git status --short` showed existing user changes plus the work in
this spike:

```text
 M .gitignore
 M MODULE.bazel
 M MODULE.bazel.lock
 M Taskfile.yml
 M docs/design/README.md
?? bazel/
?? docs/design/bazel-multiarch-image-phase1-spike-handoff.md
?? docs/design/bazel-multiarch-image.md
?? docs/design/bazel-multiarch-image-phase1-toolchain-debug-handoff.md
?? scripts/bazel_image_spike.sh
```

## Phase 2 continuation results

The first runtime-image slice was added without changing the Dockerfile/Buildx
path:

- `rules_img` now packages `//main:avalanchego` with `image_from_binary`.
- The pinned Debian 12 slim base is referenced by its multi-platform index
  digest, `sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171`.
- The image preserves the current `/avalanchego/build/avalanchego` path,
  `/avalanchego/build` working directory, empty entrypoint, and `./avalanchego`
  command.
- The amd64 image is loaded with `image_load` using the eager Docker strategy.
  The inner builder mounts `/var/run/docker.sock` and includes the Debian
  `docker.io` client package.

The updated spike passed:

```text
./scripts/nix_run.sh ./scripts/bazel_image_spike.sh
```

The loaded amd64 Bazel image ran in a fresh `linux/amd64` Debian 12 slim
container and reported the expected stamped commit. The arm64 Bazel binary
also built and ran in a fresh `linux/arm64` Debian 12 slim container. Shellcheck,
buildifier, and `git diff --check` passed.

This is intentionally only the first amd64 runtime image. The next session
should implement Phase 3: configure `image_from_binary` to produce the amd64
and arm64 manifests and compose them into one image index. Load or push that
index to a local registry, inspect its platform metadata, and run both
architectures with explicit `--platform` selections.

## Phase 3 continuation results

Commit `9fc1165d58b83167b34a921946bdcba3b36a90d5` adds the multi-platform
runtime-image spike. It changes only `bazel/image/BUILD.bazel` and
`scripts/bazel_image_spike.sh`; the Dockerfile/Buildx path remains unchanged.
`docs/design/bazel-multiarch-image.md` was not changed.

- `image_from_binary(name = "avalanchego")` now specifies both
  `:linux_amd64` and `:linux_arm64`. With two platforms, `rules_img` creates
  an image index and uses platform transitions to create each manifest.
- `push_avalanchego` pushes that index to
  `localhost:5000/avalanchego-bazel:phase3` with `image_push`.
- The spike starts a disposable `registry:2` container on the host network,
  so the inner builder can reach it as `localhost:5000`. Its exit trap stops
  only the registry it started.
- The inner push uses `IMG_INSECURE=1` and `--insecure` because this local
  registry serves plain HTTP. Do not copy that configuration to a production
  registry.
- The spike uses `docker buildx imagetools inspect` to print index metadata,
  then runs the registry image with `--platform linux/amd64` and
  `--platform linux/arm64`.

The required command passed:

```bash
./scripts/nix_run.sh ./scripts/bazel_image_spike.sh
```

The resulting OCI index was reported as
`sha256:a68b9357b727e769a85bd716470b17ef1bc5bdc4f626791dab7cf4071e315665`.
`imagetools inspect` reported an amd64 manifest and an arm64/v8 manifest.
Both explicit platform runs printed the expected AvalancheGo version stamped
with commit `f7c242c5d0a3370e794dc7fbdbe41593d778011f`.

`git diff --check`, ShellCheck on `scripts/bazel_image_spike.sh`, and
Buildifier check mode on `bazel/image/BUILD.bazel` also passed. The registry is
stopped after the spike, so its pushed image is deliberately not retained.

### Review and maintenance constraints

- This remains a spike, not a production publishing interface. The registry,
  repository, and tag are intentionally hard-coded for local validation. A
  supported path must make destination, authentication, TLS, and tagging
  explicit instead of reusing `IMG_INSECURE`.
- The fixed host port `5000` and `docker run --network host` require a Linux
  Docker host with that port available. This is the simplest way for the
  containerized inner Bazel process to reach a host-owned disposable registry;
  it is not portable to all Docker Desktop network models.
- Docker cannot load a multi-platform index into its legacy image store as one
  local image. Pushing to a registry preserves the index and permits explicit
  platform pulls. Do not replace the registry push with `image_load` if the
  goal is to validate the combined index.
- The script still builds and runs each bare binary before it pushes the index.
  The index's split platform transitions then build/package both manifests in a
  separate inner Bazel invocation. This duplicates analysis and can be slow,
  but keeps Phase 1 binary validation independent from OCI packaging. Reduce
  this duplication only after preserving equivalent binary and image runtime
  checks.
- The runtime image continues to use the pinned Debian 12 slim multi-platform
  base and preserves `/avalanchego/build/avalanchego`,
  `/avalanchego/build`, empty entrypoint, and `./avalanchego` command. Changes
  to the base digest, executable path, or image config need a comparison with
  the Dockerfile image and both-platform runtime validation.
- The Docker socket remains a precondition because the outer Bazel target loads
  the locked builder image. The inner push communicates with the registry over
  the network; do not treat the socket mount as a general registry credential
  or publishing mechanism.

### Recommended next work

First decide whether Bazel image publishing is intended to be a supported
production path. If it is, define the registry naming, tag/stamping, TLS and
credential model, retention policy, and CI ownership before generalizing the
spike. Then add CI coverage that verifies the index platform list and runs
both explicit platform selections. Keep the Dockerfile/Buildx test path as an
independent comparison until image configuration and release semantics are
agreed.

## Phase 4 continuation results

Commit `a445a45d2e` adds local Bazel image entrypoints and Bazel CI validation.
It does not add a production publishing interface, and
`docs/design/bazel-multiarch-image.md` remains unchanged.

### Scope

Bazel image publishing remains a local-validation spike only. The fixed
`localhost:5000` registry, `IMG_INSECURE`, `--insecure`, and host networking
remain limited to the test path. No production destination naming, tagging,
TLS, authentication, credentials, retention policy, or publishing ownership
has been defined.

### Entrypoints and validation

`Taskfile.yml` now provides:

- `bazel-build-image`, which invokes `scripts/bazel_image_spike.sh
  --build-only`. It builds the amd64 and arm64 binaries and the combined image
  index, but does not start a registry or run containers.
- `bazel-test-build-image`, which invokes the full script. It retains the
  direct amd64 and arm64 binary runtime checks, pushes the index to the
  disposable registry, inspects the index metadata, and runs both image
  platforms explicitly.

The script now resolves each direct binary through that architecture's
`bazel-bin` symlink rather than selecting the first matching cached output.
It checks that the ELF machine matches the requested architecture before each
runtime check. This avoids stale output from a different target platform being
validated as the requested one.

The following passed locally with a fresh `BAZEL_IMAGE_CACHE_ROOT`:

```bash
BAZEL_IMAGE_CACHE_ROOT="$PWD/.cache/bazel-image-task-validation" \
  task bazel-build-image
BAZEL_IMAGE_CACHE_ROOT="$PWD/.cache/bazel-image-task-validation" \
  task bazel-test-build-image
```

The test run reported an OCI index with `linux/amd64` and `linux/arm64/v8`
manifests. Both direct binary checks and both explicit image-platform runs
reported the stamped commit `9fc1165d58b83167b34a921946bdcba3b36a90d5`.

ShellCheck, `bash -n`, Actionlint, Task dry-runs, and `git diff --check` also
passed. The disposable registry was removed by the script. The pre-existing
`kind-registry` container was not changed.

### CI ownership

`.github/workflows/bazel-ci.yml` has an `image` job. On Linux it prepares
Bazel, sets up QEMU, and runs:

```bash
./scripts/run_task.sh bazel-test-build-image
```

The job is included in `bazel-required`. On non-Linux Bazel CI runners, the
Docker-specific step is skipped and the job succeeds. The normal
Dockerfile/Buildx image validation remains only in Go CI; it was not replaced
or combined with the Bazel image job.

The updated Bazel CI workflow has not run remotely yet. Its first run should
compare its result with the independent Dockerfile/Buildx CI job. Do not move
the local registry settings into a production publishing path.
