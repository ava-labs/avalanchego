#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/log_ci_disk_state.sh"
bash_bin="$(command -v bash)"

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

stub_dir="${workdir}/bin"
mkdir -p "${stub_dir}"

make_stub() {
  local name="$1"
  local body="$2"
  cat >"${stub_dir}/${name}" <<EOF
#!${bash_bin}
set -euo pipefail
${body}
EOF
  chmod +x "${stub_dir}/${name}"
}

make_stub date 'echo "Fri Jul 18 00:00:00 UTC 2026"'
make_stub uname 'echo "Linux"'
# shellcheck disable=SC2016
make_stub df '
if [[ "${1:-}" == "-i" ]]; then
  printf "%s\n" \
    "Filesystem     Inodes IUsed IFree IUse% Mounted on" \
    "/dev/root         100    10    90   10% /"
else
  printf "%s\n" \
    "Filesystem      Size  Used Avail Use% Mounted on" \
    "/dev/root       100G   50G   50G  50% /"
fi'
# shellcheck disable=SC2016
make_stub du 'printf "7G\t%s\n" "$2"'
# shellcheck disable=SC2016
make_stub docker '
case "${1:-}" in
  system)
    echo "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE"
    ;;
  info)
    echo "Docker Root Dir: /var/lib/docker"
    ;;
  *)
    echo "unexpected docker invocation: $*" >&2
    exit 1
    ;;
esac'

home_dir="${workdir}/home"
mkdir -p \
  "${home_dir}/.cache/bazel" \
  "${home_dir}/.cache/bazel-repository-cache" \
  "${home_dir}/.cache/bazel-go-repository-modcache" \
  "${home_dir}/.cache/bazelisk"

run_case() {
  local name="$1"
  shift
  PATH="${stub_dir}" HOME="${home_dir}" "${bash_bin}" "${script}" "$@" >"${workdir}/${name}.out"
}

run_case generic --group generic
run_case bazel --group bazel
run_case docker --group docker
run_case combined --group generic --group bazel --group docker

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

assert_contains "${workdir}/generic.out" "== Generic runner/root disk =="
assert_contains "${workdir}/generic.out" "/dev/root       100G   50G   50G  50% /"
assert_contains "${workdir}/generic.out" "Filesystem     Inodes IUsed IFree IUse% Mounted on"

assert_contains "${workdir}/bazel.out" "== Bazel disk usage =="
assert_contains "${workdir}/bazel.out" "7G"
assert_contains "${workdir}/bazel.out" ".cache/bazel-repository-cache"
assert_contains "${workdir}/bazel.out" ".cache/bazel-go-repository-modcache"

assert_contains "${workdir}/docker.out" "== Docker/image-build disk usage =="
assert_contains "${workdir}/docker.out" "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE"
assert_contains "${workdir}/docker.out" "Docker Root Dir: /var/lib/docker"

assert_contains "${workdir}/combined.out" "== Generic runner/root disk =="
assert_contains "${workdir}/combined.out" "== Bazel disk usage =="
assert_contains "${workdir}/combined.out" "== Docker/image-build disk usage =="

echo "log_ci_disk_state tests passed"
