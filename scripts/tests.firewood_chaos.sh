#!/usr/bin/env bash

set -euo pipefail

# Firewood Chaos Test
#
# Usage:
#   ./tests.firewood_chaos.sh [test-name]
#
# To see available tests: use `help` as the test name or invoke
# without a test name and without required env vars.
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
#     START_BLOCK: The starting block height (inclusive).
#     END_BLOCK: The ending block height (inclusive).
#     MIN_WAIT_TIME: The minimum amount of time to wait before crashing.
#     MAX_WAIT_TIME: The maximum amount of time to wait before crashing.

show_usage() {
    cat <<EOF
Usage: $0 [test-name]

Available tests:
    help                    - Show this help message
    101-250k                - Blocks 101-250k with Firewood
    archive-101-250k        - Blocks 101-250k with Firewood archive mode
EOF
}

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

# Set defaults based on test name (if provided)
TEST_NAME="${1:-}"
if [[ -n "$TEST_NAME" ]]; then
    shift
    case "$TEST_NAME" in
        help)
            show_usage
            exit 0
            ;;
        101-250k)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-1m-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-firewood-100}"
            START_BLOCK="${START_BLOCK:-101}"
            END_BLOCK="${END_BLOCK:-250000}"
            MIN_WAIT_TIME="${MIN_WAIT_TIME:-120s}"
            MAX_WAIT_TIME="${MAX_WAIT_TIME:-150s}"
            ;;
        archive-101-250k)
            BLOCK_DIR_SRC="${BLOCK_DIR_SRC:-cchain-mainnet-blocks-1m-ldb}"
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-firewood-archive-100}"
            START_BLOCK="${START_BLOCK:-101}"
            END_BLOCK="${END_BLOCK:-250000}"
            MIN_WAIT_TIME="${MIN_WAIT_TIME:-120s}"
            MAX_WAIT_TIME="${MAX_WAIT_TIME:-150s}"
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
    EXECUTION_DATA_DIR="${EXECUTION_DATA_DIR:-/tmp/chaos-test-${TEST_NAME:-custom}-${TIMESTAMP}}"

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
    echo "  Timeouts:"
    [[ -n "${MIN_WAIT_TIME:-}" ]] && echo "    MIN_WAIT_TIME: ${MIN_WAIT_TIME}" || echo "    MIN_WAIT_TIME: (not set)"
    [[ -n "${MAX_WAIT_TIME:-}" ]] && echo "    MAX_WAIT_TIME: ${MAX_WAIT_TIME}" || echo "    MAX_WAIT_TIME: (not set)"
    exit 1
fi

# Validate block range
if [[ -z "${START_BLOCK:-}" || -z "${END_BLOCK:-}" ]]; then
    error "START_BLOCK and END_BLOCK are required"
fi

echo "=== Firewood Chaos Test: ${TEST_NAME:-custom} ==="
echo "Blocks: ${START_BLOCK} - ${END_BLOCK}"
echo "Crashing between ${MIN_WAIT_TIME} and ${MAX_WAIT_TIME}"

echo "=== Running Chaos Test ==="
go run ./tests/reexecute/chaos \
    --start-block="${START_BLOCK}" \
    --end-block="${END_BLOCK}" \
    --current-state-dir="${CURRENT_STATE_DIR}" \
    --block-dir="${BLOCK_DIR}" \
    --min-wait-time="${MIN_WAIT_TIME}" \
    --max-wait-time="${MAX_WAIT_TIME}"