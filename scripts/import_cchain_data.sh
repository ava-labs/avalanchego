#!/usr/bin/env bash

set -euo pipefail

# Import C-Chain blocks and state from S3
#
# Required env vars:
#   CURRENT_STATE_DIR_SRC - S3 object key for state (e.g., cchain-current-state-hashdb-full-100)
#   EXECUTION_DATA_DIR    - Local directory to store imported data
#
# Optional env vars:
#   BLOCK_DIR_SRC         - S3 object key for blocks (e.g., cchain-mainnet-blocks-200-ldb)
#
# Result:
#   Creates $EXECUTION_DATA_DIR/current-state (always)
#   Creates $EXECUTION_DATA_DIR/blocks (if BLOCK_DIR_SRC is set)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
S3_BOOTSTRAP_BUCKET="${S3_BOOTSTRAP_BUCKET:-s3://avalanchego-bootstrap-testing}"

: "${CURRENT_STATE_DIR_SRC:?CURRENT_STATE_DIR_SRC must be set}"
: "${EXECUTION_DATA_DIR:?EXECUTION_DATA_DIR must be set}"

if [[ -n "${BLOCK_DIR_SRC:-}" ]]; then
    echo "=== Importing blocks from S3 ==="
    "${SCRIPT_DIR}/copy_dir.sh" "${S3_BOOTSTRAP_BUCKET}/${BLOCK_DIR_SRC}/**" "${EXECUTION_DATA_DIR}/blocks"
fi

echo "=== Importing state from S3 ==="
"${SCRIPT_DIR}/copy_dir.sh" "${S3_BOOTSTRAP_BUCKET}/${CURRENT_STATE_DIR_SRC}/**" "${EXECUTION_DATA_DIR}/current-state"
