#!/usr/bin/env bash

set -euo pipefail

cat >> "$HOME/.bazelrc" <<EOF
# Added by .github/actions/setup-bazel
common --repository_cache=${REPOSITORY_CACHE_DIR}
common --repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1
common --repo_env=GOMODCACHE=${GO_MOD_CACHE_DIR}
EOF

remote_cache_rc="$HOME/.bazelrc.avalanchego-remote-cache"
rm -f "$remote_cache_rc"
cat >> "$HOME/.bazelrc" <<EOF
# CI-only remote cache, if configured.
try-import $remote_cache_rc
EOF

if [[ "${BAZEL_REMOTE_CACHE_ENABLED:-true}" == "true" && -n "${BAZEL_REMOTE_CACHE_URL:-}" && -n "${BAZEL_REMOTE_CACHE_AUTH_HEADER:-}" ]]; then
  cat > "$remote_cache_rc" <<EOF
build --remote_cache=${BAZEL_REMOTE_CACHE_URL}
build --remote_upload_local_results=true
build --remote_timeout=30
build --remote_retries=2
build --remote_retry_max_delay=2s
build --remote_cache_compression
build --remote_header="${BAZEL_REMOTE_CACHE_AUTH_HEADER}"
EOF
fi

echo "BAZEL_REMOTE_CACHE_RC=$remote_cache_rc" >> "$GITHUB_ENV"

echo 'RUN_TASK_PREFER_BAZEL=1' >> "$GITHUB_ENV"
