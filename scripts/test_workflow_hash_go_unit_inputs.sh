#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workdir="$(mktemp -d "${repo_root}/.embed-hash-test.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT

(
  cd "${workdir}"
  git init -q
  go mod init example.com/embed-hash-test >/dev/null
  printf 'package fixture\nimport _ "embed"\n//go:embed input.txt\nvar input string\n' > fixture.go
  printf 'package fixture\nimport _ "embed"\n//go:embed test-input.txt\nvar testInput string\n' > fixture_test.go
  printf 'first' > input.txt
  printf 'test first' > test-input.txt
  mkdir testdata
  printf 'fixture first' > testdata/input.txt
  git add go.mod fixture.go fixture_test.go input.txt test-input.txt testdata/input.txt
)

hash() { GOWORK=off "${repo_root}/scripts/workflow-hash-go-unit-inputs.sh" "${workdir}"; }
initial="$(hash)"
printf 'second' > "${workdir}/input.txt"
[[ "$(hash)" != "${initial}" ]] || { echo 'production embed did not change hash' >&2; exit 1; }
initial="$(hash)"
printf 'test second' > "${workdir}/test-input.txt"
[[ "$(hash)" != "${initial}" ]] || { echo 'test embed did not change hash' >&2; exit 1; }
initial="$(hash)"
printf 'fixture second' > "${workdir}/testdata/input.txt"
[[ "$(hash)" != "${initial}" ]] || { echo 'testdata did not change hash' >&2; exit 1; }
initial="$(hash)"
printf '\nvar changed = true\n' >> "${workdir}/fixture.go"
[[ "$(hash)" != "${initial}" ]] || { echo 'Go source did not change hash' >&2; exit 1; }
initial="$(hash)"
printf 'unrelated' > "${workdir}/unrelated.txt"
[[ "$(hash)" == "${initial}" ]] || { echo 'unrelated file changed hash' >&2; exit 1; }
echo 'Go unit input hash tests passed'
