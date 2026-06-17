#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <bazel-label> [args...]" >&2
  exit 2
fi

label="$1"
shift

tool_path="$("$(dirname "$0")/bazel_built_file.sh" "$label")"
exec "$tool_path" "$@"
