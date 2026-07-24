#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/cache_bazel_ci_build_dependencies.sh
# ./scripts/nix_run.sh ./scripts/cache_bazel_ci_build_dependencies.sh
#
# Used by `task bazel:cache-ci-build-dependencies` in the Bazel CI setup job,
# after the metadata check. This fetches only the dependencies needed by the
# checked-in Bazel CI target patterns instead of trying to cache every possible
# Bazel dependency.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${REPO_ROOT}/scripts/bazel_ci_dependency_list.sh"

while IFS= read -r target; do
  [[ -n "${target}" ]] || continue
  bazelisk fetch "${target}"
done < <(bazel_ci_bootstrap_targets)

while IFS= read -r target_set; do
  [[ -n "${target_set}" ]] || continue
  read -r -a target_args <<<"${target_set}"
  bazelisk fetch "${target_args[@]}"
done < <(bazel_ci_target_patterns)
