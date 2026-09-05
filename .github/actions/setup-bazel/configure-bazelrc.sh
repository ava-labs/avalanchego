#!/usr/bin/env bash

set -euo pipefail

cat >> "$HOME/.bazelrc" <<EOF
# Added by .github/actions/setup-bazel
common --repository_cache=${REPOSITORY_CACHE_DIR}
common --repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1
common --repo_env=GOMODCACHE=${GO_MOD_CACHE_DIR}
EOF

BAZEL_REMOTE_CACHE_ENABLED="${BAZEL_REMOTE_CACHE_ENABLED:-true}"
BAZEL_REMOTE_CACHE_URL="${BAZEL_REMOTE_CACHE_URL:-}"
BAZEL_REMOTE_CACHE_AUTH_HEADER="${BAZEL_REMOTE_CACHE_AUTH_HEADER:-}"

if [[ "$BAZEL_REMOTE_CACHE_ENABLED" == "true" && -z "$BAZEL_REMOTE_CACHE_AUTH_HEADER" ]]; then
  echo "::warning::Bazel remote cache is enabled but BAZEL_REMOTE_CACHE_AUTH_HEADER is empty; continuing without the remote cache."
fi

if [[ "$BAZEL_REMOTE_CACHE_ENABLED" == "true" && -n "$BAZEL_REMOTE_CACHE_URL" && -n "$BAZEL_REMOTE_CACHE_AUTH_HEADER" ]]; then
  case "$BAZEL_REMOTE_CACHE_URL" in
    grpcs://*) ;;
    *)
      echo "error: BAZEL_REMOTE_CACHE_URL must use the grpcs:// scheme; see docs/bazel.md#why-the-remote-cache-uses-grpc" >&2
      exit 1
      ;;
  esac

  cat >> "$HOME/.bazelrc" <<EOF
# CI-only remote cache
build --remote_cache=${BAZEL_REMOTE_CACHE_URL}
build --remote_upload_local_results=true
build --remote_timeout=60
build --remote_retries=3
build --remote_cache_compression
build --remote_header="${BAZEL_REMOTE_CACHE_AUTH_HEADER}"
EOF
fi

echo 'RUN_TASK_PREFER_BAZEL=1' >> "$GITHUB_ENV"
