#!/usr/bin/env bash

set -euo pipefail

# C-Chain Re-execution Benchmark Script
#
# Usage:
#   ./benchmark_cchain_range.sh [test-name]
#
# Running without arguments will output available tests.
#
# Test names configure defaults for S3 sources and block ranges.
# All defaults can be overridden via environment variables.
#
# Environment variables:
#   Data sources (provide S3 sources OR local paths):
#     BLOCK_DIR_SRC: S3 object key for blocks (triggers S3 import).
#     CURRENT_STATE_DIR_SRC: S3 object key for state (triggers S3 import).
#     BLOCK_DIR: Path to local block directory.
#     CURRENT_STATE_DIR: Path to local current state directory.
#
#   Required:
#     START_BLOCK: The starting block height (exclusive).
#     END_BLOCK: The ending block height (inclusive).
#
#   Optional:
#     CONFIG: VM config preset (default, archive, firewood).
#     LABELS: Comma-separated key=value pairs for metric labels.
#     BENCHMARK_OUTPUT_FILE: If set, benchmark output is also written to this file.
#     METRICS_SERVER_ENABLED: If set, enables the metrics server.
#     METRICS_SERVER_PORT: If set, determines the port the metrics server will listen to.
#     METRICS_COLLECTOR_ENABLED: If set, enables the metrics collector.
#     PUSH_POST_STATE: S3 destination to push current-state after execution.

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
EOF
}

# Set defaults based on test name (if provided)
TEST_NAME="${1:-}"
if [[ -n "$TEST_NAME" ]]; then
    shift
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
        *)
            error "Unknown test '$TEST_NAME'"
            ;;
    esac
fi

# Determine data source: S3 import or local paths
if [[ -n "${BLOCK_DIR_SRC:-}" && -n "${CURRENT_STATE_DIR_SRC:-}" ]]; then
    # S3 mode - import data
    TIMESTAMP=$(date '+%Y%m%d-%H%M%S')
    EXECUTION_DATA_DIR="${EXECUTION_DATA_DIR:-/tmp/reexec-${TEST_NAME:-custom}-${TIMESTAMP}}"

    BLOCK_DIR_SRC="${BLOCK_DIR_SRC}" \
    CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC}" \
    EXECUTION_DATA_DIR="${EXECUTION_DATA_DIR}" \
    "${SCRIPT_DIR}/import_cchain_data.sh"

    BLOCK_DIR="${EXECUTION_DATA_DIR}/blocks"
    CURRENT_STATE_DIR="${EXECUTION_DATA_DIR}/current-state"
elif [[ -n "${BLOCK_DIR_SRC:-}" || -n "${CURRENT_STATE_DIR_SRC:-}" ]]; then
    error "Both BLOCK_DIR_SRC and CURRENT_STATE_DIR_SRC must be provided together"
elif [[ -z "${BLOCK_DIR:-}" || -z "${CURRENT_STATE_DIR:-}" ]]; then
    show_usage
    echo ""
    echo "Env vars status:"
    echo "  S3 sources:"
    [[ -n "${BLOCK_DIR_SRC:-}" ]] && echo "    BLOCK_DIR_SRC: ${BLOCK_DIR_SRC}" || echo "    BLOCK_DIR_SRC: (not set)"
    [[ -n "${CURRENT_STATE_DIR_SRC:-}" ]] && echo "    CURRENT_STATE_DIR_SRC: ${CURRENT_STATE_DIR_SRC}" || echo "    CURRENT_STATE_DIR_SRC: (not set)"
    echo "  Local paths:"
    [[ -n "${BLOCK_DIR:-}" ]] && echo "    BLOCK_DIR: ${BLOCK_DIR}" || echo "    BLOCK_DIR: (not set)"
    [[ -n "${CURRENT_STATE_DIR:-}" ]] && echo "    CURRENT_STATE_DIR: ${CURRENT_STATE_DIR}" || echo "    CURRENT_STATE_DIR: (not set)"
    echo "  Block range:"
    [[ -n "${START_BLOCK:-}" ]] && echo "    START_BLOCK: ${START_BLOCK}" || echo "    START_BLOCK: (not set)"
    [[ -n "${END_BLOCK:-}" ]] && echo "    END_BLOCK: ${END_BLOCK}" || echo "    END_BLOCK: (not set)"
    exit 1
fi

# Validate block range
if [[ -z "${START_BLOCK:-}" || -z "${END_BLOCK:-}" ]]; then
    error "START_BLOCK and END_BLOCK are required"
fi

echo "=== C-Chain Re-execution: ${TEST_NAME:-custom} ==="
echo "Blocks: ${START_BLOCK} - ${END_BLOCK}"
echo "Config: ${CONFIG:-default}"

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
