#!/usr/bin/env bash
set -euo pipefail

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
  printf 'version: "3"\nvars:\n  NIX_RUN: original\ntasks:\n  unrelated:\n    cmd: original\n' > Taskfile.yml
  git add .
)

hash() { GOWORK=off "${REPO_ROOT}/scripts/workflow-hash-go-${1}-inputs.sh" "${WORKDIR}"; }

# Every case checks both keys, including the key that must remain unchanged.
check_change() {
  local input="$1" affected="$2" mode before after
  mkdir -p "${WORKDIR}/$(dirname "${input}")"
  if [[ ! -f "${WORKDIR}/${input}" ]]; then
    printf 'original\n' > "${WORKDIR}/${input}"
    git -C "${WORKDIR}" add "${input}"
  fi
  local unit_before e2e_before
  unit_before="$(hash unit)"
  e2e_before="$(hash e2e)"
  printf '\nchanged\n' >> "${WORKDIR}/${input}"
  for mode in unit e2e; do
    before="${unit_before}"
    if [[ "${mode}" == e2e ]]; then before="${e2e_before}"; fi
    after="$(hash "${mode}")"
    if [[ "${affected}" == both || "${affected}" == "${mode}" ]]; then
      if [[ "${after}" == "${before}" ]]; then
        echo "${input}: ${mode} hash did not change" >&2
        exit 1
      fi
    elif [[ "${after}" != "${before}" ]]; then
      echo "${input}: unrelated ${mode} hash changed" >&2
      exit 1
    fi
  done
  # Restore valid Go, YAML, and module files before the next case.
  git -C "${WORKDIR}" checkout -- "${input}"
}

for input in input.txt test-input.txt testdata/input.txt; do
  check_change "${input}" both
done
# Keep syntax valid for go list.
for input in fixture.go go.mod go.work; do
  before_unit="$(hash unit)"; before_e2e="$(hash e2e)"
  printf '\n// changed\n' >> "${WORKDIR}/${input}"
  [[ "$(hash unit)" != "${before_unit}" && "$(hash e2e)" != "${before_e2e}" ]]
done

for input in flake.nix flake.lock nix/go/default.nix scripts/constants.sh \
  scripts/run_task.sh scripts/nix_run.sh scripts/workflow-hash-go-cache-inputs.sh \
  .github/actions/setup-go-for-project/action.yml; do
  check_change "${input}" both
done
for input in taskfiles/go-unit.yml .github/workflows/go-unit.yml \
  scripts/tests.unit.sh scripts/normalize_testdata_mtimes.sh \
  scripts/workflow-prepare-go-unit-cache.sh scripts/workflow-hash-go-unit-inputs.sh; do
  check_change "${input}" unit
done
for input in taskfiles/go-e2e.yml scripts/build.sh scripts/build_xsvm.sh \
  scripts/tests.e2e.sh scripts/run_ginkgo.sh scripts/git_commit.sh \
  scripts/workflow-check-go-e2e-cache.sh scripts/workflow-validate-go-build-cache.sh \
  scripts/workflow-hash-go-e2e-inputs.sh .github/actions/install-nix/action.yml \
  .github/actions/run-monitored-tmpnet-cmd/action.yml .github/actions/validate-go-e2e-cache/action.yml; do
  check_change "${input}" e2e
done
for input in docs/ci.md .github/workflows/go.yml .github/workflows/go-scheduled.yml; do
  check_change "${input}" neither
done

# Unrelated root tasks must not invalidate either key; global settings must.
before_unit="$(hash unit)"; before_e2e="$(hash e2e)"
printf '  another:\n    cmd: changed\n' >> "${WORKDIR}/Taskfile.yml"
[[ "$(hash unit)" == "${before_unit}" && "$(hash e2e)" == "${before_e2e}" ]]
printf 'version: "3"\nvars:\n  NIX_RUN: changed\ntasks: {}\n' > "${WORKDIR}/Taskfile.yml"
[[ "$(hash unit)" != "${before_unit}" && "$(hash e2e)" != "${before_e2e}" ]]

# Verify that the workflows use separate entrypoints.
grep -F "\$(./scripts/workflow-hash-go-unit-inputs.sh)" "${REPO_ROOT}/.github/workflows/go-unit.yml" >/dev/null
for workflow in go.yml go-scheduled.yml; do
  grep -F "\$(./scripts/workflow-hash-go-e2e-inputs.sh)" "${REPO_ROOT}/.github/workflows/${workflow}" >/dev/null
done
# Included Taskfiles must preserve public task names and the root wrapper path.
for task_name in test-unit test-unit-race-shuffle test-e2e test-e2e-race-serial; do
  task --dry --taskfile "${REPO_ROOT}/Taskfile.yml" "${task_name}" > "${WORKDIR}/task.log" 2>&1
  grep -F "${REPO_ROOT}/scripts/nix_run.sh" "${WORKDIR}/task.log" >/dev/null
  if grep -F '/taskfiles/scripts/nix_run.sh' "${WORKDIR}/task.log"; then
    echo "${task_name}: included Taskfile changed the wrapper path" >&2
    exit 1
  fi
done
task --summary --taskfile "${REPO_ROOT}/Taskfile.yml" test-unit > "${WORKDIR}/task.log"
grep -F 'NO_RACE: "true"' "${WORKDIR}/task.log" >/dev/null
grep -F 'NO_SHUFFLE: "true"' "${WORKDIR}/task.log" >/dev/null
task --summary --taskfile "${REPO_ROOT}/Taskfile.yml" test-e2e-race-serial > "${WORKDIR}/task.log"
grep -F 'E2E_SERIAL: "1"' "${WORKDIR}/task.log" >/dev/null
echo 'Independent Go unit and E2E input hash tests passed'
