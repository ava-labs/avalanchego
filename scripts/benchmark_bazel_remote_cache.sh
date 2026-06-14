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
  --benchmark-args 'build //main:avalanchego' \
  [--benchmark-args 'build --config=race //main:avalanchego' ...] \
  [--warm-log-must-contain '(cached) PASSED'] \
  [--warm-log-must-contain 'Executed 0 out of 1 test: 1 test passes.']

Benchmark Bazel remote caching with a CI-style split between:
  1. one measured setup command that populates repository_cache
  2. one or more measured benchmark commands that reuse that repository_cache

For each benchmark command, the script measures three runs with fresh local
Bazel output state each time:
  - no remote cache
  - cold remote cache (empty cache; populates it)
  - warm remote cache (reuses the populated cache)

Representative cache latency is determined before the benchmark starts unless
BAZEL_REMOTE_CACHE_LATENCY_MS is set. In measured mode, the script runs
`bazel run //tools/measure-http-latency` against
BAZEL_REMOTE_CACHE_LATENCY_URL (default: https://ec2.us-east-1.amazonaws.com/),
then validates that a toxiproxy-induced HTTP path to bazel-remote is within
BAZEL_REMOTE_CACHE_LATENCY_TOLERANCE_MS of the measured target.

The benchmark fails unless the warm remote-cache run is faster than both the
no-cache and cold-cache runs for every configured benchmark command.

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
declare -a BENCHMARK_ARGS_SPECS=()
declare -a WARM_LOG_MUST_CONTAIN_PATTERNS=()

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
[[ ${#BENCHMARK_ARGS_SPECS[@]} -gt 0 ]] || die "at least one --benchmark-args is required"

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
# the script measures average TTFB to a public AWS us-east-1 regional endpoint
# using //tools/measure-http-latency.
#
# Why this shape:
# - average TTFB is a coarse but useful approximation of tiny request latency,
#   which is the most relevant first-order effect for cache lookup traffic
# - a public regional AWS endpoint lets the benchmark approximate runner to
#   us-east-1 network distance before a real deployed bazel-remote exists
# - live measurement avoids stale hard-coded latency values as runner routing
#   and network conditions change over time
# - BAZEL_REMOTE_CACHE_LATENCY_MS exists specifically for fast local iteration;
#   when set, the script intentionally skips provenance-oriented live
#   measurement and future proxy-path validation
#
# Trade-off:
# this does not claim to reproduce the exact latency of a future EKS-hosted
# bazel-remote deployment. It is only a first-pass approximation of expected
# regional network distance. Measuring against a real deployed bazel-remote is
# the next planned refinement of the experiment.
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

FAIL_FAST_FETCH_PATTERN='An error occurred during the fetch of repository|i/o timeout|reading \.\./\.\./go\.mod|no such file or directory'

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

  grep -E '^(ERROR:|INFO: Repository )|i/o timeout|timed out|no such file or directory|reading ../../go.mod' "${log_file}" | head -n 40 >&2 || true
}

run_bazel_command() {
  local phase_name="$1"
  local command_spec="$2"
  local remote_cache="$3"
  local output_base="$4"
  local log_file="$5"
  local timeout_seconds="$6"
  local fail_fast_mode="$7"
  local start end elapsed status
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

extract_avg_ttfb_ms() {
  local latency_json_log="$1"

  jq -r '.avgTtfbMs | tostring' "${latency_json_log}"
}

render_latency_text_report() {
  local latency_json_log="$1"
  local latency_text_log="$2"

  {
    jq -r '"url=\(.url)"' "${latency_json_log}"
    jq -r '"samples=\(.samples)"' "${latency_json_log}"
    jq -r '.statusCodes | to_entries | sort_by(.key | tonumber)[] | "http_code[\(.key)]=\(.value)"' "${latency_json_log}"
    jq -r '"avg_connect=\(.avgConnectMs)ms avg_tls=\(.avgTlsMs)ms avg_ttfb=\(.avgTtfbMs)ms avg_total=\(.avgTotalMs)ms"' "${latency_json_log}"
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

proxy_upstream_address() {
  local remote_cache_url="$1"
  [[ "${remote_cache_url}" == http://* ]] || die "expected http remote cache URL, got: ${remote_cache_url}"
  printf '%s\n' "${remote_cache_url#http://}"
}

create_latency_proxy() {
  local remote_cache_url="$1"
  local proxy_name="$2"
  local latency_ms="$3"
  local listen_port upstream_address rounded_latency_ms

  listen_port="$(choose_port)"
  upstream_address="$(proxy_upstream_address "${remote_cache_url}")"
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

  printf 'http://127.0.0.1:%s\n' "${listen_port}"
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

  observed_latency_ms="$(extract_avg_ttfb_ms "${latency_json_log}")"
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

  REPRESENTATIVE_LATENCY_MS="$(extract_avg_ttfb_ms "${latency_json_log}")"
}

prepare_proxied_bazel_remote_http() {
  local validation_remote_cache_dir validation_remote_cache_url validation_proxy_url

  validation_remote_cache_dir="${CACHE_DIR}/latency-validation"
  mkdir -p "${validation_remote_cache_dir}"

  TOXIPROXY_API_URL="$(start_toxiproxy "benchmark-toxiproxy.log")"
  echo "Using toxiproxy API: ${TOXIPROXY_API_URL}"

  validation_remote_cache_url="$(start_bazel_remote "${validation_remote_cache_dir}" "latency-validation-bazel-remote.log")"
  echo "Using latency-validation bazel-remote: ${validation_remote_cache_url}"

  validation_proxy_url="$(create_latency_proxy "${validation_remote_cache_url}" "latency-validation-http-cache" "${REPRESENTATIVE_LATENCY_MS}")"
  echo "Using proxied bazel-remote HTTP endpoint: ${validation_proxy_url}"

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

  no_cache_time="$(run_bazel_command "no-cache (${label})" "${benchmark_spec}" "" "${no_cache_output_base}" "${LOG_DIR}/benchmark-${index}-no-cache.log" "${BENCHMARK_TIMEOUT_SECONDS}" 0)"
  remove_output_base "${no_cache_output_base}"

  remote_cache_url="$(start_bazel_remote "${benchmark_remote_cache_dir}" "benchmark-${index}-bazel-remote.log")"
  echo "Using bazel-remote for benchmark ${index}: ${remote_cache_url}"

  proxied_remote_cache_url="$(create_latency_proxy "${remote_cache_url}" "benchmark-${index}-http-cache" "${REPRESENTATIVE_LATENCY_MS}")"
  echo "Using proxied bazel-remote HTTP endpoint for benchmark ${index}: ${proxied_remote_cache_url}"

  cold_cache_time="$(run_bazel_command "cold-remote-cache (${label})" "${benchmark_spec}" "${proxied_remote_cache_url}" "${cold_cache_output_base}" "${LOG_DIR}/benchmark-${index}-cold-remote.log" "${BENCHMARK_TIMEOUT_SECONDS}" 0)"
  remove_output_base "${cold_cache_output_base}"
  warm_cache_time="$(run_bazel_command "warm-remote-cache (${label})" "${benchmark_spec}" "${proxied_remote_cache_url}" "${warm_cache_output_base}" "${LOG_DIR}/benchmark-${index}-warm-remote.log" "${BENCHMARK_TIMEOUT_SECONDS}" 0)"
  remove_output_base "${warm_cache_output_base}"

  stop_bazel_remote

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
  if [[ ${#WARM_LOG_MUST_CONTAIN_PATTERNS[@]} -gt 0 ]] \
    && ! warm_log_contains_all_patterns "${LOG_DIR}/benchmark-${index}-warm-remote.log"; then
    echo "FAIL: warm remote cache log did not contain required patterns for: bazelisk ${label}" >&2
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
if [[ ${#WARM_LOG_MUST_CONTAIN_PATTERNS[@]} -gt 0 ]]; then
  echo "PASS: warm remote cache logs contained all required patterns"
fi
SUCCESS=true
