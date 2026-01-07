#!/usr/bin/env bash

set -euo pipefail

# Firewood state access script
#
# Usage:
#   ./tests.firewood_state_access.sh
#
# To see available tests, use `help` as the test name or invoke without a test
# name and without required env vars.
#
# Note: state access tests can only be run with Firewood archival states.
#
# Environment variables:
#   Data sources (provide S3 sources OR local paths):
#     CURRENT_STATE_DIR_SRC: S3 object key for state (triggers S3 import).
#     CURRENT_STATE_DIR: Path to local current state directory.
#
#   Required:
#     START_BLOCK: The starting block height to query (inclusive).
#     END_BLOCK: The ending block height to query (inclusive).

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

  firewood-archive-250k          - Query the first 250k blocks with Firewood archive.
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
        firewood-archive-250k)
            CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC:-cchain-current-state-firewood-archive-250k}"
            START_BLOCK="${START_BLOCK:-0}"
            END_BLOCK="${END_BLOCK:-250_000}"
            ;;
        *)
            error "Unknown test '$TEST_NAME'"
            ;;
    esac
fi

# Determine data source: S3 import or local path
if [[ -n "${CURRENT_STATE_DIR_SRC:-}" ]]; then
    # S3 mode - import data
    TIMESTAMP=$(date '+%Y%m%d-%H%M%S')
    EXECUTION_DATA_DIR="${EXECUTION_DATA_DIR:-/tmp/reexec-${TEST_NAME:-custom}-${TIMESTAMP}}"

    CURRENT_STATE_DIR_SRC="${CURRENT_STATE_DIR_SRC}" \
    EXECUTION_DATA_DIR="${EXECUTION_DATA_DIR}" \
    "${SCRIPT_DIR}/import_cchain_data.sh"

    CURRENT_STATE_DIR="${EXECUTION_DATA_DIR}/current-state"
elif [[ -z "${CURRENT_STATE_DIR:-}" ]]; then
    show_usage
    echo ""
    echo "Env vars status:"
    echo "  S3 source:"
    [[ -n "${CURRENT_STATE_DIR_SRC:-}" ]] && echo "    CURRENT_STATE_DIR_SRC: ${CURRENT_STATE_DIR_SRC}" || echo "    CURRENT_STATE_DIR_SRC: (not set)"
    echo "  Local path:"
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

echo "=== State Access Test: ${TEST_NAME:-custom} ==="
echo "Querying between: ${START_BLOCK} and ${END_BLOCK}"

echo "=== Running Test ==="
go run ./tests/reexecute/stateaccess \
    --current-state-dir="${CURRENT_STATE_DIR}" \
    --start-block="${START_BLOCK}" \
    --end-block="${END_BLOCK}"