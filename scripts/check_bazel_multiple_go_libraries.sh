#!/usr/bin/env bash

# Gazelle can add a replacement go_library when a package/importpath is renamed
# without removing the old rule from the checked-in BUILD.bazel.  Bazel then attempts
# to build both targets, and the stale one often references files that no longer
# exist. This check keeps the policy simple: package-local BUILD.bazel files should
# define at most one go_library rule, so stale rename content fails early under
# bazel-check-metadata instead of surfacing later as a compile error.

set -euo pipefail

if ! [[ "$0" =~ scripts/check_bazel_multiple_go_libraries.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

failed=0

while IFS= read -r -d '' build_file; do
  count=$(
    awk '
      /^[[:space:]]*go_library\($/ {
        count++
      }
      END {
        print count + 0
      }
    ' "${build_file}"
  )

  if (( count > 1 )); then
    echo "${build_file}: contains ${count} go_library rules"
    failed=1
  fi
done < <(
  find . \
    -path './.git' -prune -o \
    -path './.direnv' -prune -o \
    -path './bazel-*' -prune -o \
    -path './.bazel/patches/build_files' -prune -o \
    -name 'BUILD.bazel' -type f -print0
)

if (( failed )); then
  cat >&2 <<'EOF'
Each BUILD.bazel file must define at most one go_library rule.
Multiple go_library rules in one directory are usually stale metadata from a package rename or move.
Remove the duplicate rule and rerun:
  task bazel-check-metadata
EOF
  exit 1
fi
