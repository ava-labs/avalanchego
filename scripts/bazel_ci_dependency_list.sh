#!/usr/bin/env bash

set -euo pipefail

# Checked-in list of Bazel CI target patterns used to prepare the build
# dependency cache.
#
# e.g.,
# source ./scripts/bazel_ci_dependency_list.sh
# bazel_ci_target_patterns       # Bazel target patterns whose dependencies setup prepares for later CI jobs
#
# Update this file when Bazel CI starts running a new command. The targets are
# fetched by ./scripts/cache_bazel_ci_build_dependencies.sh and checked by
# ./scripts/run_bazel_ci_command.sh.

bazel_ci_target_patterns() {
  cat <<'EOF'
//main:avalanchego
//...
//ids:ids_test
EOF
}

# Additional dependencies required by Bazel commands in Go CI that do not take
# target patterns. `bazel mod tidy` uses this generated repository to update
# use_repo() calls in MODULE.bazel.
bazel_ci_additional_dependency_targets() {
  cat <<'EOF'
@@buildozer++buildozer_binary+buildozer_binary//:buildozer.exe
EOF
}

bazel_ci_dependency_targets() {
  bazel_ci_target_patterns
  bazel_ci_additional_dependency_targets
}
