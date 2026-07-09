#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/check_bazel_metadata.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

print_help() {
  cat >&2 <<'EOF'
Bazel metadata is not current with this source tree.
If your branch is behind its base, rebase or merge first, then run:
  task bazel-generate-metadata
Commit the resulting changes and rerun:
  task bazel-check-metadata
EOF
}

if ! ./scripts/run_task.sh check-bazel-fmt; then
  print_help
  exit 1
fi

if ! ./scripts/run_task.sh check-bazel-gazelle-generate; then
  print_help
  exit 1
fi

if ! ./scripts/run_task.sh check-bazel-generate-unit-test-suites; then
  print_help
  exit 1
fi

if ! ./scripts/run_task.sh check-bazel-multiple-go-libraries; then
  exit 1
fi

# Must run after fmt and gazelle since either could modify source BUILD
# files in .bazel/patches/build_files/. In practice gazelle skips them
# (they have `# gazelle:ignore`) but buildifier will reformat them.
if ! ./scripts/run_task.sh check-bazel-generate-patches; then
  print_help
  exit 1
fi

# Refresh Bazel module metadata here because `bazel mod tidy` alone may not
# fully update MODULE.bazel.lock. This keeps later Bazel commands from
# rewriting the lockfile unexpectedly.
if ! ./scripts/run_task.sh check-bazel-module-metadata; then
  print_help
  exit 1
fi
