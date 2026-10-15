#!/usr/bin/env bash

set -euo pipefail

# Checked-in list of Bazel CI target patterns used to prepare the build
# dependency cache, plus tools required by Bazel metadata checks.
#
# e.g.,
# source ./scripts/bazel_ci_dependency_list.sh
# bazel_ci_target_patterns       # Bazel target patterns whose dependencies setup prepares for later CI jobs
#
# Update this file when Bazel CI starts running a new command. The targets are
# fetched by ./scripts/cache_bazel_ci_build_dependencies.sh. The target patterns
# are checked by ./scripts/run_bazel_ci_command.sh; additional dependencies are
# validated by running their consumers with repository downloads disabled.

bazel_ci_target_patterns() {
  cat <<'EOF'
//main:avalanchego
//...
//ids:ids_test
EOF
}

# Tools required by Bazel metadata checks and commands without target patterns.
# //... does not include external targets such as Buildifier. `bazel mod tidy`
# uses Buildozer to update use_repo() calls in MODULE.bazel.
bazel_ci_additional_dependency_targets() {
  cat <<'EOF'
@buildifier_prebuilt//:buildifier
@@buildozer++buildozer_binary+buildozer_binary//:buildozer.exe
EOF
}

bazel_ci_dependency_targets() {
  bazel_ci_target_patterns
  bazel_ci_additional_dependency_targets
}
