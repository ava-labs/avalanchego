#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: $0 <GOCACHE> <fixture-hash> <clear-test-results> <matched-key>" >&2
  exit 2
fi

cache_dir="$1"
fixture_hash="$2"
clear_test_results="$3"
matched_key="$4"
fixture_marker="${cache_dir}/ci-testdata-hash"

# The cache key includes fixtures, but a fallback can come from a different
# fixture set. Normalized fixture mtimes hide that change from Go's test cache.
# Preserve test results on other fallbacks so unaffected packages can reuse them.
if [[ "${clear_test_results}" == true ]] || {
  [[ -n "${matched_key}" ]] &&
  { [[ ! -f "${fixture_marker}" ]] || [[ "$(<"${fixture_marker}")" != "${fixture_hash}" ]]; }
}; then
  go clean -testcache
fi

# Keep this marker in GOCACHE so actions/cache saves it with the build cache.
mkdir -p "${cache_dir}"
printf '%s\n' "${fixture_hash}" > "${fixture_marker}"
