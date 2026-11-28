#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/cache_bazel_ci_build_dependencies.sh
# ./scripts/nix_run.sh ./scripts/cache_bazel_ci_build_dependencies.sh
#
# Used by `task bazel-cache-ci-build-dependencies` in the Bazel CI setup job,
# before downloads are disabled and metadata is checked. This fetches dependencies
# for the checked-in CI target patterns and additional tools, instead of every
# possible Bazel dependency.
#
# BAZEL selects the Bazel command (default: bazelisk). BAZEL_STARTUP_OPTIONS adds
# startup options, such as a temporary --output_base. The Bazel image build uses
# both to run this script with its mounted Bazel binary.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${REPO_ROOT}/scripts/bazel_ci_dependency_list.sh"

read -r -a startup_options <<<"${BAZEL_STARTUP_OPTIONS:-}"
bazel_cmd=("${BAZEL:-bazelisk}" ${startup_options[@]+"${startup_options[@]}"})

while IFS= read -r target_set; do
  [[ -n "${target_set}" ]] || continue
  read -r -a target_args <<<"${target_set}"
  "${bazel_cmd[@]}" fetch "${target_args[@]}"
done < <(bazel_ci_dependency_targets)

# bazel-check-metadata also runs `bazel mod deps`. It has no target patterns,
# but it must load every repository in the module graph with repository and
# Go module downloads disabled.
"${bazel_cmd[@]}" mod deps --lockfile_mode=error >/dev/null
