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

if command -v toxiproxy-server >/dev/null 2>&1 && command -v toxiproxy-cli >/dev/null 2>&1; then
  TOXIPROXY_SERVER_LAUNCHER=(toxiproxy-server)
  TOXIPROXY_CLI_LAUNCHER=(toxiproxy-cli)
elif command -v nix >/dev/null 2>&1; then
  TOXIPROXY_SERVER_LAUNCHER=(nix shell nixpkgs#toxiproxy -c toxiproxy-server)
  TOXIPROXY_CLI_LAUNCHER=(nix shell nixpkgs#toxiproxy -c toxiproxy-cli)
else
  echo "error: toxiproxy not found on PATH and nix is unavailable" >&2
  exit 1
fi

print_usage() {
  cat <<'EOF'
Usage: scripts/benchmark_bazel_remote_cache.sh \
  --setup-args 'fetch --all' \
  [--build-benchmark-args 'build //main:avalanchego' ...] \
  [--test-benchmark-args 'test //... -- -//graft/...' ...] \
  [--test-impacted-scope '//...' --test-impacted-scope '-//graft/...'] \
  [--test-impacted-range 'HEAD..'] \
  [--warm-log-must-contain '(cached) PASSED'] \
  [--warm-log-must-contain 'Executed 0 out of 1 test: 1 test passes.']

Benchmark Bazel remote caching with a CI-style split between:
  1. one measured setup command that populates repository_cache
  2. one or more measured benchmark commands that reuse that repository_cache

For build benchmark commands, the script measures five runs with fresh local
Bazel output state each time:
  - no remote cache
  - HTTP cold remote cache (empty cache; populates it)
  - HTTP warm remote cache (reuses the populated cache)
  - gRPC cold remote cache
  - gRPC warm remote cache

For test benchmark commands, the script measures four runs with fresh local
Bazel output state each time:
  - no remote cache
  - HTTP cold remote cache
  - HTTP warm remote cache
  - impacted-target mode (selector time plus selected-test execution time)

Representative cache latency is determined before the benchmark starts unless
BAZEL_REMOTE_CACHE_LATENCY_MS is set. In measured mode, the script runs
`bazel run //tools/measure-http-latency -- --mode=warm-h2` against
BAZEL_REMOTE_CACHE_LATENCY_URL (default: https://ec2.us-east-1.amazonaws.com/),
then validates that a toxiproxy-induced HTTP path to bazel-remote is within
BAZEL_REMOTE_CACHE_LATENCY_TOLERANCE_MS of the measured target.

The benchmark fails unless each warm remote-cache run is faster than both the
corresponding no-cache and cold-cache runs for every configured cached
benchmark command. Impacted-target timings are reported for comparison but are
not treated as a pass/fail assertion.

By default, setup-side dependency caches are reused across local runs under
`$XDG_CACHE_HOME/av-bazel-remote-cache-benchmark/` when `XDG_CACHE_HOME` is
set, otherwise `~/.cache/av-bazel-remote-cache-benchmark/`.
Set `BAZEL_REMOTE_CACHE_FRESH_SETUP_CACHE=1` to force fresh setup caches.
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
require_cmd grep
require_cmd jq
require_cmd mktemp

SETUP_ARGS_SPEC=""
declare -a BUILD_BENCHMARK_ARGS_SPECS=()
declare -a TEST_BENCHMARK_ARGS_SPECS=()
declare -a TEST_IMPACTED_SCOPES=()
TEST_IMPACTED_RANGE=""
declare -a WARM_LOG_MUST_CONTAIN_PATTERNS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --setup-args)
      shift
      [[ $# -gt 0 ]] || die "--setup-args requires a value"
      [[ -z "${SETUP_ARGS_SPEC}" ]] || die "--setup-args may only be specified once"
      SETUP_ARGS_SPEC="$1"
      ;;
    --build-benchmark-args)
      shift
      [[ $# -gt 0 ]] || die "--build-benchmark-args requires a value"
      BUILD_BENCHMARK_ARGS_SPECS+=("$1")
      ;;
    --test-benchmark-args)
      shift
      [[ $# -gt 0 ]] || die "--test-benchmark-args requires a value"
      TEST_BENCHMARK_ARGS_SPECS+=("$1")
      ;;
    --test-impacted-scope)
      shift
      [[ $# -gt 0 ]] || die "--test-impacted-scope requires a value"
      TEST_IMPACTED_SCOPES+=("$1")
      ;;
    --test-impacted-range)
      shift
      [[ $# -gt 0 ]] || die "--test-impacted-range requires a value"
      [[ -z "${TEST_IMPACTED_RANGE}" ]] || die "--test-impacted-range may only be specified once"
      TEST_IMPACTED_RANGE="$1"
      ;;
    --warm-log-must-contain)
      shift
      [[ $# -gt 0 ]] || die "--warm-log-must-contain requires a value"
      WARM_LOG_MUST_CONTAIN_PATTERNS+=("$1")
      ;;
    *)
      print_usage >&2
      die "unknown argument: $1"
      ;;
  esac
  shift
done

[[ -n "${SETUP_ARGS_SPEC}" ]] || die "--setup-args is required"
if [[ ${#BUILD_BENCHMARK_ARGS_SPECS[@]} -eq 0 && ${#TEST_BENCHMARK_ARGS_SPECS[@]} -eq 0 ]]; then
  die "at least one --build-benchmark-args or --test-benchmark-args is required"
fi
if [[ ${#TEST_BENCHMARK_ARGS_SPECS[@]} -gt 0 && ${#TEST_IMPACTED_SCOPES[@]} -eq 0 ]]; then
  die "at least one --test-impacted-scope is required when --test-benchmark-args is used"
fi

# Task definitions are the intended configuration layer for this script, so the
# CLI accepts shell-style strings like 'build --config=race //main:avalanchego'.
# We still parse those strings into argv before exec'ing bazelisk so quoting is
# explicit and the actual command line remains readable in task definitions.
#
# This intentionally relies on shell-style parsing because the task layer owns
# these strings. If the harness ever needs machine-generated argv with arbitrary
# data, the interface should move to an argv-oriented delimiter format instead.
declare -a PARSED_COMMAND_ARGS=()
load_command_array() {
  local spec="$1"

  eval "set -- ${spec}"
  PARSED_COMMAND_ARGS=("$@")
  [[ ${#PARSED_COMMAND_ARGS[@]} -gt 0 ]] || die "command spec must not be empty: ${spec}"
}

TMP_ROOT="$(mktemp -d -t bazel-remote-cache-bench.XXXXXX)"
CACHE_DIR="${TMP_ROOT}/remote-cache"
SETUP_CACHE_ROOT_DEFAULT="${XDG_CACHE_HOME:-${HOME}/.cache}/av-bazel-remote-cache-benchmark"
SETUP_CACHE_ROOT="${BAZEL_REMOTE_CACHE_SETUP_CACHE_ROOT:-${SETUP_CACHE_ROOT_DEFAULT}}"
USE_FRESH_SETUP_CACHE="${BAZEL_REMOTE_CACHE_FRESH_SETUP_CACHE:-0}"
if [[ "${USE_FRESH_SETUP_CACHE}" == "1" ]]; then
  REPOSITORY_CACHE_DIR="${TMP_ROOT}/repository-cache"
  GO_REPOSITORY_MODCACHE_DIR="${TMP_ROOT}/go-mod-cache"
else
  REPOSITORY_CACHE_DIR="${SETUP_CACHE_ROOT}/repository-cache"
  GO_REPOSITORY_MODCACHE_DIR="${SETUP_CACHE_ROOT}/go-mod-cache"
fi
LOG_DIR="${TMP_ROOT}/logs"
mkdir -p "${CACHE_DIR}" "${REPOSITORY_CACHE_DIR}" "${GO_REPOSITORY_MODCACHE_DIR}" "${LOG_DIR}"

DEFAULT_REPRESENTATIVE_LATENCY_URL="https://ec2.us-east-1.amazonaws.com/"
REPRESENTATIVE_LATENCY_URL="${BAZEL_REMOTE_CACHE_LATENCY_URL:-${DEFAULT_REPRESENTATIVE_LATENCY_URL}}"
REPRESENTATIVE_LATENCY_SAMPLES="${BAZEL_REMOTE_CACHE_LATENCY_SAMPLES:-10}"
REPRESENTATIVE_LATENCY_TIMEOUT="${BAZEL_REMOTE_CACHE_LATENCY_TIMEOUT:-10s}"
REPRESENTATIVE_LATENCY_OVERRIDE_MS="${BAZEL_REMOTE_CACHE_LATENCY_MS:-}"
REPRESENTATIVE_LATENCY_MS=""
# Default measured mode validates that the proxied cache path stays within a
# small tolerance of the measured target. Override mode skips that validation
# because its purpose is fast local iteration, not provenance.
REPRESENTATIVE_LATENCY_TOLERANCE_MS="${BAZEL_REMOTE_CACHE_LATENCY_TOLERANCE_MS:-15}"
SETUP_TIMEOUT_SECONDS="${BAZEL_REMOTE_CACHE_SETUP_TIMEOUT_SECONDS:-0}"
BENCHMARK_TIMEOUT_SECONDS="${BAZEL_REMOTE_CACHE_BENCHMARK_TIMEOUT_SECONDS:-0}"

# This benchmark is intended to model CI workers talking to a remote cache
# service that is not colocated with the runner. When latency simulation is
# enabled in a future iteration, the local benchmark host should talk to
# bazel-remote through a proxy that injects representative delay instead of
# talking to bazel-remote directly.
#
# The representative delay is determined before each benchmark run. By default
# the script measures warm reused-connection HTTP/2 request latency to a public
# AWS us-east-1 regional endpoint using //tools/measure-http-latency in
# --mode=warm-h2.
#
# Why this shape:
# - persistent gRPC is much closer to reused-connection request/response
#   latency than to cold-connect TTFB, so warm-h2 is a better first-order proxy
# - the tool measures request write -> first response byte on reused
#   connections, which avoids repeatedly charging TCP/TLS setup costs that a
#   warm gRPC channel would not pay
# - a public regional AWS endpoint lets the benchmark approximate runner to
#   us-east-1 network distance before a real deployed bazel-remote exists
# - live measurement avoids stale hard-coded latency values as runner routing
#   and network conditions change over time
# - BAZEL_REMOTE_CACHE_LATENCY_MS exists specifically for fast local iteration;
#   when set, the script intentionally skips provenance-oriented live
#   measurement and future proxy-path validation
#
# Trade-off:
# this still does not claim to reproduce the exact behavior of a future
# EKS-hosted bazel-remote deployment. It is a better no-server approximation of
# steady-state cache request latency. Measuring against a real deployed
# bazel-remote remains the next planned refinement of the experiment.
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
REMOTE_HTTP_URL=""
REMOTE_GRPC_TARGET=""
TOXIPROXY_PID=""
TOXIPROXY_LOG=""
TOXIPROXY_API_URL=""
KEEP_TMP="${DEBUG:-0}"
SUCCESS=false
stop_bazel_remote() {
  if [[ -n "${REMOTE_PID}" ]] && kill -0 "${REMOTE_PID}" >/dev/null 2>&1; then
    kill "${REMOTE_PID}" >/dev/null 2>&1 || true
    wait "${REMOTE_PID}" >/dev/null 2>&1 || true
  fi
  REMOTE_PID=""
  REMOTE_HTTP_URL=""
  REMOTE_GRPC_TARGET=""
}

stop_toxiproxy() {
  if [[ -n "${TOXIPROXY_PID}" ]] && kill -0 "${TOXIPROXY_PID}" >/dev/null 2>&1; then
    kill "${TOXIPROXY_PID}" >/dev/null 2>&1 || true
    wait "${TOXIPROXY_PID}" >/dev/null 2>&1 || true
  fi
  TOXIPROXY_PID=""
  TOXIPROXY_API_URL=""
}

cleanup() {
  stop_toxiproxy
  stop_bazel_remote

  if [[ "${KEEP_TMP}" == "1" ]]; then
    echo "kept temporary workspace for inspection: ${TMP_ROOT}" >&2
    return
  fi

  chmod -R u+w "${TMP_ROOT}" >/dev/null 2>&1 || true
  rm -rf "${TMP_ROOT}" >/dev/null 2>&1 || true

  if [[ "${SUCCESS}" != true ]]; then
    echo "temporary workspace removed; re-run with DEBUG=1 to retain it for inspection" >&2
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
  local http_port grpc_port attempts
  attempts=10

  stop_bazel_remote

  for ((i = 1; i <= attempts; i++)); do
    http_port="$(choose_port)"
    grpc_port="$(choose_port)"
    REMOTE_LOG="${LOG_DIR}/${log_name}"

    "${BAZEL_REMOTE_LAUNCHER[@]}" \
      --dir "${cache_dir}" \
      --max_size 5 \
      --http_address "127.0.0.1:${http_port}" \
      --grpc_address "127.0.0.1:${grpc_port}" \
      >"${REMOTE_LOG}" 2>&1 &
    REMOTE_PID=$!

    sleep 1
    if kill -0 "${REMOTE_PID}" >/dev/null 2>&1; then
      REMOTE_HTTP_URL="http://127.0.0.1:${http_port}"
      REMOTE_GRPC_TARGET="127.0.0.1:${grpc_port}"
      return 0
    fi

    wait "${REMOTE_PID}" >/dev/null 2>&1 || true
    REMOTE_PID=""
  done

  die "failed to start bazel-remote; last log:\n$(tail -n 20 "${REMOTE_LOG}" 2>/dev/null || true)"
}

start_toxiproxy() {
  local log_name="$1"
  local port attempts
  attempts=10

  stop_toxiproxy

  for ((i = 1; i <= attempts; i++)); do
    port="$(choose_port)"
    TOXIPROXY_LOG="${LOG_DIR}/${log_name}"
    TOXIPROXY_API_URL="http://127.0.0.1:${port}"

    "${TOXIPROXY_SERVER_LAUNCHER[@]}" \
      -host 127.0.0.1 \
      -port "${port}" \
      >"${TOXIPROXY_LOG}" 2>&1 &
    TOXIPROXY_PID=$!

    sleep 1
    if kill -0 "${TOXIPROXY_PID}" >/dev/null 2>&1 \
      && "${TOXIPROXY_CLI_LAUNCHER[@]}" --host "${TOXIPROXY_API_URL}" list >/dev/null 2>&1; then
      echo "${TOXIPROXY_API_URL}"
      return 0
    fi

    wait "${TOXIPROXY_PID}" >/dev/null 2>&1 || true
    TOXIPROXY_PID=""
    TOXIPROXY_API_URL=""
  done

  die "failed to start toxiproxy; last log:\n$(tail -n 20 "${TOXIPROXY_LOG}" 2>/dev/null || true)"
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

FAIL_FAST_FETCH_PATTERN='An error occurred during the fetch of repository|Error in fail:|i/o timeout|reading \.\./\.\./go\.mod|copy_file_range: is a directory|no such file or directory'

run_logged_command() {
  local timeout_seconds="$1"
  local fail_fast_mode="$2"
  local log_file="$3"
  shift 3

  : >"${log_file}"
  "$@" >"${log_file}" 2>&1 &
  local cmd_pid=$!
  local start now matched_line

  start="$(date +%s)"
  while kill -0 "${cmd_pid}" >/dev/null 2>&1; do
    if [[ "${fail_fast_mode}" == "1" ]]; then
      matched_line="$(grep -m 1 -E "${FAIL_FAST_FETCH_PATTERN}" "${log_file}" || true)"
      if [[ -n "${matched_line}" ]]; then
        kill "${cmd_pid}" >/dev/null 2>&1 || true
        wait "${cmd_pid}" >/dev/null 2>&1 || true
        echo "error: fail-fast fetch failure detected: ${matched_line}" >&2
        return 125
      fi
    fi

    if [[ "${timeout_seconds}" != "0" ]]; then
      now="$(date +%s)"
      if (( now - start > timeout_seconds )); then
        kill "${cmd_pid}" >/dev/null 2>&1 || true
        wait "${cmd_pid}" >/dev/null 2>&1 || true
        echo "error: command timed out after ${timeout_seconds}s" >&2
        return 124
      fi
    fi

    sleep 1
  done

  wait "${cmd_pid}"
}

print_failure_context() {
  local log_file="$1"

  grep -E '^(ERROR:|INFO: Repository )|Error in fail:|i/o timeout|timed out|copy_file_range: is a directory|no such file or directory|reading ../../go.mod' "${log_file}" | head -n 40 >&2 || true
}

run_bazel_argv() {
  local phase_name="$1"
  local command_text="$2"
  local remote_cache="$3"
  local output_base="$4"
  local log_file="$5"
  local timeout_seconds="$6"
  local fail_fast_mode="$7"
  shift 7
  local start end elapsed status
  local -a cmd command_args

  command_args=("$@")
  [[ ${#command_args[@]} -gt 0 ]] || die "command argv must not be empty for phase ${phase_name}"

  # --output_base is a Bazel startup flag, but --repository_cache is a command
  # flag, so they must be placed on opposite sides of the subcommand token.
  cmd=("${NIX_RUN}" bazelisk
    "--output_base=${output_base}"
    "${command_args[0]}"
    --color=no
    --curses=no
    --show_progress_rate_limit=60
    "--repository_cache=${REPOSITORY_CACHE_DIR}"
    # Gazelle go_repository keeps an internal per-output-base module cache by
    # default, which defeats the goal of a shared setup phase when each measured
    # run gets a fresh output base. Force it onto a host-level shared GOMODCACHE
    # so setup can seed Go module downloads once and later fresh-output-base
    # runs can reuse them without re-downloading.
    "--repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1"
    "--repo_env=GOMODCACHE=${GO_REPOSITORY_MODCACHE_DIR}"
    --disk_cache=)

  if [[ -n "${remote_cache}" ]]; then
    cmd+=("--remote_cache=${remote_cache}" "--remote_upload_local_results=true")
  else
    cmd+=(--remote_cache=)
  fi

  if [[ ${#command_args[@]} -gt 1 ]]; then
    cmd+=("${command_args[@]:1}")
  fi

  echo >&2
  echo "==> ${phase_name}" >&2
  echo "command: bazelisk ${command_text}" >&2
  if [[ -n "${remote_cache}" ]]; then
    echo "remote_cache: ${remote_cache}" >&2
  else
    echo "remote_cache: disabled" >&2
  fi
  echo "repository_cache: ${REPOSITORY_CACHE_DIR}" >&2
  echo "go_repository mod cache: ${GO_REPOSITORY_MODCACHE_DIR}" >&2
  echo "output_base: ${output_base}" >&2

  start="$(now_seconds)"
  set +e
  run_logged_command "${timeout_seconds}" "${fail_fast_mode}" "${log_file}" "${cmd[@]}"
  status=$?
  set -e
  if [[ ${status} -ne 0 ]]; then
    echo "log: ${log_file}" >&2
    print_failure_context "${log_file}"
    tail -n 40 "${log_file}" >&2 || true
    if [[ ${status} -eq 124 ]]; then
      die "${phase_name} timed out after ${timeout_seconds}s"
    fi
    if [[ ${status} -eq 125 ]]; then
      die "${phase_name} hit a fail-fast fetch error"
    fi
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

run_bazel_command() {
  local phase_name="$1"
  local command_spec="$2"
  local remote_cache="$3"
  local output_base="$4"
  local log_file="$5"
  local timeout_seconds="$6"
  local fail_fast_mode="$7"

  load_command_array "${command_spec}"
  run_bazel_argv \
    "${phase_name}" \
    "${command_spec}" \
    "${remote_cache}" \
    "${output_base}" \
    "${log_file}" \
    "${timeout_seconds}" \
    "${fail_fast_mode}" \
    "${PARSED_COMMAND_ARGS[@]}"
}

less_than() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left < right) }'
}

warm_log_contains_all_patterns() {
  local log_file="$1"
  local pattern

  for pattern in "${WARM_LOG_MUST_CONTAIN_PATTERNS[@]}"; do
    if ! grep -F -q -- "${pattern}" "${log_file}"; then
      echo "missing warm-log pattern: ${pattern}" >&2
      return 1
    fi
  done
}

greater_than_zero() {
  awk -v value="$1" 'BEGIN { exit !(value > 0) }'
}

round_positive_ms_to_int() {
  awk -v value="$1" 'BEGIN { printf "%d", int(value + 0.5) }'
}

extract_representative_latency_ms() {
  local latency_json_log="$1"

  jq -r '.avgWriteToFirstByteMs | tostring' "${latency_json_log}"
}

render_latency_text_report() {
  local latency_json_log="$1"
  local latency_text_log="$2"

  {
    jq -r '"url=\(.url)"' "${latency_json_log}"
    jq -r '"mode=\(.mode)"' "${latency_json_log}"
    jq -r '"samples=\(.samples)"' "${latency_json_log}"
    jq -r '"reused_samples=\(.reusedSamples)"' "${latency_json_log}"
    jq -r '.statusCodes | to_entries | sort_by(.key | tonumber)[] | "http_code[\(.key)]=\(.value)"' "${latency_json_log}"
    jq -r '"avg_connect=\(.avgConnectMs)ms avg_tls=\(.avgTlsMs)ms avg_ttfb=\(.avgTtfbMs)ms avg_write_to_first_byte=\(.avgWriteToFirstByteMs)ms avg_total=\(.avgTotalMs)ms"' "${latency_json_log}"
  } >"${latency_text_log}"
}

measure_http_latency() {
  local label="$1"
  local url="$2"
  local latency_json_log="$3"
  local latency_text_log="$4"

  echo "${label}"
  "${NIX_RUN}" bazelisk run //tools/measure-http-latency -- \
    --url "${url}" \
    --mode warm-h2 \
    --samples "${REPRESENTATIVE_LATENCY_SAMPLES}" \
    --timeout "${REPRESENTATIVE_LATENCY_TIMEOUT}" \
    --format json \
    >"${latency_json_log}"

  render_latency_text_report "${latency_json_log}" "${latency_text_log}"
  cat "${latency_text_log}"
}

within_tolerance() {
  awk -v observed="$1" -v target="$2" -v tolerance="$3" '
    BEGIN {
      diff = observed - target
      if (diff < 0) {
        diff = -diff
      }
      exit !(diff <= tolerance)
    }
  '
}

create_latency_proxy_for_address() {
  local upstream_address="$1"
  local proxy_name="$2"
  local latency_ms="$3"
  local listen_port rounded_latency_ms

  listen_port="$(choose_port)"
  rounded_latency_ms="$(round_positive_ms_to_int "${latency_ms}")"

  "${TOXIPROXY_CLI_LAUNCHER[@]}" --host "${TOXIPROXY_API_URL}" \
    create \
    --listen "127.0.0.1:${listen_port}" \
    --upstream "${upstream_address}" \
    "${proxy_name}" \
    >/dev/null

  "${TOXIPROXY_CLI_LAUNCHER[@]}" --host "${TOXIPROXY_API_URL}" \
    toxic add \
    --type latency \
    --downstream \
    --toxicName "${proxy_name}-latency" \
    --attribute "latency=${rounded_latency_ms}" \
    "${proxy_name}" \
    >/dev/null

  printf '127.0.0.1:%s\n' "${listen_port}"
}

create_http_latency_proxy() {
  local remote_cache_url="$1"
  local proxy_name="$2"
  local latency_ms="$3"

  [[ "${remote_cache_url}" == http://* ]] || die "expected http remote cache URL, got: ${remote_cache_url}"
  printf 'http://%s\n' "$(create_latency_proxy_for_address "${remote_cache_url#http://}" "${proxy_name}" "${latency_ms}")"
}

create_grpc_latency_proxy() {
  local remote_cache_target="$1"
  local proxy_name="$2"
  local latency_ms="$3"

  printf 'grpc://%s\n' "$(create_latency_proxy_for_address "${remote_cache_target}" "${proxy_name}" "${latency_ms}")"
}

validate_proxied_http_latency() {
  local proxied_remote_cache_url="$1"
  local latency_json_log latency_text_log observed_latency_ms

  if [[ -n "${REPRESENTATIVE_LATENCY_OVERRIDE_MS}" ]]; then
    echo "Skipping proxied HTTP latency validation because override mode is active."
    return 0
  fi

  latency_json_log="${LOG_DIR}/proxied-bazel-remote-http-latency.json"
  latency_text_log="${LOG_DIR}/proxied-bazel-remote-http-latency.txt"

  measure_http_latency \
    "Validating proxied bazel-remote HTTP latency" \
    "${proxied_remote_cache_url}" \
    "${latency_json_log}" \
    "${latency_text_log}"

  observed_latency_ms="$(extract_representative_latency_ms "${latency_json_log}")"
  printf 'proxy target latency: %sms\n' "${REPRESENTATIVE_LATENCY_MS}"
  printf 'proxy observed latency: %sms\n' "${observed_latency_ms}"
  printf 'proxy tolerance: %sms\n' "${REPRESENTATIVE_LATENCY_TOLERANCE_MS}"

  if ! within_tolerance "${observed_latency_ms}" "${REPRESENTATIVE_LATENCY_MS}" "${REPRESENTATIVE_LATENCY_TOLERANCE_MS}"; then
    die "proxied bazel-remote HTTP latency ${observed_latency_ms}ms was not within ${REPRESENTATIVE_LATENCY_TOLERANCE_MS}ms of target ${REPRESENTATIVE_LATENCY_MS}ms"
  fi
}

determine_representative_latency_ms() {
  if [[ -n "${REPRESENTATIVE_LATENCY_OVERRIDE_MS}" ]]; then
    greater_than_zero "${REPRESENTATIVE_LATENCY_OVERRIDE_MS}" || die "BAZEL_REMOTE_CACHE_LATENCY_MS must be > 0 when set"
    REPRESENTATIVE_LATENCY_MS="${REPRESENTATIVE_LATENCY_OVERRIDE_MS}"
    echo "Using representative cache latency override: ${REPRESENTATIVE_LATENCY_MS}ms"
    echo "Skipping live measurement and proxy-path validation because override mode is intended for fast local iteration."
    return 0
  fi

  local latency_json_log latency_text_log
  latency_json_log="${LOG_DIR}/representative-latency.json"
  latency_text_log="${LOG_DIR}/representative-latency.txt"

  printf 'Representative latency probe: url=%s samples=%s timeout=%s\n' \
    "${REPRESENTATIVE_LATENCY_URL}" \
    "${REPRESENTATIVE_LATENCY_SAMPLES}" \
    "${REPRESENTATIVE_LATENCY_TIMEOUT}"

  measure_http_latency \
    "Measuring representative cache latency with bazel run //tools/measure-http-latency" \
    "${REPRESENTATIVE_LATENCY_URL}" \
    "${latency_json_log}" \
    "${latency_text_log}"

  REPRESENTATIVE_LATENCY_MS="$(extract_representative_latency_ms "${latency_json_log}")"
}

prepare_proxied_bazel_remote_http() {
  local validation_remote_cache_dir validation_remote_cache_url validation_proxy_url

  validation_remote_cache_dir="${CACHE_DIR}/latency-validation"
  mkdir -p "${validation_remote_cache_dir}"

  TOXIPROXY_API_URL="$(start_toxiproxy "benchmark-toxiproxy.log")"
  echo "Using toxiproxy API: ${TOXIPROXY_API_URL}"

  start_bazel_remote "${validation_remote_cache_dir}" "latency-validation-bazel-remote.log"
  validation_remote_cache_url="${REMOTE_HTTP_URL}"
  echo "Using latency-validation bazel-remote: ${validation_remote_cache_url}"

  validation_proxy_url="$(create_http_latency_proxy "${validation_remote_cache_url}" "latency-validation-http-cache" "${REPRESENTATIVE_LATENCY_MS}")"
  echo "Using proxied bazel-remote HTTP endpoint: ${validation_proxy_url}"

  echo "Using latency-validation bazel-remote gRPC endpoint: ${REMOTE_GRPC_TARGET}"

  validate_proxied_http_latency "${validation_proxy_url}"

  stop_bazel_remote
}

echo "Using temporary workspace: ${TMP_ROOT}"
echo "Set DEBUG=1 to retain the temporary workspace for inspection"
if [[ "${USE_FRESH_SETUP_CACHE}" == "1" ]]; then
  echo "Using fresh setup caches for this run"
else
  echo "Using persistent setup caches rooted at: ${SETUP_CACHE_ROOT}"
fi
echo "Using repository cache: ${REPOSITORY_CACHE_DIR}"
echo "Using go_repository mod cache: ${GO_REPOSITORY_MODCACHE_DIR}"
if [[ "${SETUP_TIMEOUT_SECONDS}" != "0" ]]; then
  printf 'Setup timeout backstop: %ss\n' "${SETUP_TIMEOUT_SECONDS}"
fi
if [[ "${BENCHMARK_TIMEOUT_SECONDS}" != "0" ]]; then
  printf 'Per-benchmark timeout backstop: %ss\n' "${BENCHMARK_TIMEOUT_SECONDS}"
fi
determine_representative_latency_ms
printf 'Representative cache latency input: %sms\n' "${REPRESENTATIVE_LATENCY_MS}"
prepare_proxied_bazel_remote_http

# The setup phase models the CI cache-writer job from PR 5525: start from an
# empty repository_cache, then measure a single command (typically `fetch --all`)
# that populates only external dependency state. The per-benchmark runs then use
# fresh output bases so local action/output state does not bleed across timings.
setup_output_base="${TMP_ROOT}/output-base-setup"
setup_time="$(run_bazel_command setup "${SETUP_ARGS_SPEC}" "" "${setup_output_base}" "${LOG_DIR}/setup.log" "${SETUP_TIMEOUT_SECONDS}" 1)"
remove_output_base "${setup_output_base}"

echo
echo "Setup result"
echo "------------"
echo "command: bazelisk ${SETUP_ARGS_SPEC}"
printf 'time: %ss\n' "${setup_time}"

run_cached_pair() {
  local phase_prefix="$1"
  local benchmark_spec="$2"
  local remote_cache="$3"
  local cold_output_base="$4"
  local warm_output_base="$5"
  local cold_log_file="$6"
  local warm_log_file="$7"
  local cold_time warm_time

  cold_time="$(run_bazel_command "${phase_prefix}-cold-remote-cache (${benchmark_spec})" "${benchmark_spec}" "${remote_cache}" "${cold_output_base}" "${cold_log_file}" "${BENCHMARK_TIMEOUT_SECONDS}" 0)"
  remove_output_base "${cold_output_base}"
  warm_time="$(run_bazel_command "${phase_prefix}-warm-remote-cache (${benchmark_spec})" "${benchmark_spec}" "${remote_cache}" "${warm_output_base}" "${warm_log_file}" "${BENCHMARK_TIMEOUT_SECONDS}" 0)"
  remove_output_base "${warm_output_base}"

  printf '%s\n%s\n' "${cold_time}" "${warm_time}"
}

resolve_test_impacted_range() {
  if [[ -n "${TEST_IMPACTED_RANGE}" ]]; then
    printf '%s\n' "${TEST_IMPACTED_RANGE}"
    return 0
  fi
  if [[ -n "${BAZEL_IMPACTED_DIFF_RANGE:-}" ]]; then
    printf '%s\n' "${BAZEL_IMPACTED_DIFF_RANGE}"
    return 0
  fi
  if [[ -n "${BAZEL_IMPACTED_BASE_SHA:-}" ]]; then
    printf '%s..\n' "${BAZEL_IMPACTED_BASE_SHA}"
    return 0
  fi
  printf 'HEAD..\n'
}

resolve_bazel_built_file() {
  local output_base="$1"
  local label="$2"
  local built_file

  built_file="$("${NIX_RUN}" bazelisk "--output_base=${output_base}" cquery --output=files "${label}" 2>/dev/null)"
  [[ -n "${built_file}" ]] || die "no built file found for ${label}"
  if [[ "${built_file}" != /* ]]; then
    built_file="${REPO_ROOT}/${built_file}"
  fi
  printf '%s\n' "${built_file}"
}

run_impacted_test_pair() {
  local benchmark_index="$1"
  local benchmark_spec="$2"
  local selector_output_base="$3"
  local selector_log_file="$4"
  local execution_output_base="$5"
  local execution_log_file="$6"
  local impacted_range selector_time selector_build_time selector_exec_time execution_time target_count
  local manifest_file selector_tool selector_start selector_end
  local -a selector_args execution_args

  impacted_range="$(resolve_test_impacted_range)"
  manifest_file="${TMP_ROOT}/impacted-manifest-${benchmark_index}.txt"

  selector_build_time="$(run_bazel_command \
    "impacted-selector-build (${benchmark_spec})" \
    "build //tools/impactedtests:impactedtests_bin" \
    "" \
    "${selector_output_base}" \
    "${selector_log_file}" \
    "${BENCHMARK_TIMEOUT_SECONDS}" \
    0)"
  selector_tool="$(resolve_bazel_built_file "${selector_output_base}" "//tools/impactedtests:impactedtests_bin")"

  selector_args=(manifest --range "${impacted_range}" --output "${manifest_file}")
  for scope in "${TEST_IMPACTED_SCOPES[@]}"; do
    selector_args+=(--scope "${scope}")
  done

  echo >&2
  echo "==> impacted-selector (${benchmark_spec})" >&2
  echo "command: ${selector_tool} ${selector_args[*]}" >&2
  echo "output_base: ${selector_output_base}" >&2

  selector_start="$(now_seconds)"
  set +e
  run_logged_command "${BENCHMARK_TIMEOUT_SECONDS}" 0 "${selector_log_file}" "${selector_tool}" "${selector_args[@]}"
  status=$?
  set -e
  if [[ ${status} -ne 0 ]]; then
    echo "log: ${selector_log_file}" >&2
    tail -n 40 "${selector_log_file}" >&2 || true
    die "impacted-selector (${benchmark_spec}) failed"
  fi
  selector_end="$(now_seconds)"
  selector_exec_time="$(format_seconds "${selector_start}" "${selector_end}")"
  selector_time="$(add_times "${selector_build_time}" "${selector_exec_time}")"
  echo "selector build time: ${selector_build_time}s" >&2
  echo "selector execution time: ${selector_exec_time}s" >&2
  echo "selector total time: ${selector_time}s" >&2
  remove_output_base "${selector_output_base}"

  if [[ ! -f "${manifest_file}" ]]; then
    die "impacted selector did not write manifest file: ${manifest_file}"
  fi

  target_count="$(awk 'END { print NR }' "${manifest_file}")"
  if [[ ! -s "${manifest_file}" ]]; then
    execution_time="0.000"
    printf '%s\n%s\n%s\n' "${selector_time}" "${execution_time}" "${target_count}"
    return 0
  fi

  mapfile -t execution_args < "${manifest_file}"
  execution_args=(test "${execution_args[@]}")
  execution_time="$(run_bazel_argv \
    "impacted-execution (${benchmark_spec})" \
    "test $(paste -sd ' ' "${manifest_file}")" \
    "" \
    "${execution_output_base}" \
    "${execution_log_file}" \
    "${BENCHMARK_TIMEOUT_SECONDS}" \
    0 \
    "${execution_args[@]}")"
  remove_output_base "${execution_output_base}"

  printf '%s\n%s\n%s\n' "${selector_time}" "${execution_time}" "${target_count}"
}

add_times() {
  awk -v left="$1" -v right="$2" 'BEGIN { printf "%.3f", (left + right) }'
}

declare -a BUILD_RESULT_LABELS=()
declare -a BUILD_RESULT_NO_CACHE_TIMES=()
declare -a BUILD_RESULT_HTTP_COLD_TIMES=()
declare -a BUILD_RESULT_HTTP_WARM_TIMES=()
declare -a BUILD_RESULT_GRPC_COLD_TIMES=()
declare -a BUILD_RESULT_GRPC_WARM_TIMES=()
declare -a TEST_RESULT_LABELS=()
declare -a TEST_RESULT_NO_CACHE_TIMES=()
declare -a TEST_RESULT_HTTP_COLD_TIMES=()
declare -a TEST_RESULT_HTTP_WARM_TIMES=()
declare -a TEST_RESULT_IMPACTED_SELECTOR_TIMES=()
declare -a TEST_RESULT_IMPACTED_EXECUTION_TIMES=()
declare -a TEST_RESULT_IMPACTED_TOTAL_TIMES=()
declare -a TEST_RESULT_IMPACTED_TARGET_COUNTS=()
all_passed=true
benchmark_index=0

for build_spec in "${BUILD_BENCHMARK_ARGS_SPECS[@]}"; do
  label="${build_spec}"

  echo
  echo "Build benchmark: bazelisk ${label}"
  echo "========================================"

  no_cache_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-no-cache"
  http_cold_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-http-cold-remote"
  http_warm_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-http-warm-remote"
  grpc_cold_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-grpc-cold-remote"
  grpc_warm_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-grpc-warm-remote"
  benchmark_remote_cache_dir="${CACHE_DIR}/benchmark-${benchmark_index}"
  mkdir -p "${benchmark_remote_cache_dir}"

  no_cache_time="$(run_bazel_command "no-cache (${label})" "${build_spec}" "" "${no_cache_output_base}" "${LOG_DIR}/benchmark-${benchmark_index}-no-cache.log" "${BENCHMARK_TIMEOUT_SECONDS}" 0)"
  remove_output_base "${no_cache_output_base}"

  start_bazel_remote "${benchmark_remote_cache_dir}" "benchmark-${benchmark_index}-bazel-remote.log"
  remote_cache_url="${REMOTE_HTTP_URL}"
  remote_cache_grpc_target="${REMOTE_GRPC_TARGET}"
  echo "Using bazel-remote HTTP endpoint for benchmark ${benchmark_index}: ${remote_cache_url}"
  echo "Using bazel-remote gRPC endpoint for benchmark ${benchmark_index}: ${remote_cache_grpc_target}"

  proxied_http_remote_cache_url="$(create_http_latency_proxy "${remote_cache_url}" "benchmark-${benchmark_index}-http-cache" "${REPRESENTATIVE_LATENCY_MS}")"
  echo "Using proxied bazel-remote HTTP endpoint for benchmark ${benchmark_index}: ${proxied_http_remote_cache_url}"

  mapfile -t http_cache_times < <(run_cached_pair \
    "http" \
    "${build_spec}" \
    "${proxied_http_remote_cache_url}" \
    "${http_cold_output_base}" \
    "${http_warm_output_base}" \
    "${LOG_DIR}/benchmark-${benchmark_index}-http-cold-remote.log" \
    "${LOG_DIR}/benchmark-${benchmark_index}-http-warm-remote.log")
  http_cold_time="${http_cache_times[0]}"
  http_warm_time="${http_cache_times[1]}"

  stop_bazel_remote
  rm -rf "${benchmark_remote_cache_dir}"
  mkdir -p "${benchmark_remote_cache_dir}"

  start_bazel_remote "${benchmark_remote_cache_dir}" "benchmark-${benchmark_index}-grpc-bazel-remote.log"
  remote_cache_url="${REMOTE_HTTP_URL}"
  remote_cache_grpc_target="${REMOTE_GRPC_TARGET}"
  echo "Using bazel-remote HTTP endpoint for benchmark ${benchmark_index} gRPC pair: ${remote_cache_url}"
  echo "Using bazel-remote gRPC endpoint for benchmark ${benchmark_index} gRPC pair: ${remote_cache_grpc_target}"

  proxied_grpc_remote_cache_url="$(create_grpc_latency_proxy "${remote_cache_grpc_target}" "benchmark-${benchmark_index}-grpc-cache" "${REPRESENTATIVE_LATENCY_MS}")"
  echo "Using proxied bazel-remote gRPC endpoint for benchmark ${benchmark_index}: ${proxied_grpc_remote_cache_url}"

  mapfile -t grpc_cache_times < <(run_cached_pair \
    "grpc" \
    "${build_spec}" \
    "${proxied_grpc_remote_cache_url}" \
    "${grpc_cold_output_base}" \
    "${grpc_warm_output_base}" \
    "${LOG_DIR}/benchmark-${benchmark_index}-grpc-cold-remote.log" \
    "${LOG_DIR}/benchmark-${benchmark_index}-grpc-warm-remote.log")
  grpc_cold_time="${grpc_cache_times[0]}"
  grpc_warm_time="${grpc_cache_times[1]}"

  stop_bazel_remote

  BUILD_RESULT_LABELS+=("${label}")
  BUILD_RESULT_NO_CACHE_TIMES+=("${no_cache_time}")
  BUILD_RESULT_HTTP_COLD_TIMES+=("${http_cold_time}")
  BUILD_RESULT_HTTP_WARM_TIMES+=("${http_warm_time}")
  BUILD_RESULT_GRPC_COLD_TIMES+=("${grpc_cold_time}")
  BUILD_RESULT_GRPC_WARM_TIMES+=("${grpc_warm_time}")

  if ! less_than "${http_warm_time}" "${no_cache_time}"; then
    echo "FAIL: warm HTTP remote cache was not faster than no cache for: bazelisk ${label}" >&2
    all_passed=false
  fi
  if ! less_than "${http_warm_time}" "${http_cold_time}"; then
    echo "FAIL: warm HTTP remote cache was not faster than cold HTTP remote cache for: bazelisk ${label}" >&2
    all_passed=false
  fi
  if ! less_than "${grpc_warm_time}" "${no_cache_time}"; then
    echo "FAIL: warm gRPC remote cache was not faster than no cache for: bazelisk ${label}" >&2
    all_passed=false
  fi
  if ! less_than "${grpc_warm_time}" "${grpc_cold_time}"; then
    echo "FAIL: warm gRPC remote cache was not faster than cold gRPC remote cache for: bazelisk ${label}" >&2
    all_passed=false
  fi
  if [[ ${#WARM_LOG_MUST_CONTAIN_PATTERNS[@]} -gt 0 ]] \
    && ! warm_log_contains_all_patterns "${LOG_DIR}/benchmark-${benchmark_index}-http-warm-remote.log"; then
    echo "FAIL: warm HTTP remote cache log did not contain required patterns for: bazelisk ${label}" >&2
    all_passed=false
  fi
  if [[ ${#WARM_LOG_MUST_CONTAIN_PATTERNS[@]} -gt 0 ]] \
    && ! warm_log_contains_all_patterns "${LOG_DIR}/benchmark-${benchmark_index}-grpc-warm-remote.log"; then
    echo "FAIL: warm gRPC remote cache log did not contain required patterns for: bazelisk ${label}" >&2
    all_passed=false
  fi

  benchmark_index=$((benchmark_index + 1))
done

for test_spec in "${TEST_BENCHMARK_ARGS_SPECS[@]}"; do
  label="${test_spec}"

  echo
  echo "Test benchmark: bazelisk ${label}"
  echo "========================================"

  no_cache_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-no-cache"
  http_cold_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-http-cold-remote"
  http_warm_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-http-warm-remote"
  selector_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-impacted-selector"
  impacted_execution_output_base="${TMP_ROOT}/output-base-benchmark-${benchmark_index}-impacted-execution"
  benchmark_remote_cache_dir="${CACHE_DIR}/benchmark-${benchmark_index}"
  mkdir -p "${benchmark_remote_cache_dir}"

  no_cache_time="$(run_bazel_command "no-cache (${label})" "${test_spec}" "" "${no_cache_output_base}" "${LOG_DIR}/benchmark-${benchmark_index}-no-cache.log" "${BENCHMARK_TIMEOUT_SECONDS}" 0)"
  remove_output_base "${no_cache_output_base}"

  start_bazel_remote "${benchmark_remote_cache_dir}" "benchmark-${benchmark_index}-bazel-remote.log"
  remote_cache_url="${REMOTE_HTTP_URL}"
  echo "Using bazel-remote HTTP endpoint for benchmark ${benchmark_index}: ${remote_cache_url}"

  proxied_http_remote_cache_url="$(create_http_latency_proxy "${remote_cache_url}" "benchmark-${benchmark_index}-http-cache" "${REPRESENTATIVE_LATENCY_MS}")"
  echo "Using proxied bazel-remote HTTP endpoint for benchmark ${benchmark_index}: ${proxied_http_remote_cache_url}"

  mapfile -t http_cache_times < <(run_cached_pair \
    "http" \
    "${test_spec}" \
    "${proxied_http_remote_cache_url}" \
    "${http_cold_output_base}" \
    "${http_warm_output_base}" \
    "${LOG_DIR}/benchmark-${benchmark_index}-http-cold-remote.log" \
    "${LOG_DIR}/benchmark-${benchmark_index}-http-warm-remote.log")
  http_cold_time="${http_cache_times[0]}"
  http_warm_time="${http_cache_times[1]}"

  stop_bazel_remote

  mapfile -t impacted_times < <(run_impacted_test_pair \
    "${benchmark_index}" \
    "${test_spec}" \
    "${selector_output_base}" \
    "${LOG_DIR}/benchmark-${benchmark_index}-impacted-selector.log" \
    "${impacted_execution_output_base}" \
    "${LOG_DIR}/benchmark-${benchmark_index}-impacted-execution.log")
  impacted_selector_time="${impacted_times[0]}"
  impacted_execution_time="${impacted_times[1]}"
  impacted_target_count="${impacted_times[2]}"
  impacted_total_time="$(add_times "${impacted_selector_time}" "${impacted_execution_time}")"

  TEST_RESULT_LABELS+=("${label}")
  TEST_RESULT_NO_CACHE_TIMES+=("${no_cache_time}")
  TEST_RESULT_HTTP_COLD_TIMES+=("${http_cold_time}")
  TEST_RESULT_HTTP_WARM_TIMES+=("${http_warm_time}")
  TEST_RESULT_IMPACTED_SELECTOR_TIMES+=("${impacted_selector_time}")
  TEST_RESULT_IMPACTED_EXECUTION_TIMES+=("${impacted_execution_time}")
  TEST_RESULT_IMPACTED_TOTAL_TIMES+=("${impacted_total_time}")
  TEST_RESULT_IMPACTED_TARGET_COUNTS+=("${impacted_target_count}")

  if ! less_than "${http_warm_time}" "${no_cache_time}"; then
    echo "FAIL: warm HTTP remote cache was not faster than no cache for: bazelisk ${label}" >&2
    all_passed=false
  fi
  if ! less_than "${http_warm_time}" "${http_cold_time}"; then
    echo "FAIL: warm HTTP remote cache was not faster than cold HTTP remote cache for: bazelisk ${label}" >&2
    all_passed=false
  fi
  if [[ ${#WARM_LOG_MUST_CONTAIN_PATTERNS[@]} -gt 0 ]] \
    && ! warm_log_contains_all_patterns "${LOG_DIR}/benchmark-${benchmark_index}-http-warm-remote.log"; then
    echo "FAIL: warm HTTP remote cache log did not contain required patterns for: bazelisk ${label}" >&2
    all_passed=false
  fi

  benchmark_index=$((benchmark_index + 1))
done

echo
echo "Benchmark results"
echo "-----------------"
printf 'setup %-63s %8ss\n' "bazelisk ${SETUP_ARGS_SPEC}" "${setup_time}"
for index in "${!BUILD_RESULT_LABELS[@]}"; do
  echo
  echo "bazelisk ${BUILD_RESULT_LABELS[index]}"
  printf '  no-cache   %20ss\n' "${BUILD_RESULT_NO_CACHE_TIMES[index]}"
  printf '  http-cold  %20ss\n' "${BUILD_RESULT_HTTP_COLD_TIMES[index]}"
  printf '  http-warm  %20ss\n' "${BUILD_RESULT_HTTP_WARM_TIMES[index]}"
  printf '  grpc-cold  %20ss\n' "${BUILD_RESULT_GRPC_COLD_TIMES[index]}"
  printf '  grpc-warm  %20ss\n' "${BUILD_RESULT_GRPC_WARM_TIMES[index]}"
done
for index in "${!TEST_RESULT_LABELS[@]}"; do
  echo
  echo "bazelisk ${TEST_RESULT_LABELS[index]}"
  printf '  no-cache            %12ss\n' "${TEST_RESULT_NO_CACHE_TIMES[index]}"
  printf '  http-cold           %12ss\n' "${TEST_RESULT_HTTP_COLD_TIMES[index]}"
  printf '  http-warm           %12ss\n' "${TEST_RESULT_HTTP_WARM_TIMES[index]}"
  printf '  impacted-selector   %12ss\n' "${TEST_RESULT_IMPACTED_SELECTOR_TIMES[index]}"
  printf '  impacted-execution  %12ss\n' "${TEST_RESULT_IMPACTED_EXECUTION_TIMES[index]}"
  printf '  impacted-total      %12ss\n' "${TEST_RESULT_IMPACTED_TOTAL_TIMES[index]}"
  printf '  impacted-targets    %12s\n' "${TEST_RESULT_IMPACTED_TARGET_COUNTS[index]}"
done

if [[ "${all_passed}" != true ]]; then
  exit 1
fi

echo "PASS: warm remote cache runs were faster than both no cache and their corresponding cold remote cache runs for every cached benchmark command"
if [[ ${#WARM_LOG_MUST_CONTAIN_PATTERNS[@]} -gt 0 ]]; then
  echo "PASS: warm remote cache logs contained all required patterns"
fi
SUCCESS=true
