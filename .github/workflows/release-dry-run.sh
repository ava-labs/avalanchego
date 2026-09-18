#!/usr/bin/env bash
# release-dry-run.sh — local end-to-end dry-run of the release validation chain.
#
# Usage: release-dry-run.sh <TAG>
#
# Runs the same gates as release.yml's validate-tag job (classify, check-exists,
# assert-notes-exist), then resolves the previous tag, prints the release body
# preview, and prints the expected manifest. Does NOT build any artifacts.
# Does NOT mutate GitHub state.
#
# Requires:
#   - GH_REPO or GITHUB_REPOSITORY set (for the existence check)
#   - `gh` authenticated for the existence check
#   - `awk` + `jq` available on PATH (via nix or host)
#
# Exit codes match the underlying helpers (the first failing helper short-
# circuits the chain via `set -e`).

set -euo pipefail

TAG="${1:?Usage: release-dry-run.sh <TAG>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> [1/5] classify-tag"
"${HERE}/release-classify-tag.sh" "$TAG"

echo "==> [2/5] check-exists (read-only API call against ${GH_REPO:-${GITHUB_REPOSITORY:-<unset>}})"
"${HERE}/release-check-exists.sh" "$TAG"

echo "==> [3/5] assert-notes-exist"
"${HERE}/release-assert-notes-exist.sh" "$TAG"

echo "==> [4/5] resolve previous tag + build release body (preview)"
prev="$("${HERE}/release-resolve-previous-tag.sh" "$TAG")"
echo "previous_tag=${prev:-<none>}"
"${HERE}/release-build-body.sh" "$TAG" "$prev"

echo
echo "==> [5/5] expected manifest"
"${HERE}/release-expected-manifest.sh" "$TAG"

echo
echo "Dry-run for ${TAG} passed all gates. No artifacts built; no GitHub state mutated."
