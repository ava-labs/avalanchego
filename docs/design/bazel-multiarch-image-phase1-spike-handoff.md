# Bazel Multiarch Image Phase 1 Spike Handoff

## Scope

This record describes the Phase 1 builder-image spike. It does not change the
multiarch image design or select the remaining C/C++ toolchain implementation.

The existing Dockerfile and Buildx path remain unchanged.

## Implemented files

- `MODULE.bazel`
  - Adds `rules_img` `0.3.19`.
  - Adds `rules_distroless` `0.9.4` with a temporary Git override at
    `a9e2012bf5935f7a8fa9c17a768abbbbd135f2a3`.
  - Uses `apt.install(mergedusr = True)` from that override.
  - Locks an amd64 Debian Bookworm snapshot from `20250101T000000Z`.
  - Declares native GCC, `gcc-aarch64-linux-gnu`, native and arm64 libc
    development packages, Git, certificates, patch, zip, and unzip.
  - Pins the Debian 12 slim amd64 base manifest and Bazel `8.0.1`.
- `MODULE.bazel.lock` and `bazel/image/bookworm.lock.json`
  - Lock Bazel modules and the 116-package Debian closure.
- `bazel/image/BUILD.bazel`
  - Defines `linux_amd64` and `linux_arm64` platforms.
  - Creates a `rules_img` builder manifest and `load_builder` target.
  - Adds the full Bazel binary and generated CA bundle to the image.
- `scripts/bazel_image_spike.sh`
  - Builds and loads the outer builder image.
  - Mounts the source tree read-only.
  - Uses separate outer and inner repository, disk, and output caches under
    `.cache/bazel-image/`.
  - Creates a fixed workspace-status script from the host Git commit. This is
    necessary because a read-only worktree mount does not include its external
    Git worktree metadata.
  - Builds `//main:avalanchego` with release stamping for amd64 and arm64.
- `Taskfile.yml`
  - Adds `bazel-image-spike`.
- `.gitignore`
  - Ignores `.cache/bazel-image/`.

## Temporary rules_distroless override

The published BCR release `rules_distroless@0.9.4` cannot use
`apt.install(mergedusr = True)`. Without this mode, package layers replace the
Debian 12 `/bin -> /usr/bin` symlink with a directory. The resulting builder
has no `/bin/sh`, and inner Bazel cannot run.

This is the known upstream issue
<https://github.com/bazel-contrib/rules_distroless/issues/53>.

Commit `a9e2012bf5935f7a8fa9c17a768abbbbd135f2a3` adds `mergedusr` to the
Bzlmod `apt.install` API. Keep the override until a BCR release contains that
commit. Then remove `git_override` and use the released version.

## Validation completed

Run:

```bash
./scripts/nix_run.sh ./scripts/bazel_image_spike.sh
```

Results:

- The outer builder target built and loaded successfully.
- The rebuilt builder preserves executable `/bin/sh`.
- The builder contains native `gcc`, `aarch64-linux-gnu-gcc`, Git, the CA
  bundle, and `/usr/aarch64-linux-gnu/lib/libc.so.6`.
- The inner amd64 release build succeeded.
- Its output is an x86-64 dynamically linked ELF binary. Its loader is
  `/lib64/ld-linux-x86-64.so.2`.
- An unchanged outer builder rebuild used action-cache hits.
- `shellcheck scripts/bazel_image_spike.sh`,
  `buildifier -mode=check MODULE.bazel bazel/image/BUILD.bazel`, and
  `git diff --check` passed.

The validation host was arm64 NixOS. Docker emulated the amd64 builder. Do not
use its elapsed time as an amd64 CI measurement.

## Current blocker

The inner arm64 command reaches Bazel analysis and fails with:

```text
Unable to find a CC toolchain using toolchain resolution.
Target: @rules_cc//cc:current_cc_toolchain
Platform: //bazel/image:linux_arm64
```

Setting `CC=aarch64-linux-gnu-gcc` is not enough. The CGO C targets require a
Bazel-resolved C/C++ toolchain for `linux_arm64`.

Do not add AvalancheGo runtime OCI rules yet. First add and validate a Debian
native and aarch64 cross C/C++ toolchain registration for the inner build.

## Required next work

1. Research a maintained Bazel 8 C/C++ toolchain-definition method for Debian
   host tools and `aarch64-linux-gnu-gcc`.
2. Add the smallest platform-constrained native and arm64 C/C++ toolchains.
3. Run the spike again. Capture `--toolchain_resolution_debug` and relevant
   CGO compile/link commands.
4. Confirm the arm64 output is an AArch64, dynamically linked ELF binary with
   interpreter `/lib/ld-linux-aarch64.so.1`.
5. Run the unchanged spike a second time. Check disk-cache hits for both
   platforms.
6. Run both binaries in fresh target `debian:12-slim` containers. The amd64
   runner is the authoritative CI validation environment.

## Constraints to retain

- Keep the Dockerfile/Buildx path unchanged.
- Scope only the standard AvalancheGo Debian image.
- Keep Debian 12 slim and dynamic glibc linking.
- CI uses one amd64 runner and cross-compiles amd64 plus arm64.
- The builder is a Bazel OCI target. Docker runs it. Inner Bazel builds the
  executable and later the final OCI images/index.
- Keep outer and inner caches separate.
- Do not change `docs/design/bazel-multiarch-image.md` without discussion.
