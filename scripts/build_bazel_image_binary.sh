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
# This CI check validates the Bazel build path, not just the packaged binary.
# Reusing a BuildKit layer would skip Bazel and could hide failures under the
# current cache settings or download restrictions. Secret changes alone do not
# invalidate that layer. A unique invocation key forces Bazel to run, while
# still allowing Bazel to reuse its own cached results.
test -n "${BAZEL_INVOCATION_KEY}"
# Best-effort cache separation, not a complete record of compiler inputs.
# Keep the base reference tied to FROM, including its pinned image digest.
builder_identity="$(
  {
    printf 'base=%s\narchitecture=%s\nbazel=%s\n' \
      "${BAZEL_BUILDER_IMAGE:?}" "${TARGETARCH:?}" "$(cat .bazelversion)"
    dpkg-query -W -f='${binary:Package}=${Version}\n' | LC_ALL=C sort
  } | sha256sum | cut -d' ' -f1
)"
bazel build --compilation_mode=opt --config=release --stamp --announce_rc=false \
  --repository_disable_download --repo_env=GOPROXY=off \
  ${remote_cache_options[@]+"${remote_cache_options[@]}"} \
  --action_env="BAZEL_IMAGE_BUILDER=${builder_identity}" //main:avalanchego
install -m 0755 bazel-bin/main/main_/main /tmp/avalanchego
