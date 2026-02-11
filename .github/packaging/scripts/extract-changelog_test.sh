#!/usr/bin/env bash

# Tests for extract-changelog.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXTRACT="${SCRIPT_DIR}/extract-changelog.sh"

WORK_DIR=$(mktemp -d)
trap 'rm -rf "${WORK_DIR}"' EXIT

pass=0
fail=0

assert_contains() {
    local file="$1" pattern="$2" msg="$3"
    if grep -q "${pattern}" "${file}"; then
        echo "  PASS: ${msg}"
        pass=$((pass + 1))
    else
        echo "  FAIL: ${msg}"
        echo "    expected pattern: ${pattern}"
        echo "    file contents:"
        sed 's/^/    /' "${file}"
        fail=$((fail + 1))
    fi
}

assert_not_contains() {
    local file="$1" pattern="$2" msg="$3"
    if ! grep -q "${pattern}" "${file}"; then
        echo "  PASS: ${msg}"
        pass=$((pass + 1))
    else
        echo "  FAIL: ${msg}"
        echo "    unexpected pattern found: ${pattern}"
        fail=$((fail + 1))
    fi
}

# ── Test 1: Released version with nested bullets ────────────────────

echo "=== Test 1: Released version with nested bullets ==="

cat > "${WORK_DIR}/releases.md" <<'EOF'
# Release Notes

## [v1.5.0](https://github.com/example/releases/tag/v1.5.0)

This is a great release.

### Config

- Added:
  - `--new-flag-one`
  - `--new-flag-two`
- Deprecated:
  - `--old-flag`

### Fixes

- Fixed a critical bug in the parser.
- Improved startup performance.

### What's Changed

**Full Changelog**: https://github.com/example/compare/v1.4.0...v1.5.0

## [v1.4.0](https://github.com/example/releases/tag/v1.4.0)

Older release.
EOF

"${EXTRACT}" "1.5.0" "${WORK_DIR}/releases.md" "${WORK_DIR}/out1.yml"

assert_contains "${WORK_DIR}/out1.yml" "semver: 1.5.0" "has correct version"
assert_contains "${WORK_DIR}/out1.yml" "note:.*--new-flag-one" "captures indented bullet --new-flag-one"
assert_contains "${WORK_DIR}/out1.yml" "note:.*--new-flag-two" "captures indented bullet --new-flag-two"
assert_contains "${WORK_DIR}/out1.yml" "note:.*--old-flag" "captures indented bullet --old-flag"
assert_contains "${WORK_DIR}/out1.yml" "note:.*Fixed a critical bug" "captures top-level bullet"
assert_contains "${WORK_DIR}/out1.yml" "note:.*Improved startup" "captures second top-level bullet"
assert_not_contains "${WORK_DIR}/out1.yml" "Added:" "filters bare label 'Added:'"
assert_not_contains "${WORK_DIR}/out1.yml" "Deprecated:" "filters bare label 'Deprecated:'"
assert_not_contains "${WORK_DIR}/out1.yml" "Full Changelog" "skips What's Changed section"
assert_not_contains "${WORK_DIR}/out1.yml" '`' "strips backticks from output"

# ── Test 2: Pending (unreleased) version ────────────────────────────

echo "=== Test 2: Pending (unreleased) version ==="

cat > "${WORK_DIR}/releases2.md" <<'EOF'
# Release Notes

## Pending (v2.0.0)

### Fixes

- Fixed memory leak.

## [v1.0.0](https://github.com/example/releases/tag/v1.0.0)

Old release.
EOF

"${EXTRACT}" "2.0.0" "${WORK_DIR}/releases2.md" "${WORK_DIR}/out2.yml"

assert_contains "${WORK_DIR}/out2.yml" "semver: 2.0.0" "has correct version for pending"
assert_contains "${WORK_DIR}/out2.yml" "note:.*Fixed memory leak" "captures pending version bullet"

# ── Test 3: Version not found (fallback) ────────────────────────────

echo "=== Test 3: Version not found (fallback) ==="

"${EXTRACT}" "99.99.99" "${WORK_DIR}/releases.md" "${WORK_DIR}/out3.yml" 2>/dev/null

assert_contains "${WORK_DIR}/out3.yml" "semver: 99.99.99" "fallback has correct version"
assert_contains "${WORK_DIR}/out3.yml" "releases/tag/v99.99.99" "fallback has release link"

# ── Test 4: Missing file (fallback) ─────────────────────────────────

echo "=== Test 4: Missing file (fallback) ==="

"${EXTRACT}" "1.0.0" "${WORK_DIR}/nonexistent.md" "${WORK_DIR}/out4.yml" 2>/dev/null

assert_contains "${WORK_DIR}/out4.yml" "semver: 1.0.0" "fallback on missing file"

# ── Test 5: Valid YAML output ────────────────────────────────────────

echo "=== Test 5: Valid YAML structure ==="

assert_contains "${WORK_DIR}/out1.yml" "^---" "starts with YAML document separator"
assert_contains "${WORK_DIR}/out1.yml" "  packager:" "has packager field"
assert_contains "${WORK_DIR}/out1.yml" "  changes:" "has changes field"

# ── Summary ──────────────────────────────────────────────────────────

echo ""
echo "=== Results: ${pass} passed, ${fail} failed ==="

if [[ "${fail}" -gt 0 ]]; then
    exit 1
fi
