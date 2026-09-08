#!/usr/bin/env bash

# Run Go test rules. Bazel target patterns can include non-Go tests such as
# gazelle_test. Those tests do not accept Go test flags, including the shuffle
# flag used by scheduled tests. Query rule types first so Bazel sends Go flags
# only to Go test binaries.

set -euo pipefail

usage() {
  echo "Usage: $0 <all|smoke> [bazel test options...]" >&2
  exit 1
}

[[ $# -gt 0 ]] || usage

scope="$1"
shift

case "$scope" in
  all)
    query_scope='//...'
    ;;
  smoke)
    query_scope='//ids:ids_test'
    ;;
  *)
    usage
    ;;
esac

query="kind(\"go_test rule\", ${query_scope} except attr(\"tags\", \"manual\", ${query_scope}))"
targets=()
while IFS= read -r target; do
  targets+=("$target")
done < <(bazelisk query "$query")

((${#targets[@]} > 0)) || {
  echo "error: no Go test targets found for ${scope}" >&2
  exit 1
}

export BAZEL_CI_TARGET_PATTERNS="${query_scope}"
exec "$(dirname "${BASH_SOURCE[0]}")/run_bazel_ci_command.sh" test "$@" "${targets[@]}"
