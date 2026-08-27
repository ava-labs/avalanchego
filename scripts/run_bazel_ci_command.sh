#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/run_bazel_ci_command.sh test //main:...
# ./scripts/run_bazel_ci_command.sh test //... -- -//graft/...
# BAZEL_CI_ENFORCE_DEPENDENCY_LIST=1 ./scripts/run_bazel_ci_command.sh test //graft/subnet-evm/...
#
# This is the Bazel CI wrapper for Bazel commands that take target patterns.  In CI it
# can reject commands whose target patterns are missing from
# scripts/bazel_ci_dependency_list.sh to ensure that jobs only use targets that setup
# has been configured to cache the build dependencies for.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${REPO_ROOT}/scripts/bazel_ci_dependency_list.sh"

usage() {
  echo "Usage: $0 <bazel-subcommand> [bazel args...]" >&2
  exit 1
}

[[ $# -gt 0 ]] || usage

extract_target_set() {
  local seen_separator=0
  local -a targets=()
  local arg

  for arg in "$@"; do
    if [[ "${seen_separator}" == "1" ]]; then
      targets+=("${arg}")
      continue
    fi

    if [[ "${arg}" == "--" ]]; then
      seen_separator=1
      targets+=("--")
      continue
    fi

    if [[ "${arg}" == -* ]]; then
      continue
    fi

    targets+=("${arg}")
  done

  printf '%s' "${targets[*]}"
}

assert_target_patterns_are_listed() {
  local target_patterns="$1"
  local allowed_target_patterns

  while IFS= read -r allowed_target_patterns; do
    [[ -n "${allowed_target_patterns}" ]] || continue
    if [[ "${allowed_target_patterns}" == "${target_patterns}" ]]; then
      return 0
    fi
  done < <(bazel_ci_target_patterns)

  {
    echo "error: Bazel CI command is not covered by setup's checked-in target pattern list"
    echo "target patterns: ${target_patterns}"
    echo "expected one of:"
    bazel_ci_target_patterns | sed 's/^/  - /'
  } >&2
  exit 1
}

subcommand="$1"
shift

if [[ -n "${BAZEL_CI_ENFORCE_DEPENDENCY_LIST-}" ]]; then
  target_patterns="$(extract_target_set "$@")"
  [[ -n "${target_patterns}" ]] || {
    echo "error: unable to determine Bazel target patterns for CI dependency-list enforcement" >&2
    exit 1
  }
  assert_target_patterns_are_listed "${target_patterns}"
fi

exec bazelisk "${subcommand}" "$@"
