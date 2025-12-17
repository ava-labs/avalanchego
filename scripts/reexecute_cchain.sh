#!/usr/bin/env bash

set -euo pipefail

# C-Chain Re-execution Script
# Usage: ./scripts/reexecute_cchain.sh <test-name>
#
# Preset tests handle S3 import and execution automatically.
# Environment variables (BENCHMARK_OUTPUT_FILE, METRICS_*, etc.) are passed through.
#
# Optional env vars:
#   PUSH_POST_STATE - S3 destination to push current-state after execution (requires AWS credentials)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
S3_BOOTSTRAP_BUCKET="${S3_BOOTSTRAP_BUCKET:-s3://avalanchego-bootstrap-testing}"

# CI-aware error function
error() {
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
        echo "::error::$1"
    else
        echo "Error: $1" >&2
    fi
    exit 1
}

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <test-name>"
    echo ""
    echo "Available tests:"
    echo "  test                         - Quick test run (blocks 101-200, hashdb)"
    echo "  hashdb-101-250k              - Blocks 101-250k with hashdb"
    echo "  hashdb-archive-101-250k      - Blocks 101-250k with hashdb archive"
    echo "  hashdb-33m-33m500k           - Blocks 33m-33.5m with hashdb"
    echo "  firewood-101-250k            - Blocks 101-250k with firewood"
    echo "  firewood-33m-33m500k         - Blocks 33m-33.5m with firewood"
    echo "  firewood-33m-40m             - Blocks 33m-40m with firewood"
    echo "  custom                       - Custom run using env vars:"
    echo "                                 S3_BLOCK_DIR, S3_STATE_DIR, START_BLOCK, END_BLOCK, CONFIG"
    exit 1
fi

TEST_NAME="$1"

# Set defaults based on test name. Env vars can override any value.
case "$TEST_NAME" in
    test)
        S3_BLOCK_DIR="${S3_BLOCK_DIR:-cchain-mainnet-blocks-200-ldb}"
        S3_STATE_DIR="${S3_STATE_DIR:-cchain-current-state-hashdb-full-100}"
        START_BLOCK="${START_BLOCK:-101}"
        END_BLOCK="${END_BLOCK:-200}"
        ;;
    hashdb-101-250k)
        S3_BLOCK_DIR="${S3_BLOCK_DIR:-cchain-mainnet-blocks-1m-ldb}"
        S3_STATE_DIR="${S3_STATE_DIR:-cchain-current-state-hashdb-full-100}"
        START_BLOCK="${START_BLOCK:-101}"
        END_BLOCK="${END_BLOCK:-250000}"
        ;;
    hashdb-archive-101-250k)
        S3_BLOCK_DIR="${S3_BLOCK_DIR:-cchain-mainnet-blocks-1m-ldb}"
        S3_STATE_DIR="${S3_STATE_DIR:-cchain-current-state-hashdb-archive-100}"
        START_BLOCK="${START_BLOCK:-101}"
        END_BLOCK="${END_BLOCK:-250000}"
        CONFIG="${CONFIG:-archive}"
        ;;
    hashdb-33m-33m500k)
        S3_BLOCK_DIR="${S3_BLOCK_DIR:-cchain-mainnet-blocks-30m-40m-ldb}"
        S3_STATE_DIR="${S3_STATE_DIR:-cchain-current-state-hashdb-full-33m}"
        START_BLOCK="${START_BLOCK:-33000001}"
        END_BLOCK="${END_BLOCK:-33500000}"
        ;;
    firewood-101-250k)
        S3_BLOCK_DIR="${S3_BLOCK_DIR:-cchain-mainnet-blocks-1m-ldb}"
        S3_STATE_DIR="${S3_STATE_DIR:-cchain-current-state-firewood-100}"
        START_BLOCK="${START_BLOCK:-101}"
        END_BLOCK="${END_BLOCK:-250000}"
        CONFIG="${CONFIG:-firewood}"
        ;;
    firewood-33m-33m500k)
        S3_BLOCK_DIR="${S3_BLOCK_DIR:-cchain-mainnet-blocks-30m-40m-ldb}"
        S3_STATE_DIR="${S3_STATE_DIR:-cchain-current-state-firewood-33m}"
        START_BLOCK="${START_BLOCK:-33000001}"
        END_BLOCK="${END_BLOCK:-33500000}"
        CONFIG="${CONFIG:-firewood}"
        ;;
    firewood-33m-40m)
        S3_BLOCK_DIR="${S3_BLOCK_DIR:-cchain-mainnet-blocks-30m-40m-ldb}"
        S3_STATE_DIR="${S3_STATE_DIR:-cchain-current-state-firewood-33m}"
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

# Validate required variables
missing=()
[[ -z "${S3_BLOCK_DIR:-}" ]] && missing+=("S3_BLOCK_DIR")
[[ -z "${S3_STATE_DIR:-}" ]] && missing+=("S3_STATE_DIR")
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

echo "=== Importing blocks from S3 ==="
"${SCRIPT_DIR}/copy_dir.sh" "${S3_BOOTSTRAP_BUCKET}/${S3_BLOCK_DIR}/**" "${EXECUTION_DATA_DIR}/blocks"

echo "=== Importing state from S3 ==="
"${SCRIPT_DIR}/copy_dir.sh" "${S3_BOOTSTRAP_BUCKET}/${S3_STATE_DIR}/**" "${EXECUTION_DATA_DIR}/current-state"

echo "=== Running re-execution ==="
BLOCK_DIR="${EXECUTION_DATA_DIR}/blocks" \
CURRENT_STATE_DIR="${EXECUTION_DATA_DIR}/current-state" \
START_BLOCK="${START_BLOCK}" \
END_BLOCK="${END_BLOCK}" \
CONFIG="${CONFIG}" \
"${SCRIPT_DIR}/benchmark_cchain_range.sh"

if [[ -n "${PUSH_POST_STATE:-}" ]]; then
    echo "=== Pushing post-state to S3 ==="
    "${SCRIPT_DIR}/copy_dir.sh" "${EXECUTION_DATA_DIR}/current-state/" "${PUSH_POST_STATE}"
fi
