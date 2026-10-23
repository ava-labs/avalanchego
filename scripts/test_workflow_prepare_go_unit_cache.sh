#!/usr/bin/env bash
set -euo pipefail

# Verify Go keeps reusable test results on compatible fallbacks and reruns tests
# when fixtures change, their marker is missing, or a scheduled run requests it.
# Every preparation step must retain compiled package output.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT
# Execute the workflow's configuration step without mocking its policy.
awk '
  /^        run: \|$/ { copying = 1; next }
  copying && /^      - / { exit }
  copying { sub(/^          /, ""); print }
' "${REPO_ROOT}/.github/workflows/go-unit.yml" > "${WORKDIR}/test-config.sh"
check_config() {
  local task="$1" reuse="$2" mode="$3" clear="$4"
  local output="${WORKDIR}/config-output"
  : > "${output}"
  TEST_TASK="${task}" REUSE_TEST_RESULTS="${reuse}" GITHUB_OUTPUT="${output}" \
    bash "${WORKDIR}/test-config.sh"
  grep -qx "test-task=${task}" "${output}"
  grep -qx "cache-mode=${mode}" "${output}"
  grep -qx "clear-test-results=${clear}" "${output}"
}
check_config test-unit true build-and-test-results false
check_config test-unit false build-only true
check_config test-unit-race-shuffle false build-only true
for task in test-unit-race-shuffle test-unit-race unknown; do
  if TEST_TASK="${task}" REUSE_TEST_RESULTS=true GITHUB_OUTPUT="${WORKDIR}/config-output" \
    bash "${WORKDIR}/test-config.sh"; then
    echo "unsupported cached test configuration was accepted: ${task}" >&2
    exit 1
  fi
done

export GOCACHE="${WORKDIR}/cache"
mkdir -p "${GOCACHE}" "${WORKDIR}/module"
cd "${WORKDIR}/module"
GOWORK=off go mod init example.com/cache-reuse >/dev/null
printf 'package cache\nconst value = 1\n' > cache.go
printf 'package cache\nimport "testing"\nfunc TestCache(t *testing.T) { if value != 1 { t.Fatal(value) } }\n' > cache_test.go
COMPILED_ARCHIVE="$(GOWORK=off go list -export -f '{{.Export}}' .)"
[[ -f "${COMPILED_ARCHIVE}" ]] || { echo 'compiled package archive was not created' >&2; exit 1; }

run_test() { GOWORK=off go test -v .; }
assert_cached() {
  local output
  if ! output="$(run_test 2>&1)"; then
    printf '%s\n' "${output}" >&2
    exit 1
  fi
  [[ "${output}" == *'(cached)'* ]] || { echo 'test result was not reused' >&2; exit 1; }
}
assert_ran() {
  local output
  if ! output="$(run_test 2>&1)"; then
    printf '%s\n' "${output}" >&2
    exit 1
  fi
  [[ "${output}" != *'(cached)'* ]] || { echo 'test result was reused' >&2; exit 1; }
}

prepare_cache() {
  "${REPO_ROOT}/scripts/workflow-prepare-go-unit-cache.sh" "${GOCACHE}" "$@"
  [[ -f "${COMPILED_ARCHIVE}" ]] || { echo 'compiled package archive was removed' >&2; exit 1; }
}

run_test >/dev/null
assert_cached
# A cache miss must preserve local test results and initialize the fixture marker.
prepare_cache fixture-a false ''
assert_cached
# A fallback with the same fixtures must preserve reusable test results.
prepare_cache fixture-a false fallback-key
assert_cached
# A fallback with different fixtures must expire the old test results.
prepare_cache fixture-b false fallback-key
assert_ran
assert_cached
# Scheduled jobs must expire test results even with the same fixture hash.
prepare_cache fixture-b true exact-key
assert_ran
assert_cached
# Older fallback entries without a fixture marker cannot be trusted.
rm "${GOCACHE}/ci-testdata-hash"
prepare_cache fixture-b false fallback-key
assert_ran
assert_cached
# Neither assertion may accept a failing Go test, regardless of its output.
for assertion in assert_ran assert_cached; do
  message='intentional failure'
  if [[ "${assertion}" == assert_cached ]]; then
    message='intentional failure (cached)'
  fi
  printf 'package cache\nimport "testing"\nfunc TestFailure(t *testing.T) { t.Fatal("%s") }\n' "${message}" > failure_test.go
  if ("${assertion}") > "${WORKDIR}/failure.log" 2>&1; then
    echo "${assertion} accepted a failing Go test" >&2
    exit 1
  fi
  grep -q 'intentional failure' "${WORKDIR}/failure.log"
done

echo 'Go unit-cache preparation tests passed'
