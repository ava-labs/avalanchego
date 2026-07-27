#!/usr/bin/env bash

set -euo pipefail

# Checked-in list of Bazel CI target patterns used to prepare the build
# dependency cache.
#
# e.g.,
# source ./scripts/bazel_ci_dependency_list.sh
# bazel_ci_bootstrap_targets     # Needed before the first ./scripts/run_task.sh ... in CI
# bazel_ci_target_patterns       # Bazel target patterns whose dependencies setup prepares for later CI jobs
#
# Update this file when Bazel CI starts running a new command. The setup action
# uses the bootstrap targets directly. The rest are fetched by
# ./scripts/cache_bazel_ci_build_dependencies.sh and checked by
# ./scripts/run_bazel_ci_command.sh.

bazel_ci_bootstrap_targets() {
  cat <<'EOF'
//tools/external:task
EOF
}

bazel_ci_target_patterns() {
  cat <<'EOF'
//main:avalanchego
//... -- -//graft/...
//graft/coreth/... //graft/evm/...
//graft/subnet-evm/...
EOF
}
