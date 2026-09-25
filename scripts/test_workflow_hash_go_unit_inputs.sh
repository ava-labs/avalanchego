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
# Configuration changes must select a new archive even when Go source is unchanged.
# Otherwise, Go can rerun tests and trigger the exact-hit validation failure.
configuration_paths=(
  Taskfile.yml
  .github/workflows/go-unit.yml
  .github/actions/setup-go-for-project/action.yml
  scripts/tests.unit.sh
  scripts/constants.sh
  scripts/run_task.sh
  scripts/nix_run.sh
  scripts/normalize_testdata_mtimes.sh
  scripts/workflow-hash-go-unit-inputs.sh
  scripts/workflow-prepare-go-unit-cache.sh
)
for configuration_path in "${configuration_paths[@]}"; do
  mkdir -p "${WORKDIR}/$(dirname "${configuration_path}")"
  printf '# Original configuration\n' > "${WORKDIR}/${configuration_path}"
  git -C "${WORKDIR}" add "${configuration_path}"
  INITIAL="$(hash)"
  printf '# Changed configuration\n' >> "${WORKDIR}/${configuration_path}"
  [[ "$(hash)" != "${INITIAL}" ]] || { echo "${configuration_path} did not change hash" >&2; exit 1; }
done

mkdir -p "${WORKDIR}/docs"
printf 'Original documentation\n' > "${WORKDIR}/docs/ci.md"
git -C "${WORKDIR}" add docs/ci.md
INITIAL="$(hash)"
printf 'Changed documentation\n' >> "${WORKDIR}/docs/ci.md"
[[ "$(hash)" == "${INITIAL}" ]] || { echo 'unrelated documentation changed hash' >&2; exit 1; }
printf 'unrelated' > "${WORKDIR}/unrelated.txt"
[[ "$(hash)" == "${INITIAL}" ]] || { echo 'unrelated file changed hash' >&2; exit 1; }
echo 'Go unit input hash tests passed'
