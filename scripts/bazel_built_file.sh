#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <bazel-label>" >&2
  exit 2
fi

label="$1"
repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

bazelisk build "$label" >/dev/null
built_file="$(bazelisk cquery --output=files "$label" 2>/dev/null)"

if [[ -z "$built_file" ]]; then
  echo "ERROR: no built file found for $label" >&2
  exit 1
fi

if [[ "$built_file" != /* ]]; then
  built_file="$repo_root/$built_file"
fi

printf '%s\n' "$built_file"
