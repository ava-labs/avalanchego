#!/usr/bin/env bash

set -euo pipefail

# Integration test for scripts/run_task.sh.
#
# This script builds a tiny fake PATH so we can verify the launcher's policy
# without depending on the caller's real environment.
#
# Covered cases:
# - a real `task` on PATH wins
# - otherwise we fall back to `go`
# - Bazel is used only when RUN_TASK_PREFER_BAZEL=1
# - non-PATH backends preserve the caller's working directory
# - missing tools fail clearly

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
launcher="${repo_root}/scripts/run_task.sh"
bash_bin="$(command -v bash)"

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

stub_dir="${workdir}/bin"
util_dir="${workdir}/util-bin"
mkdir -p "${stub_dir}" "${util_dir}"

# Give the launcher only the small set of commands it needs so the test stays
# hermetic and PATH resolution is easy to reason about.
ln -s "${bash_bin}" "${util_dir}/bash"
for tool in dirname grep head env cat which pwd sed; do
  ln -s "$(command -v "${tool}")" "${util_dir}/${tool}"
done

# Most stubs only need to record which backend was chosen, the forwarded
# arguments, and the working directory observed by the child process.
make_stub() {
  local name="$1"
  cat >"${stub_dir}/${name}" <<EOF
#!${bash_bin}
set -euo pipefail
printf '%s\n' '${name}' >"${workdir}/called"
printf '%s\n' "\$*" >"${workdir}/args"
printf '%s\n' "\$(pwd)" >"${workdir}/pwd"
EOF
  chmod +x "${stub_dir}/${name}"
}

assert_called() {
  local expected_name="$1"
  local expected_args="$2"
  local actual_name actual_args
  actual_name="$(<"${workdir}/called")"
  actual_args="$(<"${workdir}/args")"
  if [[ "${actual_name}" != "${expected_name}" ]]; then
    echo "expected ${expected_name}, got ${actual_name}" >&2
    exit 1
  fi
  if [[ "${actual_args}" != "${expected_args}" ]]; then
    echo "expected args: ${expected_args}" >&2
    echo "actual args:   ${actual_args}" >&2
    exit 1
  fi
}

assert_pwd() {
  local expected_pwd="$1"
  local actual_pwd
  actual_pwd="$(<"${workdir}/pwd")"
  if [[ "${actual_pwd}" != "${expected_pwd}" ]]; then
    echo "expected pwd: ${expected_pwd}" >&2
    echo "actual pwd:   ${actual_pwd}" >&2
    exit 1
  fi
}

reset_observations() {
  rm -f \
    "${workdir}/called" \
    "${workdir}/args" \
    "${workdir}/pwd"
}

run_case() {
  local path_entries="$1"
  shift

  reset_observations
  PATH="${path_entries}:${util_dir}" "${bash_bin}" "${launcher}" "$@"
}

run_case_in_dir() {
  local path_entries="$1"
  local run_dir="$2"
  shift 2

  reset_observations
  (
    cd "${run_dir}"
    PATH="${path_entries}:${util_dir}" "${bash_bin}" "${launcher}" "$@"
  )
}

# Backend stubs used by the scenarios below.
make_stub task

cat >"${stub_dir}/fake-task" <<EOF
#!${bash_bin}
set -euo pipefail
printf '%s\n' 'fake-task' >"${workdir}/called"
printf '%s\n' "\$*" >"${workdir}/args"
printf '%s\n' "\$(pwd)" >"${workdir}/pwd"
EOF
chmod +x "${stub_dir}/fake-task"

cat >"${stub_dir}/go" <<EOF
#!${bash_bin}
set -euo pipefail
printf '%s\n' 'go' >"${workdir}/called"
printf '%s\n' "\$*" >"${workdir}/args"
printf '%s\n' "\$(pwd)" >"${workdir}/pwd"
EOF
chmod +x "${stub_dir}/go"

cat >"${stub_dir}/bazelisk" <<EOF
#!${bash_bin}
set -euo pipefail
if [[ "\$1" == "build" ]]; then
  exit 0
fi
if [[ "\$1" == "cquery" ]]; then
  printf '%s\n' "${stub_dir}/fake-task"
  exit 0
fi
exit 1
EOF
chmod +x "${stub_dir}/bazelisk"

# A real task binary on PATH should win immediately.
run_case "${stub_dir}" hello world
assert_called task "--taskfile ${repo_root}/Taskfile.yml --dir ${repo_root} hello world"
assert_pwd "${repo_root}"

# Without task on PATH, the launcher should fall back to go.
rm "${stub_dir}/task"
caller_dir="${workdir}/caller"
mkdir -p "${caller_dir}"
run_case_in_dir "${stub_dir}" "${caller_dir}" hello world
assert_called go "tool -modfile=${repo_root}/tools/external/go.mod task --taskfile ${repo_root}/Taskfile.yml --dir ${repo_root} hello world"
assert_pwd "${caller_dir}"

# Bazel should only be used when CI explicitly asks for it.
RUN_TASK_PREFER_BAZEL=1 run_case_in_dir "${stub_dir}" "${caller_dir}" hello world
assert_called fake-task "--taskfile ${repo_root}/Taskfile.yml --dir ${repo_root} hello world"
assert_pwd "${caller_dir}"

# If go is unavailable and Bazel was not requested, the launcher should fail
# clearly.
rm "${stub_dir}/go"
if PATH="${stub_dir}:${util_dir}" "${bash_bin}" "${launcher}" hello world >"${workdir}/stdout" 2>"${workdir}/stderr"; then
  echo "expected missing-go-without-bazel-preference case to fail" >&2
  exit 1
fi
if ! grep -q "Unable to launch task" "${workdir}/stderr"; then
  echo "missing-go-without-bazel-preference case did not print expected error" >&2
  exit 1
fi

# With neither go nor Bazel available, we should get the generic launcher
# failure message.
rm "${stub_dir}/bazelisk"
if PATH="${stub_dir}:${util_dir}" "${bash_bin}" "${launcher}" hello world >"${workdir}/stdout" 2>"${workdir}/stderr"; then
  echo "expected missing-tools case to fail" >&2
  exit 1
fi

if ! grep -q "Unable to launch task" "${workdir}/stderr"; then
  echo "missing-tools case did not print expected error" >&2
  exit 1
fi

echo "run_task launcher tests passed"
