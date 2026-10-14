#!/usr/bin/env bash
set -euo pipefail

# Verify the cache key changes for Go source, workspace metadata, testdata,
# and production/test embedded files, but not for unrelated files.
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
INITIAL="$(hash)"
printf 'second' > "${WORKDIR}/input.txt"
[[ "$(hash)" != "${INITIAL}" ]] || { echo 'production embed did not change hash' >&2; exit 1; }
INITIAL="$(hash)"
printf 'test second' > "${WORKDIR}/test-input.txt"
[[ "$(hash)" != "${INITIAL}" ]] || { echo 'test embed did not change hash' >&2; exit 1; }
INITIAL="$(hash)"
printf 'fixture second' > "${WORKDIR}/testdata/input.txt"
[[ "$(hash)" != "${INITIAL}" ]] || { echo 'testdata did not change hash' >&2; exit 1; }
INITIAL="$(hash)"
printf '\nvar changed = true\n' >> "${WORKDIR}/fixture.go"
[[ "$(hash)" != "${INITIAL}" ]] || { echo 'Go source did not change hash' >&2; exit 1; }
INITIAL="$(hash)"
printf '\n// Workspace configuration changed.\n' >> "${WORKDIR}/go.work"
[[ "$(hash)" != "${INITIAL}" ]] || { echo 'workspace metadata did not change hash' >&2; exit 1; }
INITIAL="$(hash)"
printf 'unrelated' > "${WORKDIR}/unrelated.txt"
[[ "$(hash)" == "${INITIAL}" ]] || { echo 'unrelated file changed hash' >&2; exit 1; }
echo 'Go unit input hash tests passed'
