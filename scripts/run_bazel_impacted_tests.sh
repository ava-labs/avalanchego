#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: run_bazel_impacted_tests.sh [options]

Options:
  --range <range>         Git diff range to analyze.
  --base-sha <sha>        Compatibility alias for --range <sha>... Defaults to BAZEL_IMPACTED_BASE_SHA.
  --scope <expr>          Bazel target pattern/scope. Repeatable.
  --fallback-task <task>  Task to run when no base SHA/range is available.
  --print-only            Print impacted test labels instead of running bazel test.
EOF
}

require_command() {
  local command_name="$1"
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "ERROR: ${command_name} is required but not found on PATH" >&2
    exit 1
  }
}

range_arg=""
base_sha="${BAZEL_IMPACTED_BASE_SHA:-}"
fallback_task=""
print_only=0
scopes=()

run_fallback_task() {
  if [[ -z "$fallback_task" ]]; then
    return 1
  fi

  echo "falling back to full task ${fallback_task}" >&2
  exec task "$fallback_task"
}

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
    --scope)
      scopes+=("$2")
      shift 2
      ;;
    --fallback-task)
      fallback_task="$2"
      shift 2
      ;;
    --print-only)
      print_only=1
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

if [[ ${#scopes[@]} -eq 0 ]]; then
  echo "at least one --scope is required" >&2
  exit 2
fi

if [[ -z "$range_arg" && -n "$base_sha" ]]; then
  range_arg="${base_sha}.."
fi

if [[ -z "$range_arg" ]]; then
  if [[ -n "$fallback_task" ]]; then
    echo "No impacted diff range configured; running the full Bazel target set" >&2
    run_fallback_task
  fi

  echo "No impacted diff range configured" >&2
  exit 2
fi

printf 'WARNING: selective Bazel test mode enabled; running impacted tests for range %s\n' "$range_arg" >&2

require_command git
require_command bazelisk
if [[ -n "$fallback_task" ]]; then
  require_command task
fi

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

scratch_dir=$(mktemp -d)
impacted_tests="${scratch_dir}/impacted-tests.txt"

cleanup() {
  rm -rf "$scratch_dir"
}
trap cleanup EXIT

args=(go run ./tools/impactedtests manifest --range "$range_arg" --output "$impacted_tests")
for scope in "${scopes[@]}"; do
  args+=(--scope "$scope")
done

if ! "${args[@]}"; then
  echo "failed to compute impacted test manifest for range ${range_arg}" >&2
  run_fallback_task || exit $?
fi

if (( print_only )); then
  cat "$impacted_tests"
  exit 0
fi

if [[ ! -s "$impacted_tests" ]]; then
  echo "no impacted test targets selected for requested scopes" >&2
  exit 0
fi

mapfile -t test_targets < "$impacted_tests"
printf 'running impacted tests:\n' >&2
printf '  %s\n' "${test_targets[@]}" >&2
exec bazelisk test "${test_targets[@]}"
