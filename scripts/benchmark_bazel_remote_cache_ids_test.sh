#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NIX_RUN="${REPO_ROOT}/scripts/nix_run.sh"
TARGET="//ids:ids_test"

if command -v bazel-remote >/dev/null 2>&1; then
  BAZEL_REMOTE_LAUNCHER=(bazel-remote)
elif command -v nix >/dev/null 2>&1; then
  BAZEL_REMOTE_LAUNCHER=(nix run nixpkgs#bazel-remote --)
else
  echo "error: bazel-remote not found on PATH and nix is unavailable" >&2
  exit 1
fi

print_usage() {
  cat <<'EOF'
Usage: scripts/benchmark_bazel_remote_cache_ids_test.sh

Benchmark Bazel remote caching for the ids_test Bazel target.

Runs three test invocations with fresh local Bazel state each time:
  1. no remote cache
  2. cold remote cache (empty cache; populates it)
  3. warm remote cache (reuses the populated cache)

The benchmark fails unless the warm remote-cache run is faster than both the
no-cache and cold-cache runs.
EOF
}

if [[ ${1-} == "-h" || ${1-} == "--help" ]]; then
  print_usage
  exit 0
fi

if [[ $# -ne 0 ]]; then
  print_usage >&2
  exit 1
fi

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

require_cmd awk
require_cmd perl
require_cmd grep
require_cmd mktemp

TMP_ROOT="$(mktemp -d -t bazel-remote-cache-bench.XXXXXX)"
CACHE_DIR="${TMP_ROOT}/remote-cache"
REPOSITORY_CACHE_DIR="$(${NIX_RUN} bazelisk info repository_cache 2>/dev/null | tail -n 1)"
[[ -n "${REPOSITORY_CACHE_DIR}" ]] || die "failed to determine Bazel repository cache path"
LOG_DIR="${TMP_ROOT}/logs"
mkdir -p "${CACHE_DIR}" "${LOG_DIR}"

REMOTE_PID=""
REMOTE_LOG=""
SUCCESS=false
cleanup() {
  if [[ -n "${REMOTE_PID}" ]] && kill -0 "${REMOTE_PID}" >/dev/null 2>&1; then
    kill "${REMOTE_PID}" >/dev/null 2>&1 || true
    wait "${REMOTE_PID}" >/dev/null 2>&1 || true
  fi

  if [[ "${SUCCESS}" == true ]]; then
    chmod -R u+w "${TMP_ROOT}" >/dev/null 2>&1 || true
    rm -rf "${TMP_ROOT}" >/dev/null 2>&1 || true
  else
    echo "kept temporary workspace for inspection: ${TMP_ROOT}" >&2
  fi
}
trap cleanup EXIT

choose_port() {
  local candidate
  candidate="$((20000 + RANDOM % 10000))"
  echo "${candidate}"
}

start_bazel_remote() {
  local port attempts
  attempts=10

  for ((i = 1; i <= attempts; i++)); do
    port="$(choose_port)"
    REMOTE_LOG="${LOG_DIR}/bazel-remote.log"

    "${BAZEL_REMOTE_LAUNCHER[@]}" \
      --dir "${CACHE_DIR}" \
      --max_size 5 \
      --http_address "127.0.0.1:${port}" \
      --grpc_address none \
      >"${REMOTE_LOG}" 2>&1 &
    REMOTE_PID=$!

    sleep 1
    if kill -0 "${REMOTE_PID}" >/dev/null 2>&1; then
      echo "http://127.0.0.1:${port}"
      return 0
    fi

    wait "${REMOTE_PID}" >/dev/null 2>&1 || true
    REMOTE_PID=""
  done

  die "failed to start bazel-remote; last log:\n$(tail -n 20 "${REMOTE_LOG}" 2>/dev/null || true)"
}

now_seconds() {
  perl -MTime::HiRes=time -e 'printf "%.6f\n", time'
}

format_seconds() {
  awk -v start="$1" -v end="$2" 'BEGIN { printf "%.3f", (end - start) }'
}

extract_summary() {
  local log_file="$1"
  local summary

  summary="$(grep -E '^(INFO|ERROR): .*process' "${log_file}" | tail -n 1 || true)"
  if [[ -z "${summary}" ]]; then
    summary="$(grep -E '^(INFO|ERROR): Build completed|^(INFO|ERROR): Elapsed time:' "${log_file}" | tail -n 1 || true)"
  fi
  echo "${summary}"
}

remove_output_base() {
  local output_base="$1"

  # Keep output bases only long enough to finish the corresponding timing so
  # the benchmark's peak disk usage stays lower on constrained runners. Bazel
  # may leave read-only artifacts behind, so make the tree writable first. On
  # failure, cleanup still preserves the whole temp workspace for inspection.
  chmod -R u+w "${output_base}" >/dev/null 2>&1 || true
  rm -rf "${output_base}"
}

prepare_seed_external_repositories() {
  local seed_output_base="$1"
  local log_file="${LOG_DIR}/prepare-seed-external-repositories.log"

  echo
  echo "==> prepare seed external repositories"
  echo "command: bazelisk test ${TARGET} (unmeasured setup run)"

  if ! "${NIX_RUN}" bazelisk \
    "--output_base=${seed_output_base}" \
    test \
    --color=no \
    --curses=no \
    --show_progress_rate_limit=60 \
    "--repository_cache=${REPOSITORY_CACHE_DIR}" \
    --disk_cache= \
    --remote_cache= \
    "${TARGET}" \
    >"${log_file}" 2>&1; then
    echo "log: ${log_file}" >&2
    tail -n 40 "${log_file}" >&2 || true
    die "failed to prepare seed external repositories"
  fi
}

seed_external_repositories() {
  local seed_output_base="$1"
  local target_output_base="$2"

  mkdir -p "${target_output_base}"
  ln -s "${seed_output_base}/external" "${target_output_base}/external"
}

run_test() {
  local name="$1"
  local remote_cache="$2"
  local output_base="$3"
  local log_file="${LOG_DIR}/${name}.log"
  local start end elapsed
  local -a cmd

  cmd=("${NIX_RUN}" bazelisk
    "--output_base=${output_base}"
    test
    --color=no
    --curses=no
    --show_progress_rate_limit=60
    "--repository_cache=${REPOSITORY_CACHE_DIR}"
    --disk_cache=
    "${TARGET}")

  if [[ -n "${remote_cache}" ]]; then
    cmd+=("--remote_cache=${remote_cache}" "--remote_upload_local_results=true")
  else
    cmd+=("--remote_cache=")
  fi

  echo >&2
  echo "==> ${name}" >&2
  echo "command: bazelisk test ${TARGET}" >&2
  if [[ -n "${remote_cache}" ]]; then
    echo "remote_cache: ${remote_cache}" >&2
  else
    echo "remote_cache: disabled" >&2
  fi
  echo "output_base: ${output_base}" >&2

  start="$(now_seconds)"
  if ! "${cmd[@]}" >"${log_file}" 2>&1; then
    echo "log: ${log_file}" >&2
    tail -n 40 "${log_file}" >&2 || true
    die "${name} failed"
  fi
  end="$(now_seconds)"
  elapsed="$(format_seconds "${start}" "${end}")"

  echo "time: ${elapsed}s" >&2
  local summary
  summary="$(extract_summary "${log_file}")"
  if [[ -n "${summary}" ]]; then
    echo "summary: ${summary}" >&2
  fi
  printf '%s\n' "${elapsed}"
}

less_than() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left < right) }'
}

remote_cache_url="$(start_bazel_remote)"

echo "Using temporary workspace: ${TMP_ROOT}"
echo "Using target: ${TARGET}"
echo "Using bazel-remote: ${remote_cache_url}"
echo "Using repository cache: ${REPOSITORY_CACHE_DIR}"

seed_output_base="${TMP_ROOT}/output-base-seed"
no_cache_output_base="${TMP_ROOT}/output-base-no-cache"
cold_cache_output_base="${TMP_ROOT}/output-base-cold-remote"
warm_cache_output_base="${TMP_ROOT}/output-base-warm-remote"

prepare_seed_external_repositories "${seed_output_base}"
seed_external_repositories "${seed_output_base}" "${no_cache_output_base}"
seed_external_repositories "${seed_output_base}" "${cold_cache_output_base}"
seed_external_repositories "${seed_output_base}" "${warm_cache_output_base}"

no_cache_time="$(run_test no-cache "" "${no_cache_output_base}")"
remove_output_base "${no_cache_output_base}"
cold_cache_time="$(run_test cold-remote-cache "${remote_cache_url}" "${cold_cache_output_base}")"
remove_output_base "${cold_cache_output_base}"
warm_cache_time="$(run_test warm-remote-cache "${remote_cache_url}" "${warm_cache_output_base}")"
remove_output_base "${warm_cache_output_base}"
remove_output_base "${seed_output_base}"

echo
echo "Results"
echo "-------"
printf 'no-cache          %8ss\n' "${no_cache_time}"
printf 'cold-remote-cache %8ss\n' "${cold_cache_time}"
printf 'warm-remote-cache %8ss\n' "${warm_cache_time}"

pass=true
if ! less_than "${warm_cache_time}" "${no_cache_time}"; then
  echo "FAIL: warm remote cache was not faster than no cache" >&2
  pass=false
fi
if ! less_than "${warm_cache_time}" "${cold_cache_time}"; then
  echo "FAIL: warm remote cache was not faster than cold remote cache" >&2
  pass=false
fi

if [[ "${pass}" != true ]]; then
  exit 1
fi

echo "PASS: warm remote cache was faster than both no cache and cold remote cache"
SUCCESS=true
