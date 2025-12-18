#!/usr/bin/env bash

set -euo pipefail

# C-Chain Re-execution Benchmark Script
#
# Usage:
#   ./benchmark_cchain_range.sh [test-name]
#
# Running without arguments will output available tests.
#
# When test-name is provided, runs a predefined test that automatically imports data from S3.
#
# When no test-name (backward compatible):
#   Expects these environment variables to be set:
#     BLOCK_DIR: Path to the block directory.
#     CURRENT_STATE_DIR: Path to the current state directory.
#     START_BLOCK: The starting block height (exclusive).
#     END_BLOCK: The ending block height (inclusive).
#
# Optional env vars (all modes):
#   CONFIG: VM config preset (default, archive, firewood).
#   LABELS: Comma-separated key=value pairs for metric labels.
#   BENCHMARK_OUTPUT_FILE: If set, benchmark output is also written to this file.
#   METRICS_SERVER_ENABLED: If set, enables the metrics server.
#   METRICS_SERVER_PORT: If set, determines the port the metrics server will listen to.
#   METRICS_COLLECTOR_ENABLED: If set, enables the metrics collector.
#   PUSH_POST_STATE: S3 destination to push current-state after execution.
#
# For 'custom' test or S3 import with other tests:
#   BLOCK_DIR_SRC: S3 object key for blocks.
#   CURRENT_STATE_DIR_SRC: S3 object key for state.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# CI-aware error function
error() {
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
        echo "::error::$1"
    else
        echo "Error: $1" >&2
    fi
    exit 1
}

show_usage() {
    cat <<EOF
Usage: $0 [test-name]

Available tests:
  help                         - Show this help message
  default                      - Quick test run (blocks 101-200, hashdb)
  hashdb-101-250k              - Blocks 101-250k with hashdb
  hashdb-archive-101-250k      - Blocks 101-250k with hashdb archive
  hashdb-33m-33m500k           - Blocks 33m-33.5m with hashdb
  firewood-101-250k            - Blocks 101-250k with firewood
  firewood-33m-33m500k         - Blocks 33m-33.5m with firewood
  firewood-33m-40m             - Blocks 33m-40m with firewood
  custom                       - Custom run using env vars:
                                 BLOCK_DIR_SRC, CURRENT_STATE_DIR_SRC, START_BLOCK, END_BLOCK, CONFIG

Without test-name, expects env vars: BLOCK_DIR, CURRENT_STATE_DIR, START_BLOCK, END_BLOCK
EOF
}

# Check if a test name was provided
if [[ $# -ge 1 ]]; then
    TEST_NAME="$1"
    shift

    # Set defaults based on test name. Env vars can override any value.
    case "$TEST_NAME" in
        help)
            show_usage
            exit 0
            ;;
        default)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-200-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-hashdb-full-100}"
            START_BLOCK="${START_BLOCK:-101}"
            END_BLOCK="${END_BLOCK:-200}"
            ;;
        hashdb-101-250k)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-1m-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-hashdb-full-100}"
            START_BLOCK="${START_BLOCK:-101}"
            END_BLOCK="${END_BLOCK:-250000}"
            ;;
        hashdb-archive-101-250k)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-1m-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-hashdb-archive-100}"
            START_BLOCK="${START_BLOCK:-101}"
            END_BLOCK="${END_BLOCK:-250000}"
            CONFIG="${CONFIG:-archive}"
            ;;
        hashdb-33m-33m500k)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-30m-40m-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-hashdb-full-33m}"
            START_BLOCK="${START_BLOCK:-33000001}"
            END_BLOCK="${END_BLOCK:-33500000}"
            ;;
        firewood-101-250k)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-1m-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-firewood-100}"
            START_BLOCK="${START_BLOCK:-101}"
            END_BLOCK="${END_BLOCK:-250000}"
            CONFIG="${CONFIG:-firewood}"
            ;;
        firewood-33m-33m500k)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-30m-40m-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-firewood-33m}"
            START_BLOCK="${START_BLOCK:-33000001}"
            END_BLOCK="${END_BLOCK:-33500000}"
            CONFIG="${CONFIG:-firewood}"
            ;;
        firewood-33m-40m)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-30m-40m-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-firewood-33m}"
            START_BLOCK="${START_BLOCK:-33000001}"
            END_BLOCK="${END_BLOCK:-40000000}"
            CONFIG="${CONFIG:-firewood}"
            ;;
        custom)
            # All configuration from env vars
            ;;
        *)
            error "Unknown test '$TEST_NAME'"
            ;;
    esac

    # Validate required variables for test mode
    missing=()
    [[ -z "${BLOCK_DIR_SRC:-}" ]] && missing+=("BLOCK_DIR_SRC")
    [[ -z "${CURRENT_STATE_DIR_SRC:-}" ]] && missing+=("CURRENT_STATE_DIR_SRC")
    [[ -z "${START_BLOCK:-}" ]] && missing+=("START_BLOCK")
    [[ -z "${END_BLOCK:-}" ]] && missing+=("END_BLOCK")

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "Missing required env vars: ${missing[*]}"
    fi

    TIMESTAMP=$(date '+%Y%m%d-%H%M%S')
    EXECUTION_DATA_DIR="${EXECUTION_DATA_DIR:-/tmp/reexec-${TEST_NAME}-${TIMESTAMP}}"

    echo "=== C-Chain Re-execution: ${TEST_NAME} ==="
    echo "Blocks: ${START_BLOCK} - ${END_BLOCK}"
    echo "Config: ${CONFIG:-default}"
    echo "Data dir: ${EXECUTION_DATA_DIR}"

    # Import data from S3
    BLOCK_DIR_SRC="${BLOCK_DIR_SRC}" \
    CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC}" \
    EXECUTION_DATA_DIR="${EXECUTION_DATA_DIR}" \
    "${SCRIPT_DIR}/import_cchain_data.sh"

    # Set local paths for benchmark
    BLOCK_DIR="${EXECUTION_DATA_DIR}/blocks"
    CURRENT_STATE_DIR="${EXECUTION_DATA_DIR}/current-state"
else
    # No test name - backward compatible mode with local paths
    # Show usage if required env vars are missing
    if [[ -z "${BLOCK_DIR:-}" || -z "${CURRENT_STATE_DIR:-}" || -z "${START_BLOCK:-}" || -z "${END_BLOCK:-}" ]]; then
        show_usage
        exit 1
    fi
fi

echo "=== Running re-execution ==="
go run github.com/ava-labs/avalanchego/tests/reexecute/c \
  --block-dir="${BLOCK_DIR}" \
  --current-state-dir="${CURRENT_STATE_DIR}" \
  ${RUNNER_TYPE:+--runner="${RUNNER_TYPE}"} \
  ${CONFIG:+--config="${CONFIG}"} \
  --start-block="${START_BLOCK}" \
  --end-block="${END_BLOCK}" \
  ${LABELS:+--labels="${LABELS}"} \
  ${BENCHMARK_OUTPUT_FILE:+--benchmark-output-file="${BENCHMARK_OUTPUT_FILE}"} \
  ${METRICS_SERVER_ENABLED:+--metrics-server-enabled="${METRICS_SERVER_ENABLED}"} \
  ${METRICS_SERVER_PORT:+--metrics-server-port="${METRICS_SERVER_PORT}"} \
  ${METRICS_COLLECTOR_ENABLED:+--metrics-collector-enabled="${METRICS_COLLECTOR_ENABLED}"}

if [[ -n "${PUSH_POST_STATE:-}" ]]; then
    echo "=== Pushing post-state to S3 ==="
    "${SCRIPT_DIR}/copy_dir.sh" "${CURRENT_STATE_DIR}/" "${PUSH_POST_STATE}"
fi
