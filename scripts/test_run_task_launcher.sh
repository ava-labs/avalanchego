#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
launcher="${repo_root}/scripts/run_task.sh"
bash_bin="$(command -v bash)"

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

stub_dir="${workdir}/bin"
util_dir="${workdir}/util-bin"
mkdir -p "${stub_dir}" "${util_dir}"

ln -s "${bash_bin}" "${util_dir}/bash"
for tool in dirname grep head env cat which; do
  ln -s "$(command -v "${tool}")" "${util_dir}/${tool}"
done

make_stub() {
  local name="$1"
  cat >"${stub_dir}/${name}" <<EOF
#!${bash_bin}
set -euo pipefail
printf '%s\n' '${name}' >"${workdir}/called"
printf '%s\n' "\$*" >"${workdir}/args"
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

run_case() {
  local name="$1"
  local path_entries="$2"
  shift 2

  rm -f "${workdir}/called" "${workdir}/args" "${workdir}/bazel-calls"
  PATH="${path_entries}:${util_dir}" "${bash_bin}" "${launcher}" "$@"
}

make_stub task

cat >"${stub_dir}/fake-task" <<EOF
#!${bash_bin}
set -euo pipefail
printf '%s\n' 'fake-task' >"${workdir}/called"
printf '%s\n' "\$*" >"${workdir}/args"
EOF
chmod +x "${stub_dir}/fake-task"

cat >"${stub_dir}/bazel" <<EOF
#!${bash_bin}
set -euo pipefail
printf '%s\n' "\$*" >>"${workdir}/bazel-calls"
if [[ "\$1" == "build" ]]; then
  exit 0
fi
if [[ "\$1" == "cquery" ]]; then
  printf '%s\n' "${stub_dir}/fake-task"
  exit 0
fi
exit 1
EOF
chmod +x "${stub_dir}/bazel"

run_case prefer-task "${stub_dir}" hello world
assert_called task "hello world"

rm "${stub_dir}/task"
run_case fallback-to-bazel "${stub_dir}" hello world
assert_called fake-task "hello world"
if [[ "$(sed -n '1p' "${workdir}/bazel-calls")" != "build //tools/external:task" ]]; then
  echo "expected first bazel call to build task target" >&2
  exit 1
fi
if [[ "$(sed -n '2p' "${workdir}/bazel-calls")" != "cquery --output=files //tools/external:task" ]]; then
  echo "expected second bazel call to cquery task target" >&2
  exit 1
fi

rm "${stub_dir}/bazel"
if PATH="${stub_dir}:${util_dir}" "${bash_bin}" "${launcher}" hello world >"${workdir}/stdout" 2>"${workdir}/stderr"; then
  echo "expected missing-tools case to fail" >&2
  exit 1
fi

if ! grep -q "Unable to launch task" "${workdir}/stderr"; then
  echo "missing-tools case did not print expected error" >&2
  exit 1
fi

echo "run_task launcher tests passed"
