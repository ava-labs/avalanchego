#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT
export GOCACHE="${workdir}/cache"
mkdir -p "${GOCACHE}" "${workdir}/module"
cd "${workdir}/module"
GOWORK=off go mod init example.com/cache-reuse >/dev/null
printf 'package cache\nimport "testing"\nfunc TestCache(t *testing.T) {}\n' > cache_test.go

run_test() { GOWORK=off go test -v .; }
assert_cached() { [[ "$(run_test)" == *'(cached)'* ]] || { echo 'test result was not reused' >&2; exit 1; }; }
assert_ran() { [[ "$(run_test)" != *'(cached)'* ]] || { echo 'test result was reused' >&2; exit 1; }; }

run_test >/dev/null
assert_cached
# A fallback with the same fixtures must preserve reusable test results.
"${repo_root}/scripts/workflow-prepare-go-unit-cache.sh" "${GOCACHE}" fixture-a false ''
assert_cached
"${repo_root}/scripts/workflow-prepare-go-unit-cache.sh" "${GOCACHE}" fixture-a false fallback-key
assert_cached
# A fallback with different fixtures must expire the old test results.
"${repo_root}/scripts/workflow-prepare-go-unit-cache.sh" "${GOCACHE}" fixture-b false fallback-key
assert_ran
assert_cached
# Scheduled jobs must expire test results even with the same fixture hash.
"${repo_root}/scripts/workflow-prepare-go-unit-cache.sh" "${GOCACHE}" fixture-b true exact-key
assert_ran
assert_cached
# Older fallback entries without a fixture marker cannot be trusted.
rm "${GOCACHE}/ci-testdata-hash"
"${repo_root}/scripts/workflow-prepare-go-unit-cache.sh" "${GOCACHE}" fixture-b false fallback-key
assert_ran
assert_cached
echo 'Go unit-cache preparation tests passed'
