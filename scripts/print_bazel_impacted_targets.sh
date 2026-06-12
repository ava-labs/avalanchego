#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: print_bazel_impacted_targets.sh [options]

Print newline-delimited impacted Bazel targets to stdout.

By default, this compares the current working tree (including uncommitted
changes) against HEAD. Pass --range to use an explicit diff range or
--base-sha for compatibility with older callers.

Options:
  --range <range>    Git diff range to analyze.
  --base-sha <sha>   Compatibility alias for --range <sha>... Defaults to BAZEL_IMPACTED_BASE_SHA or HEAD.
  --output <path>    Write impacted targets to this file instead of stdout.
  --verbose          Accepted for compatibility; logging is handled by the Go tool.
EOF
}

range_arg=""
base_sha="${BAZEL_IMPACTED_BASE_SHA:-}"
output_path=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --range)
      range_arg="$2"
      shift 2
      ;;
    --base-sha)
      base_sha="$2"
      shift 2
      ;;
    --output)
      output_path="$2"
      shift 2
      ;;
    --verbose)
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

if [[ -z "$range_arg" ]]; then
  if [[ -z "$base_sha" ]]; then
    base_sha=$(git rev-parse HEAD)
  fi
  range_arg="${base_sha}.."
fi

args=(go run ./tools/impactedtests labels --range "$range_arg")
if [[ -n "$output_path" ]]; then
  args+=(--output "$output_path")
fi

exec "${args[@]}"
