#!/usr/bin/env bash

set -euo pipefail

# The Go unit-test workflow runs this script after an exact GOCACHE restore.
# It fails if any Go test package ran instead of reporting (cached).
#
# Example:
# ./scripts/workflow-validate-go-unit-cache.sh "$RUNNER_TEMP/go-unit-test.log"

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <unit-test-log>" >&2
  exit 2
fi

log_path="$1"
found_test_result=false
while IFS= read -r line; do
  if [[ ! "${line}" =~ ^ok[[:space:]] ]]; then
    continue
  fi

  found_test_result=true
  if [[ ! "${line}" =~ [[:space:]]\(cached\)([[:space:]]|$) ]]; then
    echo "uncached Go unit-test result: ${line}" >&2
    exit 1
  fi
done <"${log_path}"

if [[ "${found_test_result}" != true ]]; then
  echo "no Go unit-test results found" >&2
  exit 1
fi
