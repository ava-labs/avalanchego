#!/usr/bin/env bash

# Resolve the pinned upstream override using real Bazel in a network-disabled
# container. Prepare image inputs first; do not reuse extracted repositories.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache_root="${BAZEL_IMAGE_CACHE_ROOT:-${repo_root}/.cache/bazel-image}"
repository_cache="${BAZEL_IMAGE_REPOSITORY_CACHE:-${cache_root}/outer-repository}"
root="$(mktemp -d "${cache_root}/test-repository-cache.XXXXXX")"
trap 'echo "Repository cache test outputs: $root"' EXIT
mkdir -p "$root/prepared" "$root/missing" "$root/empty-repository"

resolve_override() {
  local output="$1"
  local repository="$2"
  docker run --pull=never --rm \
    --user "$(id -u):$(id -g)" \
    --network none \
    --workdir /workspace \
    --env HOME=/cache/home \
    --mount "type=bind,src=$repo_root,dst=/workspace,readonly" \
    --mount "type=bind,src=$repository,dst=/inputs/repository,readonly" \
    --mount "type=bind,src=$output,dst=/cache" \
    avalanchego-bazel-builder:local /usr/local/bin/bazel \
    --batch --output_user_root=/cache/output \
    fetch --repo=@rules_distroless \
    --repository_cache=/inputs/repository \
    --repository_disable_download --repo_env=GOPROXY=off \
    --lockfile_mode=error
}

# A fresh output root must resolve the override from saved downloads alone.
if ! resolve_override "$root/prepared" "$repository_cache" > "$root/prepared.log" 2>&1; then
  echo 'prepared repository cache could not resolve rules_distroless without networking' >&2
  tail -60 "$root/prepared.log" >&2
  exit 1
fi

# Missing downloads must fail through Bazel, not fall back to a Git fetch.
if resolve_override "$root/missing" "$root/empty-repository" > "$root/missing.log" 2>&1; then
  echo 'rules_distroless unexpectedly resolved with an empty repository cache' >&2
  exit 1
fi
grep -Eq 'Failed to download repository @@rules_distroless[^:]*: download is disabled' "$root/missing.log"
if grep -Fq 'git fetch' "$root/missing.log"; then
  echo 'missing repository inputs triggered a Git fetch' >&2
  exit 1
fi
echo 'Upstream override resolved from prepared inputs; empty inputs failed with downloads disabled'
