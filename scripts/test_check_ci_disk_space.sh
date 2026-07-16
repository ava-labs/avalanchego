#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/check_ci_disk_space.sh"

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

assert_contains() {
  local file="$1"
  local expected="$2"
  if ! grep -Fq -- "${expected}" "${file}"; then
    echo "expected ${file} to contain: ${expected}" >&2
    echo "actual contents:" >&2
    cat "${file}" >&2
    exit 1
  fi
}

assert_output_equals() {
  local file="$1"
  local expected="$2"
  local actual
  actual="$(<"${file}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "expected ${file} to equal: ${expected}" >&2
    echo "actual contents: ${actual}" >&2
    exit 1
  fi
}

run_case() {
  local name="$1"
  local forced_free_gb="$2"
  shift 2

  local stdout="${workdir}/${name}.stdout"
  local stderr="${workdir}/${name}.stderr"
  local github_output="${workdir}/${name}.github_output"

  : >"${github_output}"

  if ! CI_FORCE_FREE_GB="${forced_free_gb}" GITHUB_OUTPUT="${github_output}" bash "${script}" "$@" >"${stdout}" 2>"${stderr}"; then
    echo "case ${name} unexpectedly failed" >&2
    cat "${stderr}" >&2
    exit 1
  fi
}

run_case healthy 25 --min-gb 20
assert_contains "${workdir}/healthy.stdout" "Free disk on /: 25G available"
assert_contains "${workdir}/healthy.stdout" "threshold: 20G"
assert_contains "${workdir}/healthy.stdout" "status: healthy"
assert_output_equals "${workdir}/healthy.github_output" "below_threshold=false"

run_case low 19 --min-gb 20
assert_contains "${workdir}/low.stdout" "Free disk on /: 19G available"
assert_contains "${workdir}/low.stdout" "threshold: 20G"
assert_contains "${workdir}/low.stdout" "status: below threshold"
assert_output_equals "${workdir}/low.github_output" "below_threshold=true"

run_case still-low 0 --min-gb 20
assert_contains "${workdir}/still-low.stdout" "Free disk on /: 0G available"
assert_contains "${workdir}/still-low.stdout" "status: below threshold"
assert_output_equals "${workdir}/still-low.github_output" "below_threshold=true"

echo "check_ci_disk_space tests passed"
