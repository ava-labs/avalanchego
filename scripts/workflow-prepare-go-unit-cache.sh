#!/usr/bin/env bash
set -euo pipefail

# Keep Go test results on compatible fallback restores, but expire them when
# normalized fixtures change or scheduled jobs must rerun every test.
# Example (fallback with matching fixtures):
#   ./scripts/workflow-prepare-go-unit-cache.sh '<fixture-hash>' false '<restored-cache-key>'

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <fixture-hash> <clear-test-results> <matched-key>" >&2
  exit 2
fi

CACHE_DIR="$(go env GOCACHE)"
FIXTURE_HASH="$1"
CLEAR_TEST_RESULTS="$2"
MATCHED_KEY="$3"
FIXTURE_MARKER="${CACHE_DIR}/ci-testdata-hash"

# The cache key includes fixtures, but a fallback can come from a different
# fixture set. Normalized fixture mtimes hide that change from Go's test cache.
# Preserve test results on other fallbacks so unaffected packages can reuse them.
if [[ "${CLEAR_TEST_RESULTS}" == true ]] || {
  [[ -n "${MATCHED_KEY}" ]] &&
  { [[ ! -f "${FIXTURE_MARKER}" ]] || [[ "$(<"${FIXTURE_MARKER}")" != "${FIXTURE_HASH}" ]]; }
}; then
  go clean -testcache
fi

# Keep this marker in GOCACHE so actions/cache saves it with the build cache.
mkdir -p "${CACHE_DIR}"
printf '%s\n' "${FIXTURE_HASH}" > "${FIXTURE_MARKER}"
