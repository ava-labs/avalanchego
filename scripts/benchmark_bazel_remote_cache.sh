#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NIX_RUN="${REPO_ROOT}/scripts/nix_run.sh"

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
Usage: scripts/benchmark_bazel_remote_cache.sh \
  --setup-args 'fetch --all' \
  --benchmark-args 'build //main:avalanchego' \
  [--benchmark-args 'build --config=race //main:avalanchego' ...]

Benchmark Bazel remote caching with a CI-style split between:
  1. one measured setup command that populates repository_cache
  2. one or more measured benchmark commands that reuse that repository_cache

For each benchmark command, the script measures three runs with fresh local
Bazel output state each time:
  - no remote cache
  - cold remote cache (empty cache; populates it)
  - warm remote cache (reuses the populated cache)

The benchmark fails unless the warm remote-cache run is faster than both the
no-cache and cold-cache runs for every configured benchmark command.
EOF
}

if [[ ${1-} == "-h" || ${1-} == "--help" ]]; then
  print_usage
  exit 0
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
require_cmd python3

SETUP_ARGS_SPEC=""
declare -a BENCHMARK_ARGS_SPECS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --setup-args)
      shift
      [[ $# -gt 0 ]] || die "--setup-args requires a value"
      [[ -z "${SETUP_ARGS_SPEC}" ]] || die "--setup-args may only be specified once"
      SETUP_ARGS_SPEC="$1"
      ;;
    --benchmark-args)
      shift
      [[ $# -gt 0 ]] || die "--benchmark-args requires a value"
      BENCHMARK_ARGS_SPECS+=("$1")
      ;;
    *)
      print_usage >&2
      die "unknown argument: $1"
      ;;
  esac
  shift
done

[[ -n "${SETUP_ARGS_SPEC}" ]] || die "--setup-args is required"
[[ ${#BENCHMARK_ARGS_SPECS[@]} -gt 0 ]] || die "at least one --benchmark-args is required"

# Task definitions are the intended configuration layer for this script, so the
# CLI accepts shell-style strings like 'build --config=race //main:avalanchego'.
# We still parse those strings into argv before exec'ing bazelisk so quoting is
# explicit and the actual command line remains readable in task definitions.
parse_arg_spec() {
  local spec="$1"
  python3 - "$spec" <<'PY'
import shlex
import sys

for arg in shlex.split(sys.argv[1]):
    print(arg)
PY
}

# Convert a single shell-style command string into a bash array. We accept this
# small amount of shell-like parsing because the repo task codifies the command
# strings; if the harness ever needs machine-generated argv with arbitrary data,
# the interface should move to an argv-oriented delimiter format instead.
declare -a PARSED_COMMAND_ARGS=()
load_command_array() {
  local spec="$1"

  mapfile -t PARSED_COMMAND_ARGS < <(parse_arg_spec "$spec")
  [[ ${#PARSED_COMMAND_ARGS[@]} -gt 0 ]] || die "command spec must not be empty: ${spec}"
}

TMP_ROOT="$(mktemp -d -t bazel-remote-cache-bench.XXXXXX)"
CACHE_DIR="${TMP_ROOT}/remote-cache"
REPOSITORY_CACHE_DIR="${TMP_ROOT}/repository-cache"
LOG_DIR="${TMP_ROOT}/logs"
mkdir -p "${CACHE_DIR}" "${REPOSITORY_CACHE_DIR}" "${LOG_DIR}"

# This benchmark is intended to model CI workers talking to a remote cache
# service that is not colocated with the runner. When latency simulation is
# enabled in a future iteration, the local benchmark host should talk to
# bazel-remote through a proxy that injects representative delay instead of
# talking to bazel-remote directly.
#
# The representative delay should be measured separately from GitHub Actions
# runners to an AWS us-east-1 hosted endpoint using curl. That measurement is
# not part of the benchmark subject itself; it is only an input used to
# approximate the runner-to-cache network distance we expect in deployment.
#
# The intended comparison for each benchmark command is:
#   - no cache
#   - HTTP remote cache, cold
#   - HTTP remote cache, warm
#   - gRPC remote cache, cold
#   - gRPC remote cache, warm
#
# The point is to understand how much realistic cache latency changes Bazel
# execution time and whether HTTP remains sufficient under that latency or
# whether gRPC provides enough additional value to justify requiring it.

REMOTE_PID=""
REMOTE_LOG=""
SUCCESS=false
stop_bazel_remote() {
  if [[ -n "${REMOTE_PID}" ]] && kill -0 "${REMOTE_PID}" >/dev/null 2>&1; then
    kill "${REMOTE_PID}" >/dev/null 2>&1 || true
    wait "${REMOTE_PID}" >/dev/null 2>&1 || true
  fi
  REMOTE_PID=""
}

cleanup() {
  stop_bazel_remote

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
  local cache_dir="$1"
  local log_name="$2"
  local port attempts
  attempts=10

  stop_bazel_remote

  for ((i = 1; i <= attempts; i++)); do
    port="$(choose_port)"
    REMOTE_LOG="${LOG_DIR}/${log_name}"

    "${BAZEL_REMOTE_LAUNCHER[@]}" \
      --dir "${cache_dir}" \
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

  # Each run gets a fresh output base for isolation, but successful runs do not
  # need to retain that local action/output state once their timing is recorded.
  # Bazel may leave read-only artifacts behind, so make the tree writable before
  # deleting to keep peak disk usage lower on constrained CI runners while still
  # preserving the full temp workspace on failure for inspection.
  chmod -R u+w "${output_base}" >/dev/null 2>&1 || true
  rm -rf "${output_base}"
}

run_bazel_command() {
  local phase_name="$1"
  local command_spec="$2"
  local remote_cache="$3"
  local output_base="$4"
  local log_file="$5"
  local start end elapsed
  local -a cmd

  load_command_array "${command_spec}"

  # --output_base is a Bazel startup flag, but --repository_cache is a command
  # flag, so they must be placed on opposite sides of the subcommand token.
  cmd=("${NIX_RUN}" bazelisk
    "--output_base=${output_base}"
    "${PARSED_COMMAND_ARGS[0]}"
    --color=no
    --curses=no
    --show_progress_rate_limit=60
    "--repository_cache=${REPOSITORY_CACHE_DIR}"
    --disk_cache=)

  if [[ -n "${remote_cache}" ]]; then
    cmd+=("--remote_cache=${remote_cache}" "--remote_upload_local_results=true")
  else
    cmd+=(--remote_cache=)
  fi

  if [[ ${#PARSED_COMMAND_ARGS[@]} -gt 1 ]]; then
    cmd+=("${PARSED_COMMAND_ARGS[@]:1}")
  fi

  echo >&2
  echo "==> ${phase_name}" >&2
  echo "command: bazelisk ${command_spec}" >&2
  if [[ -n "${remote_cache}" ]]; then
    echo "remote_cache: ${remote_cache}" >&2
  else
    echo "remote_cache: disabled" >&2
  fi
  echo "repository_cache: ${REPOSITORY_CACHE_DIR}" >&2
  echo "output_base: ${output_base}" >&2

  start="$(now_seconds)"
  if ! "${cmd[@]}" >"${log_file}" 2>&1; then
    echo "log: ${log_file}" >&2
    tail -n 40 "${log_file}" >&2 || true
    die "${phase_name} failed"
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

echo "Using temporary workspace: ${TMP_ROOT}"
echo "Using repository cache: ${REPOSITORY_CACHE_DIR}"

# The setup phase models the CI cache-writer job from PR 5525: start from an
# empty repository_cache, then measure a single command (typically `fetch --all`)
# that populates only external dependency state. The per-benchmark runs then use
# fresh output bases so local action/output state does not bleed across timings.
setup_output_base="${TMP_ROOT}/output-base-setup"
setup_time="$(run_bazel_command setup "${SETUP_ARGS_SPEC}" "" "${setup_output_base}" "${LOG_DIR}/setup.log")"
remove_output_base "${setup_output_base}"

echo
echo "Setup result"
echo "------------"
echo "command: bazelisk ${SETUP_ARGS_SPEC}"
printf 'time: %ss\n' "${setup_time}"

declare -a RESULT_LABELS=()
declare -a RESULT_NO_CACHE_TIMES=()
declare -a RESULT_COLD_CACHE_TIMES=()
declare -a RESULT_WARM_CACHE_TIMES=()
all_passed=true

for index in "${!BENCHMARK_ARGS_SPECS[@]}"; do
  benchmark_spec="${BENCHMARK_ARGS_SPECS[index]}"
  label="${benchmark_spec}"

  echo
  echo "Benchmark: bazelisk ${label}"
  echo "========================================"

  no_cache_output_base="${TMP_ROOT}/output-base-benchmark-${index}-no-cache"
  cold_cache_output_base="${TMP_ROOT}/output-base-benchmark-${index}-cold-remote"
  warm_cache_output_base="${TMP_ROOT}/output-base-benchmark-${index}-warm-remote"
  # Each benchmark command gets its own empty remote cache so the "cold" run is
  # actually cold for that command instead of inheriting artifacts from a prior
  # benchmark section.
  benchmark_remote_cache_dir="${CACHE_DIR}/benchmark-${index}"
  mkdir -p "${benchmark_remote_cache_dir}"

  no_cache_time="$(run_bazel_command "no-cache (${label})" "${benchmark_spec}" "" "${no_cache_output_base}" "${LOG_DIR}/benchmark-${index}-no-cache.log")"
  remove_output_base "${no_cache_output_base}"

  remote_cache_url="$(start_bazel_remote "${benchmark_remote_cache_dir}" "benchmark-${index}-bazel-remote.log")"
  echo "Using bazel-remote for benchmark ${index}: ${remote_cache_url}"

  cold_cache_time="$(run_bazel_command "cold-remote-cache (${label})" "${benchmark_spec}" "${remote_cache_url}" "${cold_cache_output_base}" "${LOG_DIR}/benchmark-${index}-cold-remote.log")"
  remove_output_base "${cold_cache_output_base}"
  warm_cache_time="$(run_bazel_command "warm-remote-cache (${label})" "${benchmark_spec}" "${remote_cache_url}" "${warm_cache_output_base}" "${LOG_DIR}/benchmark-${index}-warm-remote.log")"
  remove_output_base "${warm_cache_output_base}"

  RESULT_LABELS+=("${label}")
  RESULT_NO_CACHE_TIMES+=("${no_cache_time}")
  RESULT_COLD_CACHE_TIMES+=("${cold_cache_time}")
  RESULT_WARM_CACHE_TIMES+=("${warm_cache_time}")

  if ! less_than "${warm_cache_time}" "${no_cache_time}"; then
    echo "FAIL: warm remote cache was not faster than no cache for: bazelisk ${label}" >&2
    all_passed=false
  fi
  if ! less_than "${warm_cache_time}" "${cold_cache_time}"; then
    echo "FAIL: warm remote cache was not faster than cold remote cache for: bazelisk ${label}" >&2
    all_passed=false
  fi
done

echo
echo "Benchmark results"
echo "-----------------"
printf 'setup %-63s %8ss\n' "bazelisk ${SETUP_ARGS_SPEC}" "${setup_time}"
for index in "${!RESULT_LABELS[@]}"; do
  echo
  echo "bazelisk ${RESULT_LABELS[index]}"
  printf '  no-cache          %8ss\n' "${RESULT_NO_CACHE_TIMES[index]}"
  printf '  cold-remote-cache %8ss\n' "${RESULT_COLD_CACHE_TIMES[index]}"
  printf '  warm-remote-cache %8ss\n' "${RESULT_WARM_CACHE_TIMES[index]}"
done

if [[ "${all_passed}" != true ]]; then
  exit 1
fi

echo "PASS: warm remote cache was faster than both no cache and cold remote cache for every benchmark command"
SUCCESS=true
