#!/usr/bin/env bash

# Run only Go test rules in a named repository scope. Bazel target patterns can
# include non-Go tests such as gazelle_test. Those tests do not accept Go test
# flags, including the shuffle flag used by scheduled tests. Query rule types
# first. This makes Bazel send Go flags only to Go test binaries.

set -euo pipefail

usage() {
  echo "Usage: $0 <all|avalanchego|coreth|subnet-evm|smoke> [bazel test options...]" >&2
  exit 1
}

[[ $# -gt 0 ]] || usage

scope="$1"
shift

case "$scope" in
  all)
    query_scope='//...'
    ;;
  avalanchego)
    query_scope='(//... except //graft/... except //bazel/image/...)'
    dependency_target_patterns='//... -- -//graft/... -//bazel/image/...'
    ;;
  coreth)
    query_scope='(//graft/coreth/... union //graft/evm/...)'
    dependency_target_patterns='//graft/coreth/... //graft/evm/...'
    ;;
  subnet-evm)
    query_scope='//graft/subnet-evm/...'
    ;;
  smoke)
    query_scope='//ids:ids_test'
    ;;
  *)
    usage
    ;;
esac

# Most scopes use the same syntax for Bazel queries and target patterns.
dependency_target_patterns="${dependency_target_patterns:-${query_scope}}"
query="kind(\"go_test rule\", ${query_scope} except attr(\"tags\", \"manual\", ${query_scope}))"
targets=()
while IFS= read -r target; do
  targets+=("$target")
done < <(bazelisk query "$query")

((${#targets[@]} > 0)) || {
  echo "error: no Go test targets found for ${scope}" >&2
  exit 1
}

export BAZEL_CI_TARGET_PATTERNS="${dependency_target_patterns}"
exec "$(dirname "${BASH_SOURCE[0]}")/run_bazel_ci_command.sh" test "$@" "${targets[@]}"
