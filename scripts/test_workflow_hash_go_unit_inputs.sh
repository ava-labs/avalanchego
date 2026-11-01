#!/usr/bin/env bash
set -euo pipefail

# Check that source, metadata, fixtures, embeds, and test configuration change
# the cache key, but unrelated files do not.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d "${REPO_ROOT}/.embed-hash-test.XXXXXX")"
trap 'rm -rf "${WORKDIR}"' EXIT

(
  cd "${WORKDIR}"
  git init -q
  go mod init example.com/embed-hash-test >/dev/null
  GOWORK=off go work init .
  printf 'package fixture\nimport _ "embed"\n//go:embed input.txt\nvar input string\n' > fixture.go
  printf 'package fixture\nimport _ "embed"\n//go:embed test-input.txt\nvar testInput string\n' > fixture_test.go
  printf 'first' > input.txt
  printf 'test first' > test-input.txt
  mkdir testdata
  printf 'fixture first' > testdata/input.txt
  git add go.mod go.work fixture.go fixture_test.go input.txt test-input.txt testdata/input.txt
)

hash() { GOWORK=off "${REPO_ROOT}/scripts/workflow-hash-go-unit-inputs.sh" "${WORKDIR}"; }

assert_hash_changed() {
  local before_hash="$1"
  local description="$2"

  if [[ "$(hash)" == "${before_hash}" ]]; then
    echo "${description} did not change hash" >&2
    exit 1
  fi
}

assert_hash_unchanged() {
  local before_hash="$1"
  local description="$2"

  if [[ "$(hash)" != "${before_hash}" ]]; then
    echo "${description} changed hash" >&2
    exit 1
  fi
}
INITIAL="$(hash)"
printf 'second' > "${WORKDIR}/input.txt"
assert_hash_changed "${INITIAL}" 'production embed'
INITIAL="$(hash)"
printf 'test second' > "${WORKDIR}/test-input.txt"
assert_hash_changed "${INITIAL}" 'test embed'
INITIAL="$(hash)"
printf 'fixture second' > "${WORKDIR}/testdata/input.txt"
assert_hash_changed "${INITIAL}" 'testdata'
INITIAL="$(hash)"
printf '\nvar changed = true\n' >> "${WORKDIR}/fixture.go"
assert_hash_changed "${INITIAL}" 'Go source'
INITIAL="$(hash)"
printf '\n// Workspace configuration changed.\n' >> "${WORKDIR}/go.work"
assert_hash_changed "${INITIAL}" 'workspace metadata'
mkdir -p "${WORKDIR}/docs"
printf 'Original documentation\n' > "${WORKDIR}/docs/ci.md"
git -C "${WORKDIR}" add docs/ci.md
INITIAL="$(hash)"
printf 'Changed documentation\n' >> "${WORKDIR}/docs/ci.md"
assert_hash_unchanged "${INITIAL}" 'unrelated documentation'
printf 'unrelated' > "${WORKDIR}/unrelated.txt"
assert_hash_unchanged "${INITIAL}" 'unrelated file'
echo 'Go unit input hash tests passed'
