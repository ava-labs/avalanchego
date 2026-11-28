#!/usr/bin/env bash
set -euo pipefail

# Used by both Dockerfile compilation stages. Online compilation inherits the
# cache URL and credentials from the secret-mounted .bazelrc.local.
# Offline compilation must not attempt cache connections, even if an imported
# rc file configures one. This override leaves the checkout's settings unchanged.
remote_cache_options=()
if [[ "${BAZEL_BUILD_MODE:?}" == offline ]]; then
  remote_cache_options=(--remote_cache=)
fi
# Secret changes do not invalidate a BuildKit layer. The invocation key forces
# Bazel to run with the current settings; Bazel can still reuse cached results.
# See docs/image-builds.md#bazel-built-avalanchego-image.
test -n "${BAZEL_INVOCATION_KEY}"
# Keep the base reference tied to FROM, including its pinned image digest.
# For fingerprint limitations, see docs/image-builds.md#bazel-built-avalanchego-image.
builder_identity="$(
  {
    printf 'base=%s\narchitecture=%s\nbazel=%s\n' \
      "${BAZEL_BUILDER_IMAGE:?}" "${TARGETARCH:?}" "$(cat .bazelversion)"
    dpkg-query -W -f='${binary:Package}=${Version}\n' | LC_ALL=C sort
  } | sha256sum | cut -d' ' -f1
)"
race_args=()
if [[ "${BAZEL_IMAGE_RACE:-0}" == 1 ]]; then
  race_args+=(--config=race)
fi
bazel build --compilation_mode=opt --config=release --stamp --announce_rc=false \
  --repository_disable_download --repo_env=GOPROXY=off \
  ${remote_cache_options[@]+"${remote_cache_options[@]}"} \
  --action_env="BAZEL_IMAGE_BUILDER=${builder_identity}" ${race_args[@]+"${race_args[@]}"} //main:avalanchego
install -m 0755 bazel-bin/main/main_/main /tmp/avalanchego
